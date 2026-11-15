package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// RecoverFriends is called once before traffic, alongside Recover. It never
// dispatches prepared work; interruption can only be resolved by observation.
func (s *GroupBindingService) RecoverFriends(ctx context.Context) error {
	// A contact selection that never entered submission cannot have approved
	// anything. Preserve the legacy flow's historical recovery semantics.
	_, err := s.DB.Platform.Exec(ctx, `UPDATE group_binding_friend_attempts a SET status='failed',attempts_used=1,
		result_code='process_interrupted_not_submitted',updated_at=clock_timestamp()
		FROM group_binding_challenges c WHERE c.id=a.challenge_id AND c.friend_flow='incoming_contact'
		AND a.status='prepared'`)
	if err != nil {
		return err
	}
	_, err = s.DB.Platform.Exec(ctx, `UPDATE group_binding_friend_attempts SET status='unknown',attempts_used=1,
		result_code='process_interrupted',updated_at=clock_timestamp() WHERE status IN ('prepared','submitting')`)
	if err == nil {
		err = s.RecoverOutboundFriends(ctx)
	}
	return err
}

func (s *GroupBindingService) ReconcileFriend(ctx context.Context, ref SessionRef, id, operation string) (GroupBindingFriendAttempt, error) {
	var out GroupBindingFriendAttempt
	if !ValidID(id) || !ValidID(operation) {
		return out, Denied(400, "group_binding_friend_invalid")
	}
	c, _, err := s.confirmationRecord(ctx, ref, id, false)
	if err != nil {
		return out, err
	}
	attempt, err := readBindingFriend(ctx, s.DB.Platform, id, "", "")
	if err == pgx.ErrNoRows || err == nil && attempt.ID != operation {
		return out, Denied(404, "group_binding_friend_attempt_not_found")
	}
	if err != nil {
		return attempt.GroupBindingFriendAttempt, err
	}
	if c.FriendFlow == "incoming_contact" && attempt.Status == "submitting" {
		if time.Now().Before(attempt.ExpiresAt.Add(bindingFriendSubmissionFence)) {
			return out, Denied(409, "group_binding_friend_operation_unresolved")
		}
		if _, err = s.DB.Platform.Exec(ctx, `UPDATE group_binding_friend_attempts SET status='unknown',
			result_code='submission_deadline_elapsed',updated_at=clock_timestamp()
			WHERE id=$1 AND status='submitting' AND expires_at<=clock_timestamp()-interval '3 seconds'`, operation); err != nil {
			return out, err
		}
		attempt, err = readBindingFriend(ctx, s.DB.Platform, id, "", "")
		if err != nil {
			return out, err
		}
	}
	if attempt.Status != "unknown" {
		return attempt.GroupBindingFriendAttempt, nil
	}
	if c.FriendFlow == "incoming_contact" {
		return s.reconcileContact(ctx, ref, c, attempt)
	}
	port, ok := s.Native.(GroupBindingFriendNativePort)
	if !ok || s.Vault == nil {
		return out, Denied(503, "group_binding_friend_native_unavailable")
	}
	payload, err := s.openBindingFriendPayload(attempt, c)
	if err != nil {
		return out, err
	}
	r := payload.Approval
	// The port gets only the original persisted selectors. Deadline expiry
	// forbids another approval but does not prevent read-only reconciliation.
	evidence, err := port.ReconcileBindingFriend(ctx, r)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	c, err = loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	if _, err = s.checkRecord(ctx, tx, ref, c, true); err != nil {
		return out, err
	}
	var state string
	if err = tx.QueryRow(ctx, `SELECT status FROM group_binding_friend_attempts WHERE id=$1 FOR UPDATE`, operation).Scan(&state); err != nil {
		return out, err
	}
	if state != "unknown" {
		stored, err := readBindingFriend(ctx, tx, id, "", "")
		return stored.GroupBindingFriendAttempt, err
	}
	if !bindingFriendPayloadMatches(payload, attempt, c) {
		return out, Denied(409, "group_binding_friend_proof_changed")
	}
	text, err := s.friendText(ctx, tx, c)
	if err != nil {
		return out, err
	}
	if text != r.ChallengeText {
		return out, Denied(409, "group_binding_friend_proof_changed")
	}
	self, err := s.connection(ctx, tx, c, &evidence.GroupBindingConnectionEvidence)
	if err != nil {
		return out, err
	}
	if self != r.RecipientID {
		return out, Denied(409, "group_binding_friend_application_changed")
	}
	if err = bindingChallengeSender(ctx, tx, c, r.SenderID); err != nil {
		return out, err
	}
	proof, err := s.bindingFriendProof(ctx, tx, c, r, attempt.Version, nil)
	if err != nil {
		return out, err
	}
	if !bindingFriendMatches(r, evidence, "approved") {
		// Pending, missing, rejected and ambiguous observations do not prove
		// that this operation succeeded and never replenish its one budget.
		return attempt.GroupBindingFriendAttempt, nil
	}
	identity := c.State == "waiting" && c.Version == attempt.Version && len(c.Identity) == 0 && c.ExpiresAt.After(time.Now())
	if err = s.finishBindingFriend(ctx, tx, c, r, proof, "native_friend_approval_observed", identity); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	attempt.Status, attempt.Code = "succeeded", "native_friend_approval_observed"
	return attempt.GroupBindingFriendAttempt, nil
}
