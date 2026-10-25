package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/security"
)

// All native identities and memberships here are isolated SQL fixtures.
func TestPostgresWorkspacesAccess(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("workspace-isolated-password-123")
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('workspace-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{12}, 32)))
	s := New(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{
			"username": name, "password": "workspace-isolated-password-123",
		}, 200)
		return c
	}
	admin := login("workspace-admin")
	chat := admin
	chat.workspace = "maibot"
	user := expect(t, admin, "POST", "/users", map[string]string{
		"username": "workspace-user", "password": "workspace-isolated-password-123", "tenant_name": "workspace-tenant",
	}, 201)
	tenantID := field(t, user, "tenant_id")
	var userID string
	if err := db.Platform.QueryRow(ctx, `SELECT id::text FROM users WHERE username='workspace-user'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	alice := login("workspace-user")
	primary := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "manager-fixture"}, 201), "id")
	aiAccount := field(t, expect(t, chat, "POST", "/accounts", map[string]string{"name": "chat-fixture"}, 201), "id")
	for _, pair := range []struct {
		client     testClient
		own, other string
	}{
		{admin, primary, aiAccount}, {chat, aiAccount, primary},
	} {
		rows := expect(t, pair.client, "GET", "/accounts", nil, 200)
		if !bytes.Contains(rows, []byte(pair.own)) || bytes.Contains(rows, []byte(pair.other)) {
			t.Fatal("account pools leaked across workspaces")
		}
		for _, path := range []string{"/native-auth", "/forget-session"} {
			expect(t, pair.client, "POST", "/accounts/"+pair.other+path, map[string]any{"version": 1}, 404)
		}
		expect(t, pair.client, "POST", "/native-signup", map[string]any{"account_id": pair.other, "version": 1}, 404)
	}
	if !bytes.Contains(expect(t, chat, "GET", "/workspaces", nil, 200), []byte(`"connection_lifecycle":"background"`)) {
		t.Fatal("missing independent lifecycle contract")
	}
	for _, path := range []string{"/users", "/plans", "/operations", "/ledger", "/test-windows", "/access/catalog"} {
		expect(t, chat, "GET", path, nil, 403)
	}
	expect(t, chat, "POST", "/operations", map[string]any{}, 403)
	expect(t, admin, "GET", "/personas", nil, 403)
	expect(t, chat, "PUT", "/personas/"+primary, map[string]any{}, 404)
	expect(t, chat, "PUT", "/personas/"+aiAccount, map[string]any{
		"nickname": "isolated-chat", "personality": "synthetic", "reply_style": "", "version": 0,
	}, 200)
	foreignWorkspace := alice
	foreignWorkspace.workspace = "maibot"
	for _, path := range []string{"/accounts", "/personas", "/groups", "/overview", "/me"} {
		expect(t, foreignWorkspace, "GET", path, nil, 403)
	}
	invalid := admin
	invalid.workspace = "invalid"
	expect(t, invalid, "GET", "/accounts", nil, 400)

	plan := field(t, expect(t, admin, "POST", "/plans", map[string]any{
		"name": "workspace-plan", "group_limit": 3, "ai_group_limit": 1,
		"modules": []string{"moderation", "ai"},
	}, 201), "id")
	expect(t, admin, "PUT", "/subscriptions/"+tenantID, map[string]any{
		"plan_id": plan, "expires_at": time.Now().Add(time.Hour), "enabled": true, "version": 0,
	}, 200)
	groups := []string{}
	for _, name := range []string{"workspace-group-a", "workspace-group-b"} {
		var id string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name) VALUES($1,$1) RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)
            VALUES($1,$2,$3)`, id, tenantID, primary); err != nil {
			t.Fatal(err)
		}
		groups = append(groups, id)
	}
	// A workspace switch is a read, never an epoch/connection mutation.
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET auth_state='authenticated' WHERE id=$1`, aiAccount); err != nil {
		t.Fatal(err)
	}
	expect(t, chat, "PATCH", "/groups/"+groups[0], map[string]any{}, 403)
	expect(t, chat, "PUT", "/groups/"+groups[0]+"/ai-service", map[string]any{}, 403)
	var epoch int64
	if err := db.Platform.QueryRow(ctx, `SELECT epoch FROM accounts WHERE id=$1`, primary).Scan(&epoch); err != nil || epoch != 1 {
		t.Fatal("workspace reads changed connection state")
	}
	t.Run("role and per-user assignments intersect the package", func(t *testing.T) {
		path := "/users/" + userID + "/access"
		expect(t, alice, "GET", "/access/catalog", nil, 403)
		expect(t, alice, "PUT", path, map[string]any{}, 403)
		expect(t, admin, "PUT", path, map[string]any{
			"role_code": "custom", "permissions": []string{"users:manage"}, "version": 0,
		}, 400)
		expect(t, admin, "PUT", path, map[string]any{
			"role_code": "viewer", "permissions": rolePermissions("viewer"), "version": 0,
		}, 200)
		expect(t, alice, "GET", "/groups", nil, 200)
		expect(t, alice, "PATCH", "/groups/"+groups[0], map[string]any{"version": 1}, 403)
		expect(t, alice, "PUT", "/groups/"+groups[0]+"/ai-service", map[string]any{"version": 1, "enabled": true}, 403)
		expect(t, admin, "PUT", path, map[string]any{
			"role_code": "manager", "permissions": rolePermissions("manager"), "version": 0,
		}, 409)
		expect(t, admin, "PUT", path, map[string]any{
			"role_code": "manager", "permissions": rolePermissions("manager"), "version": 1,
		}, 200)
		var me struct {
			Permissions []string `json:"permissions"`
		}
		if err := json.Unmarshal(expect(t, alice, "GET", "/me", nil, 200), &me); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(me.Permissions, "groups:ai") || slices.Contains(me.Permissions, "ledger:create") ||
			slices.Contains(me.Permissions, "workspaces:maibot") || slices.Contains(me.Permissions, "groups:replies") {
			t.Fatal("effective permissions escaped the package or role")
		}
		expect(t, alice, "PATCH", "/groups/"+groups[0], map[string]any{
			"version": 1, "config": map[string]any{"keyword_replies": []map[string]string{{"keyword": "x", "reply": "x"}}},
		}, 403)
		expect(t, alice, "PATCH", "/groups/"+groups[0], map[string]any{
			"version": 1, "config": map[string]any{"blocked_words": []string{"synthetic"}},
		}, 200)
	})
	t.Run("AI requests are quota checked and never run unverified actions", func(t *testing.T) {
		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for i, id := range groups {
			version := 1
			if i == 0 {
				version = 2
			}
			wg.Add(1)
			go func(id string, version int) {
				defer wg.Done()
				status, _, _ := alice.request("PUT", "/groups/"+id+"/ai-service",
					map[string]any{"version": version, "enabled": true, "invite": true})
				statuses <- status
			}(id, version)
		}
		wg.Wait()
		close(statuses)
		count := map[int]int{}
		for status := range statuses {
			count[status]++
		}
		if count[200] != 1 || count[409] != 1 {
			t.Fatalf("quota race: %v", count)
		}
		var requested, enabled, actions int
		if err := db.Platform.QueryRow(ctx, `SELECT count(*) FILTER(WHERE ai_requested),count(*) FILTER(WHERE ai_enabled)
            FROM group_bindings`).Scan(&requested, &enabled); err != nil || requested != 1 || enabled != 0 {
			t.Fatal("AI requests bypassed quotas or enabled an unverified engine", err)
		}
		if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM operations`).Scan(&actions); err != nil || actions != 0 {
			t.Fatal("intent recording dispatched a native action")
		}
		rows := expect(t, chat, "GET", "/groups", nil, 200)
		if !bytes.Contains(rows, []byte(aiAccount)) || bytes.Contains(rows, []byte("blocked_words")) {
			t.Fatal("chat workspace did not isolate management configuration")
		}
		if err := db.TenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(id) FROM accounts WHERE workspace='maibot'`).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatal("tenant database role can list shared chat identities")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var denied bool
		err := db.TenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT * FROM personas`)
			denied = err != nil
			return err
		})
		if err == nil || !denied {
			t.Fatal("tenant role can read global personas")
		}
		_, err = db.Platform.Exec(ctx, `UPDATE group_bindings SET primary_account_id=$2 WHERE group_id=$1`, groups[0], aiAccount)
		if err == nil {
			t.Fatal("chat identity can become a group manager")
		}
		_, err = db.Platform.Exec(ctx, `UPDATE group_bindings SET ai_account_id=$2 WHERE group_id=$1`, groups[0], primary)
		if err == nil {
			t.Fatal("management identity can become a chat identity")
		}
		var pending string
		var version int64
		if err := db.Platform.QueryRow(ctx, `SELECT group_id::text,version FROM group_bindings
            WHERE ai_requested`).Scan(&pending, &version); err != nil {
			t.Fatal(err)
		}
		expect(t, admin, "POST", "/groups/"+pending+"/revoke", map[string]any{"version": version}, 200)
		var active, requestedAI bool
		var state string
		if err := db.Platform.QueryRow(ctx, `SELECT active,ai_requested,ai_request_state FROM group_bindings
            WHERE group_id=$1`, pending).Scan(&active, &requestedAI, &state); err != nil || active || requestedAI || state != "disabled" {
			t.Fatal("revocation left an active AI invitation request", err)
		}
	})
	t.Run("expiration preserves reads but revokes service writes", func(t *testing.T) {
		if _, err := db.Platform.Exec(ctx, `UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1`, tenantID); err != nil {
			t.Fatal(err)
		}
		expect(t, alice, "GET", "/groups", nil, 200)
		expect(t, alice, "GET", "/ledger", nil, 200)
		expect(t, alice, "PATCH", "/groups/"+groups[0], map[string]any{"version": 2}, 403)
		expect(t, alice, "PUT", "/groups/"+groups[0]+"/ai-service", map[string]any{"version": 2, "enabled": true}, 403)
	})
	t.Run("permission changes cancel only the affected user's queued work", func(t *testing.T) {
		var adminID string
		if err := db.Platform.QueryRow(ctx, `SELECT id::text FROM users WHERE username='workspace-admin'`).Scan(&adminID); err != nil {
			t.Fatal(err)
		}
		for _, actor := range []string{userID, adminID} {
			if _, err := db.Platform.Exec(ctx, `INSERT INTO operations(tenant_id,actor_id,group_id,account_id,
                idempotency_key,request_hash,kind,account_epoch,binding_epoch,expires_at)
                SELECT tenant_id,$2::uuid,group_id,primary_account_id,$2::uuid::text,$3,'send_text',1,epoch,now()+interval '1 hour'
                FROM group_bindings WHERE group_id=$1`, groups[0], actor, []byte{1}); err != nil {
				t.Fatal(err)
			}
		}
		expect(t, admin, "PUT", "/users/"+userID+"/access", map[string]any{
			"role_code": "viewer", "permissions": rolePermissions("viewer"), "version": 2,
		}, 200)
		var mine, other string
		if err := db.Platform.QueryRow(ctx, `SELECT status FROM operations WHERE actor_id=$1`, userID).Scan(&mine); err != nil {
			t.Fatal(err)
		}
		if err := db.Platform.QueryRow(ctx, `SELECT status FROM operations WHERE actor_id=$1`, adminID).Scan(&other); err != nil {
			t.Fatal(err)
		}
		if mine != "cancelled" || other != "queued" {
			t.Fatalf("permission fencing crossed actors: user=%s admin=%s", mine, other)
		}
	})
}
