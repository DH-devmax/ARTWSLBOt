package control

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

type GroupBindingAcceptInput struct {
	NativeGroupID string `json:"native_group_id"`
	InvitationID  string `json:"invitation_id,omitempty"`
	Version       int64  `json:"version"`
	Key           string `json:"-"`
}

type CustomerGroupAcceptAttempt struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Code   string `json:"result_code"`
}

type GroupBindingInvitationPreview struct {
	NativeGroupID string    `json:"native_group_id"`
	InvitationID  string    `json:"invitation_id"`
	GroupName     string    `json:"group_name"`
	ExpiresAt     time.Time `json:"expires_at"`
	Version       int64     `json:"version"`
}

func (s *GroupBindingService) PrepareAccept(ctx context.Context, ref SessionRef, id string, in GroupBindingAcceptInput) (GroupBindingInvitationPreview, error) {
	var out GroupBindingInvitationPreview
	if !ValidID(id) || !onboardingLabel(in.NativeGroupID, 200) || in.Version < 1 ||
		in.InvitationID != "" && !onboardingLabel(in.InvitationID, 300) {
		return out, Denied(400, "group_binding_accept_invalid")
	}
	c, _, err := s.confirmationRecord(ctx, ref, id, false)
	if err != nil {
		return out, err
	}
	if c.State != "waiting" || c.Version != in.Version || !c.ExpiresAt.After(time.Now().Add(4*time.Second)) ||
		len(c.Identity) == 0 || s.Native == nil {
		return out, Denied(409, "group_binding_identity_required")
	}
	if err = requireOutboundResolved(ctx, s.DB.Platform, id); err != nil {
		return out, err
	}
	identity, err := s.Vault.Open("group-binding-identity:"+c.ID, c.Identity)
	if err != nil {
		return out, err
	}
	sender := string(identity)
	clear(identity)
	evidence, err := s.Native.BindingInvitation(ctx, GroupBindingInvitationRequest{
		GroupBindingNativeScope: c.scope(in.NativeGroupID, sender), InvitationID: in.InvitationID})
	if err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	current, err := loadBindingRecord(ctx, tx, id, false)
	if err != nil {
		return out, err
	}
	if _, err = s.checkRecord(ctx, tx, ref, current, true); err != nil {
		return out, err
	}
	if err = bindingChallengeSender(ctx, tx, current, sender); err != nil {
		return out, err
	}
	if current.ControlledScope != "" {
		var matches bool
		if err = tx.QueryRow(ctx, `SELECT native_id=$2 FROM native_groups WHERE id=$1`, current.Group, evidence.NativeGroupID).Scan(&matches); err != nil {
			return out, err
		}
		if !matches {
			return out, Denied(409, "group_binding_group_changed")
		}
	}
	if current.State != "waiting" || current.Version != in.Version || !current.ExpiresAt.After(time.Now().Add(4*time.Second)) {
		return out, Denied(409, "group_binding_scope_changed")
	}
	if err = requireOutboundResolved(ctx, tx, id); err != nil {
		return out, err
	}
	self, err := s.connection(ctx, tx, current, &evidence.GroupBindingConnectionEvidence)
	if err != nil {
		return out, err
	}
	if evidence.NativeGroupID != in.NativeGroupID || evidence.InviterID != sender || evidence.RecipientID != self ||
		!onboardingLabel(evidence.InvitationID, 300) || in.InvitationID != "" && in.InvitationID != evidence.InvitationID ||
		!evidence.ExpiresAt.After(time.Now().Add(4*time.Second)) || evidence.GroupName != "" && !onboardingLabel(evidence.GroupName, 200) {
		return out, Denied(409, "group_binding_invitation_changed")
	}
	out = GroupBindingInvitationPreview{NativeGroupID: evidence.NativeGroupID, InvitationID: evidence.InvitationID,
		GroupName: evidence.GroupName, ExpiresAt: minBindingTime(c.ExpiresAt, evidence.ExpiresAt), Version: c.Version}
	return out, nil
}

