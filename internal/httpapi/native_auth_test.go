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

type isolatedAuth struct {
	block bool
	start chan struct{}
	done  chan struct{}
	uid   uint64
	calls atomic.Int32
}

func (f *isolatedAuth) Request(context.Context, string, map[string]any) (map[string]json.RawMessage, error) {
	return nil, nil
}
func (f *isolatedAuth) Login(ctx context.Context, _ map[string]any) error {
	f.calls.Add(1)
	if f.block {
		close(f.start)
		select {
		case <-f.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (f *isolatedAuth) VerifySecurity(context.Context, string) error { return nil }
func (f *isolatedAuth) SecurityRequired() bool                       { return false }
func (f *isolatedAuth) Close()                                       {}
func (f *isolatedAuth) Finish(context.Context) (nativeauth.Session, string, error) {
	return nativeauth.Session{UID: f.uid, JWT: "synthetic-jwt-not-live", NIMID: "synthetic-peer", NIMToken: "synthetic-im-not-live"}, "synthetic-nickname", nil
}

func TestPostgresNativeAuth(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, err := security.Password("native-isolated-password-123")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"native-admin", "native-other-admin"} {
		if _, err = db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role) VALUES($1,$2,'superadmin')`, name, hash); err != nil {
			t.Fatal(err)
		}
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	s := New(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{"username": name, "password": "native-isolated-password-123"}, 200)
		return c
	}
	admin, other := login("native-admin"), login("native-other-admin")
	config := expect(t, admin, "GET", "/native-deployment", nil, 200)
	if !bytes.Contains(config, []byte(`"configured":false`)) {
		t.Fatal("unexpected deployment")
	}
	expect(t, admin, "POST", "/native-signup", map[string]any{}, 400)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	d := nativeauth.Deployment{Origin: "https://example.invalid", Headers: map[string]string{},
		Metadata: make([]uint64, 14), SigningSeed: key, BodyKey: key, ServerKey: key,
		CaptchaID: "synthetic-captcha", DeviceID: "synthetic-device", ProtocolVersion: "3.7.3"}
	encoded, _ := json.Marshal(d)
	expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 0, "configuration": string(encoded)}, 200)
	source := s.nativeRequestIDs(1)
	first, err := source(ctx, true, uint64(time.Now().UnixMilli()))
	if err != nil {
		t.Fatal(err)
	}
	// New allocator, same timestamp and a backward clock must still preserve the durable high-water mark.
	second, err := s.nativeRequestIDs(1)(ctx, true, first)
	if err != nil || second != first+1 {
		t.Fatal("request nonce was reused across allocator lifetimes", err)
	}
	third, err := source(ctx, true, 1)
	if err != nil || third != second+1 {
		t.Fatal("request nonce regressed after clock rollback", err)
	}
	if _, err = s.nativeRequestIDs(999)(ctx, true, third); err == nil {
		t.Fatal("stale deployment allocated request nonce")
	}
	if _, err = db.Tenant.Exec(ctx, `SELECT sealed FROM native_deployment`); err == nil {
		t.Fatal("tenant role read deployment secrets")
	}
	if _, err = db.Tenant.Exec(ctx, `SELECT credential_sealed FROM accounts`); err == nil {
		t.Fatal("tenant role read native session secrets")
	}
	expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 0, "configuration": string(encoded)}, 409)
	config = expect(t, admin, "GET", "/native-deployment", nil, 200)
	if bytes.Contains(config, []byte("signing_seed")) || bytes.Contains(config, []byte(key)) {
		t.Fatal("configuration secret exposed")
	}
	user := expect(t, admin, "POST", "/users", map[string]string{
		"username": "native-tenant", "password": "native-isolated-password-123", "tenant_name": "native-tenant",
	}, 201)
	if field(t, user, "tenant_id") == "" {
		t.Fatal("missing tenant")
	}
	tenant := login("native-tenant")
	expect(t, tenant, "GET", "/native-deployment", nil, 403)
	expect(t, tenant, "POST", "/native-signup", map[string]any{}, 403)
	newAccount := func(name string) string {
		return field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": name}, 201), "id")
	}
	start := func(c testClient, account string, version int) string {
		return field(t, expect(t, c, "POST", "/accounts/"+account+"/native-auth", map[string]any{"version": version}, 201), "transaction_id")
	}
	action := func(c testClient, account, id string) {
		expect(t, c, "POST", "/accounts/"+account+"/native-auth/"+id, map[string]string{
			"action": "password", "account": "synthetic", "password": "synthetic-not-live", "captcha": "synthetic-human",
		}, 202)
	}
	wait := func(c testClient, account, id, state string) json.RawMessage {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		var result json.RawMessage
		for time.Now().Before(deadline) {
			result = expect(t, c, "GET", "/accounts/"+account+"/native-auth/"+id, nil, 200)
			if field(t, result, "status") == state {
				return result
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("transaction did not reach %s: %s", state, result)
		return nil
	}
	var uid uint64 = 123
	s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) {
		return &isolatedAuth{uid: uid}, nil
	}
	account := newAccount("native-success")
	expect(t, tenant, "POST", "/accounts/"+account+"/native-auth", map[string]any{"version": 1}, 403)
	id := start(admin, account, 1)
	if same := start(admin, account, 1); same != id {
		t.Fatal("duplicate start did not reuse transaction")
	}
	expect(t, other, "GET", "/accounts/"+account+"/native-auth/"+id, nil, 404)
	expect(t, other, "POST", "/accounts/"+account+"/native-auth", map[string]any{"version": 1}, 409)
	expect(t, other, "POST", "/native-signup", map[string]any{"account_id": account, "version": 1}, 409)
	action(admin, account, id)
	wait(admin, account, id, "authenticated")
	var sealed []byte
	var state, nativeID string
	var version int
	if err = db.Platform.QueryRow(ctx, `SELECT credential_sealed,auth_state,native_uid,version FROM accounts WHERE id=$1`, account).
		Scan(&sealed, &state, &nativeID, &version); err != nil {
		t.Fatal(err)
	}
	if state != "authenticated" || nativeID != "123" || version != 2 || bytes.Contains(sealed, []byte("synthetic-jwt")) {
		t.Fatal("incorrect credential persistence")
	}
	plain, err := vault.Open("native-session:"+account, sealed)
	if err != nil || !bytes.Contains(plain, []byte("synthetic-jwt-not-live")) {
		t.Fatal("sealed session not recoverable")
	}
	clear(plain)
	if _, err = vault.Open("native-session:wrong-account", sealed); err == nil {
		t.Fatal("session encryption not bound to account")
	}
	list := expect(t, admin, "GET", "/accounts", nil, 200)
	if strings.Contains(string(list), "synthetic-jwt") || strings.Contains(string(list), "credential_sealed") {
		t.Fatal("session exposed in account list")
	}
	duplicate := newAccount("native-duplicate")
	duplicateTx := start(admin, duplicate, 1)
	action(admin, duplicate, duplicateTx)
	failed := wait(admin, duplicate, duplicateTx, "connection_pending")
	if field(t, failed, "error") != "native_identity_already_bound" {
		t.Fatal("duplicate native identity accepted")
	}
	// A paused login must recheck the account version and original site session at commit.
	for _, mode := range []string{"version", "logout", "deployment", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			uid++
			fake := &isolatedAuth{uid: uid, block: true, start: make(chan struct{}), done: make(chan struct{})}
			s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return fake, nil }
			c := login("native-admin")
			a := newAccount("native-fence-" + mode)
			tx := start(c, a, 1)
			action(c, a, tx)
			select {
			case <-fake.start:
			case <-time.After(3 * time.Second):
				t.Fatal("login did not start")
			}
			switch mode {
			case "version":
				expect(t, admin, "PATCH", "/accounts/"+a, map[string]any{"version": 1, "testing_enabled": true}, 200)
			case "logout":
				expect(t, c, "POST", "/auth/logout", map[string]any{}, 200)
			case "deployment":
				expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 1, "configuration": string(encoded)}, 200)
			case "cancel":
				expect(t, c, "DELETE", "/accounts/"+a+"/native-auth/"+tx, nil, 200)
			}
			close(fake.done)
			if mode == "version" {
				result := wait(c, a, tx, "connection_pending")
				if field(t, result, "error") != "native_account_changed" {
					t.Fatal("account version not rechecked")
				}
			}
			if mode == "logout" {
				// A separate session of the same admin can inspect, but cannot revive the expired commit session.
				result := wait(admin, a, tx, "connection_pending")
				if field(t, result, "error") != "native_site_session_expired" {
					t.Fatal("site session not rechecked")
				}
			}
			if mode == "cancel" || mode == "deployment" {
				expect(t, admin, "GET", "/accounts/"+a+"/native-auth/"+tx, nil, 404)
			}
			var has bool
			if err = db.Platform.QueryRow(ctx, `SELECT credential_sealed IS NOT NULL FROM accounts WHERE id=$1`, a).Scan(&has); err != nil || has {
				t.Fatal("revoked login persisted credentials", err)
			}
		})
	}
	expect(t, admin, "POST", "/accounts/"+account+"/forget-session", map[string]any{"version": 3}, 200)
	var forgotten bool
	if err = db.Platform.QueryRow(ctx, `SELECT credential_sealed IS NULL AND auth_state='not_authenticated' AND native_uid='123' FROM accounts WHERE id=$1`, account).Scan(&forgotten); err != nil || !forgotten {
		t.Fatal("forget did not erase credential while preserving native identity")
	}
}
