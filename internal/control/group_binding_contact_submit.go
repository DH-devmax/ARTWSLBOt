package control

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *GroupBindingService) prepareContactApproval(ctx context.Context, ref SessionRef, id string, in GroupBindingContactApproval, reject bool) (bindingFriendRecord, bindingFriendPayload, bool, error) {
	var record bindingFriendRecord
	var p bindingFriendPayload
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return record, p, false, err
	}
	defer tx.Rollback(ctx)
	// Serialize hourly counts and reservations before taking challenge/user locks.
	// This transaction commits before the native port is called.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798505)`); err != nil {
		return record, p, false, err
	}
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return record, p, false, err
	}
	if _, err = s.checkRecord(ctx, tx, ref, c, true); err != nil {
		return record, p, false, err
	}
	record, err = readBindingFriend(ctx, tx, id, "", "")
	if err == pgx.ErrNoRows || err == nil && record.ID != in.OperationID {
		return record, p, false, Denied(404, "group_binding_friend_attempt_not_found")
	}
	if err != nil {
		return record, p, false, err
	}
	p, err = s.openContact(record, c)
	if err != nil {
		return record, p, false, err
	}
	if p.Approval.RejectExpired != reject {
		return record, p, false, Denied(409, "group_binding_friend_decision_changed")
	}
	if record.Version != in.Version {
		return record, p, false, Denied(409, "version_conflict")
	}
	if record.Status != "prepared" {
		return record, p, false, nil
	}
	if err = requirePendingOutboundResolved(ctx, tx, id); err != nil {
		return record, p, false, err
	}
	if err = contactPending(c, record.Version); err != nil {
		return record, p, false, err
	}
	if err = requireControlledChallenge(ctx, tx, c); err != nil {
		return record, p, false, err
	}
	if err = s.applicantPermission(ctx, tx, c); err != nil {
		return record, p, false, err
	}
	if _, err = requireBindingFriendGate(ctx, tx, c.Platform, c.Account, record.GateVersion); err != nil {
		return record, p, false, err
	}
	// The sealed selection is an immutable connection/version fence, not a
	// fresh observation. The native port rereads it and the final fence below
	// compares all saved identifiers against fresh evidence.
	if _, err = s.connection(ctx, tx, c, nil); err != nil {
		return record, p, false, err
	}
	if !reject || c.ControlledScope != "" {
		if err = bindingChallengeSender(ctx, tx, c, p.Approval.SenderID); err != nil {
			return record, p, false, err
		}
	}
	var actorCount, accountCount int
	err = tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM group_binding_friend_attempts WHERE actor_id=$1 AND attempts_used=1 AND updated_at>clock_timestamp()-interval '1 hour'),
		(SELECT count(*) FROM group_binding_friend_attempts WHERE account_id=$2 AND attempts_used=1 AND updated_at>clock_timestamp()-interval '1 hour')`,
		c.Applicant, c.Account).Scan(&actorCount, &accountCount)
	if err != nil {
		return record, p, false, err
	}
	if actorCount >= 3 || accountCount >= 12 {
		return record, p, false, Denied(429, "group_binding_friend_rate_limited")
	}
	if c.ControlledScope != "" {
		if err = ReserveControlledScopeNativeTx(ctx, tx, c.ControlledScope, "onboarding", record.ID); err != nil {
			return record, p, false, err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE group_binding_friend_attempts SET status='submitting',attempts_used=1,
		updated_at=clock_timestamp() WHERE id=$1 AND status='prepared' AND attempts_used=0
		AND expires_at>clock_timestamp()+interval '3 seconds'`, record.ID)
	if err != nil {
		return record, p, false, err
	}
	if tag.RowsAffected() != 1 {
		return record, p, false, Denied(409, "group_binding_friend_attempt_expired")
	}
	if err = tx.Commit(ctx); err != nil {
		return record, p, false, err
	}
	record.Status = "submitting"
	return record, p, true, nil
}

func (s *GroupBindingService) ApproveContact(ctx context.Context, ref SessionRef, id string, in GroupBindingContactApproval) (GroupBindingChallenge, error) {
	return s.decideContact(ctx, ref, id, in, false)
}

func (s *GroupBindingService) RejectExpiredContact(ctx context.Context, ref SessionRef, id string, in GroupBindingContactApproval) (GroupBindingChallenge, error) {
	return s.decideContact(ctx, ref, id, in, true)
}

