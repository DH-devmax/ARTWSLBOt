package nativeauth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOnboardingLookupReasonClassificationIsBoundedAndRouteSpecific(t *testing.T) {
	for _, tc := range []struct {
		name, raw, code, diagnostic string
		number                      int
	}{
		{"generic_negative", `"private phone=13800000000 jwt=synthetic-secret"`, "native_business_rejected", "provider_reason_unrecognized", -1},
		{"generic_default", `"private phone=13800000000"`, "native_business_rejected", "provider_reason_unrecognized", 1001},
		{"empty", `""`, "native_business_rejected", "provider_reason_empty", -1},
		{"null", `null`, "native_business_rejected", "provider_reason_empty", 1001},
		{"nontext", `{"token":"synthetic-secret"}`, "native_response_invalid", "provider_reason_nontext", -1},
		{"oversized", `"` + strings.Repeat("x", 513) + `"`, "native_response_invalid", "provider_reason_oversized", 1001},
		{"escaped_oversized", `"` + strings.Repeat(`\u0078`, 513) + `"`, "native_response_invalid", "provider_reason_oversized", 1001},
		{"frequency_text", `"请求过于频繁，请稍后再试。"`, "native_rate_limited", "provider_reason_classified", -1},
		{"frequency_suffix_not_exact", `"请求过于频繁 phone=13800000000"`, "native_business_rejected", "provider_reason_unrecognized", -1},
		{"frequency_code", `"private token"`, "native_rate_limited", "provider_code_classified", 1043},
		{"session", `"private token"`, "native_reauth_required", "provider_code_classified", 1053},
		{"token", `"private token"`, "native_reauth_required", "provider_code_classified", 1014},
		{"parameters", `""`, "native_request_parameters_rejected", "provider_code_classified", 1000},
		{"missing", `""`, "native_request_parameters_rejected", "provider_code_classified", 1002},
		{"permission", `""`, "native_onboarding_lookup_permission_denied", "provider_code_classified", 1008},
		{"captcha", `""`, "native_captcha_rejected", "provider_code_classified", 1040},
		{"account", `""`, "native_onboarding_lookup_account_unavailable", "provider_code_classified", 1041},
		{"version", `""`, "native_client_version_rejected", "provider_code_classified", 1200},
		{"ip", `""`, "native_ip_restricted", "provider_code_classified", 1201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, diagnostic := onboardingLookupRejection(tc.number, json.RawMessage(tc.raw))
			if code != tc.code || diagnostic != tc.diagnostic {
				t.Fatal("wrong safe lookup classification", code, diagnostic)
			}
			if strings.Contains(code+diagnostic, "private") || strings.Contains(code+diagnostic, "13800000000") {
				t.Fatal("provider content escaped lookup classification")
			}
		})
	}
	for _, route := range []string{"/v1/user/login", onboardingOutboundApplyRoute, "/v1/friend/unknown"} {
		if onboardingLookupRoute(route) {
			t.Fatal("lookup classification expanded beyond its two reads")
		}
	}
}
