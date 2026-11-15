package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type isolatedMobileAuth struct {
	session  nativeauth.Session
	logins   atomic.Int32
	requests atomic.Int32
	loginErr error
}

func (f *isolatedMobileAuth) Request(_ context.Context, route string, _ map[string]any) (map[string]json.RawMessage, error) {
	f.requests.Add(1)
	if route == "/v1/verify/sms-anon" {
		return map[string]json.RawMessage{"key": json.RawMessage(`"synthetic-mobile-sms-key"`)}, nil
	}
	return nil, &nativeauth.Error{Code: "native_unsupported_route"}
}
func (f *isolatedMobileAuth) Login(context.Context, map[string]any) error {
	f.logins.Add(1)
	return f.loginErr
}
func (f *isolatedMobileAuth) VerifySecurity(context.Context, string) error { return nil }
func (f *isolatedMobileAuth) SecurityRequired() bool                       { return false }
func (f *isolatedMobileAuth) Close()                                       {}
func (f *isolatedMobileAuth) Finish(context.Context) (nativeauth.Session, string, error) {
	return f.session, "Synthetic mobile", nil
}

func TestPostgresNativeMobileSearchAuth(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, err := security.Password("synthetic-mobile-password-123")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mobile-admin", "mobile-other"} {
		if _, err = db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
			VALUES($1,$2,'superadmin')`, name, hash); err != nil {
			t.Fatal(err)
		}
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	t.Cleanup(s.NativeMobile.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		client := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
		expect(t, client, "POST", "/auth/login", map[string]string{
			"username": name, "password": "synthetic-mobile-password-123"}, 200)
		return client
	}
	admin, other := login("mobile-admin"), login("mobile-other")
	boolField := func(data json.RawMessage, key string) bool {
		t.Helper()
		var body map[string]json.RawMessage
		var value bool
		if json.Unmarshal(data, &body) != nil || json.Unmarshal(body[key], &value) != nil {
			t.Fatalf("missing boolean response field %s", key)
		}
		return value
	}
	expect(t, admin, "POST", "/users", map[string]string{
		"username": "mobile-customer", "password": "synthetic-mobile-password-123", "tenant_name": "Synthetic customer"}, 201)
	customer := login("mobile-customer")
	expect(t, customer, "GET", "/native-mobile-deployment", nil, 403)
	if data := expect(t, admin, "GET", "/native-mobile-deployment", nil, 200); boolField(data, "configured") {
		t.Fatal("mobile configuration created by GET")
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	d := nativeauth.Deployment{Origin: "https://example.invalid", Headers: map[string]string{},
		Metadata: make([]uint64, 14), SigningSeed: key, BodyKey: key, ServerKey: key,
		CaptchaID: "synthetic-mobile-captcha", DeviceID: "synthetic-mobile-device",
		ProtocolVersion: nativeauth.MobileProtocolVersion, AppVersion: 337, AppHash: nativeauth.MobileAppHash}
	encoded, _ := json.Marshal(d)
	expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 0, "configuration": string(encoded)}, 400)
	desktop := d
	desktop.ProtocolVersion, desktop.AppVersion, desktop.AppHash = nativeauth.DesktopProtocolVersion, 0, ""
	desktopEncoded, _ := json.Marshal(desktop)
	expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 0, "configuration": string(desktopEncoded)}, 200)
	expect(t, admin, "PUT", "/native-mobile-deployment", map[string]any{"version": 0, "configuration": string(encoded)}, 200)
	expect(t, admin, "PUT", "/native-mobile-deployment", map[string]any{"version": 0, "configuration": string(encoded)}, 409)
	response := expect(t, admin, "GET", "/native-mobile-deployment", nil, 200)
	for _, hidden := range []string{"signing_seed", "body_key", "app_hash", key} {
		if bytes.Contains(response, []byte(hidden)) {
			t.Fatal("mobile configuration disclosed private fields")
		}
	}
	account := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "Synthetic existing WSL"}, 201), "id")
	if _, err = db.Platform.Exec(ctx, `UPDATE accounts SET native_uid='123',auth_state='authenticated',
		credential_sealed=$2,connection_enabled=true,epoch=3,version=5 WHERE id=$1`,
		account, []byte("synthetic-desktop-preserved")); err != nil {
		t.Fatal(err)
	}
	mobile := &isolatedMobileAuth{session: nativeauth.Session{Origin: d.Origin, UID: 123, Kind: 1, Device: 44,
		JWT: "synthetic-mobile-jwt-private", MAC: bytes.Repeat([]byte{4}, 16),
		ProtocolVersion: nativeauth.MobileProtocolVersion, AccountID: "456"}}
	s.NativeMobile.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return mobile, nil }
	path := "/accounts/" + account + "/native-mobile-auth"
	expect(t, customer, "GET", path, nil, 403)
	expect(t, customer, "POST", path, map[string]int{"version": 5}, 403)
	expect(t, admin, "POST", path, map[string]int{"version": 4}, 409)
	start := expect(t, admin, "POST", path, map[string]int{"version": 5}, 201)
	id := field(t, start, "transaction_id")
	if id == "" || mobile.logins.Load() != 0 {
		t.Fatal("starting an authorization attempted a native login")
	}
	endpoint := path + "/" + id
	if second := field(t, expect(t, admin, "POST", path, map[string]int{"version": 5}, 201), "transaction_id"); second != id {
		t.Fatal("starting twice created another mobile authorization")
	}
	expect(t, other, "GET", endpoint, nil, 404)
	expect(t, other, "POST", path, map[string]int{"version": 5}, 409)
	expect(t, admin, "POST", endpoint, map[string]string{"action": "signup_submit"}, 400)
	expect(t, admin, "POST", endpoint, map[string]string{"action": "password", "account": "synthetic",
		"password": "synthetic-not-live", "captcha": "synthetic-human"}, 202)
	wait := func(endpoint, state string) json.RawMessage {
		t.Helper()
		until := time.Now().Add(3 * time.Second)
		for time.Now().Before(until) {
			data := expect(t, admin, "GET", endpoint, nil, 200)
			if field(t, data, "status") == state {
				return data
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("mobile authorization did not reach expected state")
		return nil
	}
	wait(endpoint, "authenticated")
	for i := 0; i < 3; i++ {
		data := expect(t, admin, "GET", path, nil, 200)
		if !boolField(data, "available") || bytes.Contains(data, []byte(mobile.session.JWT)) {
			t.Fatal("mobile status is unavailable or disclosed a credential")
		}
	}
	if mobile.logins.Load() != 1 {
		t.Fatal("polling retried mobile login")
	}
	var credential []byte
	var epoch, version int64
	var connected bool
	if err = db.Platform.QueryRow(ctx, `SELECT credential_sealed,epoch,version,connection_enabled
		FROM accounts WHERE id=$1`, account).Scan(&credential, &epoch, &version, &connected); err != nil ||
		string(credential) != "synthetic-desktop-preserved" || epoch != 3 || version != 5 || !connected {
		t.Fatal("companion authentication changed the receive account", err)
	}
	for _, table := range []string{"native_mobile_sessions", "native_mobile_deployment"} {
		if _, err = db.Tenant.Exec(ctx, "SELECT * FROM "+table); err == nil {
			t.Fatal("tenant database role read mobile credentials")
		}
	}
	first, err := s.nativeRequestIDs(1)(ctx, uint64(time.Now().UnixNano()))
	second, mobileErr := s.nativeMobileRequestIDs(1)(ctx, first)
	third, desktopErr := s.nativeRequestIDs(1)(ctx, first)
	if err != nil || mobileErr != nil || desktopErr != nil || second != first+1 || third != second+1 {
		t.Fatal("mobile and desktop did not share durable request IDs")
	}
	expect(t, admin, "DELETE", "/accounts/"+account+"/native-mobile-session", map[string]int{"version": 2}, 409)
	expect(t, admin, "DELETE", "/accounts/"+account+"/native-mobile-session", map[string]int{"version": 1}, 200)
	if data := expect(t, admin, "GET", path, nil, 200); boolField(data, "available") {
		t.Fatal("revoked mobile authorization is still available")
	}
	// A second authorization cannot silently attach another native account.
	mobile.session.UID = 124
	start = expect(t, admin, "POST", path, map[string]int{"version": 5}, 201)
	endpoint = path + "/" + field(t, start, "transaction_id")
	expect(t, admin, "POST", endpoint, map[string]string{"action": "password", "account": "synthetic-other",
		"password": "synthetic-not-live", "captcha": "synthetic-human"}, 202)
	data := wait(endpoint, "connection_pending")
	if !strings.Contains(string(data), "native_mobile_identity_mismatch") {
		t.Fatal("another native identity was not rejected")
	}
	expect(t, admin, "DELETE", endpoint, nil, 200)
	expect(t, admin, "PUT", "/native-mobile-deployment", map[string]any{"version": 1, "configuration": string(encoded)}, 200)
	if _, err = s.nativeMobileRequestIDs(1)(ctx, third); err == nil {
		t.Fatal("old mobile configuration allocated a nonce")
	}
	if err = db.Platform.QueryRow(ctx, `SELECT epoch,version,connection_enabled FROM accounts WHERE id=$1`,
		account).Scan(&epoch, &version, &connected); err != nil || epoch != 3 || version != 5 || !connected {
		t.Fatal("replacing mobile configuration changed desktop connection", err)
	}
	// Invalid successful SMS responses retain one bounded diagnostic, never a
	// credential or an automatic second authentication request.
	rejected := &isolatedMobileAuth{loginErr: &nativeauth.Error{
		Code: "native_mobile_login_response_invalid", Diagnostic: "login_security_flags_invalid", Submitted: true}}
	s.NativeMobile.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return rejected, nil }
	start = expect(t, admin, "POST", path, map[string]int{"version": 5}, 201)
	id = field(t, start, "transaction_id")
	endpoint = path + "/" + id
	expect(t, admin, "POST", endpoint, map[string]string{"action": "sms_request",
		"account": "13800000000", "captcha": "synthetic-mobile-human"}, 202)
	wait(endpoint, "sms_required")
	expect(t, admin, "POST", endpoint, map[string]string{"action": "sms_verify",
		"code": "123456", "captcha": "synthetic-mobile-human"}, 202)
	data = wait(endpoint, "unknown")
	var view nativeauth.View
	if json.Unmarshal(data, &view) != nil || view.FailureStage != "sms_login" ||
		view.Error != "native_mobile_login_response_invalid" ||
		view.Diagnostic != "login_security_flags_invalid" ||
		view.DiagnosticVersion != nativeauth.AuthDiagnosticVersion {
		t.Fatal("mobile failure lost its safe diagnostic or SMS stage")
	}
	for range 3 {
		expect(t, admin, "GET", endpoint, nil, 200)
	}
	expect(t, admin, "POST", endpoint, map[string]string{"action": "sms_verify",
		"code": "123456", "captcha": "synthetic-mobile-human"}, 409)
	expect(t, admin, "DELETE", endpoint, nil, 200)
	expect(t, admin, "GET", endpoint, nil, 404)
	var detail []byte
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if err = db.Platform.QueryRow(ctx, `SELECT detail FROM audit_events
			WHERE action='native_auth_rejected' AND resource_id=$1`, account).Scan(&detail); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var diagnostic nativeauth.FailureDiagnostic
	if json.Unmarshal(detail, &diagnostic) != nil || diagnostic.FailureStage != "sms_login" ||
		diagnostic.Diagnostic != "login_security_flags_invalid" ||
		diagnostic.DiagnosticVersion != nativeauth.AuthDiagnosticVersion {
		t.Fatal("mobile diagnostic did not survive closing the transaction", err)
	}
	for _, secret := range []string{"13800000000", "123456", "synthetic-mobile-human",
		"synthetic-mobile-sms-key", "synthetic-mobile-password", mobile.session.JWT} {
		if bytes.Contains(data, []byte(secret)) || bytes.Contains(detail, []byte(secret)) {
			t.Fatal("mobile response diagnostic disclosed authentication input")
		}
	}
	if bytes.Contains(detail, []byte(id)) {
		t.Fatal("authentication transaction handle persisted in diagnostic audit")
	}
	var auditCount, savedCount int
	if err = db.Platform.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM audit_events WHERE action='native_auth_rejected' AND resource_id=$1),
		(SELECT count(*) FROM native_mobile_sessions WHERE account_id=$1::uuid
			AND credential_sealed IS NOT NULL)`, account).Scan(&auditCount, &savedCount); err != nil || auditCount != 1 || savedCount != 0 || rejected.logins.Load() != 1 ||
		rejected.requests.Load() != 1 {
		t.Fatalf("mobile failure: err=%v audits=%d credentials=%d logins=%d requests=%d",
			err, auditCount, savedCount, rejected.logins.Load(), rejected.requests.Load())
	}
	if err = db.Platform.QueryRow(ctx, `SELECT credential_sealed,epoch,version,connection_enabled
		FROM accounts WHERE id=$1`, account).Scan(&credential, &epoch, &version, &connected); err != nil ||
		string(credential) != "synthetic-desktop-preserved" || epoch != 3 || version != 5 || !connected {
		t.Fatal("mobile response failure changed the receive account", err)
	}
}
