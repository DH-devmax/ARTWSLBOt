package nativeauth

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

type signupClient struct {
	fakeClient
	prepared  *atomic.Bool
	registers atomic.Int32
	resultErr error
}

func (f *signupClient) Request(ctx context.Context, route string, in map[string]any) (map[string]json.RawMessage, error) {
	if route != "/v1/user/register" {
		return f.fakeClient.Request(ctx, route, in)
	}
	if !f.prepared.Load() {
		panic("registration sent before durable barrier")
	}
	f.registers.Add(1)
	f.params = in
	if f.resultErr != nil {
		return nil, f.resultErr
	}
	return map[string]json.RawMessage{"accountId": json.RawMessage(`456`)}, nil
}

func TestNativeSignupIsSeparateFromLogin(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	var prepared, recorded atomic.Bool
	f := &signupClient{prepared: &prepared}
	m.Factory = func(Deployment) (AuthClient, error) { return f, nil }
	view, err := m.StartSignup("owner", "account", syntheticDeployment(), SignupHooks{
		BeforeSubmit: func(context.Context, string) error { prepared.Store(true); return nil },
		Complete: func(_ context.Context, id, account string, err error) error {
			if account != "456" || err != nil {
				t.Error("invalid registration outcome")
			}
			recorded.Store(true)
			return nil
		},
	}, nil)
	if err != nil || view.Purpose != "signup" {
		t.Fatal(view, err)
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "password", Agreed: true, Captcha: "synthetic"}); err == nil {
		t.Fatal("login action accepted in signup transaction")
	}
	if same, err := m.StartSignup("owner", "account", syntheticDeployment(), testSignupHooks(), nil); err != nil || same.ID != view.ID {
		t.Fatal("same-purpose reopening replaced registration", err)
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "signup_sms", Account: "13800000000", Captcha: "synthetic"}); err == nil {
		t.Fatal("SMS sent without consent")
	}
	_, err = m.Action("owner", "account", view.ID, Input{Action: "signup_sms", Account: "13800000000", Nickname: "synthetic", Agreed: true, Captcha: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, view.ID, "signup_sms_required")
	if f.params["ty"] != "VERIFY_FOR_REGISTER" || f.params["Key"] != "" {
		t.Fatal("incorrect official registration SMS contract")
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "sms_verify", Code: "123456", Agreed: true, Captcha: "synthetic"}); err == nil {
		t.Fatal("registration SMS reused for login")
	}
	_, err = m.Action("owner", "account", view.ID, Input{
		Action: "signup_submit", Account: "13900000000", Password: "synthetic1", Code: "123456",
		Nickname: " synthetic ", Agreed: true, Captcha: "synthetic",
	})
	if err != nil {
		t.Fatal(err)
	}
	view = waitState(t, m, view.ID, "registered")
	if !recorded.Load() || view.RegisteredAccount != "456" || view.NativeUID != "" || f.registers.Load() != 1 {
		t.Fatal("registration was not recorded or was mistaken for login", view)
	}
	if f.params["password"] != "synthetic1" || f.params["passwd"] != nil ||
		f.params["nickName"] != "synthetic" || f.params["key"] != "synthetic-challenge" ||
		f.params["phone"].(map[string]any)["nationalNumber"] != int64(13800000000) {
		t.Fatal("registration request changed bound phone or official field names")
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "signup_submit", Agreed: true}); err == nil {
		t.Fatal("successful registration repeated")
	}
	if _, err = m.Start("owner", "account", syntheticDeployment(), nil); err != nil {
		t.Fatal("completed registration blocked subsequent login", err)
	}
}

func TestSignupUnknownOrUnrecordedOutcomeCannotRepeat(t *testing.T) {
	for _, recordFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "upstream-unknown", true: "record-failed"}[recordFailure], func(t *testing.T) {
			m := NewManager()
			t.Cleanup(m.Close)
			var prepared, recorded atomic.Bool
			f := &signupClient{prepared: &prepared}
			if !recordFailure {
				f.resultErr = &Error{Code: "native_transport_unknown", Submitted: true}
			}
			m.Factory = func(Deployment) (AuthClient, error) { return f, nil }
			view, err := m.StartSignup("owner", "account", syntheticDeployment(), SignupHooks{
				BeforeSubmit: func(context.Context, string) error { prepared.Store(true); return nil },
				Complete: func(context.Context, string, string, error) error {
					recorded.Store(true)
					if recordFailure {
						return rejection("synthetic-record-failure")
					}
					return nil
				},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = m.Action("owner", "account", view.ID, Input{Action: "signup_sms", Account: "13800000000", Nickname: "synthetic", Agreed: true, Captcha: "synthetic"})
			waitState(t, m, view.ID, "signup_sms_required")
			in := Input{Action: "signup_submit", Password: "synthetic1", Code: "123456", Nickname: "synthetic", Agreed: true, Captcha: "synthetic"}
			if _, err = m.Action("owner", "account", view.ID, in); err != nil {
				t.Fatal(err)
			}
			waitState(t, m, view.ID, "unknown")
			if _, err = m.Action("owner", "account", view.ID, in); err == nil || f.registers.Load() != 1 || !recorded.Load() {
				t.Fatal("unknown registration retried")
			}
		})
	}
}

func TestSignupInputPolicy(t *testing.T) {
	for _, password := range []string{"123456", "abcdef", "!@#$%^", "x1", "abcdef12345678901", "ab1\ncd", "      ", "ab1\u2028cd", "abc|12", "abc\u4e2d12"} {
		if signupPassword(password) {
			t.Fatal("accepted invalid registration password")
		}
	}
	if !signupPassword("abcdef1") || !signupPassword("abc!de") || signupNickname(" \n") || signupNickname("a\nb") {
		t.Fatal("incorrect registration input validation")
	}
}
