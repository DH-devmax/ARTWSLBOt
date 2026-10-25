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

type isolatedSignup struct {
	isolatedAuth
	registers atomic.Int32
	outcome   atomic.Int32
	accountID atomic.Uint64
	start     chan struct{}
	release   chan struct{}
}

func (f *isolatedSignup) Request(ctx context.Context, route string, in map[string]any) (map[string]json.RawMessage, error) {
	if route == "/v1/verify/sms-anon" {
		return map[string]json.RawMessage{"key": json.RawMessage(`"synthetic-register-key"`)}, nil
	}
	if route != "/v1/user/register" {
		return nil, &nativeauth.Error{Code: "unexpected_route"}
	}
	f.registers.Add(1)
	if f.start != nil {
		close(f.start)
		<-f.release
	}
	switch f.outcome.Load() {
	case 1:
		return nil, &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	case 2:
		return nil, &nativeauth.Error{Code: "native_invalid_sms", Number: 1010, Submitted: true}
	case 3:
		return nil, &nativeauth.Error{Code: "native_dns_fake_ip"}
	}
	id, _ := json.Marshal(f.accountID.Load())
	return map[string]json.RawMessage{"accountId": id}, nil
}

func (f *isolatedSignup) Finish(context.Context) (nativeauth.Session, string, error) {
	id, _ := json.Marshal(f.accountID.Load())
	return nativeauth.Session{UID: 789, JWT: "synthetic-jwt", NIMID: "synthetic-peer",
		NIMToken: "synthetic-im", AccountID: string(id)}, "synthetic", nil
}

