package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"wangshangliao/internal/control"
	"wangshangliao/internal/lajiaoliao"
)

func TestBindingChiliLookupDiagnosticStagesAndNoMutation(t *testing.T) {
	for _, stage := range []string{"search", "friend_info", "sent_requests"} {
		t.Run(stage, func(t *testing.T) {
			cause := &lajiaoliao.OnboardingLookupError{
				Stage: stage,
				Cause: &lajiaoliao.Failure{Code: "lajiaoliao_onboarding_outbound_shape", Submitted: true},
			}
			err := bindingOutboundLookupError(fmt.Errorf("phone=13800000000 token=synthetic-secret: %w", cause))
			var diagnostic *control.GroupBindingLookupFailure
			if !errors.As(err, &diagnostic) || diagnostic.Diagnostic.Platform != "lajiaoliao" ||
				diagnostic.Diagnostic.Stage != stage || diagnostic.Diagnostic.Category != "response_invalid" ||
				diagnostic.Diagnostic.ProviderCode != 0 {
				t.Fatal("missing bounded stage diagnostic", err)
			}
			if stage == "sent_requests" {
				other := diagnostic.Diagnostic
				other.Platform = "wsl"
				if _, ok := control.SafeGroupBindingLookupDiagnostic(other); ok {
					t.Fatal("Chili-only stage escaped to another platform")
				}
			}
			raw, marshalErr := json.Marshal(diagnostic.Diagnostic)
			if marshalErr != nil || strings.Contains(string(raw), "Cause") {
				t.Fatal("cause escaped safe diagnostic")
			}
			response := httptest.NewRecorder()
			sendError(response, err)
			if response.Code != 502 || strings.Contains(response.Body.String(), "13800000000") ||
				strings.Contains(response.Body.String(), "synthetic-secret") ||
				!strings.Contains(response.Body.String(), `"lookup_diagnostic"`) {
				t.Fatal("lookup response leaked private cause or lost safe diagnostic")
			}
			var mutation *lajiaoliao.Failure
			if errors.As(bindingOutboundSendError(cause), &mutation) && mutation.Submitted {
				t.Fatal("pre-submit relationship read became an unknown mutation")
			}
		})
	}
	for _, stage := range []string{"", "arbitrary-raw-response"} {
		err := bindingOutboundLookupError(&lajiaoliao.OnboardingLookupError{
			Stage: stage, Cause: &lajiaoliao.Failure{Code: "lajiaoliao_onboarding_outbound_shape"},
		})
		var diagnostic *control.GroupBindingLookupFailure
		if errors.As(err, &diagnostic) {
			t.Fatal("unreviewed stage escaped whitelist")
		}
	}
}

func TestBindingChiliLookupDiagnosticsMatchActualClientFailures(t *testing.T) {
	for _, tc := range []struct {
		code, category, public string
		status                 int
	}{
		{"lajiaoliao_request_rejected", "upstream_rejected", "group_binding_outbound_rejected", 409},
		{"lajiaoliao_token_expired", "session_expired", "lajiaoliao_reauth_required", 409},
		{"lajiaoliao_kicked", "session_expired", "lajiaoliao_reauth_required", 409},
		{"lajiaoliao_reauth_required", "session_expired", "lajiaoliao_reauth_required", 409},
		{"lajiaoliao_rate_limited", "rate_limited", "group_binding_outbound_rate_limited", 429},
		{"lajiaoliao_response_invalid", "response_invalid", "group_binding_outbound_response_invalid", 502},
		{"lajiaoliao_result_unknown", "transport_unavailable", "group_binding_outbound_unavailable", 503},
	} {
		t.Run(tc.code, func(t *testing.T) {
			cause := &lajiaoliao.OnboardingLookupError{
				Stage: "search", Cause: &lajiaoliao.Failure{Code: tc.code, Submitted: true},
			}
			err := bindingOutboundLookupError(cause)
			var diagnostic *control.GroupBindingLookupFailure
			if !errors.As(err, &diagnostic) || diagnostic.Diagnostic.Category != tc.category ||
				diagnostic.Failure.Code != tc.public {
				t.Fatal("actual client failure lost its safe category", err)
			}
			response := httptest.NewRecorder()
			sendError(response, err)
			if response.Code != tc.status {
				t.Fatal("wrong lookup response status", response.Code)
			}
			var mutation *lajiaoliao.Failure
			if errors.As(bindingOutboundSendError(cause), &mutation) {
				t.Fatal("pre-submit read became a native mutation")
			}
		})
	}
}
