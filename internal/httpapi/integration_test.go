package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/security"
)

type testClient struct {
	client    *http.Client
	base      string
	workspace string
}

func (c testClient) request(method, path string, body any) (int, json.RawMessage, string) {
	var input io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		input = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+"/api/v1"+path, input)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	if c.workspace != "" {
		req.Header.Set("X-Workspace", c.workspace)
	}
	u, _ := url.Parse(c.base)
	for _, cookie := range c.client.Jar.Cookies(u) {
		if cookie.Name == "wsl_csrf" {
			req.Header.Set("X-CSRF-Token", cookie.Value)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, "transport_error"
	}
	defer resp.Body.Close()
	var envelope struct {
		Data json.RawMessage `json:"data"`
		Msg  string          `json:"msg"`
	}
	json.NewDecoder(resp.Body).Decode(&envelope)
	return resp.StatusCode, envelope.Data, envelope.Msg
}

func expect(t *testing.T, c testClient, method, path string, body any, status int) json.RawMessage {
	t.Helper()
	actual, data, msg := c.request(method, path, body)
	if actual != status {
		t.Fatalf("%s %s: got %d (%s), want %d", method, path, actual, msg, status)
	}
	return data
}

func field(t *testing.T, b json.RawMessage, key string) string {
	t.Helper()
	var data map[string]json.RawMessage
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := json.Unmarshal(data[key], &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// This suite can only reset wsl_test. It never truncates the development database.
func integrationDB(t *testing.T) *database.DB {
	t.Helper()
	if os.Getenv("WSL_INTEGRATION") != "1" {
		t.Skip("set WSL_INTEGRATION=1 with a local .env to run PostgreSQL integration tests")
	}
	data, err := os.ReadFile("../../.env")
	if err != nil {
		t.Fatal("integration .env unavailable")
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && !strings.HasPrefix(k, "#") {
			env[k] = v
		}
	}
	dsn := func(key, db string) string {
		u, err := url.Parse(env[key])
		if err != nil || u.Hostname() != "127.0.0.1" {
			t.Fatal("integration tests require explicit localhost database URLs")
		}
		u.Path = "/" + db
		if os.Getenv("WSL_CONTAINER_TEST") == "1" {
			// A fixed Compose alias only, never a caller-supplied production host.
			u.Host = "postgres:5432"
		}
		return u.String()
	}
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, dsn("MIGRATION_DATABASE_URL", "postgres"))
	if err != nil {
		t.Fatal("cannot connect to integration owner database")
	}
	var exists bool
	err = owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='wsl_test')`).Scan(&exists)
	if err == nil && !exists {
		_, err = owner.Exec(ctx, `CREATE DATABASE wsl_test OWNER wsl_owner`)
	}
	owner.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := pgx.Connect(ctx, dsn("MIGRATION_DATABASE_URL", "wsl_test"))
	if err != nil {
		t.Fatal("cannot connect to isolated test database")
	}
	var name string
	if err = reset.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || name != "wsl_test" {
		t.Fatal("refusing to reset a non-test database")
	}
	if _, err = reset.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public; REVOKE CREATE ON SCHEMA public FROM PUBLIC`); err != nil {
		t.Fatal(err)
	}
	reset.Close(ctx)
	if err = database.Migrate(ctx, dsn("MIGRATION_DATABASE_URL", "wsl_test")); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(ctx, dsn("DATABASE_URL", "wsl_test"), dsn("TENANT_DATABASE_URL", "wsl_test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestPostgresWorkflow(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("isolated-test-password-123")
	var adminID string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('admin',$1,'superadmin') RETURNING id::text`, hash).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	server := httptest.NewServer(New(db, vault, "http://127.0.0.1:5173", false).Handler())
	t.Cleanup(server.Close)
	client := func(username string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{
			"username": username, "password": "isolated-test-password-123",
		}, 200)
		return c
	}
	admin := client("admin")
	expect(t, admin, "GET", "/me", nil, 200)
	u, _ := url.Parse(server.URL)
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/accounts", strings.NewReader(`{"name":"csrf-test"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := admin.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("a cookie alone must not authorize a mutation")
	}
	if len(admin.client.Jar.Cookies(u)) != 2 {
		t.Fatal("session and CSRF cookies missing")
	}
	createUser := func(name string) (testClient, string) {
		b := expect(t, admin, "POST", "/users", map[string]string{
			"username": name, "password": "isolated-test-password-123", "tenant_name": name + "-tenant",
		}, 201)
		return client(name), field(t, b, "tenant_id")
	}
	alice, tenantA := createUser("alice")
	bob, tenantB := createUser("bob")
	plan := field(t, expect(t, admin, "POST", "/plans", map[string]any{
		"name": "test", "group_limit": 2, "ai_group_limit": 1,
		"monthly_ai_tokens": 1000, "modules": []string{"finance", "activities", "moderation"},
	}, 201), "id")
	for _, tenant := range []string{tenantA, tenantB} {
		expect(t, admin, "PUT", "/subscriptions/"+tenant, map[string]any{
			"plan_id": plan, "expires_at": time.Now().Add(time.Hour), "enabled": true, "version": 0,
		}, 200)
	}
	expect(t, alice, "GET", "/users", nil, 403)
	expect(t, alice, "GET", "/plans", nil, 403)
	expect(t, alice, "POST", "/users", map[string]any{}, 403)
	account := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "shared-test"}, 201), "id")
	expect(t, admin, "PATCH", "/accounts/"+account, map[string]any{"version": 1, "connection_enabled": true}, 409)
	// Only isolated SQL fixtures can simulate a trusted native observation.
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET connection_enabled=true,testing_enabled=true WHERE id=$1`, account); err != nil {
		t.Fatal(err)
	}
	newClaim := func(c testClient, label string) (string, string, string) {
		t.Helper()
		var group string
		err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name,isolated_test)
            VALUES($1,$1,true) RETURNING id::text`, label).Scan(&group)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Platform.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
            VALUES($1,$2,'admin',now(),'native')`, account, group)
		if err != nil {
			t.Fatal(err)
		}
		code, _ := security.Token()
		_, err = db.Platform.Exec(ctx, `INSERT INTO claim_codes(code_hash,group_id,account_id,account_epoch,expires_at)
            VALUES($1,$2,$3,1,now()+interval '10 minutes')`, security.Digest(code), group, account)
		if err != nil {
			t.Fatal(err)
		}
		claim := field(t, expect(t, c, "POST", "/claims", map[string]string{
			"code": code, "native_identity": "verified-by-admin-out-of-band",
		}, 201), "id")
		return group, claim, code
	}
	approve := map[string]any{"approve": true, "identity_verified": true, "note": "isolated test evidence"}
	groupA, claimA, codeA := newClaim(alice, "group-a")
	var bound bool
	if err := db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_bindings WHERE group_id=$1)`, groupA).Scan(&bound); err != nil || bound {
		t.Fatal("a submitted code must not confer group ownership")
	}
	expect(t, bob, "POST", "/claims", map[string]string{"code": codeA, "native_identity": "other-user"}, 400)
	expect(t, admin, "POST", "/claims/"+claimA+"/review", map[string]any{"approve": true, "note": "no identity verification"}, 400)
	expect(t, admin, "POST", "/claims/"+claimA+"/review", approve, 200)
	expect(t, admin, "POST", "/claims/"+claimA+"/review", approve, 409)
	groupB, claimB, _ := newClaim(bob, "group-b")
	expect(t, admin, "POST", "/claims/"+claimB+"/review", approve, 200)
	_, second, _ := newClaim(alice, "group-a-2")
	_, third, _ := newClaim(alice, "group-a-3")
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, claim := range []string{second, third} {
		wg.Add(1)
		go func(claim string) {
			defer wg.Done()
			status, _, _ := admin.request("POST", "/claims/"+claim+"/review", approve)
			statuses <- status
		}(claim)
	}
	wg.Wait()
	close(statuses)
	successes, conflicts := 0, 0
	for status := range statuses {
		if status == 200 {
			successes++
		}
		if status == 409 {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("quota race: %d succeeded, %d rejected", successes, conflicts)
	}
	t.Run("RLS and API boundaries", func(t *testing.T) {
		var groups []map[string]any
		b := expect(t, alice, "GET", "/groups?tenant_id="+tenantB, nil, 200)
		if err := json.Unmarshal(b, &groups); err != nil || len(groups) != 2 {
			t.Fatalf("wrong group count: %s", b)
		}
		for _, group := range groups {
			if group["tenant_id"] != tenantA || group["group_id"] == groupB {
				t.Fatal("cross-tenant group disclosure")
			}
		}
		expect(t, alice, "PATCH", "/groups/"+groupB, map[string]any{"version": 1, "config": map[string]any{}}, 404)
		err := db.TenantTx(ctx, tenantA, func(tx pgx.Tx) error {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM group_bindings WHERE tenant_id=$1`, tenantB).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("RLS exposed another tenant")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.Tenant.QueryRow(ctx, `SELECT count(*) FROM group_bindings`).Scan(&count); err != nil || count != 0 {
			t.Fatal("tenant scope leaked from a pooled transaction")
		}
		err = db.TenantTx(ctx, tenantA, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT credential_sealed FROM accounts`)
			return err
		})
		if err == nil {
			t.Fatal("tenant role can read shared account credentials")
		}
		zero := field(t, expect(t, admin, "POST", "/plans", map[string]any{
			"name": "zero", "group_limit": 0, "ai_group_limit": 0, "modules": []string{},
		}, 201), "id")
		expect(t, admin, "PUT", "/subscriptions/"+tenantA, map[string]any{
			"plan_id": zero, "expires_at": time.Now().Add(time.Hour), "enabled": true, "version": 1,
		}, 409)
	})
	t.Run("version fences and test cancellation", func(t *testing.T) {
		update := map[string]any{"version": 1, "config": map[string]any{"blocked_words": []string{"fixture"}}, "management_enabled": false}
		expect(t, alice, "PATCH", "/groups/"+groupA, update, 200)
		expect(t, alice, "PATCH", "/groups/"+groupA, update, 409)
		update["version"], update["ai_enabled"] = 2, true
		expect(t, alice, "PATCH", "/groups/"+groupA, update, 409)
		testInput := map[string]any{"group_id": groupA, "account_ids": []string{account},
			"target_ids": []string{"fixture-member"}, "actions": []string{"send_text"}, "seconds": 301, "budget": 10}
		expect(t, admin, "POST", "/test-windows", testInput, 400)
		testInput["seconds"] = 300
		window := field(t, expect(t, admin, "POST", "/test-windows", testInput, 201), "id")
		expect(t, alice, "GET", "/test-windows", nil, 403)
		expect(t, admin, "POST", "/test-windows/"+window+"/cancel", map[string]any{}, 200)
	})
	t.Run("immutable exact financial entries", func(t *testing.T) {
		entry := map[string]string{"group_id": groupA, "member_id": "member", "kind": "reward",
			"amount": "12.34", "external_key": "fixture-finance-001", "note": "isolated manual evidence"}
		expect(t, alice, "POST", "/ledger", entry, 201)
		expect(t, alice, "POST", "/ledger", entry, 409)
		entry["external_key"], entry["amount"] = "fixture-finance-002", "0.001"
		expect(t, alice, "POST", "/ledger", entry, 400)
		entry["group_id"], entry["amount"] = groupB, "1.00"
		expect(t, alice, "POST", "/ledger", entry, 404)
		var cents int64
		if err := db.Platform.QueryRow(ctx, `SELECT amount_cents FROM ledger_entries`).Scan(&cents); err != nil || cents != 1234 {
			t.Fatal("financial amount changed precision")
		}
		if _, err := db.Platform.Exec(ctx, `UPDATE ledger_entries SET amount_cents=1`); err == nil {
			t.Fatal("platform runtime may mutate immutable financial history")
		}
	})
	t.Run("activity draw is one-time and scoped", func(t *testing.T) {
		activity := field(t, expect(t, alice, "POST", "/activities", map[string]any{
			"group_id": groupA, "name": "test activity", "closes_at": time.Now().Add(time.Hour), "winners": 1,
		}, 201), "id")
		expect(t, alice, "POST", "/activities/"+activity+"/draw", map[string]int{"version": 1}, 409)
		if _, err := db.Platform.Exec(ctx, `INSERT INTO activity_members(activity_id,tenant_id,member_id)
            VALUES($1,$2,'member')`, activity, tenantB); err == nil {
			t.Fatal("activity member accepted a mismatched tenant")
		}
		if _, err := db.Platform.Exec(ctx, `INSERT INTO activity_members(activity_id,tenant_id,member_id)
            VALUES($1,$2,'member')`, activity, tenantA); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Platform.Exec(ctx, `UPDATE activities SET closes_at=now()-interval '1 second' WHERE id=$1`, activity); err != nil {
			t.Fatal(err)
		}
		expect(t, bob, "POST", "/activities/"+activity+"/draw", map[string]int{"version": 1}, 404)
		expect(t, alice, "POST", "/activities/"+activity+"/draw", map[string]int{"version": 1}, 200)
		expect(t, alice, "POST", "/activities/"+activity+"/draw", map[string]int{"version": 1}, 409)
	})
	t.Run("PostgreSQL inbox transaction", func(t *testing.T) {
		ledger := &gateway.PostgresLedger{DB: db.Platform, Vault: vault}
		message := gateway.Message{AccountID: account, GroupID: groupA, NativeID: "fixture-1",
			SenderID: "unknown", ReceivedAt: time.Now(), Payload: []byte("secret body"), Resolved: false}
		for range 2 {
			if err := ledger.Commit(ctx, []gateway.Message{message}); err != nil {
				t.Fatal(err)
			}
		}
		var count int
		var sealed []byte
		var state string
		if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM inbox`).Scan(&count); err != nil || count != 1 {
			t.Fatal("duplicate inbox delivery was not deduplicated")
		}
		if err := db.Platform.QueryRow(ctx, `SELECT payload_sealed,state FROM inbox`).Scan(&sealed, &state); err != nil || state != "unresolved" {
			t.Fatal("unknown sender did not persist in unresolved state")
		}
		plain, err := vault.Open("inbox:"+account+":"+groupA+":fixture-1", sealed)
		if err != nil || string(plain) != "secret body" || bytes.Contains(sealed, []byte("secret body")) {
			t.Fatal("inbox encryption failed")
		}
		message.NativeID = "fixture-2"
		bad := message
		bad.GroupID = "00000000-0000-0000-0000-000000000000"
		if err := ledger.Commit(ctx, []gateway.Message{message, bad}); err == nil {
			t.Fatal("invalid batch unexpectedly committed")
		}
		if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM inbox`).Scan(&count); err != nil || count != 1 {
			t.Fatal("failed batch partially committed")
		}
		unknown := message
		unknown.GroupID, unknown.NativeGroupID, unknown.NativeID = "", "not-yet-synchronized", "new-unknown-group-message"
		if err := ledger.Commit(ctx, []gateway.Message{unknown}); err != nil {
			t.Fatal("initial group discovery blocked durable ingress:", err)
		}
		var native string
		if err := db.Platform.QueryRow(ctx, `SELECT g.native_id FROM inbox i JOIN native_groups g ON g.id=i.group_id
            WHERE i.native_message_id=$1 AND i.state='unresolved' AND i.binding_version IS NULL`,
			unknown.NativeID).Scan(&native); err != nil || native != unknown.NativeGroupID {
			t.Fatal("unknown native group did not persist without a roster lookup")
		}
	})
	t.Run("native role updates and authorization revocation", func(t *testing.T) {
		var chatAccount string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,workspace) VALUES('chat-role-fixture','maibot')
            RETURNING id::text`).Scan(&chatAccount); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Platform.Exec(ctx, `UPDATE group_bindings SET management_enabled=true,ai_enabled=true,ai_account_id=$2
            WHERE group_id=$1`, groupA, chatAccount); err != nil {
			t.Fatal(err)
		}
		var prior int64
		if err := db.Platform.QueryRow(ctx, `SELECT epoch FROM group_bindings WHERE group_id=$1`, groupA).Scan(&prior); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Platform.Exec(ctx, `INSERT INTO operations(tenant_id,actor_id,group_id,account_id,idempotency_key,request_hash,
            kind,account_epoch,binding_epoch,expires_at)
            VALUES($1,$2,$3,$4,'before-role-change',$5,'send_text',1,$6,now()+interval '1 hour')`,
			tenantA, adminID, groupA, account, []byte{1}, prior); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if changed, err := control.ApplyNativeRole(ctx, db.Platform, account, groupA, "member", now); err != nil || !changed {
			t.Fatal("trusted role update failed", err)
		}
		if changed, err := control.ApplyNativeRole(ctx, db.Platform, account, groupA, "admin", now.Add(-time.Second)); err != nil || changed {
			t.Fatal("stale roster restored native privileges", err)
		}
		var management, ai bool
		var epoch int64
		if err := db.Platform.QueryRow(ctx, `SELECT management_enabled,ai_enabled,epoch FROM group_bindings WHERE group_id=$1`,
			groupA).Scan(&management, &ai, &epoch); err != nil || management || ai || epoch != prior+1 {
			t.Fatal("role loss did not fence group automation")
		}
		var status string
		if err := db.Platform.QueryRow(ctx, `SELECT status FROM operations WHERE idempotency_key='before-role-change'`).Scan(&status); err != nil || status != "cancelled" {
			t.Fatal("role loss did not cancel queued actions")
		}
		expect(t, alice, "POST", "/groups/"+groupB+"/revoke", map[string]int{"version": 1}, 404)
		expect(t, bob, "POST", "/groups/"+groupB+"/revoke", map[string]int{"version": 1}, 200)
		var active bool
		var owner string
		if err := db.Platform.QueryRow(ctx, `SELECT active,tenant_id::text FROM group_bindings WHERE group_id=$1`, groupB).Scan(&active, &owner); err != nil || active || owner != tenantB {
			t.Fatal("revocation deleted or reassigned the historical owner")
		}
		if _, _, err := control.IssueClaimCode(ctx, db.Platform, account, groupB); err != control.ErrOwned {
			t.Fatal("rejoining a revoked group allowed automatic ownership transfer", err)
		}
		var fresh string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name) VALUES('code-issue-test','test')
            RETURNING id::text`).Scan(&fresh); err != nil {
			t.Fatal(err)
		}
		if _, err := control.ApplyNativeRole(ctx, db.Platform, account, fresh, "admin", time.Now()); err != nil {
			t.Fatal(err)
		}
		code, expires, err := control.IssueClaimCode(ctx, db.Platform, account, fresh)
		if err != nil || len(code) != 43 || time.Until(expires) < 9*time.Minute || time.Until(expires) > 10*time.Minute {
			t.Fatal("one-time code issuance failed", err)
		}
		if _, _, err := control.IssueClaimCode(ctx, db.Platform, account, fresh); err != control.ErrLiveCode {
			t.Fatal("repeated roster scans regenerated a live code", err)
		}
	})
	expect(t, admin, "PATCH", "/accounts/"+account, map[string]any{"version": 1, "connection_enabled": false}, 200)
	expect(t, admin, "PATCH", "/accounts/"+account, map[string]any{"version": 1, "name": "stale update"}, 409)
	expect(t, admin, "POST", "/auth/logout", map[string]any{}, 200)
	expect(t, admin, "GET", "/me", nil, 401)
}
