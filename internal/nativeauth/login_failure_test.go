package nativeauth

import (
	"context"
	"testing"
)

func TestLoginFailuresIdentifyTheFailedStep(t *testing.T) {
	upstream := &Error{Code: "native_login_rejected_unspecified", Number: -1, Submitted: true}
	for _, test := range []struct {
		stage, state string
		fake         *fakeClient
		commitErr    error
	}{
		{"password_login", "failed", &fakeClient{loginErr: upstream}, nil},
		{"device_verification", "failed", &fakeClient{device: true, requestErr: upstream}, nil},
		{"im_credentials", "connection_pending", &fakeClient{finishErr: upstream}, nil},
		{"session_save", "connection_pending", &fakeClient{}, rejection("native_auth_failed")},
	} {
		t.Run(test.stage, func(t *testing.T) {
			m := NewManager()
			t.Cleanup(m.Close)
			f := test.fake
			m.Factory = func(Deployment) (AuthClient, error) { return f, nil }
			view, err := m.Start("owner", "account", syntheticDeployment(), func(context.Context, Session, string) error {
				return test.commitErr
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Action("owner", "account", view.ID, Input{
				Action: "password", Account: "13800000000", Password: "synthetic-pass", Captcha: "synthetic-human",
			})
			if err != nil {
				t.Fatal(err)
			}
			view = waitState(t, m, view.ID, test.state)
			if view.FailureStage != test.stage || view.Error == "" {
				t.Fatalf("wrong failure stage: %s (%s)", view.FailureStage, view.Error)
			}
			if test.stage == "password_login" &&
				(view.ProviderCode != -1 || f.params["type"] != "LOGIN_TYPE_PHONE_PWD" ||
					f.params["passwd"] != "synthetic-pass" || f.calls.Load() != 1) {
				t.Fatal("phone password login changed its payload or retried")
			}
			if _, err := m.Poll("another-owner", "account", view.ID); err == nil {
				t.Fatal("login failure exposed to another user")
			}
		})
	}
}

func TestSMSAndSecurityFailuresDoNotMasqueradeAsPasswordFailures(t *testing.T) {
	upstream := &Error{Code: "native_business_rejected", Number: -1, Submitted: true}
	for _, test := range []struct {
		stage, action, prepare, state string
		fake                          *fakeClient
	}{
		{"login_sms", "sms_request", "", "failed", &fakeClient{requestErr: upstream}},
		{"sms_login", "sms_verify", "sms_request", "sms_required", &fakeClient{loginErr: upstream}},
		{"security_verification", "security_verify", "password", "security_required", &fakeClient{securityErr: upstream}},
	} {
		t.Run(test.stage, func(t *testing.T) {
			m := NewManager()
			t.Cleanup(m.Close)
			f := test.fake
			if test.prepare == "password" {
				f.safe.Store(true)
			}
			m.Factory = func(Deployment) (AuthClient, error) { return f, nil }
			view, err := m.Start("owner", "account", syntheticDeployment(), nil)
			if err != nil {
				t.Fatal(err)
			}
			input := Input{Account: "13800000000", Password: "synthetic-pass",
				Captcha: "synthetic-human", Code: "123456"}
			if test.prepare != "" {
				input.Action = test.prepare
				if _, err = m.Action("owner", "account", view.ID, input); err != nil {
					t.Fatal(err)
				}
				waitState(t, m, view.ID, test.state)
			}
			input.Action = test.action
			if _, err = m.Action("owner", "account", view.ID, input); err != nil {
				t.Fatal(err)
			}
			view = waitState(t, m, view.ID, test.state)
			if view.FailureStage != test.stage || view.ProviderCode != -1 {
				t.Fatalf("lost step or provider code: %s (%d)", view.FailureStage, view.ProviderCode)
			}
		})
	}
}
