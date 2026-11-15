package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFailureDiagnosticUsesOnlyBoundedMetadata(t *testing.T) {
	view := View{ID: "synthetic-transaction-ticket", AccountID: "account", Purpose: "signup",
		FailureStage: "signup_sms", Error: "native_sms_rejected_unspecified", ProviderCode: 1001,
		Diagnostic: "provider_reason_unrecognized", DiagnosticVersion: AuthDiagnosticVersion,
		CaptchaID: "synthetic-captcha", Nickname: "synthetic-private-name"}
	event, ok := failureDiagnostic("owner", "account", view)
	if !ok || event.ActorID != "owner" || event.AccountID != "account" || event.ProviderCode != 1001 {
		t.Fatal("trusted scope or business code missing")
	}
	data, _ := json.Marshal(event)
	var fields map[string]any
	json.Unmarshal(data, &fields)
	if len(fields) != 6 {
		t.Fatal("unexpected audit payload fields")
	}
	for _, value := range []string{"synthetic-transaction-ticket", "synthetic-captcha", "synthetic-private-name", "owner", "account_id"} {
		if strings.Contains(string(data), value) {
			t.Fatal("authentication content reached diagnostic audit")
		}
	}
	for _, field := range []string{"purpose", "stage", "error", "diagnostic", "scope"} {
		modified := view
		owner := "owner"
		switch field {
		case "purpose":
			modified.Purpose = "13800000000"
		case "stage":
			modified.FailureStage = "token=synthetic-secret"
		case "error":
			modified.Error = "password=synthetic-password"
		case "diagnostic":
			modified.Diagnostic = "otp=123456"
		case "scope":
			owner = ""
		}
		if _, ok := failureDiagnostic(owner, "account", modified); ok {
			t.Fatal("unbounded diagnostic accepted")
		}
	}
	view.ProviderCode = 13800000000
	event, ok = failureDiagnostic("owner", "account", view)
	if !ok || event.ProviderCode != 0 {
		t.Fatal("unverified numeric value persisted")
	}
	view.Purpose = "login"
	if _, ok = failureDiagnostic("owner", "account", view); ok {
		t.Fatal("signup stage persisted as a login")
	}
}

func TestFailureRecordingDoesNotLockReplayOrRetainTransaction(t *testing.T) {
	recorded := make(chan FailureDiagnostic, 1)
	release := make(chan struct{})
	done := make(chan struct{})
	m := NewManagerWithDiagnostics(func(ctx context.Context, event FailureDiagnostic) {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 4*time.Second {
			t.Error("diagnostic recorder has no bounded deadline")
		}
		recorded <- event
		<-release
		close(done)
	})
	t.Cleanup(m.Close)
	fake := &fakeClient{requestErr: &Error{Code: "native_sms_rejected_unspecified", Number: 1001,
		Diagnostic: "provider_reason_unrecognized", Submitted: true}}
	m.Factory = func(Deployment) (AuthClient, error) { return fake, nil }
	view, err := m.StartSignup("owner", "account", syntheticDeployment(), testSignupHooks(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "signup_sms", Account: "13800000000",
		Captcha: "synthetic-ticket", Agreed: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-recorded:
		if event.FailureStage != "signup_sms" || event.DiagnosticVersion != AuthDiagnosticVersion {
			t.Error("safe outcome missing")
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("diagnostic was not recorded")
	}
	for range 3 {
		if _, err = m.Poll("owner", "account", view.ID); err != nil {
			t.Error("recorder holds manager lock")
		}
	}
	if _, err = m.Cancel("other-owner", "account", view.ID); err == nil {
		t.Error("foreign actor cancelled authentication")
	}
	if _, err = m.Cancel("owner", "account", view.ID); err != nil {
		t.Error(err)
	}
	close(release)
	<-done
	if fake.calls.Load() != 1 || len(recorded) != 0 {
		t.Fatal("diagnostic recording replayed or duplicated native SMS")
	}
	if _, err = m.Poll("owner", "account", view.ID); err == nil {
		t.Fatal("diagnostic recording extended authentication lifetime")
	}
}

func TestMobileResponseFailureDiagnosticsDoNotRetry(t *testing.T) {
	recorded := make(chan FailureDiagnostic, 1)
	m := NewMobileManagerWithDiagnostics(func(_ context.Context, event FailureDiagnostic) { recorded <- event })
	t.Cleanup(m.Close)
	fake := &fakeClient{loginErr: mobileLoginFailure("login_account_mismatch")}
	m.Factory = func(Deployment) (AuthClient, error) { return fake, nil }
	view, err := m.Start("owner", "account", syntheticMobileDeployment(),
		func(context.Context, Session, string) error {
			t.Error("unverified mobile response reached persistence")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Action("owner", "account", view.ID, Input{Action: "password", Account: "456",
		Password: "synthetic-private-password", Captcha: "synthetic-private-captcha"}); err != nil {
		t.Fatal(err)
	}
	view = waitState(t, m, view.ID, "unknown")
	if view.Diagnostic != "login_account_mismatch" || view.DiagnosticVersion != AuthDiagnosticVersion ||
		view.FailureStage != "password_login" {
		t.Fatal("mobile mismatch did not retain its safe classification")
	}
	select {
	case event := <-recorded:
		if event.Diagnostic != "login_account_mismatch" || event.Error != "native_mobile_login_response_invalid" {
			t.Fatal("wrong diagnostic persisted")
		}
		raw, _ := json.Marshal(event)
		for _, secret := range []string{"synthetic-private-password", "synthetic-private-captcha", view.ID, "456"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatal("sensitive mobile data reached the audit payload")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mobile diagnostic not recorded")
	}
	for range 3 {
		if _, err := m.Poll("owner", "account", view.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Action("owner", "account", view.ID, Input{Action: "password",
		Account: "456", Password: "synthetic-private-password", Captcha: "synthetic-private-captcha"}); err == nil ||
		fake.calls.Load() != 1 {
		t.Fatal("mobile unknown response was automatically retried")
	}
}
