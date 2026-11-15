package control

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type GroupBindingReview struct {
	Version int64 `json:"version"`
	Approve bool  `json:"approve"`
}

func (s *GroupBindingService) Confirm(ctx context.Context, ref SessionRef, id string, in GroupBindingConfirmation) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || in.Version < 1 || !onboardingKey(in.Key) {
		return out, Denied(400, "group_binding_request_invalid")
	}
	initial, _, err := s.confirmationRecord(ctx, ref, id, false)
	if err != nil {
		return out, err
	}
	if initial.State == "bound" || initial.State == "review" {
		if initial.ConfirmKey != in.Key {
			return out, Denied(409, "idempotency_conflict")
		}
		return s.Read(ctx, ref, id)
	}
	if initial.State != "proven" || initial.Version != in.Version || !initial.ExpiresAt.After(time.Now()) {
		return out, Denied(409, "group_binding_challenge_not_confirmable")
	}
	if err = requireOutboundResolved(ctx, s.DB.Platform, id); err != nil {
		return out, err
	}
	proof, roles, err := s.currentProof(ctx, initial)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return out, err
	}
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	actor, err := s.checkRecord(ctx, tx, ref, c, true)
	if err != nil {
		return out, err
	}
	if c.ConfirmKey == in.Key && (c.State == "bound" || c.State == "review") {
		return s.readTx(ctx, tx, ref, actor, id)
	}
	if c.Version != in.Version || c.State != "proven" || !c.ExpiresAt.After(time.Now()) {
		return out, Denied(409, "group_binding_challenge_not_confirmable")
	}
	if err = s.validateProof(ctx, tx, c, proof, roles); err != nil {
		return out, err
	}
	if err = bindingCapacity(ctx, tx, c.Tenant, c.Platform, c.Account, c.Group, c.ID); err != nil {
		return out, err
	}
	if c.ProofRole == "owner" && c.TransferFrom == "" {
		if err = s.bindProvenGroup(ctx, tx, c, proof); err != nil {
			return out, err
		}
		if err = s.saveOwnership(ctx, tx, c, proof); err != nil {
			return out, err
		}
		_, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET state='bound',confirm_key=$2,confirmed_at=clock_timestamp(),
			version=version+1,blocked_reason='' WHERE id=$1`, id, in.Key)
	} else {
		// The authenticated message proves an administrator, not website tenant
		// ownership. The independent superadmin review remains mandatory.
		_, err = tx.Exec(ctx, `INSERT INTO group_claims(tenant_id,group_id,account_id,applicant_id,native_identity,
			onboarding_challenge_id,proof_method) VALUES($1,$2,$3,$4,'authenticated-message',$5,$6)`,
			c.Tenant, c.Group, c.Account, c.Applicant, c.ID, bindingProofKind(c))
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET state='review',confirm_key=$2,confirmed_at=clock_timestamp(),
				reservation_until=created_at+interval '24 hours',version=version+1,blocked_reason=$3 WHERE id=$1`,
				id, in.Key, bindingReviewReason(c.TransferFrom))
		}
	}
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, actor, c.Tenant, "group_binding_confirmed", id,
		map[string]any{"platform": c.Platform, "role": c.ProofRole, "mode": c.Mode}); err != nil {
		return out, err
	}
	out, err = s.readTx(ctx, tx, ref, actor, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GroupBindingChallenge{}, err
	}
	return out, nil
}

