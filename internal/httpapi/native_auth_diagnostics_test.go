package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type rejectedDiagnosticAuth struct {
	isolatedAuth
	requests atomic.Int32
}

func (f *rejectedDiagnosticAuth) Request(_ context.Context, route string, _ map[string]any) (map[string]json.RawMessage, error) {
	f.requests.Add(1)
	if route != "/v1/verify/sms-anon" {
		return nil, &nativeauth.Error{Code: "unexpected_route"}
	}
	return nil, &nativeauth.Error{Code: "native_sms_rejected_unspecified", Number: 1001,
		Diagnostic: "provider_reason_unrecognized", Submitted: true}
}

func TestPostgresNativeAuthDiagnosticSurvivesClosedTransaction(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("isolated-diagnostic-password-123")
	var actor string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('diagnostic-admin',$1,'superadmin') RETURNING id`, hash).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{
		"username": "diagnostic-admin", "password": "isolated-diagnostic-password-123"}, 200)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	d := nativeauth.Deployment{Origin: "https://example.invalid", Headers: map[string]string{},
		Metadata: make([]uint64, 14), SigningSeed: key, BodyKey: key, ServerKey: key,
		CaptchaID: "synthetic-captcha", DeviceID: "synthetic-device", ProtocolVersion: nativeauth.DesktopProtocolVersion}
	encoded, _ := json.Marshal(d)
	expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 0, "configuration": string(encoded)}, 200)
	fake := &rejectedDiagnosticAuth{}
	s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return fake, nil }
	account := field(t, expect(t, admin, "POST", "/accounts", map[string]any{"name": "diagnostic-robot"}, 201), "id")
	other := field(t, expect(t, admin, "POST", "/accounts", map[string]any{"name": "other-robot"}, 201), "id")
	id := field(t, expect(t, admin, "POST", "/native-signup",
		map[string]any{"account_id": account, "version": 1}, 201), "transaction_id")
	endpoint := "/accounts/" + account + "/native-auth/" + id
	expect(t, admin, "POST", endpoint, map[string]any{"action": "signup_sms",
		"account": "13800000000", "agreed": true, "captcha": "synthetic-ticket"}, 202)
	var detail []byte
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		err := db.Platform.QueryRow(ctx, `SELECT detail FROM audit_events WHERE action='native_auth_rejected'
            AND actor_id=$1 AND resource_id=$2 AND tenant_id IS NULL`, actor, account).Scan(&detail)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var diagnostic nativeauth.FailureDiagnostic
	if len(detail) == 0 || json.Unmarshal(detail, &diagnostic) != nil ||
		diagnostic.FailureStage != "signup_sms" || diagnostic.ProviderCode != 1001 ||
		diagnostic.Diagnostic != "provider_reason_unrecognized" ||
		diagnostic.DiagnosticVersion != nativeauth.AuthDiagnosticVersion {
		t.Fatal("bounded diagnostic was not persisted")
	}
	for _, secret := range []string{"13800000000", "synthetic-ticket", "synthetic-captcha", id, "password", "phone"} {
		if bytes.Contains(detail, []byte(secret)) {
			t.Fatal("sensitive content persisted in diagnostic audit")
		}
	}
	for range 3 {
		expect(t, admin, "GET", endpoint, nil, 200)
	}
	expect(t, admin, "DELETE", endpoint, nil, 200)
	expect(t, admin, "GET", endpoint, nil, 404)
	var count, foreign, registrations int
	if err := db.Platform.QueryRow(ctx, `SELECT
        count(*) FILTER(WHERE resource_id=$1),count(*) FILTER(WHERE resource_id=$2)
        FROM audit_events WHERE action='native_auth_rejected'`, account, other).Scan(&count, &foreign); err != nil ||
		count != 1 || foreign != 0 || fake.requests.Load() != 1 {
		t.Fatal("audit was lost, duplicated or attributed to another account")
	}
	if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM native_signup_attempts`).Scan(&registrations); err != nil || registrations != 0 {
		t.Fatal("SMS rejection submitted native registration")
	}
	var pristine bool
	if err := db.Platform.QueryRow(ctx, `SELECT credential_sealed IS NULL AND native_uid IS NULL
        AND auth_state='not_authenticated' FROM accounts WHERE id=$1`, account).Scan(&pristine); err != nil || !pristine {
		t.Fatal("diagnostic recording activated authentication")
	}
	// A late diagnostic cannot recreate audit data for a deleted account.
	diagnostic.ActorID, diagnostic.AccountID = actor, "00000000-0000-0000-0000-000000000001"
	s.recordNativeAuthDiagnostic(ctx, diagnostic)
	if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM audit_events
        WHERE action='native_auth_rejected'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("late diagnostic recreated removed account history")
	}
}
