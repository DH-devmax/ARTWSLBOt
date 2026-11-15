package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *GroupBindingService) RecoverOutboundFriends(ctx context.Context) error {
	_, err := s.DB.Platform.Exec(ctx, `UPDATE group_binding_outbound_attempts
		SET status=CASE WHEN status='prepared' THEN 'failed' ELSE 'unknown' END,
			result_code='process_interrupted',updated_at=clock_timestamp()
		WHERE status IN ('prepared','submitting')`)
	return err
}

func (s *GroupBindingService) ReconcileOutboundFriend(ctx context.Context, ref SessionRef, id, operation string, in GroupBindingConfirmation) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || !ValidID(operation) || in.Version < 1 || !onboardingKey(in.Key) {
		return out, Denied(400, "group_binding_outbound_invalid")
	}
	c, _, err := s.confirmationRecord(ctx, ref, id, false)
	if err != nil {
		return out, err
	}
	if c.Version != in.Version {
		return out, Denied(409, "version_conflict")
	}
	record, err := readBindingOutbound(ctx, s.DB.Platform, id)
	if err == pgx.ErrNoRows || err == nil && record.ID != operation {
		return out, Denied(404, "group_binding_outbound_attempt_not_found")
	}
	if err != nil {
		return out, err
	}
	if record.Status == "submitting" {
		// Every native submission has a final deadline no later than the sealed
		// operation expiry. Allow that deadline and its bounded final fence to
		// elapse before exposing a stranded submission to read-only reconciliation.
		if time.Now().Before(record.ExpiresAt.Add(bindingFriendSubmissionFence)) {
			return out, Denied(409, "group_binding_outbound_operation_unresolved")
		}
		tag, err := s.DB.Platform.Exec(ctx, `UPDATE group_binding_outbound_attempts SET status='unknown',
			result_code='submission_deadline_elapsed',updated_at=clock_timestamp()
			WHERE id=$1 AND status='submitting' AND expires_at<=clock_timestamp()-interval '3 seconds'`, record.ID)
		if err != nil {
			return out, err
		}
		if tag.RowsAffected() != 1 {
			return s.Read(ctx, ref, id)
		}
		record.Status = "unknown"
	}
	if record.Status != "submitted" && record.Status != "unknown" {
		return s.Read(ctx, ref, id)
	}
	port, ok := s.Native.(GroupBindingOutboundNativePort)
	if !ok || s.Vault == nil {
		return out, Denied(503, "group_binding_outbound_unavailable")
	}
	payload, err := s.openBindingOutbound(record, c)
	if err != nil {
		return out, err
	}
	// Only the original durable request selectors reach this read-only port.
	// A current friendship is contact evidence, never a website identity proof.
	e, err := port.ReconcileBindingOutbound(ctx, payload.Request)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	c, err = loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	actor, err := s.checkRecord(ctx, tx, ref, c, true)
	if err != nil {
		return out, err
	}
	if c.Version != in.Version || !outboundRequestMatches(payload.Request, e) {
		return out, Denied(409, "group_binding_outbound_target_changed")
	}
	if _, err = s.connection(ctx, tx, onboardingOutboundSource(c, payload.Controlled), &e.GroupBindingConnectionEvidence); err != nil {
		return out, err
	}
	if err = bindingOutboundTarget(ctx, tx, c.Platform, e.Target.NativeID, payload.Controlled); err != nil {
		return out, err
	}
	state, code := "", ""
	if e.State == "friends" && e.Target.AlreadyFriend {
		state, code = "friends", "native_friend_relationship_observed"
	} else if record.Status == "unknown" && e.State == "pending" && onboardingLabel(e.ApplicationID, 300) {
		state, code = "submitted", "native_friend_request_observed"
	} else if e.State == "rejected" && onboardingLabel(e.ApplicationID, 300) {
		state, code = "failed", "native_friend_request_rejected"
	}
	if state != "" {
		tag, err := tx.Exec(ctx, `UPDATE group_binding_outbound_attempts SET status=$2,result_code=$3,
			updated_at=clock_timestamp() WHERE id=$1 AND status IN ('submitted','unknown')`, operation, state, code)
		if err != nil {
			return out, err
		}
		if tag.RowsAffected() > 0 {
			if err = onboardingAudit(ctx, tx, actor, c.Tenant, "group_binding_outbound_reconciled", operation,
				map[string]any{"challenge_id": id, "platform": c.Platform, "status": state}); err != nil {
				return out, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return s.Read(ctx, ref, id)
}
