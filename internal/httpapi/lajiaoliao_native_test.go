package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresChiliNativeDebugUsesOwnPlatformAndDurableUnknown(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{73}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	actor, account, group, tenant := control.NewID(), control.NewID(), control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	credential := []byte("synthetic-chili-session")
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'chili-debug','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO accounts(id,name,workspace,platform,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
		VALUES($1,'chili-debug','lajiaoliao','lajiaoliao','self-string','authenticated',$2,true,true)`, account, credential)
	exec(`INSERT INTO tenants(id,name) VALUES($1,'chili-tenant')`, tenant)
	exec(`INSERT INTO native_groups(id,platform,native_id,name,isolated_test)
		VALUES($1,'lajiaoliao','group-string','chili-group',true)`, group)
	exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,active,management_enabled,ai_enabled)
		VALUES($1,$2,$3,true,false,false)`, group, tenant, account)
	exec(`INSERT INTO account_groups(account_id,group_id,role,observation_source,observed_at)
		VALUES($1,$2,'owner','native',now())`, account, group)
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(server.URL)
	jar.SetCookies(origin, []*http.Cookie{{Name: "wsl_session", Value: token}, {Name: "wsl_csrf", Value: "csrf"}})
	client := testClient{client: &http.Client{Jar: jar}, base: server.URL, workspace: "lajiaoliao"}
	input := map[string]any{"group_id": group, "account_ids": []string{account}, "target_ids": []string{"target-string"},
		"actions": []string{"grant_admin", "revoke_admin"}, "admission_mode": "native_debug", "seconds": 300, "budget": 3}
	window := field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
	if _, err := service.TestScope(ctx, ref, "wsl", window, account, "grant_admin", "target-string"); err == nil {
		t.Fatal("Chili grant inherited WSL admission")
	}
	req := control.SettingRequest{Account: account, Workspace: "lajiaoliao", TestWindowID: window, Epoch: 1, Version: 1,
		Key: control.NewID(), Digest: strings.Repeat("b", 64),
		Change: nativeauth.SettingChange{Kind: "grant_admin", Group: "group-string", Target: "target-string"}}
	targetRole := "member"
	proof := func() control.SettingEvidence {
		return control.SettingEvidence{UID: "self-string", Epoch: 1, Version: 1, DeploymentVersion: 0,
			Role: "owner", TargetRole: targetRole, Target: req.Change.Target, Digest: req.Digest,
			CredentialDigest: sha256.Sum256(credential), ModerationAllowed: true, ObservedAt: time.Now()}
	}
	writes := 0
	port := func(work context.Context, fence control.SettingFence) error {
		return fence(work, proof(), func(context.Context) error { writes++; return nil })
	}
	result, err := service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || result.Status != "succeeded" || writes != 1 {
		t.Fatal("string identity or deployment-zero rejected", result, err, writes)
	}
	req.Key, req.Change.Kind, targetRole = control.NewID(), "revoke_admin", "admin"
	result, err = service.ExecuteModeration(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &lajiaoliao.Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	})
	if err != nil || result.Status != "unknown" || writes != 2 {
		t.Fatal("Chili uncertain result lost", result, err, writes)
	}
	again, err := service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || again.ID != result.ID || writes != 2 {
		t.Fatal("uncertain operation replayed", again, err, writes)
	}
	exec(`UPDATE test_windows SET cancelled=true`)
	targetRole = "member"
	recovered, err := service.Reconcile(ctx, ref, account, result.ID,
		func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
			p := proof()
			p.Matches = true
			return p, nil
		})
	if err != nil || recovered.Status != "succeeded" || writes != 2 {
		t.Fatal("read-only reconciliation failed after grant close", recovered, err)
	}
	history, err := service.TestOperations(ctx, ref, account)
	if err != nil || len(history) != 2 {
		t.Fatal("Chili durable operations absent", len(history), err)
	}
	input["actions"], input["target_ids"] = []string{"member_nickname"}, []string{"self-string"}
	window = field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
	req.TestWindowID, req.Key = window, control.NewID()
	req.Change, targetRole = nativeauth.SettingChange{Kind: "member_nickname", Group: "group-string",
		Target: "self-string", Text: "debug card"}, "owner"
	result, err = service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || result.Status != "succeeded" || writes != 3 {
		t.Fatal("native card debug required a commercial cards module", result, err, writes)
	}
	req.Key = control.NewID()
	exec(`UPDATE accounts SET epoch=2`)
	if _, err := service.ExecuteModeration(ctx, ref, req, port); err == nil || writes != 3 {
		t.Fatal("account epoch change escaped the Chili control fence", err)
	}
	exec(`UPDATE accounts SET epoch=1`)
	input["actions"] = []string{"group_accept"}
	expect(t, client, "POST", "/test-windows", input, 409)
	input["actions"] = []string{"grant_admin"}
	input["admission_mode"] = "package"
	expect(t, client, "POST", "/test-windows", input, 409)
	var untouched bool
	if err = db.Platform.QueryRow(ctx, `SELECT NOT management_enabled AND NOT ai_enabled
		AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1)
		FROM group_bindings WHERE group_id=$2`, tenant, group).Scan(&untouched); err != nil || !untouched {
		t.Fatal("debug enabled production modules", err)
	}
	t.Run("permanent_ban_has_independent_grants_and_durable_recovery", func(t *testing.T) {
		input["admission_mode"], input["actions"], input["target_ids"], input["budget"] =
			"native_debug", []string{"kick_member"}, []string{"target-string"}, 1
		window = field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
		req.TestWindowID, req.Key = window, control.NewID()
		req.Change, targetRole = nativeauth.SettingChange{Kind: "kick_member_ban",
			Group: "group-string", Target: "target-string"}, "member"
		if _, err := service.ExecuteModeration(ctx, ref, req, port); err == nil || writes != 3 {
			t.Fatal("ordinary kick grant admitted permanent ban", err, writes)
		}
		input["actions"], input["budget"] = []string{"kick_member_ban", "remove_group_ban"}, 3
		window = field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
		req.TestWindowID, req.Key = window, control.NewID()
		if _, err := service.TestScope(ctx, ref, "lajiaoliao", window, account, "kick_member_ban", "foreign-target"); err == nil {
			t.Fatal("permanent ban grant admitted an unlisted target")
		}
		noEvidence, err := service.ExecuteModeration(ctx, ref, req, port)
		if err != nil || noEvidence.Status != "failed" || writes != 3 {
			t.Fatal("permanent ban lacked explicit native list evidence", noEvidence, err, writes)
		}
		banProof := func() control.SettingEvidence {
			p := proof()
			p.GroupBanKnown, p.TargetBanned = true, true
			return p
		}
		banPort := func(work context.Context, fence control.SettingFence) error {
			return fence(work, banProof(), func(context.Context) error { writes++; return nil })
		}
		req.Key = control.NewID()
		unknown, err := service.ExecuteModeration(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
			if err := banPort(work, fence); err != nil {
				return err
			}
			return &lajiaoliao.Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
		})
		if err != nil || unknown.Status != "unknown" || writes != 4 {
			t.Fatal("permanent ban unknown result lost", unknown, err, writes)
		}
		again, err := service.ExecuteModeration(ctx, ref, req, banPort)
		if err != nil || again.ID != unknown.ID || writes != 4 {
			t.Fatal("unknown permanent ban replayed", again, err, writes)
		}
		exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, window)
		recovered, err := service.Reconcile(ctx, ref, account, unknown.ID,
			func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
				p := banProof()
				p.Matches = true
				return p, nil
			})
		if err != nil || recovered.Status != "succeeded" || writes != 4 {
			t.Fatal("closed permanent ban grant could not reconcile", recovered, err, writes)
		}
		req.Key, req.Change.Kind = control.NewID(), "remove_group_ban"
		if _, err := service.ExecuteModeration(ctx, ref, req, banPort); err == nil || writes != 4 {
			t.Fatal("cancelled grant removed a ban")
		}
		input["actions"], input["budget"] = []string{"remove_group_ban"}, 2
		window = field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
		req.TestWindowID, req.Key, targetRole = window, control.NewID(), ""
		unbanned, err := service.ExecuteModeration(ctx, ref, req, banPort)
		if err != nil || unbanned.Status != "succeeded" || writes != 5 {
			t.Fatal("known banned former member could not be unbanned", unbanned, err, writes)
		}
		req.Key = control.NewID()
		exec(`UPDATE accounts SET epoch=2 WHERE id=$1`, account)
		if _, err := service.ExecuteModeration(ctx, ref, req, banPort); err == nil || writes != 5 {
			t.Fatal("epoch change escaped the ban-removal fence")
		}
		exec(`UPDATE accounts SET epoch=1 WHERE id=$1`, account)
		client.workspace = "wsl"
		for _, mode := range []string{"package", "native_debug"} {
			input["admission_mode"] = mode
			expect(t, client, "POST", "/test-windows", input, 400)
		}
		client.workspace = "lajiaoliao"
		if err = db.Platform.QueryRow(ctx, `SELECT NOT management_enabled AND NOT ai_enabled
			AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1)
			FROM group_bindings WHERE group_id=$2`, tenant, group).Scan(&untouched); err != nil || !untouched {
			t.Fatal("ban tools activated production", err)
		}
	})
}

