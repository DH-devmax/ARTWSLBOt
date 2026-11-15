package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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

func TestPostgresNativeDebugEditorsKeepSettingsFences(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{72}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	actor, account, group, tenant := control.NewID(), control.NewID(), control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	credential := []byte("synthetic-editor-session")
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'editor-owner','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
        VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO accounts(id,name,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
        VALUES($1,'editor-account','7','authenticated',$2,true,true)`, account, credential)
	exec(`INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,'\x01',77)`)
	exec(`INSERT INTO tenants(id,name) VALUES($1,'editor-tenant')`, tenant)
	exec(`INSERT INTO native_groups(id,native_id,name,isolated_test) VALUES($1,'123','editor-group',true)`, group)
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
	client := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	input := map[string]any{"group_id": group, "account_ids": []string{account}, "target_ids": []string{"7", "8"},
		"actions": []string{"group_info", "member_nickname"}, "admission_mode": "native_debug", "seconds": 300, "budget": 4}
	window := field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
	req := control.SettingRequest{Account: account, Workspace: "wsl", TestWindowID: window,
		Epoch: 1, Version: 1, Key: control.NewID(), Digest: strings.Repeat("a", 64),
		Change: nativeauth.SettingChange{Kind: "member_nickname", Group: "123", Target: "7", Text: "fixture"}}
	proof := control.SettingEvidence{UID: "7", Epoch: 1, Version: 1, DeploymentVersion: 77,
		Role: "owner", TargetRole: "owner", Target: "7", Digest: req.Digest,
		CredentialDigest: sha256.Sum256(credential), ObservedAt: time.Now()}
	submissions := 0
	port := func(work context.Context, fence control.SettingFence) error {
		proof.ObservedAt = time.Now()
		return fence(work, proof, func(context.Context) error { submissions++; return nil })
	}
	if _, err := service.Execute(ctx, ref, req, port); err == nil {
		t.Fatal("generic settings endpoint inherited test admission")
	}
	proof.Locked = true
	out, err := service.ExecuteTestSetting(ctx, ref, req, port)
	if err != nil || out.Status != "failed" || submissions != 0 {
		t.Fatal("debug bypassed the native nickname lock", out, err)
	}
	proof.Locked, req.Key = false, control.NewID()
	out, err = service.ExecuteTestSetting(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || submissions != 1 {
		t.Fatal("self card incorrectly used moderation self protection", out, err)
	}
	again, err := service.ExecuteTestSetting(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submissions != 1 {
		t.Fatal("same-key card recovery replayed a submission", again, err)
	}
	req.Key, req.Change.Target, proof.Target, proof.TargetRole = control.NewID(), "8", "8", "member"
	out, err = service.ExecuteTestSetting(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	})
	if err != nil || out.Status != "unknown" || submissions != 2 {
		t.Fatal("unknown other-card result was lost", out, err)
	}
	exec(`UPDATE test_windows SET cancelled=true`)
	recovered, err := service.Reconcile(ctx, ref, account, out.ID,
		func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
			proof.Matches, proof.ObservedAt = true, time.Now()
			return proof, nil
		})
	if err != nil || recovered.Status != "succeeded" || submissions != 2 {
		t.Fatal("closed card grant could not reconcile without replay", recovered, err)
	}
	req.Key = control.NewID()
	if _, err := service.ExecuteTestSetting(ctx, ref, req, port); err == nil || submissions != 2 {
		t.Fatal("cancelled grant submitted another card")
	}
	input["actions"], input["budget"] = []string{"group_info"}, 1
	window = field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
	req.TestWindowID, req.Key = window, control.NewID()
	req.Change = nativeauth.SettingChange{Kind: "group_info", Group: "123", Text: "fixture-group"}
	proof.Target, proof.TargetRole, proof.ModerationAllowed = "", "", true
	out, err = service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || submissions != 3 {
		t.Fatal("reviewed group-name debug action still rejected", out, err)
	}
	history, err := service.TestOperations(ctx, ref, account)
	if err != nil || len(history) != 4 {
		t.Fatal("editor operations missing from scoped durable history", len(history), err)
	}
	req.TestWindowID, req.Key = "", control.NewID()
	req.Change = nativeauth.SettingChange{Kind: "member_nickname", Group: "123", Target: "8", Text: "fixture"}
	if _, err := service.ExecuteTestSetting(ctx, ref, req, port); err == nil || submissions != 3 {
		t.Fatal("ungrounded editor escaped into production")
	}
	var untouched bool
	if err := db.Platform.QueryRow(ctx, `SELECT NOT management_enabled AND NOT ai_enabled
        AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1)
        FROM group_bindings WHERE group_id=$2`, tenant, group).Scan(&untouched); err != nil || !untouched {
		t.Fatal("test editors activated production", err)
	}
	t.Run("private_debug_grant_remains_platform_scoped_and_independent_of_package", func(t *testing.T) {
		input["actions"], input["budget"] = []string{"send_private_text", "send_private_image", "upload_private_image"}, 3
		client.workspace = "lajiaoliao"
		expect(t, client, "POST", "/test-windows", input, 409)
		client.workspace = "wsl"
		privateWindow := field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
		scope, err := service.TestScope(ctx, ref, "wsl", privateWindow, account, "send_private_text", "8")
		if err != nil || scope.AdmissionMode != "native_debug" || scope.Remaining != 3 {
			t.Fatal("private grant still required production activation", scope, err)
		}
		if _, err := service.TestScope(ctx, ref, "wsl", privateWindow, account, "send_private_text", "9"); err == nil {
			t.Fatal("private grant admitted an unlisted recipient")
		}
		input["admission_mode"] = "package"
		expect(t, client, "POST", "/test-windows", input, 409)
		if err := db.Platform.QueryRow(ctx, `SELECT NOT management_enabled AND NOT ai_enabled
			AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1)
			FROM group_bindings WHERE group_id=$2`, tenant, group).Scan(&untouched); err != nil || !untouched {
			t.Fatal("private debug grant changed production configuration", err)
		}
	})
}

func TestPostgresNativeDebugRolesRemainIsolatedFromProduction(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{71}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	actor, account, group, tenant := control.NewID(), control.NewID(), control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	credential := []byte("synthetic-debug-session")
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'debug-owner','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
        VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO accounts(id,name,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
        VALUES($1,'debug-account','7','authenticated',$2,true,true)`, account, credential)
	exec(`INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,'\x01',77)`)
	exec(`INSERT INTO tenants(id,name) VALUES($1,'debug-tenant')`, tenant)
	exec(`INSERT INTO native_groups(id,native_id,name,isolated_test) VALUES($1,'123','debug-group',true)`, group)
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
	client := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	input := map[string]any{"group_id": group, "account_ids": []string{account}, "target_ids": []string{"8"},
		"actions": []string{"grant_admin", "revoke_admin"}, "admission_mode": "package", "seconds": 300, "budget": 3}
	expect(t, client, "POST", "/test-windows", input, 409)
	input["admission_mode"] = "unexpected"
	expect(t, client, "POST", "/test-windows", input, 400)
	input["admission_mode"], input["actions"] = "native_debug", []string{"send_file"}
	expect(t, client, "POST", "/test-windows", input, 400)
	input["actions"] = []string{"grant_admin", "revoke_admin"}
	exec(`UPDATE account_groups SET role='admin'`)
	expect(t, client, "POST", "/test-windows", input, 409)
	exec(`UPDATE account_groups SET role='owner'`)
	window := field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
	scope, err := service.TestScope(ctx, ref, "wsl", window, account, "grant_admin", "8")
	if err != nil || scope.AdmissionMode != "native_debug" || scope.GroupID != group {
		t.Fatal("explicit debug admission still required enabling production", scope, err)
	}
	req := control.SettingRequest{Account: account, Workspace: "wsl", TestWindowID: window,
		Epoch: 1, Version: 1, Key: control.NewID(), Digest: strings.Repeat("a", 64),
		Change: nativeauth.SettingChange{Kind: "grant_admin", Group: "123", Target: "8"}}
	role, targetRole := "owner", "member"
	proof := func() control.SettingEvidence {
		return control.SettingEvidence{UID: "7", Epoch: 1, Version: 1, DeploymentVersion: 77,
			Role: role, TargetRole: targetRole, Target: req.Change.Target, Digest: req.Digest,
			CredentialDigest: sha256.Sum256(credential), ModerationAllowed: true, ObservedAt: time.Now()}
	}
	submissions := 0
	port := func(work context.Context, fence control.SettingFence) error {
		return fence(work, proof(), func(context.Context) error { submissions++; return nil })
	}
	// Final proof, not the creation-time owner observation, controls the write.
	role = "admin"
	out, err := service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || out.Status != "failed" || submissions != 0 {
		t.Fatal("administrator impersonated the native owner", out, err)
	}
	role, req.Key = "owner", control.NewID()
	out, err = service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || submissions != 1 {
		t.Fatal("debug grant required a commercial activation", out, err)
	}
	again, err := service.ExecuteModeration(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submissions != 1 {
		t.Fatal("role grant repeated after same-key recovery", again, err)
	}
	req.Key, req.Change.Kind, targetRole = control.NewID(), "revoke_admin", "admin"
	out, err = service.ExecuteModeration(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	})
	if err != nil || out.Status != "unknown" || submissions != 2 {
		t.Fatal("ambiguous revoke result lost", out, err)
	}
	exec(`UPDATE test_windows SET cancelled=true`)
	targetRole = "member"
	recovered, err := service.Reconcile(ctx, ref, account, out.ID,
		func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
			p := proof()
			p.Matches = true
			return p, nil
		})
	if err != nil || recovered.Status != "succeeded" || recovered.Code != "desired_state_observed" || submissions != 2 {
		t.Fatal("closed grant could not reconcile without replaying revoke", recovered, err)
	}
	if history, err := service.TestOperations(ctx, ref, account); err != nil || len(history) != 3 {
		t.Fatal("role operations missing from durable debug history", len(history), err)
	}
	// A new scoped grant still cannot cross binding/account/deadline boundaries.
	input["budget"] = 1
	window = field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
	req.TestWindowID, req.Key = window, control.NewID()
	for _, pair := range [][2]string{
		{`UPDATE group_bindings SET active=false`, `UPDATE group_bindings SET active=true`},
		{`UPDATE accounts SET epoch=2`, `UPDATE accounts SET epoch=1`},
		{`UPDATE accounts SET testing_enabled=false`, `UPDATE accounts SET testing_enabled=true`},
		{`UPDATE native_groups SET isolated_test=false`, `UPDATE native_groups SET isolated_test=true`},
		{`UPDATE test_windows SET expires_at=now() WHERE NOT cancelled`,
			`UPDATE test_windows SET created_at=now(),expires_at=now()+interval '300 seconds' WHERE NOT cancelled`},
	} {
		exec(pair[0])
		if _, err := service.ExecuteModeration(ctx, ref, req, port); err == nil || submissions != 2 {
			t.Fatal("debug mode bypassed final scope validation", pair[0], err)
		}
		exec(pair[1])
	}
	req.TestWindowID = ""
	if _, err := service.ExecuteModeration(ctx, ref, req, port); err == nil {
		t.Fatal("debug mode leaked into an ungranted production operation")
	}
	var untouched bool
	if err := db.Platform.QueryRow(ctx, `SELECT NOT management_enabled AND NOT ai_enabled
        AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1)
        FROM group_bindings WHERE group_id=$2`, tenant, group).Scan(&untouched); err != nil || !untouched {
		t.Fatal("debug mode changed a production subscription or switch", err)
	}
	exec(`UPDATE users SET role='user',tenant_id=$2 WHERE id=$1`, actor, tenant)
	expect(t, client, "POST", "/test-windows", input, 403)
	if _, err := service.TestScope(ctx, ref, "wsl", window, account, "", ""); err == nil {
		t.Fatal("tenant inherited superadmin debugging")
	}
}
