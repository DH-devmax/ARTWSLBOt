package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

type syntheticChiliSignup struct {
	syntheticChiliAuth
	sms    func() error
	signup func(string, string, string) (lajiaoliao.Session, error)
}

func (s syntheticChiliSignup) SignupSMS(context.Context, string, string, string) error {
	return s.sms()
}
func (s syntheticChiliSignup) Signup(_ context.Context, phone, code, device string) (lajiaoliao.Session, error) {
	return s.signup(phone, code, device)
}

func TestPostgresChiliSignupScopeCooldownAndUnknownBarrier(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("synthetic-signup-only")
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('chili-signup-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{87}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	var sms, signup atomic.Int32
	unknown := false
	s.chiliAuthClient = func() chiliAuthenticator {
		return syntheticChiliSignup{
			sms: func() error {
				sms.Add(1)
				if unknown {
					return &lajiaoliao.Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
				}
				return nil
			},
			signup: func(phone, code, device string) (lajiaoliao.Session, error) {
				signup.Add(1)
				if phone != "13800000000" || code != "123456" {
					t.Error("client changed server-bound phone or OTP")
				}
				return lajiaoliao.Session{UserID: "signup-synthetic-user", ChatToken: "fixture-chat",
					IMToken: "fixture-im", DeviceID: device}, nil
			},
		}
	}
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	login := func() {
		expect(t, admin, "POST", "/auth/login", map[string]string{
			"username": "chili-signup-admin", "password": "synthetic-signup-only"}, 200)
	}
	login()
	admin.workspace = "lajiaoliao"
	create := func() string {
		id := field(t, expect(t, admin, "POST", "/accounts", map[string]any{"name": "signup fixture"}, 201), "id")
		expect(t, admin, "POST", "/accounts/"+id+"/lajiaoliao-challenge", map[string]any{}, 200)
		return "/accounts/" + id
	}
	first := create()
	request := map[string]any{"version": 1, "phone": "13800000000", "agreed": true, "captcha_verify_param": ""}
	finish := map[string]any{"version": 1, "code": "123456", "agreed": true}
	expect(t, admin, "POST", first+"/lajiaoliao-signup", finish, 409)
	request["agreed"] = false
	expect(t, admin, "POST", first+"/lajiaoliao-signup-sms", request, 400)
	request["agreed"] = true
	expect(t, admin, "POST", first+"/lajiaoliao-signup-sms", request, 200)
	if signup.Load() != 0 || sms.Load() != 1 {
		t.Fatal("SMS triggered native registration")
	}
	projection := expect(t, admin, "GET", first+"/lajiaoliao-signup", nil, 200)
	if !bytes.Contains(projection, []byte(`"sms_ready":true`)) || bytes.Contains(projection, []byte("13800000000")) {
		t.Fatal("pending SMS not restorable or leaked phone")
	}
	expect(t, admin, "POST", first+"/lajiaoliao-signup-sms", request, 429)
	second := create()
	expect(t, admin, "POST", second+"/lajiaoliao-signup-sms", request, 429)
	forged := map[string]any{"version": 1, "code": "123456", "agreed": true, "phone": "13900000000"}
	expect(t, admin, "POST", first+"/lajiaoliao-signup", forged, 400)
	// A fresh website session cannot consume the previous session's OTP flow.
	login()
	expect(t, admin, "POST", first+"/lajiaoliao-signup", finish, 409)
	if _, err := db.Platform.Exec(ctx, `UPDATE lajiaoliao_auth_state SET sms_resend_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", first+"/lajiaoliao-signup-sms", request, 200)
	expect(t, admin, "POST", first+"/lajiaoliao-signup", finish, 200)
	expect(t, admin, "POST", first+"/lajiaoliao-signup", finish, 409)
	var complete bool
	if err := db.Platform.QueryRow(ctx, `SELECT a.auth_state='authenticated' AND NOT a.connection_enabled
        AND a.credential_sealed IS NOT NULL AND st.sms_phone_sealed IS NULL AND st.sms_expires_at IS NULL
        FROM accounts a JOIN lajiaoliao_auth_state st ON st.account_id=a.id
        WHERE a.id=$1`, first[len("/accounts/"):]).Scan(&complete); err != nil || !complete || signup.Load() != 1 {
		t.Fatal("incomplete storage, replay or implicit connection", err)
	}
	unknown = true
	request["phone"] = "13900000000"
	expect(t, admin, "POST", second+"/lajiaoliao-signup-sms", request, 409)
	count := sms.Load()
	expect(t, admin, "POST", second+"/lajiaoliao-signup-sms", request, 409)
	expect(t, admin, "POST", second+"/lajiaoliao-signup", finish, 409)
	if sms.Load() != count || signup.Load() != 1 {
		t.Fatal("unknown SMS was replayed")
	}
}
