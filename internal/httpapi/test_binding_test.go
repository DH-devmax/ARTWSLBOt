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
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

func TestPostgresControlledTestBinding(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			testControlledTestBinding(t, platform)
		})
	}
}

func testControlledTestBinding(t *testing.T, platform string) {
	db := integrationDB(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	actor, owner, tenant, account, group := control.NewID(), control.NewID(), control.NewID(), control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'test-binding-admin','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
        VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO tenants(id,name) VALUES($1,'synthetic test tenant')`, tenant)
	exec(`INSERT INTO users(id,tenant_id,username,password_hash,role,tenant_role)
        VALUES($1,$2,'test-binding-owner','synthetic','user','owner')`, owner, tenant)
	exec(`INSERT INTO accounts(id,name,platform,workspace,purpose,native_uid,auth_state,credential_sealed,testing_enabled,connection_enabled)
        VALUES($1,'synthetic binding account',$2,$2,'management','synthetic-self','authenticated','\x01',true,true)`, account, platform)
	snapshot, _ := json.Marshal(map[string]any{"state": "connected", "synced_at": time.Now(), "heartbeat_at": time.Now()})
	exec(`INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
        VALUES($1,true,'synthetic-owner',now()+interval '1 minute',now(),$2)`, account, snapshot)
	exec(`INSERT INTO native_groups(id,platform,native_id,name) VALUES($1,$2,'synthetic-group','synthetic group')`, group, platform)
	exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
        VALUES($1,$2,'owner',now(),'native')`, account, group)
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{84}, 32)))
	if platform == "lajiaoliao" {
		service := &control.NativeSettingsService{DB: db, Vault: vault}
		proof := control.SettingEvidence{UID: "synthetic-self", Role: "owner", Epoch: 1, Version: 1,
			CredentialDigest: sha256.Sum256([]byte{1}), ObservedAt: time.Now()}
		observed, err := service.RecordChiliRole(ctx, ref, account, "synthetic-group", "synthetic native name", proof)
		if err != nil || observed.GroupID != group {
			t.Fatal("native Chili group label lost its role scope", err)
		}
		var name string
		if err := db.Platform.QueryRow(ctx, `SELECT name FROM native_groups WHERE id=$1`, group).Scan(&name); err != nil ||
			name != "synthetic native name" {
			t.Fatal("Chili native group name was replaced by its identifier", err)
		}
	}
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(server.URL)
	jar.SetCookies(origin, []*http.Cookie{{Name: "wsl_session", Value: token}, {Name: "wsl_csrf", Value: "csrf"}})
	admin := testClient{client: &http.Client{Jar: jar}, base: server.URL, workspace: platform}
	path := "/groups/" + group + "/test-binding"
	body := map[string]any{"account_id": account, "tenant_id": tenant, "account_version": 1,
		"confirm_isolated": true, "identity_verified": true}
	expect(t, admin, "POST", path, body, 409)
	exec(`UPDATE tenants SET owner_id=$2 WHERE id=$1`, tenant, owner)
	body["identity_verified"] = false
	expect(t, admin, "POST", path, body, 400)
	body["identity_verified"], body["confirm_isolated"] = true, false
	expect(t, admin, "POST", path, body, 400)
	body["confirm_isolated"], body["account_version"] = true, 2
	expect(t, admin, "POST", path, body, 409)
	body["account_version"] = 1
	for _, change := range []struct{ before, after string }{
		{`UPDATE accounts SET testing_enabled=false`, `UPDATE accounts SET testing_enabled=true`},
		{`UPDATE accounts SET connection_enabled=false`, `UPDATE accounts SET connection_enabled=true`},
		{`UPDATE account_receivers SET enabled=false`, `UPDATE account_receivers SET enabled=true`},
		{`UPDATE account_receivers SET lease_until=now()`, `UPDATE account_receivers SET lease_until=now()+interval '1 minute'`},
		{`UPDATE account_receivers SET observed_at=now()-interval '16 seconds'`, `UPDATE account_receivers SET observed_at=now()`},
		{`UPDATE account_groups SET observation_source='replay'`, `UPDATE account_groups SET observation_source='native'`},
		{`UPDATE account_groups SET role='member'`, `UPDATE account_groups SET role='owner'`},
		{`UPDATE account_groups SET observed_at=now()-interval '61 seconds'`, `UPDATE account_groups SET observed_at=now()`},
	} {
		exec(change.before)
		expect(t, admin, "POST", path, body, 409)
		exec(change.after)
	}
	code := security.Digest("synthetic-test-binding-code")
	exec(`INSERT INTO claim_codes(code_hash,group_id,account_id,account_epoch,expires_at)
        VALUES($1,$2,$3,1,now()+interval '1 minute')`, code, group, account)
	expect(t, admin, "POST", path, body, 409)
	exec(`UPDATE claim_codes SET consumed_at=now()`)
	exec(`INSERT INTO group_claims(tenant_id,group_id,account_id,applicant_id,native_identity)
        VALUES($1,$2,$3,$4,'synthetic')`, tenant, group, account, owner)
	expect(t, admin, "POST", path, body, 409)
	exec(`UPDATE group_claims SET state='rejected'`)
	other := admin
	other.workspace = "lajiaoliao"
	if platform == "lajiaoliao" {
		other.workspace = "wsl"
	}
	expect(t, other, "POST", path, body, 404)
	wrong := control.NewID()
	exec(`INSERT INTO accounts(id,name,platform,workspace,auth_state,credential_sealed,testing_enabled,connection_enabled)
        VALUES($1,'synthetic other account',$2,$2,'authenticated','\x01',true,true)`, wrong, other.workspace)
	exec(`INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
        VALUES($1,true,'synthetic-owner',now()+interval '1 minute',now(),$2)`, wrong, snapshot)
	body["account_id"] = wrong
	expect(t, admin, "POST", path, body, 409)
	body["account_id"] = account
	userToken, _ := security.Token()
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
        VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(userToken), security.Digest("csrf"), owner)
	userJar, _ := cookiejar.New(nil)
	userJar.SetCookies(origin, []*http.Cookie{{Name: "wsl_session", Value: userToken}, {Name: "wsl_csrf", Value: "csrf"}})
	user := testClient{client: &http.Client{Jar: userJar}, base: server.URL, workspace: platform}
	expect(t, user, "POST", path, body, 403)
	expect(t, admin, "POST", path, body, 201)
	expect(t, admin, "POST", path, body, 409)
	var safe bool
	err := db.Platform.QueryRow(ctx, `SELECT
        EXISTS(SELECT 1 FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
            WHERE b.group_id=$1 AND b.tenant_id=$2 AND b.primary_account_id=$3 AND b.active
            AND NOT b.management_enabled AND NOT b.ai_enabled AND b.config='{}' AND g.isolated_test)
        AND EXISTS(SELECT 1 FROM audit_events WHERE actor_id=$4 AND tenant_id=$2 AND resource_id=$1::text
            AND action='isolated_test_binding_authorized' AND detail->>'website_owner_id'=$5)
        AND NOT EXISTS(SELECT 1 FROM subscriptions)
        AND NOT EXISTS(SELECT 1 FROM operations)
        AND NOT EXISTS(SELECT 1 FROM test_windows)`, group, tenant, account, actor, owner).Scan(&safe)
	if err != nil || !safe {
		t.Fatal("test binding must audit the explicit owner without activating execution", err)
	}
	window := map[string]any{"group_id": group, "account_ids": []string{account}, "target_ids": []string{"synthetic-target"},
		"actions": []string{"grant_admin", "revoke_admin"}, "seconds": 300, "budget": 2, "admission_mode": "package"}
	expect(t, admin, "POST", "/test-windows", window, 409)
	window["admission_mode"] = "native_debug"
	expect(t, admin, "POST", "/test-windows", window, 201)
	next := control.NewID()
	exec(`INSERT INTO native_groups(id,platform,native_id,name) VALUES($1,$2,'synthetic-next','synthetic next')`, next, platform)
	exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
        VALUES($1,$2,'owner',now(),'native')`, account, next)
	exec(`WITH added AS (
        INSERT INTO native_groups(platform,native_id,name)
        SELECT $1,'synthetic-cap-'||i,'synthetic cap' FROM generate_series(1,29) i RETURNING id)
        INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) SELECT id,$2,$3 FROM added`,
		platform, tenant, account)
	expect(t, admin, "POST", "/groups/"+next+"/test-binding", body, 409)
	exec(`UPDATE sessions SET expires_at=now() WHERE token_hash=$1`, security.Digest(token))
	if err := control.AuthorizeTestBinding(ctx, db.Platform, ref, control.TestBindingRequest{Account: account,
		Group: next, Tenant: tenant, Platform: platform, Version: 1, ConfirmIsolated: true, IdentityVerified: true}); err == nil {
		t.Fatal("revoked administrative session authorized a test binding")
	}
}
