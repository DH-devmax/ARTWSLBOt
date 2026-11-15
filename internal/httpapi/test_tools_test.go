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

	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresToolsGrantBudgetRevocationAndRecovery(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{53}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	account, actor, tenant, plan, group, window := control.NewID(), control.NewID(), control.NewID(), control.NewID(), control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'tool-owner','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
		security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO accounts(id,name,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
        VALUES($1,'tool-account','7','authenticated',$2,true,true)`, account, []byte("synthetic-tool-session"))
	exec(`INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,77)`, []byte("synthetic-deployment"))
	exec(`INSERT INTO tenants(id,name) VALUES($1,'tool-tenant')`, tenant)
	exec(`INSERT INTO plans(id,name,group_limit,ai_group_limit,modules) VALUES($1,'tool-plan',10,0,ARRAY['moderation'])`, plan)
	exec(`INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, tenant, plan)
	exec(`INSERT INTO native_groups(id,native_id,name,isolated_test) VALUES($1,'123','tool-group',true)`, group)
	exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,active,management_enabled)
        VALUES($1,$2,$3,true,true)`, group, tenant, account)
	exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
        VALUES($1,$2,'admin',now(),'native')`, account, group)
	epochs, _ := json.Marshal(map[string]int{account: 1})
	exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget)
        VALUES($1,$2,$3,ARRAY[$4::uuid],ARRAY['8'],ARRAY['mute_member','unmute_member','kick_member'],$5,now()+interval '300 seconds',3)`,
		window, actor, group, account, epochs)
	req := control.SettingRequest{Account: account, Workspace: "wsl", Epoch: 1, Version: 1, TestWindowID: window,
		Key: control.NewID(), Digest: strings.Repeat("a", 64), Change: nativeauth.SettingChange{Kind: "mute_member", Group: "123", Target: "8", Minutes: 2}}
	proof := func() control.SettingEvidence {
		return control.SettingEvidence{UID: "7", Epoch: 1, Version: 1, DeploymentVersion: 77,
			Role: "admin", Target: req.Change.Target, TargetRole: "member", Digest: req.Digest, ModerationAllowed: true,
			CredentialDigest: sha256.Sum256([]byte("synthetic-tool-session")), ObservedAt: time.Now()}
	}
	submissions := 0
	port := func(work context.Context, fence control.SettingFence) error {
		return fence(work, proof(), func(context.Context) error { submissions++; return nil })
	}
	for _, target := range []string{"", "9"} {
		if _, err := service.TestScope(ctx, ref, "wsl", window, account, "mute_member", target); target != "" && err == nil {
			t.Fatal("target escaped grant")
		}
	}
	for _, pair := range [][2]string{
		{`UPDATE test_windows SET cancelled=true`, `UPDATE test_windows SET cancelled=false`},
		{`UPDATE plans SET modules=ARRAY['friends'] WHERE id NOT IN (SELECT plan_id FROM commercial_plans)`, `UPDATE plans SET modules=ARRAY['moderation'] WHERE id NOT IN (SELECT plan_id FROM commercial_plans)`},
		{`UPDATE accounts SET epoch=2`, `UPDATE accounts SET epoch=1`},
		{`UPDATE accounts SET testing_enabled=false`, `UPDATE accounts SET testing_enabled=true`},
		{`UPDATE test_windows SET expires_at=now()`, `UPDATE test_windows SET created_at=now(),expires_at=now()+interval '300 seconds'`},
	} {
		exec(pair[0])
		if _, err := service.ExecuteModeration(ctx, ref, req, port); err == nil || submissions != 0 {
			t.Fatal("revoked grant admitted submission", pair[0], err)
		}
		exec(pair[1])
	}
	out, err := service.ExecuteModeration(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		exec(`UPDATE test_windows SET cancelled=true`)
		return port(work, fence)
	})
	if err != nil || out.Status != "failed" || submissions != 0 {
		t.Fatal("cancellation after preparation bypassed fence", out, err)
	}
	exec(`UPDATE test_windows SET cancelled=false`)
	req.Key = control.NewID()
	out, err = service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || out.Code != "native_moderation_acknowledged" || submissions != 1 {
		t.Fatal(out, err)
	}
	again, err := service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submissions != 1 {
		t.Fatal("idempotent call repeated native mutation", again, err)
	}
	req.Key = control.NewID()
	out, err = service.ExecuteModeration(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	})
	if err != nil || out.Status != "unknown" || submissions != 2 {
		t.Fatal(out, err)
	}
	exec(`UPDATE test_windows SET cancelled=true,created_at=now()-interval '301 seconds',expires_at=now()-interval '1 second'`)
	recovered, found, err := service.InspectTestTool(ctx, ref, account, req.Key, window, req.Change)
	if err != nil || !found || recovered.ID != out.ID || submissions != 2 {
		t.Fatal("expired budget prevented read-only original-operation recovery", recovered, err)
	}
	changed := req.Change
	changed.Target = "9"
	if _, _, err := service.InspectTestTool(ctx, ref, account, req.Key, window, changed); err == nil {
		t.Fatal("same key changed immutable target")
	}
	observed, err := service.Reconcile(ctx, ref, account, out.ID, func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
		p := proof()
		p.Matches = false // Native mute list cannot prove the requested duration.
		return p, nil
	})
	if err != nil || observed.Status != "unknown" || submissions != 2 {
		t.Fatal("readback falsely succeeded or resubmitted", observed, err)
	}
	var used int
	if err := db.Platform.QueryRow(ctx, `SELECT used FROM test_windows WHERE id=$1`, window).Scan(&used); err != nil || used != 3 {
		t.Fatal("reservation refunded or duplicate charged", used, err)
	}
	exec(`UPDATE test_windows SET cancelled=false,created_at=now(),expires_at=now()+interval '300 seconds'`)
	req.Key = control.NewID()
	if _, err := service.ExecuteModeration(ctx, ref, req, port); err == nil || submissions != 2 {
		t.Fatal("exhausted grant accepted another action")
	}
	t.Run("history survives closed grants and isolates actor account platform", func(t *testing.T) {
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`UPDATE test_windows SET cancelled=true`)
		otherActor, otherAccount := control.NewID(), control.NewID()
		exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'other-tool-owner','synthetic','superadmin')`, otherActor)
		exec(`INSERT INTO accounts(id,name) VALUES($1,'other-tool-account')`, otherAccount)
		for i := 0; i < 51; i++ {
			exec(`INSERT INTO native_setting_operations(actor_id,account_id,group_id,test_window_id,kind,
                idempotency_key,request_hash,payload_sealed,account_epoch,status,expires_at)
                VALUES($1,$2,$3,$4,'mute_member',$5,'\x01','\x736563726574',1,'succeeded',now())`,
				actor, account, group, window, control.NewID())
		}
		// Another account's operation and an actor/window mismatch are never delivered.
		exec(`INSERT INTO native_setting_operations(actor_id,account_id,group_id,test_window_id,kind,
            idempotency_key,request_hash,account_epoch,status,expires_at) VALUES
            ($1,$2,$3,$4,'mute_member',$5,'\x01',1,'unknown',now()),
            ($6,$7,$3,$4,'mute_member',$8,'\x01',1,'unknown',now()),
            ($6,$2,$3,$4,'mute_member',$9,'\x01',1,'unknown',now())`,
			actor, otherAccount, group, window, control.NewID(), otherActor, account, control.NewID(), control.NewID())
		s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
		defer s.Native.Close()
		server := httptest.NewServer(s.Handler())
		defer server.Close()
		jar, _ := cookiejar.New(nil)
		origin, _ := url.Parse(server.URL)
		jar.SetCookies(origin, []*http.Cookie{{Name: "wsl_session", Value: token}})
		client := testClient{client: &http.Client{Jar: jar}, base: server.URL}
		path := "/test-tools/operations?account_id=" + account
		raw := expect(t, client, "GET", path, nil, 200)
		var records []control.TestOperation
		if err := json.Unmarshal(raw, &records); err != nil || len(records) != 50 ||
			records[0].ID != out.ID || records[0].Status != "unknown" {
			t.Fatal("unresolved operation was hidden by history limit", len(records), err)
		}
		for _, field := range []string{"payload", "request_hash", "idempotency_key", "target_id", "secret"} {
			if strings.Contains(string(raw), field) {
				t.Fatal("history exposed private request fields", field)
			}
		}
		expect(t, client, "GET", "/test-tools/operations", nil, 400)
		chili := client
		chili.workspace = "lajiaoliao"
		expect(t, chili, "GET", path, nil, 404)
		chili.workspace = "maibot"
		expect(t, chili, "GET", path, nil, 403)
		platformAccount := control.NewID()
		exec(`INSERT INTO accounts(id,name,workspace,platform) VALUES($1,'chili-tool-account','lajiaoliao','lajiaoliao')`, platformAccount)
		expect(t, client, "GET", "/test-tools/operations?account_id="+platformAccount, nil, 404)
		otherToken, _ := security.Token()
		otherRef, _ := control.WebsiteSession(otherToken)
		exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
			security.Digest(otherToken), security.Digest("csrf"), otherActor)
		if rows, err := service.TestOperations(ctx, otherRef, account); err != nil || len(rows) != 0 {
			t.Fatal("another actor inherited a window or its operations", rows, err)
		}
		exec(`UPDATE users SET role='user',tenant_id=$2 WHERE id=$1`, otherActor, tenant)
		if _, err := service.TestOperations(ctx, otherRef, account); err == nil {
			t.Fatal("non-superadmin read test operations")
		}
		exec(`DELETE FROM sessions WHERE user_id=$1`, actor)
		expect(t, client, "GET", path, nil, 401)
		if _, err := service.TestOperations(ctx, ref, account); err == nil {
			t.Fatal("revoked session read test operations")
		}
		if submissions != 2 {
			t.Fatal("history issued a native mutation")
		}
	})
}
