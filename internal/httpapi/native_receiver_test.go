package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/security"
)

func TestPostgresReceiverControl(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-receiver-password"
	hash, _ := security.Password(password)
	_, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role) VALUES('receiver-admin',$1,'superadmin')`, hash)
	if err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{19}, 32)))
	s := New(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	// The manager is not Run: tests must never contact real native services.
	s.Receiver = gateway.NewReceiverManager(db.Platform, vault, s.nativeRequestIDs)
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "receiver-admin", "password": password}, 200)
	id := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "receiver-panel"}, 201), "id")
	path := "/accounts/" + id + "/receiver"
	expect(t, admin, "PUT", path, map[string]any{"enabled": true, "revision": 0}, 409)
	expect(t, admin, "PUT", "/native-im-profile", map[string]any{"version": 0, "app_key": "synthetic-im-profile-key"}, 200)
	expect(t, admin, "PUT", "/native-im-profile", map[string]any{"version": 0, "app_key": "synthetic-im-profile-key"}, 409)
	if _, err = db.Platform.Exec(ctx, `UPDATE accounts SET auth_state='authenticated',credential_sealed='\x0102' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "PUT", path, map[string]any{"enabled": true, "revision": 0}, 200)
	expect(t, admin, "PUT", "/native-im-profile", map[string]any{"version": 1, "app_key": "synthetic-im-profile-key"}, 409)
	expect(t, admin, "POST", "/accounts/"+id+"/forget-session", map[string]int{"version": 1}, 409)
	expect(t, admin, "PUT", path, map[string]any{"enabled": false, "revision": 0}, 409)
	expect(t, admin, "PUT", path, map[string]any{"enabled": false, "revision": 1}, 200)
	var preserved bool
	if err = db.Platform.QueryRow(ctx, `SELECT credential_sealed IS NOT NULL AND NOT connection_enabled AND epoch=1
        FROM accounts WHERE id=$1`, id).Scan(&preserved); err != nil || !preserved {
		t.Fatal("receive toggle changed session or enabled native actions")
	}
	expect(t, admin, "POST", "/users", map[string]string{"username": "receiver-tenant", "password": password, "tenant_name": "receiver-tenant"}, 201)
	jar, _ = cookiejar.New(nil)
	tenant := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	expect(t, tenant, "POST", "/auth/login", map[string]string{"username": "receiver-tenant", "password": password}, 200)
	expect(t, tenant, "GET", "/native-im-profile", nil, 403)
	expect(t, tenant, "PUT", path, map[string]any{"enabled": true, "revision": 2}, 403)
	expect(t, admin, "DELETE", "/accounts/"+id, map[string]any{"version": 1, "confirm_name": "receiver-panel"}, 200)
	var count int
	if err = db.Platform.QueryRow(ctx, `SELECT count(*) FROM account_receivers WHERE account_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("removal retained receiver data")
	}
}
