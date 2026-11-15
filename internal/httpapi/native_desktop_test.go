package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresDesktopOnlyTransition(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-desktop-policy-password"
	hash, _ := security.Password(password)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('desktop-policy-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{23}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) {
		t.Fatal("legacy profile reached a native authentication factory")
		return nil, nil
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "desktop-policy-admin", "password": password}, 200)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	d := nativeauth.Deployment{Origin: "https://example.invalid", Metadata: make([]uint64, 14),
		SigningSeed: key, BodyKey: key, ServerKey: key, CaptchaID: "synthetic-captcha",
		DeviceID: "synthetic-device", ProtocolVersion: "3.7.4"}
	raw, _ := json.Marshal(d)
	sealed, _ := vault.Seal("native-deployment:v1", raw)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,7)`, sealed); err != nil {
		t.Fatal(err)
	}
	view := expect(t, admin, "GET", "/native-deployment", nil, 200)
	if !bytes.Contains(view, []byte(`"migration_required":true`)) ||
		!bytes.Contains(view, []byte(`"configured":false`)) || !bytes.Contains(view, []byte(`"version":7`)) ||
		bytes.Contains(view, []byte("signing_seed")) || bytes.Contains(view, []byte(d.DeviceID)) {
		t.Fatal("legacy configuration did not expose a safe desktop migration state")
	}
	id := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "desktop-transition"}, 201), "id")
	expect(t, admin, "POST", "/accounts/"+id+"/native-auth", map[string]int{"version": 1}, 409)
	expect(t, admin, "POST", "/native-signup", map[string]any{"account_id": id, "version": 1}, 409)
	for _, protocol := range []string{"", "3.7.1", "3.7.3", "3.7.4"} {
		d.ProtocolVersion = protocol
		rejected, _ := json.Marshal(d)
		status, _, code := admin.request("PUT", "/native-deployment", map[string]any{"version": 7, "configuration": string(rejected)})
		if status != 400 || code != "native_desktop_required" {
			t.Fatal("mobile profile import was not explicitly rejected", status, code)
		}
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET credential_sealed='\x0102',auth_state='authenticated' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	d.ProtocolVersion = nativeauth.DesktopProtocolVersion
	desktop, _ := json.Marshal(d)
	expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 7, "configuration": string(desktop)}, 200)
	view = expect(t, admin, "GET", "/native-deployment", nil, 200)
	if !bytes.Contains(view, []byte(`"configured":true`)) ||
		!bytes.Contains(view, []byte(`"login_adapter":"desktop"`)) || bytes.Contains(view, []byte(`"migration_required":true`)) {
		t.Fatal("desktop configuration was not usable after explicit import")
	}
	var preserved bool
	if err := db.Platform.QueryRow(ctx, `SELECT auth_state='reauth_required' AND epoch=2 AND version=2
        AND credential_sealed='\x0102' FROM accounts WHERE id=$1`, id).Scan(&preserved); err != nil || !preserved {
		t.Fatal("profile replacement relabeled a mobile session or erased the account")
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET auth_state='authenticated',connection_enabled=true WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled) VALUES($1,true)`, id); err != nil {
		t.Fatal(err)
	}
	// Reapply only this forward transition inside the isolated wsl_test fixture.
	migration, err := os.ReadFile("../database/migrations/014_desktop_only_auth.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Platform.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	if err = db.Platform.QueryRow(ctx, `SELECT a.auth_state='reauth_required' AND a.epoch=3 AND a.version=3
        AND a.credential_sealed='\x0102' AND NOT a.connection_enabled AND NOT r.enabled
        AND r.revision=2 AND r.snapshot='{}'::jsonb
        FROM accounts a JOIN account_receivers r ON r.account_id=a.id WHERE a.id=$1`, id).Scan(&preserved); err != nil || !preserved {
		t.Fatal("desktop migration did not fence saved sessions and reception")
	}
}