func (s *GroupBindingService) decideContact(ctx context.Context, ref SessionRef, id string, in GroupBindingContactApproval, reject bool) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || !ValidID(in.OperationID) || in.Version < 1 || !onboardingKey(in.Key) {
		return out, Denied(400, "group_binding_contact_invalid")
	}
	// Authenticate the applicant before looking up an operation, including on
	// idempotent replays. Only a website confirmation can reach this method.
	if _, _, err := s.confirmationRecord(ctx, ref, id, false); err != nil {
		return out, err
	}
	port, ok := s.Native.(GroupBindingFriendNativePort)
	if !ok || s.Vault == nil {
		return out, Denied(503, "group_binding_friend_native_unavailable")
	}
	decide := port.ApproveBindingFriend
	if reject {
		expired, ok := s.Native.(GroupBindingExpiredFriendNativePort)
		if !ok {
			return out, Denied(503, "group_binding_friend_native_unavailable")
		}
		decide = expired.RejectBindingFriend
	}
	record, p, dispatch, err := s.prepareContactApproval(ctx, ref, id, in, reject)
	if err != nil {
		return out, err
	}
	if !dispatch {
		return s.Read(ctx, ref, id)
	}
	r := p.Approval
	var fences atomic.Int32
	var attempted atomic.Bool
	err = decide(ctx, r, func(work context.Context, e GroupBindingFriendEvidence, submit func(context.Context) error) error {
		if fences.Add(1) != 1 || submit == nil || !contactMatches(r, e, "pending") ||
			!e.RequestedAt.Equal(p.RequestedAt) || e.ReceiverOwner != p.Connection.ReceiverOwner ||
			e.ReceiverRevision != p.Connection.ReceiverRevision || e.DeploymentVersion != p.Connection.DeploymentVersion ||
			e.CredentialDigest != p.Connection.CredentialDigest {
			return Denied(409, "group_binding_friend_application_changed")
		}
		until, err := s.reserveBindingFriendWrite(work, r)
		if err != nil {
			return err
		}
		final, cancel := context.WithTimeout(work, bindingFriendSubmissionFence)
		defer cancel()
		tx, err := s.DB.Platform.Begin(final)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.WithoutCancel(final))
		if _, err = tx.Exec(final, `SET LOCAL lock_timeout='500ms'`); err != nil {
			return err
		}
		c, err := loadBindingRecord(final, tx, id, true)
		if err != nil {
			return err
		}
		a, err := s.checkRecord(final, tx, ref, c, true)
		if err != nil {
			return err
		}
		if err = contactPending(c, record.Version); err != nil {
			return err
		}
		if err = requirePendingOutboundResolved(final, tx, id); err != nil {
			return err
		}
		if err = requireControlledChallenge(final, tx, c); err != nil {
			return err
		}
		if err = s.applicantPermission(final, tx, c); err != nil {
			return err
		}
		if _, err = requireBindingFriendGate(final, tx, c.Platform, c.Account, record.GateVersion); err != nil {
			return err
		}
		if _, err = s.connection(final, tx, c, &e.GroupBindingConnectionEvidence); err != nil {
			return err
		}
		if !reject || c.ControlledScope != "" {
			if err = bindingChallengeSender(final, tx, c, r.SenderID); err != nil {
				return err
			}
		}
		var operation string
		err = tx.QueryRow(final, `SELECT id::text FROM group_binding_friend_attempts WHERE id=$1
			AND status='submitting' AND attempts_used=1 AND expires_at>clock_timestamp()+interval '3 seconds'
			FOR UPDATE`, record.ID).Scan(&operation)
		if err == pgx.ErrNoRows {
			return Denied(409, "group_binding_friend_attempt_expired")
		}
		if err != nil {
			return err
		}
		if err = lockBindingFriendWrite(final, tx, r, until); err != nil {
			return err
		}
		if err = final.Err(); err != nil {
			return err
		}
		attempted.Store(true)
		if err = submit(final); err != nil {
			return err
		}
		_, err = tx.Exec(final, `UPDATE business_rule_account_cooldowns SET
			next_write_at=greatest(next_write_at,clock_timestamp()+interval '5 seconds') WHERE account_id=$1`, c.Account)
		if err != nil {
			return err
		}
		action := "group_binding_contact_approved"
		if reject {
			action = "group_binding_contact_expired_rejected"
		}
		if err = onboardingAudit(final, tx, a, c.Tenant, action, record.ID,
			map[string]any{"challenge_id": c.ID, "platform": c.Platform, "identity_recorded": false}); err != nil {
			return err
		}
		return tx.Commit(final)
	})
	if err == nil && (fences.Load() != 1 || !attempted.Load()) {
		err = Denied(409, "group_binding_friend_submission_missing")
	}
	status, code := settingOutcome(err, attempted.Load())
	if status == "succeeded" {
		code = "native_friend_approval_acknowledged"
		if reject {
			code = "native_friend_expired_rejection_acknowledged"
		}
	}
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	tx, saveErr := s.DB.Platform.Begin(save)
	if saveErr != nil {
		return out, saveErr
	}
	defer tx.Rollback(save)
	// Preserve challenge-before-attempt lock order used by confirmations and
	// reconciliation. Only a confirmed rejection releases this reservation.
	_, saveErr = loadBindingRecord(save, tx, id, true)
	if saveErr == nil {
		_, saveErr = tx.Exec(save, `UPDATE group_binding_friend_attempts SET status=$2,result_code=$3,
			updated_at=clock_timestamp() WHERE id=$1 AND status='submitting'`, record.ID, status, code)
	}
	if saveErr == nil && reject && status == "succeeded" {
		saveErr = finishExpiredContact(save, tx, id)
	}
	if saveErr == nil {
		saveErr = tx.Commit(save)
	}
	if saveErr != nil {
		return out, saveErr
	}
	return s.Read(save, ref, id)
}