func TestPostgresNativeSignup(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("isolated-signup-password-123")
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('signup-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	s := New(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(func() { s.Native.Close() })
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "signup-admin", "password": "isolated-signup-password-123"}, 200)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	d := nativeauth.Deployment{Origin: "https://example.invalid", Headers: map[string]string{},
		Metadata: make([]uint64, 14), SigningSeed: key, BodyKey: key, ServerKey: key,
		CaptchaID: "synthetic-captcha", DeviceID: "synthetic-device", ProtocolVersion: "3.7.3"}
	encoded, _ := json.Marshal(d)
	expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 0, "configuration": string(encoded)}, 200)
	if _, err := db.Tenant.Exec(ctx, `SELECT transaction_id FROM native_signup_attempts`); err == nil {
		t.Fatal("tenant can read shared signup attempts")
	}
	fake := &isolatedSignup{}
	fake.accountID.Store(456)
	s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return fake, nil }
	wait := func(account, id, state string) []byte {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
			result := expect(t, admin, "GET", "/accounts/"+account+"/native-auth/"+id, nil, 200)
			if field(t, result, "status") == state {
				return result
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("signup did not reach %s", state)
		return nil
	}
	start := func() (string, string) {
		t.Helper()
		account := field(t, expect(t, admin, "POST", "/accounts", map[string]any{"name": "isolated-signup"}, 201), "id")
		result := expect(t, admin, "POST", "/native-signup", map[string]any{"account_id": account, "version": 1}, 201)
		return account, field(t, result, "transaction_id")
	}
	step := func(account, id, action string, status int) {
		t.Helper()
		expect(t, admin, "POST", "/accounts/"+account+"/native-auth/"+id, map[string]any{
			"action": action, "account": "13800000000", "password": "synthetic1", "nickname": "synthetic",
			"code": "123456", "captcha": "synthetic-human", "agreed": true,
		}, status)
	}
	account, id := start()
	// A single owner's idle login/signup tabs are mutually replacing, not competing owners.
	login := expect(t, admin, "POST", "/accounts/"+account+"/native-auth", map[string]any{"version": 1}, 201)
	expect(t, admin, "GET", "/accounts/"+account+"/native-auth/"+id, nil, 404)
	id = field(t, expect(t, admin, "POST", "/native-signup",
		map[string]any{"account_id": account, "version": 1}, 201), "transaction_id")
	expect(t, admin, "GET", "/accounts/"+account+"/native-auth/"+field(t, login, "transaction_id"), nil, 404)
	step(account, id, "signup_sms", 202)
	wait(account, id, "signup_sms_required")
	step(account, id, "signup_submit", 202)
	result := wait(account, id, "registered")
	if field(t, result, "registered_account") != "456" {
		t.Fatal("provider account ID missing")
	}
	var pristine bool
	if err := db.Platform.QueryRow(ctx, `SELECT credential_sealed IS NULL AND native_uid IS NULL AND auth_state='not_authenticated'
        FROM accounts WHERE id=$1`, account).Scan(&pristine); err != nil || !pristine {
		t.Fatal("registration impersonated an authenticated native session", err)
	}
	expect(t, admin, "POST", "/native-signup", map[string]any{"account_id": account, "version": 1}, 409)
	// The official registration account ID is not the native UID; login must match it.
	loginID := field(t, expect(t, admin, "POST", "/accounts/"+account+"/native-auth", map[string]any{"version": 1}, 201), "transaction_id")
	fake.accountID.Store(999)
	step(account, loginID, "password", 202)
	if field(t, wait(account, loginID, "failed"), "error") != "native_signup_identity_mismatch" {
		t.Fatal("login replaced the registered identity")
	}
	fake.accountID.Store(456)
	step(account, loginID, "password", 202)
	wait(account, loginID, "authenticated")

	fake.outcome.Store(1)
	unknownAccount, unknownID := start()
	step(unknownAccount, unknownID, "signup_sms", 202)
	wait(unknownAccount, unknownID, "signup_sms_required")
	step(unknownAccount, unknownID, "signup_submit", 202)
	wait(unknownAccount, unknownID, "unknown")
	count := fake.registers.Load()
	// A process-local restart must not remove the durable non-idempotent request guard.
	s.Native.Close()
	s.Native = nativeauth.NewManager()
	s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return fake, nil }
	expect(t, admin, "POST", "/native-signup", map[string]any{"account_id": unknownAccount, "version": 1}, 409)
	expect(t, admin, "POST", "/accounts/"+unknownAccount+"/native-auth", map[string]any{"version": 1}, 409)
	if fake.registers.Load() != count {
		t.Fatal("unknown registration replayed after restart")
	}
	fake.outcome.Store(2)
	retryAccount, retryID := start()
	step(retryAccount, retryID, "signup_sms", 202)
	wait(retryAccount, retryID, "signup_sms_required")
	step(retryAccount, retryID, "signup_submit", 202)
	wait(retryAccount, retryID, "signup_sms_required")
	var state string
	if err := db.Platform.QueryRow(ctx, `SELECT state FROM native_signup_attempts WHERE account_id=$1`, retryAccount).Scan(&state); err != nil || state != "rejected" {
		t.Fatal("conclusive rejection was not recorded", err)
	}
	fake.outcome.Store(0)
	fake.accountID.Store(457)
	step(retryAccount, retryID, "signup_submit", 202)
	wait(retryAccount, retryID, "registered")

	fake.outcome.Store(3)
	dnsAccount, dnsID := start()
	step(dnsAccount, dnsID, "signup_sms", 202)
	wait(dnsAccount, dnsID, "signup_sms_required")
	step(dnsAccount, dnsID, "signup_submit", 202)
	dnsView := wait(dnsAccount, dnsID, "signup_sms_required")
	var resultCode string
	if err := db.Platform.QueryRow(ctx, `SELECT state,result_code FROM native_signup_attempts
        WHERE account_id=$1`, dnsAccount).Scan(&state, &resultCode); err != nil ||
		state != "rejected" || resultCode != "native_dns_fake_ip" ||
		field(t, dnsView, "error") != "native_dns_fake_ip" {
		t.Fatal("pre-connect rejection became an unknown signup barrier", err)
	}
	fake.outcome.Store(0)
	fake.accountID.Store(459)
	step(dnsAccount, dnsID, "signup_submit", 202)
	wait(dnsAccount, dnsID, "registered")

	staleAccount, staleID := start()
	step(staleAccount, staleID, "signup_sms", 202)
	wait(staleAccount, staleID, "signup_sms_required")
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET version=version+1 WHERE id=$1`, staleAccount); err != nil {
		t.Fatal(err)
	}
	count = fake.registers.Load()
	step(staleAccount, staleID, "signup_submit", 202)
	wait(staleAccount, staleID, "signup_sms_required")
	if fake.registers.Load() != count {
		t.Fatal("stale account version reached remote registration")
	}
	expect(t, admin, "DELETE", "/accounts/"+staleAccount+"/native-auth/"+staleID, nil, 200)

	// Cancellation after submission cannot erase evidence of a known remote success.
	blocking := &isolatedSignup{start: make(chan struct{}), release: make(chan struct{})}
	blocking.accountID.Store(458)
	s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return blocking, nil }
	cancelAccount, cancelID := start()
	step(cancelAccount, cancelID, "signup_sms", 202)
	wait(cancelAccount, cancelID, "signup_sms_required")
	step(cancelAccount, cancelID, "signup_submit", 202)
	select {
	case <-blocking.start:
	case <-time.After(5 * time.Second):
		t.Fatal("registration did not submit")
	}
	expect(t, admin, "POST", "/accounts/"+cancelAccount+"/native-auth", map[string]any{"version": 1}, 409)
	expect(t, admin, "DELETE", "/accounts/"+cancelAccount+"/native-auth/"+cancelID, nil, 200)
	close(blocking.release)
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if err := db.Platform.QueryRow(ctx, `SELECT state FROM native_signup_attempts WHERE account_id=$1`, cancelAccount).Scan(&state); err == nil && state == "registered" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if state != "registered" {
		t.Fatal("cancellation lost a known remote outcome")
	}
	expect(t, admin, "POST", "/native-signup", map[string]any{"account_id": cancelAccount, "version": 1}, 409)
}
