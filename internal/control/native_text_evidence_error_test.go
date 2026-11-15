package control

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func TestNativeTextEvidenceErrorSafeClassification(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		status     int
		err        error
	}{
		{"timeout", "native_text_evidence_timeout", 503, fmt.Errorf("request: %w", context.DeadlineExceeded)},
		{"cancelled", "native_text_evidence_cancelled", 503, context.Canceled},
		{"owner", "native_text_connection_changed", 409, errors.New("native_text_connection_changed")},
		{"route", "native_text_evidence_route_invalid", 409, errors.New("native_text_route_unverified")},
		{"read_owner", "native_text_connection_changed", 409, errors.New("native_read_owner_unavailable")},
		{"gap", "native_text_evidence_gap", 409, &lajiaoliao.Failure{Code: "lajiaoliao_sequence_gap"}},
		{"unknown_read", "native_text_evidence_timeout", 503, &lajiaoliao.Failure{Code: "lajiaoliao_result_unknown"}},
		{"rate", "native_rate_limited", 429, &nativeauth.Error{Code: "native_rate_limited"}},
		{"chili_rate", "native_rate_limited", 429, &lajiaoliao.Failure{Code: "lajiaoliao_rate_limited"}},
		{"unverified", "native_text_connection_changed", 409, &lajiaoliao.Failure{Code: "lajiaoliao_connection_unverified"}},
		{"conflict", "native_text_evidence_conflict", 409, errors.New("native_text_evidence_conflict")},
		{"queue", "native_read_queue_full", 429, errors.New("native_read_queue_full")},
		{"raw_private", "native_text_evidence_unavailable", 503, errors.New("password=synthetic-secret payload=private-message")},
		{"untrusted_code", "native_text_evidence_unavailable", 503, &lajiaoliao.Failure{Code: "private-message"}},
		{"stage", "lajiaoliao_evidence_sequences_rejected", 503, &lajiaoliao.Failure{Code: "lajiaoliao_evidence_sequences_rejected"}},
		{"stage_gap", "lajiaoliao_evidence_pull_gap", 409, &lajiaoliao.Failure{Code: "lajiaoliao_evidence_pull_gap"}},
		{"history_empty", "lajiaoliao_evidence_history_empty_gap", 409, &lajiaoliao.Failure{Code: "lajiaoliao_evidence_history_empty_gap"}},
		{"history_partial", "lajiaoliao_evidence_history_partial_gap", 409, &lajiaoliao.Failure{Code: "lajiaoliao_evidence_history_partial_gap"}},
		{"history_record", "lajiaoliao_evidence_history_record_gap", 409, &lajiaoliao.Failure{Code: "lajiaoliao_evidence_history_record_gap"}},
		{"stage_rate", "lajiaoliao_evidence_schedule_rate_limited", 429, &lajiaoliao.Failure{Code: "lajiaoliao_evidence_schedule_rate_limited"}},
		{"untrusted_stage", "native_text_evidence_unavailable", 503, &lajiaoliao.Failure{Code: "lajiaoliao_evidence_private-message_gap"}},
		{"untrusted_reason", "native_text_evidence_unavailable", 503, &lajiaoliao.Failure{Code: "lajiaoliao_evidence_pull_private-message"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got *Failure
			err := nativeTextEvidenceError(tc.err)
			if !errors.As(err, &got) || got.Code != tc.code || got.Status != tc.status ||
				err.Error() != tc.code || errors.Unwrap(err) != nil {
				t.Fatal("unsafe or incorrect evidence error", err)
			}
		})
	}
	if err := nativeTextEvidenceError(nil); err != nil {
		t.Fatal("successful read became an error", err)
	}
}
