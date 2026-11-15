package control

import (
	"context"
	"errors"
	"time"
)

// Only fixed lookup diagnostics may cross the transport/control/HTTP boundary.
// Account queries and upstream reason text are deliberately absent.
type GroupBindingLookupDiagnostic struct {
	Version           int       `json:"version"`
	Platform          string    `json:"platform"`
	Stage             string    `json:"stage"`
	Category          string    `json:"category"`
	ProviderCode      int       `json:"provider_code,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	ObservedAt        time.Time `json:"observed_at"`
	RetryAfterSeconds int       `json:"retry_after_seconds,omitempty"`
}

type GroupBindingLookupFailure struct {
	Failure    *Failure
	Diagnostic GroupBindingLookupDiagnostic
}

func (e *GroupBindingLookupFailure) Error() string { return e.Failure.Error() }
func (e *GroupBindingLookupFailure) Unwrap() error { return e.Failure }

func SafeGroupBindingLookupDiagnostic(d GroupBindingLookupDiagnostic) (GroupBindingLookupDiagnostic, bool) {
	stageAllowed := d.Stage == "search" || d.Stage == "friend_info" ||
		d.Platform == "lajiaoliao" && d.Stage == "sent_requests"
	if d.Version != 1 || d.Platform != "wsl" && d.Platform != "lajiaoliao" ||
		!stageAllowed || d.ObservedAt.IsZero() {
		return GroupBindingLookupDiagnostic{}, false
	}
	switch d.Category {
	case "rate_limited", "session_expired", "parameters_rejected", "security_verification",
		"client_version", "ip_restricted", "permission_denied", "account_unavailable",
		"upstream_rejected", "transport_unavailable", "response_invalid":
	default:
		return GroupBindingLookupDiagnostic{}, false
	}
	if d.ProviderCode < -1 || d.ProviderCode > 9999 {
		d.ProviderCode = 0
	}
	switch d.Reason {
	case "provider_code_classified", "provider_reason_classified", "provider_reason_empty",
		"provider_reason_unrecognized", "provider_reason_oversized", "provider_reason_nontext",
		"http_status", "network_timeout", "network_unavailable", "connection_closed":
	case "friend_info_uid_missing", "friend_info_uid_null", "friend_info_uid_zero",
		"friend_info_uid_invalid", "friend_info_uid_mismatch",
		"friend_info_account_missing", "friend_info_account_null", "friend_info_account_zero",
		"friend_info_account_invalid", "friend_info_account_mismatch":
		if d.Platform != "wsl" || d.Stage != "friend_info" || d.Category != "response_invalid" {
			d.Reason = ""
		}
	default:
		d.Reason = ""
	}
	if d.Category != "rate_limited" || d.RetryAfterSeconds < 1 || d.RetryAfterSeconds > 86400 {
		d.RetryAfterSeconds = 0
	}
	d.ObservedAt = d.ObservedAt.UTC()
	return d, true
}

func (s *GroupBindingService) recordLookupFailure(ctx context.Context, ref SessionRef, id string,
	version int64, cause error) error {
	var failure *GroupBindingLookupFailure
	if !errors.As(cause, &failure) {
		return cause
	}
	diagnostic, valid := SafeGroupBindingLookupDiagnostic(failure.Diagnostic)
	if !valid {
		return failure.Failure
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return err
	}
	actor, err := s.checkRecord(ctx, tx, ref, c, true)
	if err != nil {
		return err
	}
	if err = outboundPending(c, version); err != nil {
		return err
	}
	if err = s.applicantPermission(ctx, tx, c); err != nil {
		return err
	}
	if _, err = s.connection(ctx, tx, c, nil); err != nil {
		return err
	}
	if diagnostic.Platform != c.Platform {
		return Denied(409, "group_binding_outbound_scope_changed")
	}
	if err = onboardingAudit(ctx, tx, actor, c.Tenant, "group_binding_outbound_lookup_failed", id, map[string]any{
		"version": diagnostic.Version, "platform": diagnostic.Platform, "stage": diagnostic.Stage,
		"category": diagnostic.Category, "provider_code": diagnostic.ProviderCode,
		"reason": diagnostic.Reason, "observed_at": diagnostic.ObservedAt,
		"retry_after_seconds": diagnostic.RetryAfterSeconds,
	}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return &GroupBindingLookupFailure{Failure: failure.Failure, Diagnostic: diagnostic}
}
