package control

import (
	"context"
	"errors"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

// Evidence failures do not change the original submission or authorize a retry.
func nativeTextEvidenceError(err error) error {
	if err == nil {
		return nil
	}
	code, status := "native_text_evidence_unavailable", 503
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code = "native_text_evidence_timeout"
	case errors.Is(err, context.Canceled):
		code = "native_text_evidence_cancelled"
	default:
		var native *nativeauth.Error
		var chili *lajiaoliao.Failure
		if errors.As(err, &native) && native.Code == "native_rate_limited" {
			code, status = "native_rate_limited", 429
		} else if errors.As(err, &chili) {
			if _, reason, ok := lajiaoliao.MediaEvidenceDiagnostic(chili.Code); ok {
				switch reason {
				case "busy", "rate_limited":
					status = 429
				case "owner_changed", "route_changed", "reauth_required", "gap", "conflict":
					status = 409
				}
				return Denied(status, chili.Code)
			}
			switch chili.Code {
			case "lajiaoliao_rate_limited":
				code, status = "native_rate_limited", 429
			case "lajiaoliao_sequence_gap":
				code, status = "native_text_evidence_gap", 409
			case "lajiaoliao_result_unknown":
				code = "native_text_evidence_timeout"
			case "lajiaoliao_reauth_required", "lajiaoliao_kicked", "lajiaoliao_connection_unverified":
				code, status = "native_text_connection_changed", 409
			}
		} else {
			switch err.Error() {
			case "native_text_connection_changed", "native_read_owner_unavailable":
				code, status = "native_text_connection_changed", 409
			case "native_text_route_unverified":
				code, status = "native_text_evidence_route_invalid", 409
			case "native_text_evidence_gap", "native_text_evidence_conflict":
				code, status = err.Error(), 409
			case "native_read_queue_full":
				code, status = "native_read_queue_full", 429
			}
		}
	}
	return Denied(status, code)
}
