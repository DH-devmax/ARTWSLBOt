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

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

type syntheticChiliAuth struct {
	login func(context.Context, string) (lajiaoliao.Session, error)
}

func (s syntheticChiliAuth) Challenge(context.Context, string) (lajiaoliao.Challenge, error) {
	return lajiaoliao.Challenge{Required: false}, nil
}
func (s syntheticChiliAuth) Login(ctx context.Context, _, _, _, device, _ string) (lajiaoliao.Session, error) {
	return s.login(ctx, device)
}
func (syntheticChiliAuth) Close() {}

func TestPostgresChiliManualLoginPersistenceAndFences(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("synthetic-chili-only")
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)VALUES('chili-auth-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{79}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	var calls atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s.chiliAuthClient = func() chiliAuthenticator {
		return syntheticChiliAuth{login: func(ctx context.Context, device string) (lajiaoliao.Session, error) {
			n := calls.Add(1)
			if n == 2 {
				entered <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return lajiaoliao.Session{}, ctx.Err()
				}
			}
			return lajiaoliao.Session{UserID: "synthetic-chili-user", ChatToken: "synthetic-secret-chat",
				IMToken: "synthetic-secret-im", DeviceID: device}, nil
		}}
	}
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "chili-auth-admin", "password": "synthetic-chili-only"}, 200)
	admin.workspace = "lajiaoliao"
	account := func() string {
		id := field(t, expect(t, admin, "POST", "/accounts", map[string]any{"name": "synthetic-auth"}, 201), "id")
		expect(t, admin, "POST", "/accounts/"+id+"/lajiaoliao-challenge", map[string]any{}, 200)
		return id
	}
	payload := map[string]any{"version": 1, "phone": "13800000000", "password": "synthetic-not-persisted", "captcha_verify_param": ""}
	first := account()
	expect(t, admin, "POST", "/accounts/"+first+"/lajiaoliao-login", payload, 200)
	var sealed []byte
	var epoch, version int64
	if err := db.Platform.QueryRow(ctx, `SELECT credential_sealed,epoch,version FROM accounts WHERE id=$1`, first).Scan(&sealed, &epoch, &version); err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open("lajiaoliao-session:"+first, sealed)
	if err != nil || epoch != 2 || version != 2 {
		t.Fatal("login did not preserve a sealed complete session with new fences", err)
	}
	defer clear(plain)
	var saved lajiaoliao.Session
	if json.Unmarshal(plain, &saved) != nil || saved.ChatToken == "" || saved.IMToken == "" || saved.DeviceID == "" {
		t.Fatal("partial native session persisted")
	}
	projection := expect(t, admin, "GET", "/accounts", nil, 200)
	if bytes.Contains(projection, []byte("synthetic-secret")) || bytes.Contains(projection, []byte("synthetic-not-persisted")) {
		t.Fatal("native credential exposed")
	}
	second := account()
	type response struct {
		status int
		code   string
	}
	done := make(chan response, 1)
	go func() {
		status, _, code := admin.request("POST", "/accounts/"+second+"/lajiaoliao-login", payload)
		done <- response{status, code}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("synthetic login did not enter")
	}
	expect(t, admin, "POST", "/accounts/"+second+"/lajiaoliao-login", payload, 409)
	expect(t, admin, "POST", "/accounts/"+second+"/forget-session", map[string]any{"version": 1}, 409)
	// A settings/epoch change during native work must invalidate final storage.
	if _, err = db.Platform.Exec(ctx, `UPDATE accounts SET version=version+1,epoch=epoch+1 WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case result := <-done:
		if result.status != 409 || result.code != "version_conflict" {
			t.Fatal("stale login stored a session", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("synthetic login did not finish")
	}
	var blocked bool
	if err = db.Platform.QueryRow(ctx, `SELECT a.credential_sealed IS NULL AND st.state='unknown'
        FROM accounts a JOIN lajiaoliao_auth_state st ON st.account_id=a.id WHERE a.id=$1`, second).Scan(&blocked); err != nil || !blocked || calls.Load() != 2 {
		t.Fatal("unknown login lost review state or was retried", err)
	}
	expect(t, admin, "POST", "/accounts/"+second+"/lajiaoliao-challenge", map[string]any{}, 409)
}