func (s *GroupBindingService) ConfirmAccept(ctx context.Context, ref SessionRef, id string, in GroupBindingAcceptInput) (CustomerGroupAcceptAttempt, error) {
	var out CustomerGroupAcceptAttempt
	if !ValidID(id) || in.Version < 1 || !onboardingKey(in.Key) || !onboardingLabel(in.NativeGroupID, 200) ||
		in.InvitationID != "" && !onboardingLabel(in.InvitationID, 300) {
		return out, Denied(400, "group_binding_accept_invalid")
	}
	c, _, err := s.confirmationRecord(ctx, ref, id, false)
	if err != nil {
		return out, err
	}
	digest := bindingAcceptDigest(id, in)
	var oldHash []byte
	err = s.DB.Platform.QueryRow(ctx, `SELECT id::text,status,result_code,request_hash FROM customer_group_accept_attempts
		WHERE challenge_id=$1 OR actor_id=$2 AND idempotency_key=$3`, id, c.Applicant, in.Key).
		Scan(&out.ID, &out.Status, &out.Code, &oldHash)
	if err == nil {
		if !bytes.Equal(digest, oldHash) {
			return CustomerGroupAcceptAttempt{}, Denied(409, "idempotency_conflict")
		}
		return out, nil
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if c.State != "waiting" || c.Version != in.Version || !c.ExpiresAt.After(time.Now().Add(4*time.Second)) || len(c.Identity) == 0 || s.Native == nil {
		return out, Denied(409, "group_binding_identity_required")
	}
	if err = requireOutboundResolved(ctx, s.DB.Platform, id); err != nil {
		return out, err
	}
	identity, err := s.Vault.Open("group-binding-identity:"+c.ID, c.Identity)
	if err != nil {
		return out, err
	}
	sender := string(identity)
	clear(identity)
	native, err := s.Native.BindingInvitation(ctx, GroupBindingInvitationRequest{
		GroupBindingNativeScope: c.scope(in.NativeGroupID, sender), InvitationID: in.InvitationID})
	if err != nil {
		return out, err
	}
	if native.NativeGroupID != in.NativeGroupID || native.InviterID != sender ||
		!onboardingLabel(native.InvitationID, 300) || in.InvitationID != "" && native.InvitationID != in.InvitationID ||
		!native.ExpiresAt.After(time.Now().Add(4*time.Second)) {
		return out, Denied(409, "group_binding_invitation_changed")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return out, err
	}
	current, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	actor, err := s.checkRecord(ctx, tx, ref, current, true)
	if err != nil {
		return out, err
	}
	if err = bindingChallengeSender(ctx, tx, current, sender); err != nil {
		return out, err
	}
	// The lock serializes different keys too: a challenge can accept at most
	// one invitation, even after a timeout or an unknown external result.
	old, stored, err := readCustomerAccept(ctx, tx, c.ID, "")
	if err == nil {
		if !bytes.Equal(stored, digest) {
			return out, Denied(409, "idempotency_conflict")
		}
		return old, nil
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if current.State != "waiting" || current.Version != in.Version || !current.ExpiresAt.After(time.Now().Add(4*time.Second)) {
		return out, Denied(409, "group_binding_challenge_expired")
	}
	if err = requireOutboundResolved(ctx, tx, id); err != nil {
		return out, err
	}
	self, err := s.connection(ctx, tx, current, &native.GroupBindingConnectionEvidence)
	if err != nil {
		return out, err
	}
	if native.RecipientID != self {
		return out, Denied(409, "group_binding_invitation_changed")
	}
	var group string
	if native.GroupName != "" && !onboardingLabel(native.GroupName, 200) {
		return out, Denied(409, "group_binding_group_name_invalid")
	}
	group, err = ensureBindingNativeGroup(ctx, tx, c.Platform, native.NativeGroupID, native.GroupName)
	if err != nil {
		return out, err
	}
	if current.Group != "" && current.Group != group {
		return out, Denied(409, "group_binding_group_changed")
	}
	var forbidden bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_bindings WHERE group_id=$1 AND (tenant_id<>$2 OR active))
		OR EXISTS(SELECT 1 FROM customer_group_accept_attempts WHERE account_id=$3 AND status IN ('prepared','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$3 AND status IN ('preparing','submitting','unknown'))`,
		group, c.Tenant, c.Account).Scan(&forbidden); err != nil {
		return out, err
	}
	if forbidden {
		return out, Denied(409, "group_binding_invitation_unavailable")
	}
	if err = bindingCapacity(ctx, tx, c.Tenant, c.Platform, c.Account, group, c.ID); err != nil {
		return out, err
	}
	if err = RequireBusinessAdmission(ctx, tx, c.Platform, group, "group_accept", "onboarding"); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET group_id=$2 WHERE id=$1 AND proof_event_id IS NULL`, c.ID, group); err != nil {
		return out, err
	}
	deadline := minBindingTime(c.ExpiresAt, native.ExpiresAt)
	out = CustomerGroupAcceptAttempt{ID: NewID(), Status: "prepared"}
	request := GroupBindingAcceptRequest{GroupBindingNativeScope: c.scope(native.NativeGroupID, sender),
		OperationID: out.ID, InvitationID: native.InvitationID, RecipientID: self, InviterID: sender, ExpiresAt: deadline}
	raw, _ := json.Marshal(request)
	defer clear(raw)
	sealed, err := s.Vault.Seal("customer-group-accept:"+out.ID, raw)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO customer_group_accept_attempts(id,challenge_id,actor_id,tenant_id,platform,
		account_id,group_id,account_epoch,account_version,request_hash,payload_sealed,idempotency_key,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, out.ID, c.ID, actor.ID, c.Tenant, c.Platform,
		c.Account, group, c.Epoch, c.AccountVersion, digest, sealed, in.Key, deadline)
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, actor, c.Tenant, "customer_group_accept_prepared", out.ID,
		map[string]any{"challenge_id": c.ID, "platform": c.Platform, "mode": c.Mode}); err != nil {
		return out, err
	}
	if current.ControlledScope != "" {
		if err = ReserveControlledScopeNativeTx(ctx, tx, current.ControlledScope, "onboarding", out.ID); err != nil {
			return out, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		// A commit error may have persisted the stable attempt. Never call the
		// native mutation unless the durable preparation was confirmed.
		return out, err
	}
	var fences atomic.Int32
	var attempted atomic.Bool
	err = s.Native.AcceptBindingInvitation(ctx, request,
		func(work context.Context, evidence GroupBindingInvitationEvidence, submit func(context.Context) error) error {
			if fences.Add(1) != 1 || submit == nil {
				return Denied(409, "group_binding_submission_limit")
			}
			if !bindingInvitationMatches(request, evidence) {
				return Denied(409, "group_binding_invitation_changed")
			}
			tag, err := s.DB.Platform.Exec(work, `UPDATE customer_group_accept_attempts SET status='submitting',
				updated_at=clock_timestamp() WHERE id=$1 AND status='prepared' AND expires_at>clock_timestamp()+interval '4 seconds'`, out.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return Denied(409, "group_binding_accept_expired")
			}
			final, cancel := context.WithTimeout(work, 3*time.Second)
			defer cancel()
			fence, err := s.DB.Platform.Begin(final)
			if err != nil {
				return err
			}
			defer fence.Rollback(context.WithoutCancel(final))
			if _, err = fence.Exec(final, `SET LOCAL lock_timeout='500ms'`); err != nil {
				return err
			}
			if _, err = fence.Exec(final, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
				return err
			}
			current, err := loadBindingRecord(final, fence, c.ID, true)
			if err != nil {
				return err
			}
			if _, err = s.checkRecord(final, fence, ref, current, true); err != nil {
				return err
			}
			if err = bindingChallengeSender(final, fence, current, sender); err != nil {
				return err
			}
			if current.State != "waiting" || current.Version != in.Version ||
				!current.ExpiresAt.After(time.Now().Add(3*time.Second)) || current.Group != group {
				return Denied(409, "group_binding_scope_changed")
			}
			var attemptID string
			err = fence.QueryRow(final, `SELECT id::text FROM customer_group_accept_attempts WHERE id=$1
				AND status='submitting' AND expires_at>clock_timestamp()+interval '3 seconds' FOR SHARE`, out.ID).Scan(&attemptID)
			if err != nil {
				return Denied(409, "group_binding_accept_expired")
			}
			if _, err = s.connection(final, fence, current, &evidence.GroupBindingConnectionEvidence); err != nil {
				return err
			}
			if err = bindingCapacity(final, fence, c.Tenant, c.Platform, c.Account, group, c.ID); err != nil {
				return err
			}
			var forbidden bool
			if err = fence.QueryRow(final, `SELECT EXISTS(SELECT 1 FROM group_bindings
				WHERE group_id=$1 AND (tenant_id<>$2 OR active))`, group, c.Tenant).Scan(&forbidden); err != nil {
				return err
			}
			if forbidden {
				return Denied(409, "group_binding_invitation_unavailable")
			}
			if err = RequireBusinessAdmission(final, fence, c.Platform, group, "group_accept", "onboarding"); err != nil {
				return err
			}
			attempted.Store(true)
			if err = submit(final); err != nil {
				return err
			}
			return fence.Commit(final)
		})
	if err == nil && (fences.Load() != 1 || !attempted.Load()) {
		err = Denied(409, "group_binding_submission_missing")
	}
	out.Status, out.Code = settingOutcome(err, attempted.Load())
	if out.Status == "succeeded" {
		out.Code = "native_invitation_accept_acknowledged"
	}
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	tag, saveErr := s.DB.Platform.Exec(save, `UPDATE customer_group_accept_attempts SET status=$2,result_code=$3,
		updated_at=clock_timestamp() WHERE id=$1 AND status IN ('prepared','submitting')`, out.ID, out.Status, out.Code)
	if saveErr != nil {
		return out, saveErr
	}
	if tag.RowsAffected() != 1 {
		persisted, _, readErr := readCustomerAccept(save, s.DB.Platform, c.ID, out.ID)
		return persisted, readErr
	}
	return out, nil
}

func bindingInvitationMatches(request GroupBindingAcceptRequest, evidence GroupBindingInvitationEvidence) bool {
	now := time.Now()
	return evidence.NativeGroupID == request.NativeGroupID && evidence.InvitationID == request.InvitationID &&
		evidence.RecipientID == request.RecipientID && evidence.InviterID == request.InviterID &&
		request.NativeSenderID == request.InviterID && request.ExpiresAt.After(now.Add(3*time.Second)) &&
		evidence.ExpiresAt.After(now.Add(3*time.Second))
}

func readCustomerAccept(ctx context.Context, q Queryer, challenge, operation string) (CustomerGroupAcceptAttempt, []byte, error) {
	var out CustomerGroupAcceptAttempt
	var digest []byte
	err := q.QueryRow(ctx, `SELECT id::text,status,result_code,request_hash FROM customer_group_accept_attempts
		WHERE challenge_id=$1 AND ($2='' OR id::text=$2)`, challenge, operation).
		Scan(&out.ID, &out.Status, &out.Code, &digest)
	return out, digest, err
}

// Recover does not execute prepared work. A crash may have occurred around an
// unconfirmed commit or native submission, so only observation may resolve it.
func (s *GroupBindingService) Recover(ctx context.Context) error {
	_, err := s.DB.Platform.Exec(ctx, `UPDATE customer_group_accept_attempts SET status='unknown',
		result_code='process_interrupted',updated_at=clock_timestamp() WHERE status IN ('prepared','submitting')`)
	if err == nil {
		_, err = s.DB.Platform.Exec(ctx, `UPDATE operator_demo_group_claims SET state='failed',
			blocked_reason='operator_demo_claim_interrupted',version=version+1 WHERE state='checking'`)
	}
	if err == nil {
		err = s.RecoverFriends(ctx)
	}
	return err
}

func (s *GroupBindingService) ReconcileAccept(ctx context.Context, ref SessionRef, id, operation string) (CustomerGroupAcceptAttempt, error) {
	var out CustomerGroupAcceptAttempt
	if !ValidID(id) || !ValidID(operation) {
		return out, Denied(400, "group_binding_accept_invalid")
	}
	c, _, err := s.confirmationRecord(ctx, ref, id, false)
	if err != nil {
		return out, err
	}
	out, _, err = readCustomerAccept(ctx, s.DB.Platform, id, operation)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "group_binding_accept_not_found")
	}
	if err != nil || out.Status != "unknown" {
		return out, err
	}
	var sealed []byte
	if err = s.DB.Platform.QueryRow(ctx, `SELECT payload_sealed FROM customer_group_accept_attempts WHERE id=$1`, operation).Scan(&sealed); err != nil {
		return out, err
	}
	raw, err := s.Vault.Open("customer-group-accept:"+operation, sealed)
	if err != nil {
		return out, err
	}
	defer clear(raw)
	var request GroupBindingAcceptRequest
	if json.Unmarshal(raw, &request) != nil || request.OperationID != operation ||
		request.AccountID != c.Account || request.Platform != c.Platform || request.AccountEpoch != c.Epoch || s.Native == nil {
		return out, Denied(409, "group_binding_evidence_changed")
	}
	roles, err := s.Native.BindingRoles(ctx, request.GroupBindingNativeScope)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = s.checkRecord(ctx, tx, ref, c, true); err != nil {
		return out, err
	}
	if _, err = s.connection(ctx, tx, c, &roles.GroupBindingConnectionEvidence); err != nil {
		return out, err
	}
	if !roles.Present || roles.NativeGroupID != request.NativeGroupID ||
		roles.RobotRole != "owner" && roles.RobotRole != "admin" && roles.RobotRole != "member" {
		return out, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE customer_group_accept_attempts SET status='succeeded',
		result_code='membership_observed',updated_at=clock_timestamp() WHERE id=$1 AND status='unknown'`, operation); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	out.Status, out.Code = "succeeded", "membership_observed"
	return out, nil
}

func minBindingTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func ensureBindingNativeGroup(ctx context.Context, tx pgx.Tx, platform, nativeID, name string) (string, error) {
	if !bindingPlatform(platform) || !onboardingLabel(nativeID, 200) || name != "" && !onboardingLabel(name, 200) {
		return "", Denied(409, "group_binding_invitation_changed")
	}
	display := name
	if display == "" {
		display = nativeID
	}
	var group string
	err := tx.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name) VALUES($1,$2,$3)
		ON CONFLICT(platform,native_id) DO UPDATE SET name=CASE WHEN $4='' THEN native_groups.name ELSE excluded.name END
		RETURNING id::text`, platform, nativeID, display, name).Scan(&group)
	return group, err
}
