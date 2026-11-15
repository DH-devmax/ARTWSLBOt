package control

import (
	"errors"

	"wangshangliao/internal/nativeauth"
)

func outboundSubmitCategory(code string) string {
	switch code {
	case "native_business_rejected", "native_http_rejected":
		return "upstream_rejected"
	case "native_rate_limited":
		return "rate_limited"
	case "native_reauth_required":
		return "session_expired"
	case "native_transport_unknown":
		return "transport_unavailable"
	case "native_response_invalid":
		return "response_invalid"
	default:
		return ""
	}
}

func outboundSubmitProviderCode(cause error, attempted bool) int {
	var native *nativeauth.Error
	if !attempted || !errors.As(cause, &native) || !native.Submitted ||
		outboundSubmitCategory(native.Code) == "" || native.Number < 1 || native.Number > 9999 {
		return 0
	}
	return native.Number
}

func outboundSubmitDiagnostic(record bindingOutboundRecord) *GroupBindingOutboundSubmitDiagnostic {
	if record.Platform != "wsl" || record.ProviderCode == nil || *record.ProviderCode < 1 ||
		*record.ProviderCode > 9999 || record.ProviderObservedAt == nil || record.ProviderObservedAt.IsZero() ||
		record.Used != 1 {
		return nil
	}
	switch record.ProviderCategory {
	case "upstream_rejected", "rate_limited", "session_expired", "transport_unavailable", "response_invalid":
	default:
		return nil
	}
	return &GroupBindingOutboundSubmitDiagnostic{Version: 1, Platform: record.Platform, Stage: "friend_apply",
		Category: record.ProviderCategory, ProviderCode: *record.ProviderCode, ObservedAt: record.ProviderObservedAt.UTC()}
}
