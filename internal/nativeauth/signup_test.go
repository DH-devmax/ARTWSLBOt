package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

type signupClient struct {
	fakeClient
	prepared  *atomic.Bool
	registers atomic.Int32
	resultErr error
}

func TestSignupSMSDiagnosticsReachViewWithoutRetryOrSecrets(t *testing.T) {
	for _, diagnostic := range []string{"provider_reason_empty", "provider_reason_unrecognized",
		"provider_reason_classified", "provider_code_classified", "provider_reason_nontext",
		"provider_reason_oversized", "13800000000 token=synthetic-secret"} {
		t.Run(diagnostic, func(t *testing.T) {
			m := NewManager()
			t.Cleanup(m.Close)
			f := &fakeClient{requestErr: &Error{Code: "native_sms_rejected_unspecified",
				Number: 1001, Submitted: true, Diagnostic: diagnostic}}
			m.Factory = func(Deployment) (AuthClient, error) { return f, nil }
			view, err := m.StartSignup("owner", "account", syntheticDeployment(), testSignupHooks(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Action("owner", "account", view.ID, Input{Action: "signup_sms",
				Account: "13800000000", Agreed: true, Captcha: "synthetic-ticket"})
			if err != nil {
				t.Fatal(err)
			}
			view = waitState(t, m, view.ID, "failed")
			if view.FailureStage != "signup_sms" || view.ProviderCode != 1001 ||
				view.Diagnostic != safeAuthDiagnostic(diagnostic) || f.calls.Load() != 1 {
				t.Fatal("safe signup diagnostic lost")
			}
			wantVersion := 0
			if view.Diagnostic != "" {
				wantVersion = AuthDiagnosticVersion
			}
			if view.DiagnosticVersion != wantVersion {
				t.Fatal("safe diagnostic version missing or unsafe result versioned")
			}
			raw, _ := json.Marshal(view)
			for _, secret := range []string{"13800000000", "synthetic-secret", "synthetic-ticket"} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Fatal("secret reached transaction view")
				}
			}
			for range 3 {
				if _, err := m.Poll("owner", "account", view.ID); err != nil {
					t.Fatal(err)
				}
			}
			time.Sleep(20 * time.Millisecond)
			if f.calls.Load() != 1 {
				t.Fatal("polling retried registration SMS")
			}
		})
	}
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
	_, err = m.Action("owner", "account", view.ID, Input{Action: "signup_sms", Account: "13800000000", Agreed: true, Captcha: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, view.ID, "signup_sms_required")
	phone, ok := f.params["phone"].(map[string]any)
	if f.params["ty"] != "VERIFY_FOR_REGISTER" || f.params["Key"] != nil || len(f.params) != 3 ||
		!ok || phone["countryCode"] != "86" || phone["nationalNumber"] != "13800000000" ||
		phone["maskedNationalNumber"] != "138****0000" || len(phone) != 3 {
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
		f.params["nickName"] != nil || f.params["key"] != "synthetic-challenge" || len(f.params) != 5 ||
		f.params["phone"].(map[string]any)["nationalNumber"] != "13800000000" ||
		f.params["phone"].(map[string]any)["countryCode"] != "86" ||
		f.params["phone"].(map[string]any)["maskedNationalNumber"] != "138****0000" {
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
	if !signupPassword("abcdef1") || !signupPassword("abc!de") {
		t.Fatal("incorrect registration input validation")
	}
}
