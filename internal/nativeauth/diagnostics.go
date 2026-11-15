package nativeauth

import (
	"context"
	"time"
)

// FailureDiagnostic contains only control-owned scope and bounded diagnostic
// enums. It deliberately excludes the authentication transaction handle.
type FailureDiagnostic struct {
	ActorID           string `json:"-"`
	AccountID         string `json:"-"`
	Purpose           string `json:"purpose"`
	FailureStage      string `json:"failure_stage"`
	Error             string `json:"error"`
	ProviderCode      int    `json:"provider_code,omitempty"`
	Diagnostic        string `json:"diagnostic"`
	DiagnosticVersion int    `json:"diagnostic_version"`
}

type DiagnosticRecorder func(context.Context, FailureDiagnostic)

func failureDiagnostic(owner, account string, view View) (FailureDiagnostic, bool) {
	if owner == "" || account == "" || safeAuthDiagnostic(view.Diagnostic) == "" {
		return FailureDiagnostic{}, false
	}
	switch view.Error {
	case "native_login_rejected_unspecified", "native_signup_rejected_unspecified",
		"native_sms_rejected_unspecified", "native_invalid_credentials", "native_invalid_sms",
		"native_verification_rejected", "native_verification_expired", "native_signup_phone_registered",
		"native_sms_rate_limited", "native_rate_limited", "native_captcha_rejected",
		"native_phone_range_restricted", "native_phone_registration_restricted",
		"native_request_parameters_rejected", "native_request_parameters_missing",
		"native_phone_not_found", "native_client_version_rejected", "native_ip_restricted",
		"native_transport_unknown", "native_mobile_login_response_invalid":
	default:
		return FailureDiagnostic{}, false
	}
	switch view.Purpose {
	case "signup":
		if view.FailureStage != "signup_sms" && view.FailureStage != "signup_submit" {
			return FailureDiagnostic{}, false
		}
	case "login":
		switch view.FailureStage {
		case "password_login", "device_verification", "login_sms", "sms_login",
			"security_verification", "im_credentials":
		default:
			return FailureDiagnostic{}, false
		}
	default:
		return FailureDiagnostic{}, false
	}
	number := 0
	switch view.ProviderCode {
	case 1000, 1001, 1002, 1030, 1036, 1037, 1038, 1039, 1040, 1043, 1044, 1047, 1200, 1201:
		number = view.ProviderCode
	}
	return FailureDiagnostic{ActorID: owner, AccountID: account, Purpose: view.Purpose,
		FailureStage: view.FailureStage, Error: view.Error, ProviderCode: number,
		Diagnostic: view.Diagnostic, DiagnosticVersion: AuthDiagnosticVersion}, true
}

func (m *Manager) recordFailure(owner, account string, view View) {
	if m.diagnostics == nil {
		return
	}
	diagnostic, ok := failureDiagnostic(owner, account, view)
	if !ok {
		return
	}
	// Recording never holds the manager lock, retries a native request or
	// extends its authentication deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	m.diagnostics(ctx, diagnostic)
}
