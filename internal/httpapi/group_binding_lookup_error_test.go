package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
)

func TestBindingOutboundLookupDiagnosticsAreTypedAndPrivate(t *testing.T) {
	for _, tc := range []struct {
		code, category, public string
		number                 int
		status                 int
	}{
		{"native_business_rejected", "upstream_rejected", "group_binding_outbound_rejected", -1, 409},
		{"native_rate_limited", "rate_limited", "group_binding_outbound_rate_limited", 1043, 429},
		{"native_reauth_required", "session_expired", "native_mobile_reauth_required", 1053, 409},
		{"native_request_parameters_rejected", "parameters_rejected", "group_binding_outbound_parameters_rejected", 1000, 409},
		{"native_captcha_rejected", "security_verification", "group_binding_outbound_security_required", 1040, 409},
		{"native_client_version_rejected", "client_version", "group_binding_outbound_client_version", 1200, 409},
		{"native_ip_restricted", "ip_restricted", "group_binding_outbound_ip_restricted", 1201, 409},
		{"native_response_invalid", "response_invalid", "group_binding_outbound_response_invalid", 0, 502},
		{"native_onboarding_outbound_changed", "response_invalid", "group_binding_outbound_target_inconsistent", 0, 409},
		{"native_transport_unknown", "transport_unavailable", "group_binding_outbound_unavailable", 502, 503},
	} {
		for _, stage := range []string{"search", "friend_info"} {
			upstream := fmt.Errorf("private query=13800000000 token=secret: %w",
				&nativeauth.OnboardingLookupError{Stage: stage, Cause: &nativeauth.Error{
					Code: tc.code, Number: tc.number, Diagnostic: "provider_reason_unrecognized",
					Submitted: true, RetryAfter: 30 * time.Second}})
			err := bindingOutboundLookupError(upstream)
			var failure *control.GroupBindingLookupFailure
			if !errors.As(err, &failure) || failure.Failure.Code != tc.public ||
				failure.Diagnostic.Category != tc.category || failure.Diagnostic.Stage != stage {
				t.Fatal("typed lookup diagnosis lost safe cause", err)
			}
			w := httptest.NewRecorder()
			sendError(w, err)
			var response struct {
				Msg  string `json:"msg"`
				Data struct {
					Diagnostic control.GroupBindingLookupDiagnostic `json:"lookup_diagnostic"`
				} `json:"data"`
			}
			if w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Msg != tc.public ||
				response.Data.Diagnostic.ProviderCode != tc.number || response.Data.Diagnostic.ObservedAt.IsZero() ||
				w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "secret") ||
				strings.Contains(w.Body.String(), "13800000000") {
				t.Fatal("lookup error response leaked content or lost actionable metadata")
			}
			var writeResult *nativeauth.Error
			if errors.As(bindingOutboundSendError(upstream), &writeResult) {
				t.Fatal("pre-submit lookup failure became an uncertain friend mutation")
			}
		}
	}
}

func TestBindingOutboundLookupRejectsUnreviewedDiagnosticMetadata(t *testing.T) {
	for _, stage := range []string{"search", "friend_info", "private-stage-with-token"} {
		err := bindingOutboundLookupError(&nativeauth.OnboardingLookupError{Stage: stage,
			Cause: &nativeauth.Error{Code: "native_business_rejected", Number: 13800000000,
				Diagnostic: "private-token=synthetic", Submitted: true}})
		w := httptest.NewRecorder()
		sendError(w, err)
		if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "synthetic") ||
			strings.Contains(w.Body.String(), "13800000000") {
			t.Fatal("unreviewed diagnostic metadata escaped response")
		}
		if stage == "private-stage-with-token" && strings.Contains(w.Body.String(), "lookup_diagnostic") {
			t.Fatal("unreviewed stage became public metadata")
		}
	}
	w := httptest.NewRecorder()
	sendError(w, bindingOutboundLookupError(&nativeauth.Error{Code: "native_business_rejected", Number: -1}))
	if strings.Contains(w.Body.String(), "lookup_diagnostic") {
		t.Fatal("an unscoped error invented a failing lookup phase")
	}
}

func TestBindingOutboundLookupIdentityReasonsAreBoundedAndRemainRejected(t *testing.T) {
	for _, field := range []string{"uid", "account"} {
		for _, state := range []string{"missing", "null", "zero", "invalid", "mismatch"} {
			reason := "friend_info_" + field + "_" + state
			t.Run(reason, func(t *testing.T) {
				cause := &nativeauth.OnboardingLookupError{
					Stage: "friend_info", Reason: reason,
					Cause: &nativeauth.Error{Code: "native_onboarding_outbound_changed",
						Diagnostic: "synthetic-private-identity"}}
				w := httptest.NewRecorder()
				sendError(w, bindingOutboundLookupError(cause))
				var response struct {
					Msg  string `json:"msg"`
					Data struct {
						Diagnostic control.GroupBindingLookupDiagnostic `json:"lookup_diagnostic"`
					} `json:"data"`
				}
				if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &response) != nil ||
					response.Msg != "group_binding_outbound_target_inconsistent" ||
					response.Data.Diagnostic.Stage != "friend_info" ||
					response.Data.Diagnostic.Category != "response_invalid" ||
					response.Data.Diagnostic.Reason != reason ||
					w.Header().Get("Cache-Control") != "no-store" ||
					strings.Contains(w.Body.String(), "synthetic-private-identity") {
					t.Fatal("identity diagnosis leaked content or lost the original rejection", w.Body.String())
				}
				var failure *control.Failure
				if !errors.As(bindingOutboundSendError(cause), &failure) || failure.Status != 409 ||
					failure.Code != "group_binding_outbound_target_inconsistent" {
					t.Fatal("pre-submit identity failure became a submitted mutation")
				}
			})
		}
	}
}

func TestBindingOutboundLookupIdentityReasonRequiresReviewedContext(t *testing.T) {
	for _, tc := range []struct {
		stage, code, reason string
	}{
		{"friend_info", "native_onboarding_outbound_changed", "friend_info_uid_zero synthetic-private"},
		{"search", "native_onboarding_outbound_changed", "friend_info_uid_zero"},
		{"friend_info", "native_business_rejected", "friend_info_uid_zero"},
	} {
		w := httptest.NewRecorder()
		sendError(w, bindingOutboundLookupError(&nativeauth.OnboardingLookupError{
			Stage: tc.stage, Reason: tc.reason,
			Cause: &nativeauth.Error{Code: tc.code, Diagnostic: "synthetic-private"}}))
		var response struct {
			Data struct {
				Diagnostic control.GroupBindingLookupDiagnostic `json:"lookup_diagnostic"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &response) != nil ||
			response.Data.Diagnostic.Reason != "" || strings.Contains(w.Body.String(), "synthetic-private") {
			t.Fatal("unreviewed identity reason or context escaped the HTTP boundary")
		}
	}
}