func (s *GroupBindingService) Review(ctx context.Context, ref SessionRef, id string, in GroupBindingReview) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || in.Version < 1 {
		return out, Denied(400, "group_binding_request_invalid")
	}
	initial, _, err := s.confirmationRecord(ctx, ref, id, true)
	if err != nil {
		return out, err
	}
	if initial.State != "review" || initial.Version != in.Version || !initial.ReservationUntil.After(time.Now()) {
		return out, Denied(409, "group_binding_review_expired")
	}
	if initial.TransferFrom != "" {
		return out, Denied(409, "group_binding_transfer_review_required")
	}
	var proof bindingProof
	var roles GroupBindingRoleEvidence
	if in.Approve {
		proof, roles, err = s.currentProof(ctx, initial)
		if err != nil {
			return out, err
		}
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return out, err
	}
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	if a.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	if c.ControlledScope != "" && c.Applicant != a.ID {
		return out, Denied(404, "group_binding_challenge_not_found")
	}
	if c.Platform != ref.assistantPlatform() || c.State != "review" || c.Version != in.Version || !c.ReservationUntil.After(time.Now()) {
		return out, Denied(409, "group_binding_review_expired")
	}
	if c.TransferFrom != "" {
		return out, Denied(409, "group_binding_transfer_review_required")
	}
	state, claimState := "rejected", "rejected"
	if in.Approve {
		if err = bindingApplicant(ctx, tx, c, true); err != nil {
			return out, err
		}
		// Recheck the applicant's effective invitation grant and operator ceiling,
		// without impersonating the superadmin as the tenant applicant.
		if err = s.applicantPermission(ctx, tx, c); err != nil {
			return out, err
		}
		if err = s.validateProof(ctx, tx, c, proof, roles); err != nil {
			return out, err
		}
		if err = s.bindProvenGroup(ctx, tx, c, proof); err != nil {
			return out, err
		}
		if err = s.saveOwnership(ctx, tx, c, proof); err != nil {
			return out, err
		}
		state, claimState = "bound", "approved"
	}
	_, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET state=$2,reviewed_by=$3,reviewed_at=clock_timestamp(),
		version=version+1,blocked_reason='' WHERE id=$1`, id, state, a.ID)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE group_claims SET state=$2,reviewed_by=$3,reviewed_at=clock_timestamp(),
		review_note='authenticated-message review' WHERE onboarding_challenge_id=$1 AND state='pending'`, id, claimState, a.ID); err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, a, c.Tenant, "group_binding_reviewed", id, map[string]any{"approved": in.Approve}); err != nil {
		return out, err
	}
	out, err = s.readTx(ctx, tx, ref, a, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GroupBindingChallenge{}, err
	}
	return out, nil
}

func (s *GroupBindingService) Cancel(ctx context.Context, ref SessionRef, id string, version int64) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || version < 1 {
		return out, Denied(400, "group_binding_request_invalid")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return out, err
	}
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	if a.Role != "superadmin" {
		a, err = onboardingOwner(ctx, tx, ref, true)
		if err != nil {
			return out, err
		}
	}
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	if !bindingRecordMatches(c, a, ref.assistantPlatform()) {
		return out, Denied(404, "group_binding_challenge_not_found")
	}
	if c.Mode == "operator_demo" {
		if err = bindingApplicant(ctx, tx, c, true); err != nil {
			return out, err
		}
	}
	if c.Version != version || c.State != "waiting" && c.State != "proven" && c.State != "review" {
		return out, Denied(409, "group_binding_challenge_not_cancellable")
	}
	if err = requireOutboundResolved(ctx, tx, id); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET state='cancelled',version=version+1 WHERE id=$1`, id); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE group_claims SET state='expired' WHERE onboarding_challenge_id=$1 AND state='pending'`, id); err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, a, c.Tenant, "group_binding_cancelled", id, nil); err != nil {
		return out, err
	}
	out, err = s.readTx(ctx, tx, ref, a, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GroupBindingChallenge{}, err
	}
	return out, nil
}

func (s *GroupBindingService) confirmationRecord(ctx context.Context, ref SessionRef, id string, admin bool) (bindingRecord, Actor, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return bindingRecord{}, Actor{}, err
	}
	defer tx.Rollback(ctx)
	a, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return bindingRecord{}, a, err
	}
	c, err := loadBindingRecord(ctx, tx, id, false)
	if err != nil {
		return c, a, err
	}
	if c.Platform != ref.assistantPlatform() {
		return c, a, Denied(404, "group_binding_challenge_not_found")
	}
	if admin {
		if a.Role != "superadmin" {
			return c, a, Denied(403, "superadmin_required")
		}
	} else {
		a, err = s.checkRecord(ctx, tx, ref, c, false)
	}
	return c, a, err
}

