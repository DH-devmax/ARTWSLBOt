package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func TestPostgresAssistantDualPlatformActionsAndContext(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-platform-assistant"
	hash, _ := security.Password(password)
	var adminID string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role) VALUES('assistant-platform-admin',$1,'superadmin') RETURNING id::text`, hash).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{91}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	defer s.StopAssistant()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	login := func(name, platform string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL, workspace: platform}
		expect(t, c, "POST", "/auth/login", map[string]string{"username": name, "password": password}, 200)
		return c
	}
	admin := login("assistant-platform-admin", "wsl")
	openSyntheticAssistantModules(t, admin)
	refFor := func(c testClient) control.SessionRef {
		u, _ := url.Parse(c.base)
		for _, cookie := range c.client.Jar.Cookies(u) {
			if cookie.Name == "wsl_session" {
				ref, _ := control.WebsiteSession(cookie.Value)
				return ref.InPlatform(c.workspace)
			}
		}
		t.Fatal("missing session")
		return control.SessionRef{}
	}
	owner := expect(t, admin, "POST", "/users", map[string]string{"username": "assistant-platform-owner", "password": password, "tenant_name": "synthetic-owner"}, 201)
	uid, tenant := field(t, owner, "id"), field(t, owner, "tenant_id")
	plan := field(t, expect(t, admin, "POST", "/plans", map[string]any{"name": "synthetic-platform", "group_limit": 300, "allowed_platforms": []string{"wsl", "lajiaoliao"},
		"modules": []string{"moderation", "replies", "cards", "management_assistant"}, "monthly_assistant_tokens": 1000000}, 201), "id")
	expect(t, admin, "PUT", "/subscriptions/"+tenant, map[string]any{"plan_id": plan, "enabled": true, "version": 0, "expires_at": time.Now().Add(time.Hour)}, 200)
	explicitTestCeiling(t, admin, tenant)
	grants := []string{"groups:read", "operations:read", "assistant:use", "assistant:propose", "assistant:approve", "assistant:action_propose", "assistant:action_approve",
		"group_action:mute_member", "group_action:publish_notice", "group_action:group_info", "group_action:send_text",
		"group_action:recall_message", "feature:blocked_words:words", "feature:blocked_words:enabled"}
	grants = append(grants, "assistant_module:workspace:query", "assistant_module:moderation:action_propose", "assistant_module:blocked_words:query", "assistant_module:blocked_words:config_propose")
	expect(t, admin, "PUT", "/users/"+uid+"/access", map[string]any{"role_code": "custom", "version": 0, "permissions": grants}, 200)
	if err := s.Gateway.Configure(ctx, control.Actor{ID: adminID, Role: "superadmin"}, control.GatewaySettings{Enabled: true, PlatformLimit: 1_000_000_000, TestLimit: 1_000_000_000},
		control.ModelRoute{Alias: "default", Enabled: true, Verified: true, Rates: aigateway.Rates{Input: 1, Output: 1},
			Model: assistant.ModelConfig{BaseURL: "https://synthetic.invalid/v1", Model: "synthetic", APIKey: "synthetic-no-network"}}); err != nil {
		t.Fatal(err)
	}
	settings := control.AssistantSettings{Enabled: true, TenantAccess: true, ApprovalsEnabled: true, MonthlyTokens: 1000000, Model: "default"}
	expect(t, admin, "PUT", "/assistant/settings", settings, 200)
	clients := map[string]testClient{"wsl": login("assistant-platform-owner", "wsl"), "lajiaoliao": login("assistant-platform-owner", "lajiaoliao")}
	accounts, groups := map[string]string{}, map[string]string{}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		a := admin
		a.workspace = platform
		accounts[platform] = field(t, expect(t, a, "POST", "/accounts", map[string]string{"name": "same-native-name"}, 201), "id")
		var groupID string
		nativeGroup := "same-native-group"
		if platform == "wsl" {
			nativeGroup = "123"
		}
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name,platform) VALUES($2,'同名测试群',$1) RETURNING id::text`, platform, nativeGroup).Scan(&groupID); err != nil {
			t.Fatal(err)
		}
		groups[platform] = groupID
		if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) VALUES($1,$2,$3)`, groups[platform], tenant, accounts[platform]); err != nil {
			t.Fatal(err)
		}
		seedSyntheticGroupOwnership(t, db, vault, groups[platform])
		if _, err := db.Platform.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source) VALUES($1,$2,'admin',now(),'native')`, accounts[platform], groups[platform]); err != nil {
			t.Fatal(err)
		}
	}
	// No selection, no model call. Same-name groups stay platform-specific.
	for platform, c := range clients {
		if _, err := s.Assistant.Context(ctx, refFor(c), "", "", 0, 30); err != nil {
			t.Fatalf("customer %s context: %v", platform, err)
		}
		raw := assistantRequest(t, c, "GET", "/context?limit=30", nil, "", 200)
		var view control.AssistantContext
		if err := json.Unmarshal(raw, &view); err != nil || view.Platform != platform || view.Total != 1 || !bytes.Contains(view.Groups, []byte(groups[platform])) {
			t.Fatal("context", platform, string(raw), err)
		}
		other := "wsl"
		if platform == "wsl" {
			other = "lajiaoliao"
		}
		if bytes.Contains(raw, []byte(groups[other])) {
			t.Fatal("cross-platform group leak")
		}
		assistantRequest(t, c, "POST", "/sessions", assistant.Scope{Platform: other, Kind: "group", GroupID: groups[other]}, "", 403)
	}
	var sibling string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role,tenant_id,tenant_role) VALUES('assistant-platform-member',$1,'user',$2,'legacy') RETURNING id::text`, hash, tenant).Scan(&sibling); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "PUT", "/users/"+sibling+"/access", map[string]any{"role_code": "custom", "version": 0, "permissions": grants}, 200)
	member := login("assistant-platform-member", "wsl")
	if raw := assistantRequest(t, member, "GET", "/context", nil, "", 200); !bytes.Contains(raw, []byte(`"total":0`)) {
		t.Fatal("member inherited owner's groups")
	}
	assistantRequest(t, member, "POST", "/sessions", assistant.Scope{Kind: "group", GroupID: groups["wsl"]}, "", 404)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO member_groups(user_id,tenant_id,group_id) VALUES($1,$2,$3)`, sibling, tenant, groups["wsl"]); err != nil {
		t.Fatal(err)
	}
	member = login("assistant-platform-member", "wsl")
	// Two same-name members require a human choice, not a guessed native ID.
	s.Assistant.NativeMembers = func(context.Context, string, string, string, string) ([]control.AssistantMember, error) {
		return []control.AssistantMember{{ID: "string-member-a", Name: "同名成员", Role: "member"}, {ID: "string-member-b", Name: "同名成员", Role: "member"}}, nil
	}
	c := clients["lajiaoliao"]
	ref := refFor(c)
	var session control.AssistantSession
	json.Unmarshal(assistantRequest(t, c, "POST", "/sessions", assistant.Scope{Platform: "lajiaoliao", Kind: "group", GroupID: groups["lajiaoliao"]}, "", 201), &session)
	if session.Scope.AccountID != accounts["lajiaoliao"] || session.Scope.Platform != "lajiaoliao" {
		t.Fatal("server did not resolve account")
	}
	assistantRequest(t, member, "GET", "/sessions/"+session.ID, nil, "", 404)
	overview := field(t, assistantRequest(t, c, "POST", "/sessions", assistant.Scope{Platform: "lajiaoliao", Kind: "overview"}, "", 201), "id")
	assistantRequest(t, c, "POST", "/sessions/"+overview+"/turns", map[string]string{"input": "未选群不能调用模型"}, "no-group-fixture", 409)
	turn, _, err := s.Assistant.CreateTurn(ctx, ref, session.ID, "action-platform-fixture", "查找同名成员")
	if err != nil {
		t.Fatal(err)
	}
	host, _, _, _, err := s.Assistant.Host(ctx, ref, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	call := assistant.ToolCall{Type: "function"}
	call.Function.Name = "find_group_members"
	call.Function.Arguments = `{"search":"同名"}`
	result, err := host.Call(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	members := result.Data.([]control.AssistantMember)
	call.Function.Name = "propose_group_action"
	body := map[string]any{"action": "mute_member", "target_token": members[0].Token, "minutes": 1}
	raw, _ := json.Marshal(body)
	call.Function.Arguments = string(raw)
	if _, err = host.Call(ctx, call); err == nil || err.Error() != "assistant_target_choice_required" {
		t.Fatal("ambiguous target accepted", err)
	}
	if _, err = s.Assistant.ChooseTarget(ctx, ref, session.ID, members[1].Token); err != nil {
		t.Fatal(err)
	}
	body["target_token"] = members[1].Token
	raw, _ = json.Marshal(body)
	call.Function.Arguments = string(raw)
	result, err = host.Call(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	action := result.Data.(control.AssistantActionPlan)
	if action.Status != "blocked" || action.Production || action.Target.ID != "string-member-b" {
		t.Fatal("production admission inherited", action)
	}
	call.Function.Name = "approve_group_action"
	call.Function.Arguments = `{}`
	if _, err = host.Call(ctx, call); err == nil || err.Error() != "assistant_invalid_tool" {
		t.Fatal("model acquired approval tool", err)
	}
	if err = s.Assistant.FinishTurn(ctx, turn.ID, nil); err != nil {
		t.Fatal(err)
	}
	// Each new adapter can propose, but cannot cross the independent production
	// gate. Message targets require a human choice even for one exact match.
	s.Assistant.NativeMessages = func(_ context.Context, platform, account, group, search string) ([]control.AssistantMessage, error) {
		ref := nativeauth.MessageReference{Platform: platform, EventID: 71, Group: group, Sender: "7",
			SourceDigest: strings.Repeat("a", 64), CreatedAt: time.Now().UnixMilli()}
		if platform == "lajiaoliao" {
			ref.Sender, ref.Conversation, ref.Sequence = "string-sender", "sg_"+group, 17
		} else {
			ref.Route = nativewire.ReplyRoute{Sender: "456", Target: "789", TimeMS: strconv.FormatInt(ref.CreatedAt, 10),
				ClientID: "11111111-2222-4333-8444-555555555555", ServerID: "9001"}
		}
		return []control.AssistantMessage{{Member: control.AssistantMember{ID: "71", Name: "synthetic message preview", Role: "message"}, Reference: ref}}, nil
	}
	for platform, client := range clients {
		platformRef := refFor(client)
		var currentSession control.AssistantSession
		json.Unmarshal(assistantRequest(t, client, "POST", "/sessions", assistant.Scope{Platform: platform, Kind: "group", GroupID: groups[platform]}, "", 201), &currentSession)
		for _, kind := range []string{"group_info", "send_text", "recall_message"} {
			currentTurn, _, e := s.Assistant.CreateTurn(ctx, platformRef, currentSession.ID, "adapter-"+platform+"-"+kind, "synthetic adapter check")
			if e != nil {
				t.Fatal(e)
			}
			currentHost, _, _, _, e := s.Assistant.Host(ctx, platformRef, currentTurn.ID)
			if e != nil {
				t.Fatal(e)
			}
			args := map[string]any{"action": kind}
			if kind == "recall_message" {
				find := assistant.ToolCall{Type: "function"}
				find.Function.Name, find.Function.Arguments = "find_group_messages", `{"search":"synthetic"}`
				candidates, e := currentHost.Call(ctx, find)
				if e != nil {
					t.Fatal(e)
				}
				choices := candidates.Data.([]control.AssistantMember)
				args["target_token"] = choices[0].Token
				raw, _ := json.Marshal(args)
				propose := assistant.ToolCall{Type: "function"}
				propose.Function.Name, propose.Function.Arguments = "propose_group_action", string(raw)
				if _, e = currentHost.Call(ctx, propose); e == nil || e.Error() != "assistant_target_choice_required" {
					t.Fatal("message target did not require human choice", e)
				}
				if _, e = s.Assistant.ChooseTarget(ctx, platformRef, currentSession.ID, choices[0].Token); e != nil {
					t.Fatal(e)
				}
			} else {
				args["text"] = "synthetic"
			}
			raw, _ := json.Marshal(args)
			propose := assistant.ToolCall{Type: "function"}
			propose.Function.Name, propose.Function.Arguments = "propose_group_action", string(raw)
			value, e := currentHost.Call(ctx, propose)
			if e != nil {
				t.Fatal(platform, kind, e)
			}
			p := value.Data.(control.AssistantActionPlan)
			if p.Status != "blocked" || p.Production {
				t.Fatal("new adapter bypassed production acceptance", p)
			}
			read := assistantRequest(t, client, "GET", "/action-plans/"+p.ID, nil, "", 200)
			if bytes.Contains(read, []byte("source_digest")) || bytes.Contains(read, []byte("sg_same-native-group")) {
				t.Fatal("sealed transport reference leaked")
			}
			assistantRequest(t, client, "POST", "/action-plans/"+p.ID+"/approve",
				map[string]any{"digest": p.Digest, "version": p.Version}, p.ID, 409)
			if kind == "send_text" {
				// Persisted synthetic attempt tests the forward migration and
				// read-only recovery without admitting or dispatching a native call.
				op := control.NewID()
				if _, e = db.Platform.Exec(ctx, `UPDATE accounts SET native_uid=$2 WHERE id=$1`, accounts[platform], "7"); e != nil {
					t.Fatal(e)
				}
				if _, e = db.Platform.Exec(ctx, `UPDATE assistant_action_plans SET status='approved',idempotency_key=$1 WHERE id=$1`, p.ID); e != nil {
					t.Fatal(e)
				}
				if _, e = db.Platform.Exec(ctx, `INSERT INTO native_setting_operations
					(id,actor_id,account_id,group_id,kind,idempotency_key,request_hash,account_epoch,status,expires_at)
					VALUES($1,$2,$3,$4,'send_text',$5,'synthetic',1,'preparing',now()+interval '15 seconds')`,
					op, uid, accounts[platform], groups[platform], p.ID); e != nil {
					t.Fatal(e)
				}
				var nonce, aad any
				if platform == "wsl" {
					nonce, aad = "7001", "7002"
				}
				if _, e = db.Platform.Exec(ctx, `INSERT INTO assistant_message_attempts
					(operation_id,platform,client_id,receiver_owner,receiver_revision,nonce,aad)
					VALUES($1,$2,$3,'synthetic-owner',1,$4::numeric,$5::numeric)`, op, platform, control.NewID(), nonce, aad); e != nil {
					t.Fatal(e)
				}
				native := s.nativeSettingsService()
				attempt, e := native.AssistantAttempt(ctx, platformRef, s.Assistant, p.ID)
				if e != nil || attempt.Platform != platform || attempt.ID != op {
					t.Fatal("durable attempt unavailable to original executor", attempt, e)
				}
				if _, e = db.Platform.Exec(ctx, `UPDATE assistant_message_attempts SET receiver_revision=2 WHERE operation_id=$1`, op); e == nil {
					t.Fatal("attempt identity was mutable")
				}
				if _, e = db.Platform.Exec(ctx, `UPDATE native_setting_operations SET status='unknown' WHERE id=$1`, op); e != nil {
					t.Fatal(e)
				}
				if _, e = native.AssistantAttempt(ctx, platformRef, s.Assistant, p.ID); e == nil {
					t.Fatal("unknown attempt became executable")
				}
				if _, e = db.Platform.Exec(ctx, `DELETE FROM native_setting_operations WHERE id=$1`, op); e != nil {
					t.Fatal(e)
				}
				if _, e = db.Platform.Exec(ctx, `UPDATE assistant_action_plans SET status='blocked',idempotency_key=NULL WHERE id=$1`, p.ID); e != nil {
					t.Fatal(e)
				}
			}
			if e = s.Assistant.FinishTurn(ctx, currentTurn.ID, nil); e != nil {
				t.Fatal(e)
			}
		}
	}
	assistantRequest(t, c, "POST", "/action-plans/"+action.ID+"/approve", map[string]any{"digest": action.Digest, "version": action.Version}, action.ID, 409)
	assistantRequest(t, c, "POST", "/action-plans/"+action.ID+"/approve", map[string]any{"digest": "forged", "version": action.Version}, action.ID, 409)
	assistantRequest(t, clients["wsl"], "GET", "/action-plans/"+action.ID, nil, "", 404)
	// RLS domain is transaction-local; a tenant/actor alone cannot read Chili.
	err = db.ActorTenantDomainTx(ctx, tenant, uid, "wsl", func(tx pgx.Tx) error {
		var n int
		e := tx.QueryRow(ctx, `SELECT count(*) FROM assistant_sessions WHERE id=$1`, session.ID).Scan(&n)
		if n != 0 {
			t.Fatal("domain widened RLS")
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.ActorTenantDomainTx(ctx, tenant, uid, "lajiaoliao", func(tx pgx.Tx) error {
		var n int
		e := tx.QueryRow(ctx, `SELECT count(*) FROM assistant_sessions WHERE id=$1`, session.ID).Scan(&n)
		if n != 1 {
			t.Fatal("Chili RLS missing")
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	// Database parent guard rejects mismatched child domains even for platform SQL.
	if _, err = db.Platform.Exec(ctx, `INSERT INTO assistant_turns(id,session_id,actor_id,tenant_id,domain,idempotency_key,input_hash,authorization_hash,status,deadline)
		VALUES($1,$2,$3,$4,'wsl','wrong-platform-child','x','x','cancelled',now())`, control.NewID(), session.ID, uid, tenant); err == nil {
		t.Fatal("parent mismatch persisted")
	}
	if _, err = db.Platform.Exec(ctx, `UPDATE assistant_action_plans SET account_epoch=account_epoch+1 WHERE id=$1`, action.ID); err == nil {
		t.Fatal("immutable plan epoch changed")
	}
	cancelled, err := s.Assistant.CancelAction(ctx, ref, action.ID)
	if err != nil || cancelled.Status != "cancelled" || cancelled.Version != 2 {
		t.Fatal("cancel action", cancelled.Status, err)
	}
	cancelled, err = s.Assistant.CancelAction(ctx, ref, action.ID)
	if err != nil || cancelled.Version != 2 {
		t.Fatal("duplicate cancellation mutated again", err)
	}
	assistantRequest(t, c, "POST", "/action-plans/"+action.ID+"/approve",
		map[string]any{"digest": action.Digest, "version": 1}, action.ID, 409)
	var nativeOperations int
	if err = db.Platform.QueryRow(ctx, `SELECT count(*) FROM native_setting_operations`).Scan(&nativeOperations); err != nil || nativeOperations != 0 {
		t.Fatal("blocked or cancelled plan created native operation", err)
	}
	// Synthetic persisted interruption, not a production grant or native call.
	// Recovery and repeated confirmation may only inspect its original operation.
	executorCalls := 0
	s.Assistant.ActionExecutor = func(context.Context, control.SessionRef, control.AssistantActionPlan) (control.SettingOperation, error) {
		executorCalls++
		return control.SettingOperation{}, fmt.Errorf("unexpected dispatch")
	}
	if _, err = db.Platform.Exec(ctx, `UPDATE assistant_action_plans SET status='approved',idempotency_key=$1 WHERE id=$1`, action.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Assistant.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Assistant.ApproveAction(ctx, ref, action.ID, action.Digest, cancelled.Version, action.ID)
	if err != nil || recovered.Status != "unknown" || executorCalls != 0 {
		t.Fatal("unknown result replayed", recovered.Status, executorCalls, err)
	}
	original := control.NewID()
	if _, err = db.Platform.Exec(ctx, `INSERT INTO native_setting_operations
		(id,actor_id,account_id,group_id,kind,idempotency_key,request_hash,account_epoch,status,result_code,expires_at)
		VALUES($1,$2,$3,$4,'mute_member',$5,'synthetic',1,'succeeded','desired_state_observed',now())`,
		original, uid, accounts["lajiaoliao"], groups["lajiaoliao"], action.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err = s.Assistant.ActionPlan(ctx, ref, action.ID)
	if err != nil || recovered.OperationID != original || recovered.Status != "succeeded" || executorCalls != 0 {
		t.Fatal("original result not reconciled", recovered.Status, err)
	}
	if _, err = db.Platform.Exec(ctx, `DELETE FROM native_setting_operations WHERE id=$1`, original); err != nil {
		t.Fatal(err)
	}
	var unsafeAudit bool
	if err = db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE action LIKE 'assistant_action_%'
		AND (detail::text LIKE '%string-member%' OR detail::text LIKE '%target%' OR detail::text LIKE '%payload%'))`).Scan(&unsafeAudit); err != nil || unsafeAudit {
		t.Fatal("unsafe assistant audit", err)
	}
	// Withdrawal invalidates stored plans and bodies before any further delivery.
	if _, err = db.Platform.Exec(ctx, `UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, groups["lajiaoliao"]); err != nil {
		t.Fatal(err)
	}
	assistantRequest(t, c, "GET", "/action-plans/"+action.ID, nil, "", 403)
	assistantRequest(t, c, "GET", "/turns/"+turn.ID+"/events?format=json", nil, "", 403)
	var operations int
	if err = db.Platform.QueryRow(ctx, `SELECT (SELECT count(*) FROM native_setting_operations)+(SELECT count(*) FROM test_windows)`).Scan(&operations); err != nil || operations != 0 {
		t.Fatal("assistant created native/test operations", operations, err)
	}
	// 300 groups over ten shared accounts, at most thirty bindings per account.
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		ids := []string{accounts[platform]}
		for i := 1; i < 5; i++ {
			a := admin
			a.workspace = platform
			ids = append(ids, field(t, expect(t, a, "POST", "/accounts", map[string]string{"name": fmt.Sprintf("%s-%d", platform, i)}, 201), "id"))
		}
		for i := 1; i < 150; i++ {
			var group string
			err = db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name,platform) VALUES($1,$2,$3) RETURNING id::text`,
				fmt.Sprintf("scope-%d", i), strings.Repeat("长群名", 12), platform).Scan(&group)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) VALUES($1,$2,$3)`, group, tenant, ids[i/30]); err != nil {
				t.Fatal(err)
			}
			seedSyntheticGroupOwnership(t, db, vault, group)
		}
		offset, seen := 0, map[string]bool{}
		for {
			var view control.AssistantContext
			raw := assistantRequest(t, clients[platform], "GET", fmt.Sprintf("/context?limit=30&offset=%d", offset), nil, "", 200)
			if err = json.Unmarshal(raw, &view); err != nil || view.Total != 150 {
				t.Fatal("pagination total", view.Total, err)
			}
			var page []struct {
				GroupID string `json:"group_id"`
			}
			json.Unmarshal(view.Groups, &page)
			if len(page) > 30 {
				t.Fatal("unbounded query")
			}
			for _, g := range page {
				if seen[g.GroupID] {
					t.Fatal("duplicate page")
				}
				seen[g.GroupID] = true
			}
			if view.NextOffset < 0 {
				break
			}
			offset = view.NextOffset
		}
		if len(seen) != 150 {
			t.Fatal("pagination omitted groups", len(seen))
		}
	}
	assistantRequest(t, member, "GET", "/context?limit=51", nil, "", 400)
	t.Run("delete conversations preserves operation and cost evidence", func(t *testing.T) {
		for _, platform := range []string{"wsl", "lajiaoliao"} {
			client := clients[platform]
			platformRef := refFor(client)
			scope := assistant.Scope{Platform: platform, Kind: "group", GroupID: groups[platform]}
			fresh, e := s.Assistant.CreateSession(ctx, platformRef, scope)
			if e != nil {
				t.Fatal(e)
			}
			other := clients["wsl"]
			if platform == "wsl" {
				other = clients["lajiaoliao"]
			}
			assistantRequest(t, other, "DELETE", "/sessions/"+fresh.ID, nil, "", 404)
			assistantRequest(t, member, "DELETE", "/sessions/"+fresh.ID, nil, "", 404)
			active, _, e := s.Assistant.CreateTurn(ctx, platformRef, fresh.ID, "delete-body-"+platform, "DELETE-SYNTHETIC-BODY")
			if e != nil {
				t.Fatal(e)
			}
			assistantRequest(t, client, "DELETE", "/sessions/"+fresh.ID, nil, "", 409)
			h, _, _, _, e := s.Assistant.Host(ctx, platformRef, active.ID)
			if e != nil {
				t.Fatal(e)
			}
			if e = h.Emit(ctx, assistant.Event{Kind: "text_delta", Data: map[string]string{"text": "DELETE-SYNTHETIC-ANSWER"}}); e != nil {
				t.Fatal(e)
			}
			var version int64
			if e = db.Platform.QueryRow(ctx, `SELECT version FROM group_bindings WHERE group_id=$1`, groups[platform]).Scan(&version); e != nil {
				t.Fatal(e)
			}
			propose := assistant.ToolCall{Type: "function"}
			propose.Function.Name = "propose_group_config_change"
			blob, _ := json.Marshal(map[string]any{"group_id": groups[platform], "base_version": version,
				"changes": map[string]any{"blocked_words": map[string]any{"words": []string{"synthetic"}}}})
			propose.Function.Arguments = string(blob)
			value, e := h.Call(ctx, propose)
			if e != nil {
				t.Fatal(e)
			}
			configPlan := value.Data.(control.AssistantPlan)
			propose.Function.Name, propose.Function.Arguments = "propose_group_action", `{"action":"group_info","text":"synthetic-deletion"}`
			value, e = h.Call(ctx, propose)
			if e != nil {
				t.Fatal(e)
			}
			actionPlan := value.Data.(control.AssistantActionPlan)
			if e = s.Assistant.FinishTurn(ctx, active.ID, nil); e != nil {
				t.Fatal(e)
			}
			usageID := control.NewID()
			if _, e = db.Platform.Exec(ctx, `INSERT INTO assistant_usage(id,turn_id,actor_id,tenant_id,domain,month,reserved,state)
				VALUES($1,$2,$3,$4,$5,current_date,17,'unknown')`, usageID, active.ID, uid, tenant, platform); e != nil {
				t.Fatal(e)
			}
			for i := 0; i < 2; i++ {
				raw := assistantRequest(t, client, "DELETE", "/sessions/"+fresh.ID, nil, "", 200)
				if !bytes.Contains(raw, []byte(`"deleted":true`)) {
					t.Fatal("delete did not reconcile", string(raw))
				}
			}
			assistantRequest(t, client, "GET", "/sessions/"+fresh.ID+"/transcript", nil, "", 404)
			assistantRequest(t, client, "GET", "/turns/"+active.ID+"/events?format=json", nil, "", 404)
			assistantRequest(t, client, "POST", "/sessions/"+fresh.ID+"/turns",
				map[string]string{"input": "DELETE-SYNTHETIC-BODY"}, "delete-body-"+platform, 404)
			assistantRequest(t, client, "GET", "/plans/"+configPlan.ID, nil, "", 404)
			assistantRequest(t, client, "POST", "/plans/"+configPlan.ID+"/approve",
				map[string]any{"digest": configPlan.Digest, "version": configPlan.Version}, configPlan.ID, 404)
			assistantRequest(t, client, "GET", "/action-plans/"+actionPlan.ID, nil, "", 404)
			assistantRequest(t, client, "POST", "/action-plans/"+actionPlan.ID+"/approve",
				map[string]any{"digest": actionPlan.Digest, "version": actionPlan.Version}, actionPlan.ID, 404)
			list := assistantRequest(t, client, "GET", fmt.Sprintf("/sessions?kind=group&group_id=%s", groups[platform]), nil, "", 200)
			if bytes.Contains(list, []byte(fresh.ID)) {
				t.Fatal("deleted session reappeared")
			}
			var bodies, events, turns, costs, audits int
			var cfg, actionState string
			if e = db.Platform.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM assistant_turns WHERE session_id=$1 AND input_sealed IS NOT NULL),
				(SELECT count(*) FROM assistant_events e JOIN assistant_turns t ON t.id=e.turn_id WHERE t.session_id=$1),
				(SELECT count(*) FROM assistant_turns WHERE session_id=$1),
				(SELECT count(*) FROM assistant_usage WHERE id=$2 AND state='unknown' AND reserved=17),
				(SELECT count(*) FROM audit_events WHERE resource_id=$1::text AND action='assistant_session_deleted'),
				(SELECT status FROM assistant_plans WHERE id=$3),
				(SELECT status FROM assistant_action_plans WHERE id=$4)`, fresh.ID, usageID, configPlan.ID, actionPlan.ID).
				Scan(&bodies, &events, &turns, &costs, &audits, &cfg, &actionState); e != nil {
				t.Fatal(e)
			}
			if bodies != 0 || events != 0 || turns != 1 || costs != 1 || audits != 1 || cfg != "cancelled" || actionState != "cancelled" {
				t.Fatal("deletion retention mismatch", bodies, events, turns, costs, audits, cfg, actionState)
			}
			next, e := s.Assistant.CreateSession(ctx, platformRef, scope)
			if e != nil || next.ID == fresh.ID || !next.IsEmpty {
				t.Fatal("deleted empty session reused", e)
			}
			assistantRequest(t, client, "DELETE", "/sessions/"+next.ID, nil, "", 200)
		}
		if executorCalls != 0 {
			t.Fatal("deletion dispatched native effects")
		}
	})
}