func finishExpiredContact(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE group_binding_challenges SET state='cancelled',version=version+1,
		blocked_reason='group_binding_friend_reapply_required'
		WHERE id=$1 AND state='waiting' AND identity_sealed IS NULL`, id)
	return err
}

func (s *GroupBindingService) reconcileContact(ctx context.Context, ref SessionRef, c bindingRecord, record bindingFriendRecord) (GroupBindingFriendAttempt, error) {
	p, err := s.openContact(record, c)
	if err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	port, ok := s.Native.(GroupBindingFriendNativePort)
	if !ok {
		return record.GroupBindingFriendAttempt, Denied(503, "group_binding_friend_native_unavailable")
	}
	e, err := port.ReconcileBindingFriend(ctx, p.Approval)
	if err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	defer tx.Rollback(ctx)
	c, err = loadBindingRecord(ctx, tx, c.ID, true)
	if err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	a, err := s.checkRecord(ctx, tx, ref, c, true)
	if err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	if _, err = s.openContact(record, c); err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	if _, err = s.connection(ctx, tx, c, &e.GroupBindingConnectionEvidence); err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	if e.State != "approved" && e.State != "rejected" ||
		!contactMatches(p.Approval, e, e.State) || !e.RequestedAt.Equal(p.RequestedAt) {
		return record.GroupBindingFriendAttempt, nil
	}
	status, code := "succeeded", "native_friend_approval_observed"
	if p.Approval.RejectExpired {
		if e.State == "approved" {
			status, code = "failed", "group_binding_friend_application_changed"
		} else {
			code = "native_friend_expired_rejection_observed"
		}
	} else if e.State == "rejected" {
		status, code = "failed", "native_friend_approval_rejected"
	}
	tag, err := tx.Exec(ctx, `UPDATE group_binding_friend_attempts SET status=$2,
		result_code=$3,updated_at=clock_timestamp() WHERE id=$1 AND status='unknown'`, record.ID, status, code)
	if err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	if tag.RowsAffected() == 1 {
		if p.Approval.RejectExpired && status == "succeeded" {
			if err = finishExpiredContact(ctx, tx, c.ID); err != nil {
				return record.GroupBindingFriendAttempt, err
			}
		}
		if err = onboardingAudit(ctx, tx, a, c.Tenant, "group_binding_contact_observed", record.ID,
			map[string]any{"challenge_id": c.ID, "platform": c.Platform, "status": status, "identity_recorded": false}); err != nil {
			return record.GroupBindingFriendAttempt, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return record.GroupBindingFriendAttempt, err
	}
	current, err := readBindingFriend(ctx, s.DB.Platform, c.ID, "", "")
	return current.GroupBindingFriendAttempt, err
}
