package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/security"
)

func TestPostgresNativeMembersAccessAndDeliveryRevocation(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-members-password"
	hash, _ := security.Password(password)
	var adminID string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('member-reader-admin',$1,'superadmin') RETURNING id::text`, hash).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	var tenant string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO tenants(name) VALUES('member-reader-tenant') RETURNING id::text`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role,tenant_id)
        VALUES('member-reader-user',$1,'user',$2)`, hash, tenant); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{43}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		client := testClient{client: &http.Client{Jar: jar}, base: server.URL}
		expect(t, client, "POST", "/auth/login", map[string]string{"username": name, "password": password}, 200)
		return client
	}
	admin, user := login("member-reader-admin"), login("member-reader-user")
	id := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "synthetic-read-panel"}, 201), "id")
	path := "/accounts/" + id + "/native-members"
	expect(t, user, "POST", path, map[string]string{"group_id": "123"}, 403)
	other := admin
	other.workspace = "maibot"
	expect(t, other, "POST", path, map[string]string{"group_id": "123"}, 404)
	expect(t, admin, "POST", path, map[string]string{"group_id": "01"}, 400)
	expect(t, admin, "POST", path, map[string]string{"group_id": "18446744073709551616"}, 400)
	expect(t, admin, "POST", path, map[string]string{"group_id": "123", "tenant_id": tenant}, 400)
	expect(t, admin, "POST", path, map[string]string{"group_id": "123"}, 503)
	rolePath := "/accounts/" + id + "/native-role"
	expect(t, user, "POST", rolePath, map[string]string{"group_id": "123"}, 403)
	expect(t, other, "POST", rolePath, map[string]string{"group_id": "123"}, 404)
	expect(t, admin, "POST", rolePath, map[string]string{"group_id": "123", "role": "admin"}, 400)
	expect(t, admin, "POST", rolePath, map[string]string{"group_id": "123", "receiver_owner": "forged"}, 400)
	expect(t, admin, "POST", rolePath, map[string]string{"group_id": "01"}, 400)
	expect(t, admin, "POST", rolePath, map[string]string{"group_id": "123"}, 503)
	statusPath := "/accounts/" + id + "/native-text-operations/" + control.NewID()
	expect(t, user, "GET", statusPath, nil, 403)
	expect(t, other, "GET", statusPath, nil, 404)
	expect(t, admin, "GET", statusPath, nil, 404)
	// No receiver runs in this suite and no native traffic is generated.
	sealed, _ := vault.Seal("native-deployment:v1", []byte(`{}`))
	if _, err := db.Platform.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,17)`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET auth_state='authenticated',credential_sealed='\x01' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(server.URL)
	request := httptest.NewRequest("POST", "/api/v1"+path, nil)
	request.SetPathValue("id", id)
	for _, cookie := range admin.client.Jar.Cookies(u) {
		request.AddCookie(cookie)
	}
	view := gateway.MemberSnapshot{AccountEpoch: 1, DeploymentVersion: 17}
	principal := Principal{ID: adminID, Role: "superadmin"}
	if err := s.memberResultAccess(request, principal, view); err != nil {
		t.Fatal("valid result refused", err)
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.memberResultAccess(request, principal, view); err == nil {
		t.Fatal("late response escaped account epoch fence")
	}
	view.AccountEpoch++
	if _, err := db.Platform.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, adminID); err != nil {
		t.Fatal(err)
	}
	if err := s.memberResultAccess(request, principal, view); err == nil {
		t.Fatal("disabled user received roster")
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE users SET disabled=false WHERE id=$1`, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, adminID); err != nil {
		t.Fatal(err)
	}
	if err := s.memberResultAccess(request, principal, view); err == nil {
		t.Fatal("revoked website session received roster")
	}
}