func (s *GroupBindingService) currentProof(ctx context.Context, c bindingRecord) (bindingProof, GroupBindingRoleEvidence, error) {
	var p bindingProof
	var roles GroupBindingRoleEvidence
	if s.Native == nil || c.ProofEvent == nil || len(c.Proof) == 0 || s.Vault == nil {
		return p, roles, Denied(409, "group_binding_native_proof_required")
	}
	plain, err := s.Vault.Open("group-binding-proof:"+c.ID, c.Proof)
	if err != nil {
		return p, roles, err
	}
	defer clear(plain)
	if json.Unmarshal(plain, &p) != nil || p.Event != *c.ProofEvent || p.SenderRole != c.ProofRole {
		return p, roles, Denied(409, "group_binding_evidence_changed")
	}
	roles, err = s.Native.BindingRoles(ctx, c.scope(p.Group, p.Sender))
	return p, roles, err
}

func (s *GroupBindingService) validateProof(ctx context.Context, tx pgx.Tx, c bindingRecord, p bindingProof, roles GroupBindingRoleEvidence) error {
	if err := requireOutboundResolved(ctx, tx, c.ID); err != nil {
		return err
	}
	if err := s.checkOutboundSender(ctx, tx, c, p.Sender); err != nil {
		return err
	}
	if !bindingRolesMatch(p, roles) {
		return Denied(409, "group_binding_native_role_changed")
	}
	if err := s.controlledRoleProof(ctx, tx, c, p); err != nil {
		return err
	}
	if _, err := s.connection(ctx, tx, c, &roles.GroupBindingConnectionEvidence); err != nil {
		return err
	}
	if err := s.requireBindingPrivateProof(ctx, tx, c, p.Sender); err != nil {
		return err
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM native_decoded_events d JOIN native_received_events e
		ON e.id=d.event_id AND e.account_id=d.account_id JOIN native_groups g ON g.id=$4
		WHERE d.event_id=$1 AND d.account_id=$2 AND d.state='decoded' AND d.payload_sealed IS NOT NULL
		AND e.payload_sealed IS NOT NULL AND e.received_at>=$3 AND e.received_at<=$5
		AND g.platform=$6 AND g.native_id=$7)`, p.Event, c.Account, c.CreatedAt, c.Group, c.ExpiresAt, c.Platform, p.Group).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return Denied(409, "group_binding_source_expired")
	}
	return nil
}

func bindingRolesMatch(p bindingProof, r GroupBindingRoleEvidence) bool {
	return r.Present && r.NativeGroupID == p.Group && r.NativeSenderID == p.Sender &&
		r.NativeOwnerID == p.Owner && r.SenderRole == p.SenderRole && r.RobotRole == p.RobotRole &&
		(p.SenderRole == "owner" && p.Owner == p.Sender || p.SenderRole == "admin" && p.Owner != p.Sender) &&
		(r.RobotRole == "owner" || r.RobotRole == "admin" || r.RobotRole == "member") &&
		p.Owner != "" && p.Sender != "" && p.Group != ""
}

func (s *GroupBindingService) applicantPermission(ctx context.Context, tx pgx.Tx, c bindingRecord) error {
	if c.Mode == "operator_demo" {
		return bindingApplicant(ctx, tx, c, true)
	}
	permissions, err := EffectivePermissions(ctx, tx, Actor{ID: c.Applicant, TenantID: c.Tenant, Role: "user"}, s.Catalog)
	if err != nil {
		return err
	}
	for _, permission := range permissions {
		if permission == "claims:create" {
			return nil
		}
	}
	return Denied(403, "permission_denied")
}

// BindCustomerGroupTx is shared with the explicit legacy manual-review path.
// The caller holds admission lock 798423 and verifies identity independently.
// Passing no challenge cannot create an authenticated ownership anchor.
func BindCustomerGroupTx(ctx context.Context, tx pgx.Tx, tenant, platform, account, group string, expectedVersion int64, challenge string) error {
	return bindCustomerGroupTx(ctx, tx, tenant, platform, account, group, expectedVersion, challenge, "")
}

func bindCustomerGroupTx(ctx context.Context, tx pgx.Tx, tenant, platform, account, group string,
	expectedVersion int64, challenge, retiredPrivateProof string) error {
	if !ValidID(tenant) || !ValidID(account) || !ValidID(group) || !bindingPlatform(platform) {
		return Denied(400, "group_binding_request_invalid")
	}
	if err := bindingCapacity(ctx, tx, tenant, platform, account, group, challenge); err != nil {
		return err
	}
	var currentTenant, state string
	var currentVersion int64
	var active bool
	err := tx.QueryRow(ctx, `SELECT b.tenant_id::text,b.version,b.active,coalesce(o.state,'')
		FROM group_bindings b LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id
		WHERE b.group_id=$1 FOR UPDATE OF b`, group).Scan(&currentTenant, &currentVersion, &active, &state)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil && (currentTenant != tenant || currentVersion != expectedVersion ||
		active && state != "paused" && (state != "" || challenge == "")) ||
		err == pgx.ErrNoRows && expectedVersion != 0 {
		return Denied(409, "group_binding_restore_not_allowed")
	}
	if err = bindingOperationsResolvedExcept(ctx, tx, group, retiredPrivateProof); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.group_binding_challenge',$1,true)`, challenge); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled,ai_enabled)
		VALUES($1,$2,$3,false,false) ON CONFLICT(group_id) DO UPDATE SET primary_account_id=excluded.primary_account_id,
		active=true,management_enabled=false,ai_enabled=false,ai_requested=false,ai_request_state='disabled',
		version=group_bindings.version+1,epoch=group_bindings.epoch+1,
		config=jsonb_set(group_bindings.config,'{feature_options}',coalesce(
			(SELECT jsonb_object_agg(key,value||jsonb_build_object('enabled',false))
				FROM jsonb_each(CASE WHEN jsonb_typeof(group_bindings.config->'feature_options')='object'
					THEN group_bindings.config->'feature_options' ELSE '{}'::jsonb END)), '{}'::jsonb))`, group, tenant, account)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.group_binding_challenge','',true)`); err != nil {
		return err
	}
	return pauseBindingWork(ctx, tx, group, "group_binding_reproved")
}

