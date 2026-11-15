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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

type fixtureModel func(context.Context, []assistant.Message, []assistant.Tool, func(string) error) (assistant.Response, error)

func (p fixtureModel) Generate(c context.Context, m []assistant.Message, t []assistant.Tool, e func(string) error) (assistant.Response, error) {
	return p(c, m, t, e)
}

func assistantRequest(t *testing.T, c testClient, method, path string, body any, key string, status int) json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, c.base+"/api/v1/assistant"+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("Idempotency-Key", key)
	if c.workspace != "" {
		req.Header.Set("X-Workspace", c.workspace)
	}
	u, _ := url.Parse(c.base)
	for _, cookie := range c.client.Jar.Cookies(u) {
		if cookie.Name == "wsl_csrf" {
			req.Header.Set("X-CSRF-Token", cookie.Value)
		}
	}
	res, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var envelope struct {
		Data json.RawMessage
		Msg  string
	}
	if err = json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: %d %s want %d", method, path, res.StatusCode, envelope.Msg, status)
	}
	return envelope.Data
}

func TestPostgresAssistantIsolationBudgetAndApproval(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-assistant-password"
	hash, _ := security.Password(password)
	_, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role) VALUES('assistant-admin',$1,'superadmin')`, hash)
	if err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{82}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	defer s.StopAssistant()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar, Timeout: 5 * time.Second}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{"username": name, "password": password}, 200)
		return c
	}
	admin := login("assistant-admin")
	openSyntheticAssistantModules(t, admin)
	account := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "synthetic-only"}, 201), "id")
	type owner struct {
		user, tenant, group string
		client              testClient
	}
	create := func(name string) owner {
		row := expect(t, admin, "POST", "/users", map[string]string{"username": name, "password": password, "tenant_name": name}, 201)
		out := owner{user: field(t, row, "id"), tenant: field(t, row, "tenant_id")}
		plan := field(t, expect(t, admin, "POST", "/plans", map[string]any{"name": name, "group_limit": 2,
			"monthly_assistant_tokens": 100000, "modules": []string{"moderation", "replies", "management_assistant"}}, 201), "id")
		expect(t, admin, "PUT", "/subscriptions/"+out.tenant, map[string]any{"plan_id": plan, "enabled": true, "version": 0, "expires_at": time.Now().Add(time.Hour)}, 200)
		explicitTestCeiling(t, admin, out.tenant)
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name) VALUES($1,$1) RETURNING id::text`, name).Scan(&out.group); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) VALUES($1,$2,$3)`, out.group, out.tenant, account); err != nil {
			t.Fatal(err)
		}
		seedSyntheticGroupOwnership(t, db, vault, out.group)
		if _, err := db.Platform.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
            VALUES($1,$2,'admin',now(),'native')`, account, out.group); err != nil {
			t.Fatal(err)
		}
		out.client = login(name)
		return out
	}
	a, b := create("assistant-a"), create("assistant-b")
	scope := assistant.Scope{Kind: "group", AccountID: account, GroupID: a.group}
	// New owners receive an explicit owner role, including separately declared assistant grants.
	grants := []string{"groups:read", "operations:read", "assistant:use", "assistant:propose", "assistant:approve", "feature:blocked_words:words", "assistant_module:blocked_words:query", "assistant_module:blocked_words:config_propose", "assistant_module:workspace:query"}
	for _, o := range []owner{a, b} {
		expect(t, admin, "PUT", "/users/"+o.user+"/access", map[string]any{"role_code": "custom", "version": 0, "permissions": grants}, 200)
	}
	a.client = login("assistant-a")
	b.client = login("assistant-b")
	var adminID, pro string
	if err = db.Platform.QueryRow(ctx, `SELECT id::text FROM users WHERE username='assistant-admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err = s.Gateway.Configure(ctx, control.Actor{ID: adminID, Role: "superadmin"}, control.GatewaySettings{Enabled: true, PlatformLimit: 100_000_000_000, TestLimit: 1_000_000_000},
		control.ModelRoute{Alias: "default", Enabled: true, Verified: true, Rates: aigateway.Rates{Input: 1_000_000, Output: 1_000_000},
			Model: assistant.ModelConfig{BaseURL: "https://models.example/v1", Model: "synthetic", APIKey: "NEVER-RETURN-THIS-KEY"}}); err != nil {
		t.Fatal(err)
	}
	if err = db.Platform.QueryRow(ctx, `SELECT id::text FROM commercial_plans WHERE code='PRO'`).Scan(&pro); err != nil {
		t.Fatal(err)
	}
	for _, o := range []owner{a, b} {
		if _, err = db.Platform.Exec(ctx, `DELETE FROM subscriptions WHERE tenant_id=$1`, o.tenant); err != nil {
			t.Fatal(err)
		}
		if _, err = (&control.BillingService{DB: db}).Confirm(ctx, control.Actor{ID: adminID, Role: "superadmin"}, control.PaymentConfirmation{
			TenantID: o.tenant, PlanVersionID: pro, IdempotencyKey: "assistant-period-" + o.tenant, PaymentReference: "synthetic-" + o.tenant, AmountMicroUSDT: 49_000_000}); err != nil {
			t.Fatal(err)
		}
	}
	var baseVersion int64
	if err = db.Platform.QueryRow(ctx, `SELECT version FROM group_bindings WHERE group_id=$1`, a.group).Scan(&baseVersion); err != nil {
		t.Fatal(err)
	}
	var settings control.AssistantSettings
	json.Unmarshal(assistantRequest(t, admin, "GET", "/settings", nil, "", 200), &settings)
	if settings.Enabled || settings.TenantAccess || settings.MonthlyTokens != 0 {
		t.Fatal("unsafe defaults")
	}
	settings.BaseURL, settings.Model, settings.APIKey = "", "default", ""
	settings.Enabled, settings.TenantAccess, settings.ApprovalsEnabled, settings.MonthlyTokens = true, true, true, 1000000
	raw := expect(t, admin, "PUT", "/assistant/settings", settings, 200)
	if bytes.Contains(raw, []byte("NEVER-RETURN")) {
		t.Fatal("model key returned")
	}
	session := field(t, assistantRequest(t, a.client, "POST", "/sessions", scope, "", 201), "id")
	assistantRequest(t, b.client, "GET", "/sessions/"+session, nil, "", 404)
	assistantRequest(t, b.client, "POST", "/sessions", scope, "", 404)
	mai := admin
	mai.workspace = "maibot"
	assistantRequest(t, mai, "GET", "/settings", nil, "", 403)
	var calls atomic.Int32
	s.AssistantProvider = func(assistant.ModelConfig) (assistant.Provider, error) {
		return fixtureModel(func(ctx context.Context, m []assistant.Message, tools []assistant.Tool, emit func(string) error) (assistant.Response, error) {
			calls.Add(1)
			response := assistant.Response{Message: assistant.Message{Role: "assistant"}, Usage: assistant.Usage{PromptTokens: 6, CompletionTokens: 4, TotalTokens: 10}}
			if m[1].Content == "slow" {
				<-ctx.Done()
				return assistant.Response{}, ctx.Err()
			}
			last := m[len(m)-1]
			call := assistant.ToolCall{ID: "read-config", Type: "function"}
			call.Function.Name = "get_group_config"
			args := map[string]any{"group_id": a.group}
			if last.Role == "tool" {
				if strings.Contains(last.Content, `"type":"propose_group_config_change"`) {
					t.Error("model continued after a human-confirmable proposal")
					response.Message.Content = "仅为建议，等待网站审批。"
					return response, emit(response.Message.Content)
				}
				call.ID = "propose"
				call.Function.Name = "propose_group_config_change"
				args["base_version"] = baseVersion
				args["changes"] = map[string]any{"blocked_words": map[string]any{"words": []string{"synthetic-word"}}}
			}
			blob, _ := json.Marshal(args)
			call.Function.Arguments = string(blob)
			response.Message.ToolCalls = []assistant.ToolCall{call}
			return response, nil
		}), nil
	}
	turn := field(t, assistantRequest(t, a.client, "POST", "/sessions/"+session+"/turns", map[string]string{"input": "propose"}, "turn-fixture-001", 202), "id")
	again := field(t, assistantRequest(t, a.client, "POST", "/sessions/"+session+"/turns", map[string]string{"input": "propose"}, "turn-fixture-001", 202), "id")
	if again != turn {
		t.Fatal("idempotency created another turn")
	}
	assistantRequest(t, a.client, "POST", "/sessions/"+session+"/turns", map[string]string{"input": "different"}, "turn-fixture-001", 409)
	wait := func(id, status string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var actual string
			if err := db.Platform.QueryRow(ctx, `SELECT status FROM assistant_turns WHERE id=$1`, id).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if actual == status {
				return
			}
			if actual != "queued" && actual != "running" {
				t.Fatalf("turn status %s want %s", actual, status)
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("turn timeout")
	}
	wait(turn, "succeeded")
	listScope := fmt.Sprintf("/sessions?kind=group&account_id=%s&group_id=%s", account, a.group)
	if raw := assistantRequest(t, a.client, "GET", listScope, nil, "", 200); !bytes.Contains(raw, []byte(`"title":"propose"`)) {
		t.Fatal("history title did not use first input", string(raw))
	}
	if calls.Load() != 2 {
		t.Fatal("model request bound/idempotency", calls.Load())
	}
	events := assistantRequest(t, a.client, "GET", "/turns/"+turn+"/events?format=json", nil, "", 200)
	if !bytes.Contains(events, []byte("synthetic-word")) {
		t.Fatal("tool event did not decode", string(events))
	}
	assistantRequest(t, b.client, "GET", "/turns/"+turn, nil, "", 404)
	var planID string
	if err = db.Platform.QueryRow(ctx, `SELECT id::text FROM assistant_plans WHERE session_id=$1`, session).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	var plan control.AssistantPlan
	json.Unmarshal(assistantRequest(t, a.client, "GET", "/plans/"+planID, nil, "", 200), &plan)
	var version int64
	db.Platform.QueryRow(ctx, `SELECT version FROM group_bindings WHERE group_id=$1`, a.group).Scan(&version)
	if version != baseVersion {
		t.Fatal("proposal mutated configuration")
	}
	assistantRequest(t, a.client, "POST", "/plans/"+planID+"/approve", map[string]any{"digest": "tampered", "version": 1}, planID, 409)
	body := map[string]any{"digest": plan.Digest, "version": plan.Version}
	db.Platform.Exec(ctx, `UPDATE account_groups SET role='member' WHERE account_id=$1 AND group_id=$2`, account, a.group)
	assistantRequest(t, a.client, "POST", "/plans/"+planID+"/approve", body, planID, 409)
	db.Platform.Exec(ctx, `UPDATE account_groups SET role='admin',observed_at=now() WHERE account_id=$1 AND group_id=$2`, account, a.group)
	approved := assistantRequest(t, a.client, "POST", "/plans/"+planID+"/approve", body, planID, 200)
	if !bytes.Contains(approved, []byte(`"applied":false`)) || !bytes.Contains(approved, []byte(`"status":"committed"`)) {
		t.Fatal("fake activation", string(approved))
	}
	assistantRequest(t, a.client, "POST", "/plans/"+planID+"/approve", body, planID, 200)
	var executions int
	db.Platform.QueryRow(ctx, `SELECT count(*) FROM assistant_plan_executions WHERE plan_id=$1`, planID).Scan(&executions)
	db.Platform.QueryRow(ctx, `SELECT version FROM group_bindings WHERE group_id=$1`, a.group).Scan(&version)
	if executions != 1 || version != baseVersion+1 {
		t.Fatal("duplicate approval", executions, version)
	}
	assistantRequest(t, a.client, "GET", "/turns/"+turn+"/events?format=json", nil, "", 403) // history is invalidated too.
	var transcript []control.AssistantTranscriptTurn
	if err = json.Unmarshal(assistantRequest(t, a.client, "GET", "/sessions/"+session+"/transcript", nil, "", 200), &transcript); err != nil {
		t.Fatal(err)
	}
	if len(transcript) != 1 || !transcript[0].Unavailable || transcript[0].Input != "" || transcript[0].Answer != "" || len(transcript[0].Cards) != 0 {
		t.Fatal("changed scope disclosed old transcript")
	}
	if raw := assistantRequest(t, a.client, "GET", listScope, nil, "", 200); bytes.Contains(raw, []byte("propose")) {
		t.Fatal("changed scope disclosed history title")
	}
	assistantRequest(t, b.client, "GET", "/sessions/"+session+"/transcript", nil, "", 404)
	var used int64
	db.Platform.QueryRow(ctx, `SELECT sum(charged_micro) FROM ai_calls WHERE external_key LIKE $1`, "assistant:"+turn+":%").Scan(&used)
	if used != 20000 {
		t.Fatal("usage settlement", used)
	}
	// Same-tenant users cannot read each other's sessions even with the same grants.
	var sibling string
	db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role,tenant_id) VALUES('assistant-sibling',$1,'user',$2) RETURNING id::text`, hash, a.tenant).Scan(&sibling)
	expect(t, admin, "PUT", "/users/"+sibling+"/access", map[string]any{"role_code": "custom", "version": 0, "permissions": grants}, 200)
	if _, err = db.Platform.Exec(ctx, `INSERT INTO member_groups(user_id,tenant_id,group_id) VALUES($1,$2,$3)`, sibling, a.tenant, a.group); err != nil {
		t.Fatal(err)
	}
	assistantRequest(t, login("assistant-sibling"), "GET", "/sessions/"+session, nil, "", 404)
	assistantRequest(t, login("assistant-sibling"), "GET", "/sessions/"+session+"/transcript", nil, "", 404)
	// RLS cannot be widened by supplying only a tenant without an actor context.
	err = db.ActorTenantTx(ctx, a.tenant, sibling, func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM assistant_sessions WHERE id=$1`, session).Scan(&n)
		if n != 0 {
			t.Fatal("RLS actor leak")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Cancellation ends a paid request; it does not start a replacement.
	session2 := field(t, assistantRequest(t, a.client, "POST", "/sessions", scope, "", 201), "id")
	slow := field(t, assistantRequest(t, a.client, "POST", "/sessions/"+session2+"/turns", map[string]string{"input": "slow"}, "turn-fixture-slow", 202), "id")
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	assistantRequest(t, a.client, "POST", "/turns/"+slow+"/cancel", map[string]any{}, "cancel-fixture", 200)
	wait(slow, "cancelled")
	refFor := func(c testClient) control.SessionRef {
		u, _ := url.Parse(c.base)
		for _, cookie := range c.client.Jar.Cookies(u) {
			if cookie.Name == "wsl_session" {
				ref, _ := control.WebsiteSession(cookie.Value)
				return ref
			}
		}
		t.Fatal("website session missing")
		return control.SessionRef{}
	}
	t.Run("one empty session and sealed first-input title", func(t *testing.T) {
		ref := refFor(a.client)
		const concurrent = 8
		var wg sync.WaitGroup
		items := make([]control.AssistantSession, concurrent)
		failures := make([]error, concurrent)
		for i := range items {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				items[i], failures[i] = s.Assistant.CreateSession(ctx, ref, scope)
			}(i)
		}
		wg.Wait()
		for i, item := range items {
			if failures[i] != nil || item.ID != items[0].ID || !item.IsEmpty {
				t.Fatal("concurrent empty session duplication", i, failures[i])
			}
		}
		var empties int
		if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM assistant_sessions s
			WHERE actor_id=$1 AND group_id=$2 AND NOT EXISTS(SELECT 1 FROM assistant_turns WHERE session_id=s.id)`,
			a.user, a.group).Scan(&empties); err != nil || empties != 1 {
			t.Fatal("empty records accumulated", empties, err)
		}
		otherScope := assistant.Scope{Kind: "group", AccountID: account, GroupID: b.group}
		otherSession, err := s.Assistant.CreateSession(ctx, refFor(b.client), otherScope)
		if err != nil || otherSession.ID == items[0].ID {
			t.Fatal("empty session crossed actors/groups", err)
		}
		first := "\n  首次问题🙂\t" + strings.Repeat("中文", 30)
		wantTitle := "首次问题🙂 " + strings.Repeat("中文", 13) + "…"
		for i := 0; i < 11; i++ {
			input := fmt.Sprintf("later-question-%d", i)
			if i == 0 {
				input = first
			}
			item, _, err := s.Assistant.CreateTurn(ctx, ref, items[0].ID, fmt.Sprintf("title-fixture-%d", i), input)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Assistant.FinishTurn(ctx, item.ID, context.Canceled); err != nil {
				t.Fatal(err)
			}
		}
		var rows []struct {
			ID, Title string
			IsEmpty   bool `json:"is_empty"`
		}
		raw := assistantRequest(t, a.client, "GET", listScope, nil, "", 200)
		if err = json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.ID == items[0].ID {
				found = true
				if row.Title != wantTitle || row.IsEmpty {
					t.Fatal("first title changed after pruning", row.Title)
				}
			}
		}
		if !found {
			t.Fatal("session missing")
		}
		var sealed []byte
		if err = db.Platform.QueryRow(ctx, `SELECT title_sealed FROM assistant_sessions WHERE id=$1`, items[0].ID).Scan(&sealed); err != nil ||
			len(sealed) == 0 || bytes.Contains(sealed, []byte("首次问题")) {
			t.Fatal("title was not sealed", err)
		}
		newEmpty, err := s.Assistant.CreateSession(ctx, ref, scope)
		if err != nil || newEmpty.ID == items[0].ID || !newEmpty.IsEmpty {
			t.Fatal("new conversation reused a nonempty session", err)
		}
	})
	t.Run("rolling transcript preserves idempotency finance and approvals", func(t *testing.T) {
		ref := refFor(a.client)
		ids := make([]string, 12)
		for i := range ids {
			input := fmt.Sprintf("history-%02d", i)
			item, created, createErr := s.Assistant.CreateTurn(ctx, ref, session, input+"-fixture", input)
			if createErr != nil || !created {
				t.Fatal("synthetic turn", i, createErr)
			}
			ids[i] = item.ID
			host, _, _, _, hostErr := s.Assistant.Host(ctx, ref, item.ID)
			if hostErr != nil {
				t.Fatal(hostErr)
			}
			chunks := 1
			if i == len(ids)-1 {
				chunks = 60 // More than one SSE page; snapshot must not lose text or replay it.
			}
			for j := 0; j < chunks; j++ {
				if emitErr := host.Emit(ctx, assistant.Event{Kind: "text_delta", Data: map[string]string{"text": "片段"}}); emitErr != nil {
					t.Fatal(emitErr)
				}
			}
			if finishErr := s.Assistant.FinishTurn(ctx, item.ID, nil); finishErr != nil {
				t.Fatal(finishErr)
			}
		}
		if decodeErr := json.Unmarshal(assistantRequest(t, a.client, "GET", "/sessions/"+session+"/transcript", nil, "", 200), &transcript); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if len(transcript) != control.AssistantHistoryLimit || transcript[0].ID != ids[2] || transcript[9].ID != ids[11] ||
			transcript[9].Input != "history-11" || transcript[9].Answer != strings.Repeat("片段", 60) || transcript[9].Cursor != 61 {
			t.Fatal("rolling snapshot lost order, text or cursor", len(transcript))
		}
		var retained, pruned, oldEvents, planExecutions int
		var oldBody bool
		if queryErr := db.Platform.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE history_pruned_at IS NOT NULL)
			FROM assistant_turns WHERE session_id=$1`, session).Scan(&retained, &pruned); queryErr != nil {
			t.Fatal(queryErr)
		}
		if queryErr := db.Platform.QueryRow(ctx, `SELECT input_sealed IS NOT NULL,(SELECT count(*) FROM assistant_events WHERE turn_id=$1)
			FROM assistant_turns WHERE id=$1`, ids[0]).Scan(&oldBody, &oldEvents); queryErr != nil {
			t.Fatal(queryErr)
		}
		if retained != 13 || pruned != 3 || oldBody || oldEvents != 0 {
			t.Fatal("overflow did not clear bodies while retaining quota/idempotency", retained, pruned, oldBody, oldEvents)
		}
		assistantRequest(t, a.client, "GET", "/turns/"+ids[0], nil, "", 410)
		assistantRequest(t, a.client, "GET", "/turns/"+ids[0]+"/events?format=json", nil, "", 410)
		reused := field(t, assistantRequest(t, a.client, "POST", "/sessions/"+session+"/turns", map[string]string{"input": "history-00"}, "history-00-fixture", 202), "id")
		if reused != ids[0] || calls.Load() != 3 {
			t.Fatal("pruned idempotency key replayed a model", reused, calls.Load())
		}
		assistantRequest(t, a.client, "POST", "/sessions/"+session+"/turns", map[string]string{"input": "changed"}, "history-00-fixture", 409)
		if queryErr := db.Platform.QueryRow(ctx, `SELECT count(*) FROM assistant_plan_executions WHERE plan_id=$1`, planID).Scan(&planExecutions); queryErr != nil || planExecutions != 1 {
			t.Fatal("rolling cleanup removed approval evidence", queryErr, planExecutions)
		}
		assistantRequest(t, a.client, "GET", "/plans/"+planID, nil, "", 200)
		if queryErr := db.Platform.QueryRow(ctx, `SELECT sum(charged_micro) FROM ai_calls WHERE external_key LIKE $1`, "assistant:"+turn+":%").Scan(&used); queryErr != nil || used != 20000 {
			t.Fatal("rolling cleanup altered financial evidence", queryErr, used)
		}
		// The other session is independent, even though it belongs to the same actor.
		assistantRequest(t, a.client, "GET", "/turns/"+slow, nil, "", 200)
		active, _, createErr := s.Assistant.CreateTurn(ctx, ref, session, "history-active-fixture", "active")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if retainErr := s.Assistant.Retain(ctx); retainErr != nil {
			t.Fatal(retainErr)
		}
		assistantRequest(t, a.client, "GET", "/turns/"+active.ID, nil, "", 200)
		if finishErr := s.Assistant.FinishTurn(ctx, active.ID, context.Canceled); finishErr != nil {
			t.Fatal(finishErr)
		}
		if retainErr := s.Assistant.Retain(ctx); retainErr != nil {
			t.Fatal(retainErr)
		}
		assistantRequest(t, a.client, "GET", "/turns/"+active.ID, nil, "", 200)
		assistantRequest(t, a.client, "GET", "/turns/"+ids[2], nil, "", 410)
	})
	siblingClient := login("assistant-sibling")
	siblingSession := field(t, assistantRequest(t, siblingClient, "POST", "/sessions", scope, "", 201), "id")
	manual, _, err := s.Assistant.CreateTurn(ctx, refFor(a.client), session2, "manual-budget-a", "manual")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.Assistant.CreateTurn(ctx, refFor(siblingClient), siblingSession, "manual-budget-b", "manual")
	if err != nil {
		t.Fatal(err)
	}
	h1, _, _, _, err := s.Assistant.Host(ctx, refFor(a.client), manual.ID)
	if err != nil {
		t.Fatal(err)
	}
	h2, _, _, _, err := s.Assistant.Host(ctx, refFor(siblingClient), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := h1.Reserve(ctx, 60000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h2.Reserve(ctx, 60000); err != nil {
		t.Fatal("runtime duplicated gateway reservation", err)
	}
	if err = h1.Settle(ctx, reservation, assistant.Usage{}, true); err != nil {
		t.Fatal(err)
	}
	meter := assistantRequest(t, a.client, "GET", "/usage", nil, "", 200)
	if !bytes.Contains(meter, []byte(`"billing_source":"shared_gateway"`)) {
		t.Fatal("unknown cost released", string(meter))
	}
	foreignMeter := assistantRequest(t, b.client, "GET", "/usage", nil, "", 200)
	if !bytes.Contains(foreignMeter, []byte(`"used_or_reserved":0`)) {
		t.Fatal("cross-tenant budget disclosure", string(foreignMeter))
	}
	bad := assistant.ToolCall{ID: "forged", Type: "function"}
	bad.Function.Name = "get_group_config"
	bad.Function.Arguments = `{"group_id":"` + a.group + `","tenant_id":"` + b.tenant + `"}`
	if _, err = h1.Call(ctx, bad); err == nil {
		t.Fatal("model identity parameter accepted")
	}
	bad.Function.Name = "propose_group_config_change"
	bad.Function.Arguments = `{"group_id":"` + a.group + `","base_version":2,"changes":{"blocked_words":{"enabled":true}}}`
	if _, err = h1.Call(ctx, bad); err == nil {
		t.Fatal("model proposed enable switch")
	}
	bad.Function.Arguments = fmt.Sprintf(`{"group_id":%q,"base_version":%d,"changes":{"blocked_words":{"words":["second"]}}}`, a.group, baseVersion+1)
	proposed, err := h1.Call(ctx, bad)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(proposed.Data)
	var expiring control.AssistantPlan
	json.Unmarshal(blob, &expiring)
	db.Platform.Exec(ctx, `UPDATE assistant_plans SET expires_at=now()-interval '1 second' WHERE id=$1`, expiring.ID)
	assistantRequest(t, a.client, "POST", "/plans/"+expiring.ID+"/approve", map[string]any{"digest": expiring.Digest, "version": 1}, expiring.ID, 409)
	// Withdrawing a field grant blocks old proposal content as well as future saves.
	proposed, err = h1.Call(ctx, bad)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ = json.Marshal(proposed.Data)
	json.Unmarshal(blob, &expiring)
	expect(t, admin, "PUT", "/users/"+a.user+"/access", map[string]any{"role_code": "custom", "version": 1, "permissions": []string{"groups:read", "assistant:use", "assistant:propose", "assistant:approve"}}, 200)
	assistantRequest(t, a.client, "GET", "/plans/"+expiring.ID, nil, "", 401)
	if h1.Check(ctx) == nil {
		t.Fatal("grant revocation did not stop running turn")
	}
	if err = s.Assistant.FinishTurn(ctx, manual.ID, context.Canceled); err != nil {
		t.Fatal(err)
	}
	if err = s.Assistant.FinishTurn(ctx, other.ID, context.Canceled); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "PUT", "/users/"+a.user+"/access", map[string]any{"role_code": "custom", "version": 2, "permissions": grants}, 200)
	a.client = login("assistant-a")
	// Reservation exhaustion prevents another model call.
	before := calls.Load()
	db.Platform.Exec(ctx, `UPDATE ai_gateway_settings SET platform_limit_micro=0`)
	empty := field(t, assistantRequest(t, a.client, "POST", "/sessions/"+session2+"/turns", map[string]string{"input": "budget"}, "turn-fixture-empty", 202), "id")
	wait(empty, "failed")
	if calls.Load() != before {
		t.Fatal("called model without budget")
	}
	var leaked bool
	db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE action LIKE 'assistant_%'
        AND (detail::text LIKE '%synthetic-word%' OR detail::text LIKE '%NEVER-RETURN%'))`).Scan(&leaked)
	if leaked {
		t.Fatal("sensitive audit data")
	}
	// Startup interruption is explicit and conservatively retains unverified reservations.
	db.Platform.Exec(ctx, `UPDATE ai_calls SET status='submitted' WHERE external_key LIKE $1`, "assistant:"+slow+":%")
	if err = s.Assistant.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Gateway.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	db.Platform.QueryRow(ctx, `SELECT status FROM ai_calls WHERE external_key LIKE $1`, "assistant:"+slow+":%").Scan(&state)
	if state != "unknown" {
		t.Fatal("lost reservation", state)
	}
	db.Platform.Exec(ctx, `UPDATE assistant_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, session)
	if err = s.Assistant.Retain(ctx); err != nil {
		t.Fatal(err)
	}
	assistantRequest(t, a.client, "GET", "/sessions/"+session, nil, "", 404)
	db.Platform.QueryRow(ctx, `SELECT sum(charged_micro) FROM ai_calls WHERE external_key LIKE $1`, "assistant:"+turn+":%").Scan(&used)
	if used != 20000 {
		t.Fatal("retention deleted budget evidence")
	}
}