func TestChiliMessageReadMatchesExactAccountConversationProjection(t *testing.T) {
	source := &lajiaoliao.ReadMessage{ID: "message", Sender: "person", Recipient: "group", Group: "group", SessionType: 3}
	selected := gateway.DecodedMessage{Platform: "lajiaoliao", NativeSender: "person", NativeTarget: "group",
		TransportPeer: "person", TransportTo: "group"}
	selected.Message.ClientID, selected.Message.Session = "message", 2
	if !chiliMessageMatches(selected, source) {
		t.Fatal("verified read did not match")
	}
	for _, edit := range []func(*lajiaoliao.ReadMessage){
		func(m *lajiaoliao.ReadMessage) { m.Group = "other" },
		func(m *lajiaoliao.ReadMessage) { m.ID = "other" },
		func(m *lajiaoliao.ReadMessage) { m.Sender = "other" },
		func(m *lajiaoliao.ReadMessage) { m.Recipient = "other" },
		func(m *lajiaoliao.ReadMessage) { m.SessionType = 1 },
	} {
		copy := *source
		edit(&copy)
		if chiliMessageMatches(selected, &copy) {
			t.Fatal("source escaped the selected projection")
		}
	}
}

func TestPostgresChiliMessageReadLeavesHistoryUntouchedAndScoped(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{74}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	actor, account := control.NewID(), control.NewID()
	token, _ := security.Token()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'chili-reader','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO accounts(id,name,workspace,platform)
		VALUES($1,'chili-reader','lajiaoliao','lajiaoliao')`, account)
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(server.URL)
	jar.SetCookies(origin, []*http.Cookie{{Name: "wsl_session", Value: token}})
	client := testClient{client: &http.Client{Jar: jar}, base: server.URL, workspace: "lajiaoliao"}
	key := strings.Repeat("d", 64)
	for _, sample := range []struct {
		group  string
		notice bool
	}{{"group-string", false}, {"foreign-group", false}, {"group-string", true}} {
		content := []byte(`{"text":"native mention","atUserList":["target-string"],"atUsersInfo":[],"isAtSelf":false}`)
		from, kind, eventKey := int32(100), int32(106), sample.group
		if sample.notice {
			from, kind, eventKey = 200, 1508, "notice-event"
			detail := `{"group":{"groupID":"group-string"},"opUser":{"userID":"operator-string"},` +
				`"kickedUserList":[{"groupID":"group-string","userID":"target-string","nickname":"fixture"}]}`
			content, _ = json.Marshal(map[string]string{"detail": detail})
		}
		raw, _ := proto.Marshal(&sdkws.MsgData{SendID: "sender", RecvID: "group-string", GroupID: sample.group,
			ClientMsgID: "message", SessionType: 3, MsgFrom: from, ContentType: kind, Content: content})
		sealed, _ := vault.Seal("native-event:"+account+":"+eventKey, raw)
		var event int64
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,payload_sealed,received_at)
			VALUES($1,$2,'group',$3,now()) RETURNING id`, account, eventKey, sealed).Scan(&event); err != nil {
			t.Fatal(err)
		}
		selected := gateway.DecodedMessage{Platform: "lajiaoliao", NativeSender: "sender", NativeTarget: "group-string",
			TransportPeer: "sender", TransportTo: "group-string"}
		selected.Message.ClientID, selected.Message.Session = "message", 2
		plain, _ := json.Marshal(selected)
		sealed, _ = vault.Seal(gateway.DecodedAAD(account, event), plain)
		exec(`INSERT INTO native_decoded_events(event_id,account_id,state,reason,scene,conversation_key,
			deployment_version,payload_sealed,received_at)
			VALUES($1,$2,'unsupported','lajiaoliao_content_type','group',$3,0,$4,now())`, event, account, key, sealed)
	}
	body := expect(t, client, "GET", "/accounts/"+account+"/conversations/"+key+"/messages", nil, 200)
	var output struct {
		Items []struct {
			State string                  `json:"state"`
			View  *lajiaoliao.MessageView `json:"native_view"`
		} `json:"items"`
	}
	if json.Unmarshal(body, &output) != nil || len(output.Items) != 3 || output.Items[1].View != nil ||
		output.Items[2].View == nil || output.Items[2].View.Mentions[0].ID != "target-string" ||
		output.Items[0].View == nil || output.Items[0].View.Notice == nil ||
		output.Items[0].View.Notice.Targets[0].ID != "target-string" {
		t.Fatal("display read lost native IDs or crossed original group", string(body))
	}
	var untouched bool
	if err := db.Platform.QueryRow(ctx, `SELECT bool_and(state='unsupported' AND reason='lajiaoliao_content_type')
		FROM native_decoded_events WHERE account_id=$1`, account).Scan(&untouched); err != nil || !untouched {
		t.Fatal("read changed historical decoder state", err)
	}
	client.workspace = "wsl"
	expect(t, client, "GET", "/accounts/"+account+"/conversations/"+key+"/messages", nil, 404)
}