func bindingReviewReason(transferFrom string) string {
	if transferFrom != "" {
		return "superadmin_transfer_review_required"
	}
	return "superadmin_review_required"
}

func bindingOperationsResolved(ctx context.Context, tx pgx.Tx, group string) error {
	return bindingOperationsResolvedExcept(ctx, tx, group, "")
}

func bindingOperationsResolvedExcept(ctx context.Context, tx pgx.Tx, group, retiredPrivateProof string) error {
	var pending bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE group_id=$1 AND status IN ('running','unknown')
		AND ($2='' OR id::text<>$2))
		OR EXISTS(SELECT 1 FROM native_setting_operations WHERE group_id=$1 AND status IN ('preparing','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM heat_chat_jobs WHERE group_id=$1 AND status IN ('sending','unknown'))
		OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE group_id=$1 AND status IN ('submitting','unknown'))
		OR EXISTS(SELECT 1 FROM customer_group_accept_attempts WHERE group_id=$1 AND status IN ('prepared','submitting','unknown'))`,
		group, retiredPrivateProof).Scan(&pending)
	if err != nil {
		return err
	}
	if pending {
		return Denied(409, "group_binding_operation_unresolved")
	}
	return nil
}

func pauseBindingWork(ctx context.Context, tx pgx.Tx, group, reason string) error {
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE operations SET status='cancelled',result_code=$2,updated_at=clock_timestamp() WHERE group_id=$1 AND status='queued'`, []any{group, reason}},
		{`UPDATE assistant_action_plans SET status='cancelled' WHERE group_id=$1 AND status IN ('pending_approval','blocked','approved')`, []any{group}},
		{`UPDATE assistant_plans SET status='cancelled' WHERE group_id=$1 AND status='pending_approval'`, []any{group}},
		{`UPDATE manual_action_plans SET status='cancelled' WHERE group_id=$1 AND status IN ('pending_approval','blocked','approved')`, []any{group}},
		{`UPDATE assistant_turns t SET cancel_requested=true FROM assistant_sessions se WHERE t.session_id=se.id AND se.group_id=$1 AND t.status IN ('queued','running')`, []any{group}},
		{`UPDATE heat_chat_jobs SET status='cancelled',result_code=$2,updated_at=clock_timestamp() WHERE group_id=$1 AND status IN ('queued','generated')`, []any{group, reason}},
		{`UPDATE business_rule_jobs SET status='cancelled',result_code=$2,updated_at=clock_timestamp() WHERE group_id=$1 AND status='queued'`, []any{group, reason}},
		{`UPDATE group_business_runtime SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE group_id=$1`, []any{group}},
	} {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			return err
		}
	}
	return nil
}
