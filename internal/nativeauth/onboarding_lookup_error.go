package nativeauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// OnboardingLookupError preserves the failing read without changing submission
// semantics. Its cause never becomes public text.
type OnboardingLookupError struct {
	Stage  string
	Reason string
	Cause  error
}

func (e *OnboardingLookupError) Error() string { return "native_onboarding_lookup_failed" }
func (e *OnboardingLookupError) Unwrap() error { return e.Cause }

// SafeOnboardingLookupReason returns only fixed identity-field classifications.
func SafeOnboardingLookupReason(reason string) string {
	switch reason {
	case "friend_info_uid_missing", "friend_info_uid_null", "friend_info_uid_zero",
		"friend_info_uid_invalid", "friend_info_uid_mismatch",
		"friend_info_account_missing", "friend_info_account_null", "friend_info_account_zero",
		"friend_info_account_invalid", "friend_info_account_mismatch":
		return reason
	default:
		return ""
	}
}

func onboardingLookupError(stage string, cause error) *OnboardingLookupError {
	out := &OnboardingLookupError{Stage: stage, Cause: cause}
	var previous *OnboardingLookupError
	if errors.As(cause, &previous) && previous.Stage == stage {
		out.Reason = SafeOnboardingLookupReason(previous.Reason)
	}
	return out
}

func onboardingLookupRoute(route string) bool {
	return route == onboardingOutboundSearchRoute || route == onboardingOutboundInfoRoute
}

// These are shared business codes in the pinned Android API schema, confined
// here to the two read routes. In particular, neither -1 nor 1001 means rate limit.
func onboardingLookupRejection(number int, raw json.RawMessage) (string, string) {
	switch number {
	case 401, 1014, 1053:
		return "native_reauth_required", "provider_code_classified"
	case 429, 1043:
		return "native_rate_limited", "provider_code_classified"
	case 1000, 1002:
		return "native_request_parameters_rejected", "provider_code_classified"
	case 1008:
		return "native_onboarding_lookup_permission_denied", "provider_code_classified"
	case 1040:
		return "native_captcha_rejected", "provider_code_classified"
	case 1041:
		return "native_onboarding_lookup_account_unavailable", "provider_code_classified"
	case 1200:
		return "native_client_version_rejected", "provider_code_classified"
	case 1201:
		return "native_ip_restricted", "provider_code_classified"
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "native_business_rejected", "provider_reason_empty"
	}
	if len(raw) > 6*512+2 {
		return "native_response_invalid", "provider_reason_oversized"
	}
	var reason string
	if json.Unmarshal(raw, &reason) != nil {
		return "native_response_invalid", "provider_reason_nontext"
	}
	if len(reason) > 512 {
		return "native_response_invalid", "provider_reason_oversized"
	}
	reason = strings.TrimRight(strings.TrimSpace(reason), "。！!")
	switch reason {
	case "":
		return "native_business_rejected", "provider_reason_empty"
	case "请求过于频繁", "请求频繁，请稍后再试", "请求过于频繁，请稍后再试":
		return "native_rate_limited", "provider_reason_classified"
	case "登录已失效", "登录已过期", "请重新登录", "TOKEN过期或者刷新,请重新登录":
		return "native_reauth_required", "provider_reason_classified"
	case "请求参数错误", "缺少请求参数":
		return "native_request_parameters_rejected", "provider_reason_classified"
	case "安全验证失败", "人机验证失败":
		return "native_captcha_rejected", "provider_reason_classified"
	default:
		return "native_business_rejected", "provider_reason_unrecognized"
	}
}
