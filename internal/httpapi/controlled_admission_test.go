package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/security"
)

func TestPostgresControlledAdmission(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			testControlledAdmissionForPlatform(t, platform)
		})
	}
}

func testControlledAdmissionForPlatform(t *testing.T, platform string) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-admission-password"
	hash, _ := security.Password(password)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('admission-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{27}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar}, base: server.URL, workspace: platform}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "admission-admin", "password": password}, 200)
	account := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "admission-panel"}, 201), "id")
	path := "/accounts/" + account
	enable := func(version int) map[string]any {
		return map[string]any{"version": version, "connection_enabled": true}
	}
	expect(t, admin, "PATCH", path, enable(1), 409)
	expect(t, admin, "PATCH", path, map[string]any{"version": 1, "testing_enabled": true}, 200)
	expect(t, admin, "PATCH", path, enable(2), 409)
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET auth_state='authenticated',
        credential_sealed='\x0102' WHERE id=$1`, account); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "PATCH", path, enable(2), 409)
	snapshot, _ := json.Marshal(map[string]any{"state": "connected", "synced_at": time.Now(), "heartbeat_at": time.Now()})
	if _, err := db.Platform.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
        VALUES($1,true,'synthetic-owner',now()+interval '1 minute',now(),$2)`, account, snapshot); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "PATCH", path, enable(1), 409)
	for _, change := range []string{
		`UPDATE account_receivers SET observed_at=now()-interval '16 seconds'`,
		`UPDATE account_receivers SET observed_at=now(),lease_until=now()`,
		`UPDATE account_receivers SET lease_until=now()+interval '1 minute',snapshot=jsonb_set(snapshot,'{state}','"blocked"')`,
		`UPDATE account_receivers SET snapshot=jsonb_set(jsonb_set(snapshot,'{state}','"connected"'),'{heartbeat_at}',to_jsonb(now()-interval '40 seconds'))`,
	} {
		if _, err := db.Platform.Exec(ctx, change); err != nil {
			t.Fatal(err)
		}
		expect(t, admin, "PATCH", path, enable(2), 409)
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE account_receivers SET snapshot=$1,observed_at=now()`, snapshot); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "PATCH", path, map[string]any{"version": 2, "connection_enabled": true, "testing_enabled": false}, 409)
	expect(t, admin, "PATCH", path, enable(2), 200)
	var preserved bool
	if err := db.Platform.QueryRow(ctx, `SELECT connection_enabled AND testing_enabled AND epoch=3 AND version=3
        AND credential_sealed='\x0102' FROM accounts WHERE id=$1`, account).Scan(&preserved); err != nil || !preserved {
		t.Fatal("test admission must preserve the session and fence the old epoch")
	}
	// Tools carry the account in their validated arguments, not in a route {id}.
	req := httptest.NewRequest("POST", "/api/v1/test-tools/call", nil)
	req.Header.Set("X-Workspace", platform)
	u, _ := url.Parse(server.URL)
	for _, cookie := range jar.Cookies(u) {
		req.AddCookie(cookie)
	}
	var actor Principal
	actor.Role = "superadmin"
	if err := db.Platform.QueryRow(ctx, `SELECT id::text FROM users WHERE username='admission-admin'`).Scan(&actor.ID); err != nil {
		t.Fatal(err)
	}
	view := gateway.MemberSnapshot{AccountEpoch: 3}
	if err := s.memberResultAccessForAccount(req, actor, account, view); err != nil {
		t.Fatalf("Tools result lost its explicit account scope: %v", err)
	}
	view.AccountEpoch = 2
	if err := s.memberResultAccessForAccount(req, actor, account, view); err == nil {
		t.Fatal("Tools delivered a stale account snapshot")
	}
	var group string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)
        VALUES($1,'123456','synthetic admission') RETURNING id::text`, platform).Scan(&group); err != nil {
		t.Fatal(err)
	}
	claimPath := "/groups/" + group + "/claim-code"
	body := map[string]string{"account_id": account}
	expect(t, admin, "POST", claimPath, body, 409)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observation_source,observed_at)
        VALUES($1,$2,'owner','replay',now())`, account, group); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", claimPath, body, 409)
	if _, err := db.Platform.Exec(ctx, `UPDATE account_groups SET observation_source='native',observed_at=now()-interval '61 seconds'`); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", claimPath, body, 409)
	if _, err := db.Platform.Exec(ctx, `UPDATE account_groups SET observed_at=now()`); err != nil {
		t.Fatal(err)
	}
	other := admin
	other.workspace = "lajiaoliao"
	if platform == "lajiaoliao" {
		other.workspace = "wsl"
	}
	expect(t, other, "POST", claimPath, body, 404)
	otherAccount := field(t, expect(t, other, "POST", "/accounts", map[string]string{"name": "other-platform"}, 201), "id")
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET testing_enabled=true,connection_enabled=true,
        auth_state='authenticated',credential_sealed='\x0102' WHERE id=$1`, otherAccount); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", claimPath, map[string]string{"account_id": otherAccount}, 409)
	chat := admin
	chat.workspace = "maibot"
	expect(t, chat, "POST", claimPath, body, 403)
	chatAccount := field(t, expect(t, chat, "POST", "/accounts",
		map[string]string{"name": "chat-admission", "platform": platform}, 201), "id")
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET testing_enabled=true,
        auth_state='authenticated',credential_sealed='\x0102' WHERE id=$1`, chatAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
        VALUES($1,true,'synthetic-owner',now()+interval '1 minute',now(),$2)`, chatAccount, snapshot); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", claimPath, map[string]string{"account_id": chatAccount}, 409)
	code := field(t, expect(t, admin, "POST", claimPath, body, 201), "code")
	expect(t, admin, "POST", claimPath, body, 409)
	var safe bool
	if err := db.Platform.QueryRow(ctx, `SELECT
        EXISTS(SELECT 1 FROM claim_codes WHERE code_hash=$1 AND account_epoch=3)
        AND EXISTS(SELECT 1 FROM audit_events WHERE action='claim_code_issued' AND actor_id IS NOT NULL)
        AND NOT EXISTS(SELECT 1 FROM audit_events WHERE detail::text LIKE '%'||$2||'%')
        AND NOT EXISTS(SELECT 1 FROM group_bindings)`, security.Digest(code), code).Scan(&safe); err != nil || !safe {
		t.Fatal("claim issue must hash the code, audit the actor and grant no ownership")
	}
	expect(t, admin, "POST", "/users", map[string]string{"username": "admission-tenant", "password": password, "tenant_name": "admission-tenant"}, 201)
	tenantJar, _ := cookiejar.New(nil)
	tenant := testClient{client: &http.Client{Jar: tenantJar}, base: server.URL, workspace: platform}
	expect(t, tenant, "POST", "/auth/login", map[string]string{"username": "admission-tenant", "password": password}, 200)
	expect(t, tenant, "POST", claimPath, body, 403)
	expect(t, tenant, "PATCH", path, enable(3), 403)
	expect(t, admin, "PATCH", path, map[string]any{"version": 3, "testing_enabled": false}, 200)
	if err := db.Platform.QueryRow(ctx, `SELECT NOT connection_enabled AND NOT testing_enabled AND epoch=4
        FROM accounts WHERE id=$1`, account).Scan(&safe); err != nil || !safe {
		t.Fatal("leaving test mode retained native admission")
	}
	var ref control.SessionRef
	for _, cookie := range jar.Cookies(u) {
		if cookie.Name == "wsl_session" {
			ref, _ = control.WebsiteSession(cookie.Value)
		}
	}
	tx, err := db.Platform.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = control.RequireTestConnectionTx(ctx, tx, ref, chatAccount, 1)
	tx.Rollback(ctx)
	if err == nil {
		t.Fatal("chat account admitted to controlled actions")
	}
	expect(t, admin, "POST", "/auth/logout", map[string]any{}, 200)
	if _, _, err := control.IssueClaimCodeForSession(ctx, db.Platform, ref, account, group); err == nil {
		t.Fatal("revoked website session issued a code")
	}
}
