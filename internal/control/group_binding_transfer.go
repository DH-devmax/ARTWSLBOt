package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Transfer approval is separate from an administrator's ordinary claim review.
// Source tenant and binding version must come from the superadmin's review view.
type GroupBindingTransferReview struct {
	Version        int64  `json:"version"`
	Approve        bool   `json:"approve"`
	SourceTenantID string `json:"source_tenant_id"`
	BindingVersion int64  `json:"binding_version"`
	Key            string `json:"-"`
}

func (s *GroupBindingService) ReviewTransfer(ctx context.Context, ref SessionRef, id string, in GroupBindingTransferReview) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || !ValidID(in.SourceTenantID) || in.Version < 1 || in.BindingVersion < 1 || !onboardingKey(in.Key) {
		return out, Denied(400, "group_binding_request_invalid")
	}
	initial, actor, err := s.confirmationRecord(ctx, ref, id, true)
	if err != nil {
		return out, err
	}
	if initial.TransferFrom != in.SourceTenantID || initial.BindingVersion != in.BindingVersion || initial.ProofRole != "owner" {
		return out, Denied(409, "group_binding_transfer_scope_changed")
	}
	if transferReviewReplay(initial, actor, in) {
		return s.Read(ctx, ref, id)
	}
	if initial.State != "review" || initial.Version != in.Version || !initial.ReservationUntil.After(time.Now()) {
		return out, Denied(409, "group_binding_review_expired")
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
	actor, err = sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	if c.ControlledScope != "" && c.Applicant != actor.ID {
		return out, Denied(404, "group_binding_challenge_not_found")
	}
	if c.Platform != ref.assistantPlatform() || c.TransferFrom != in.SourceTenantID ||
		c.BindingVersion != in.BindingVersion || c.ProofRole != "owner" {
		return out, Denied(409, "group_binding_transfer_scope_changed")
	}
	if transferReviewReplay(c, actor, in) {
		return s.readTx(ctx, tx, ref, actor, id)
	}
	if c.State != "review" || c.Version != in.Version || !c.ReservationUntil.After(time.Now()) {
		return out, Denied(409, "group_binding_review_expired")
	}
	state, claimState := "rejected", "rejected"
	if in.Approve {
		if err = bindingApplicant(ctx, tx, c, true); err != nil {
			return out, err
		}
		if err = s.applicantPermission(ctx, tx, c); err != nil {
			return out, err
		}
		if err = s.validateProof(ctx, tx, c, proof, roles); err != nil {
			return out, err
		}
		if err = s.transferBinding(ctx, tx, actor, c, in.Key); err != nil {
			return out, err
		}
		if err = s.saveOwnership(ctx, tx, c, proof); err != nil {
			return out, err
		}
		state, claimState = "bound", "approved"
	}
	if _, err = tx.Exec(ctx, `UPDATE group_binding_challenges SET state=$2,reviewed_by=$3,reviewed_at=clock_timestamp(),
		transfer_review_key=$4,version=version+1,blocked_reason='' WHERE id=$1`, c.ID, state, actor.ID, in.Key); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE group_claims SET state=$2,reviewed_by=$3,reviewed_at=clock_timestamp(),
		review_note='authenticated owner transfer review' WHERE onboarding_challenge_id=$1 AND state='pending'`,
		c.ID, claimState, actor.ID); err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, actor, c.Tenant, "group_binding_transfer_reviewed", c.ID,
		map[string]any{"approved": in.Approve, "platform": c.Platform}); err != nil {
		return out, err
	}
	out, err = s.readTx(ctx, tx, ref, actor, c.ID)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GroupBindingChallenge{}, err
	}
	return out, nil
}

func transferReviewReplay(c bindingRecord, actor Actor, in GroupBindingTransferReview) bool {
	return c.ReviewedBy == actor.ID && c.TransferReviewKey == in.Key && c.Version == in.Version+1 &&
		(in.Approve && c.State == "bound" || !in.Approve && c.State == "rejected")
}

// The caller holds the shared capacity lock, the challenge lock and a fresh
// authenticated owner proof. No native action occurs in a website transfer.
func (s *GroupBindingService) transferBinding(ctx context.Context, tx pgx.Tx, actor Actor, c bindingRecord, key string) error {
	var source string
	var version, epoch int64
	err := tx.QueryRow(ctx, `SELECT tenant_id::text,version,epoch FROM group_bindings WHERE group_id=$1 FOR UPDATE`,
		c.Group).Scan(&source, &version, &epoch)
	if err != nil {
		return err
	}
	if source != c.TransferFrom || source == c.Tenant || version != c.BindingVersion {
		return Denied(409, "group_binding_transfer_scope_changed")
	}
	if err = bindingCapacity(ctx, tx, c.Tenant, c.Platform, c.Account, c.Group, c.ID); err != nil {
		return err
	}
	if err = bindingOperationsResolved(ctx, tx, c.Group); err != nil {
		return err
	}
	// Archive only to sealed operator-owned storage. The new tenant receives
	// clean settings; finance, activities and conversation history stay scoped.
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT jsonb_build_object('binding',to_jsonb(b),'chat',
		(SELECT to_jsonb(cp) FROM group_chat_preferences cp WHERE cp.group_id=b.group_id),
		'ownership',(SELECT to_jsonb(o) FROM group_binding_ownership o WHERE o.group_id=b.group_id))
		FROM group_bindings b WHERE b.group_id=$1`, c.Group).Scan(&raw)
	if err != nil {
		return err
	}
	defer clear(raw)
	id := NewID()
	sealed, err := s.Vault.Seal("customer-group-transfer:"+id, raw)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO customer_group_transfers(id,challenge_id,group_id,platform,
		source_tenant_id,target_tenant_id,reviewed_by,review_key,source_binding_version,source_binding_epoch,archived_settings_sealed)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, c.ID, c.Group, c.Platform, source, c.Tenant,
		actor.ID, key, version, epoch, sealed); err != nil {
		return err
	}
	if err = pauseBindingWork(ctx, tx, c.Group, "group_tenant_transferred"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.group_binding_challenge',$1,true)`, c.ID); err != nil {
		return err
	}
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE test_windows SET cancelled=true WHERE group_id=$1 AND NOT cancelled`, []any{c.Group}},
		{`UPDATE claim_codes SET consumed_at=clock_timestamp() WHERE group_id=$1 AND consumed_at IS NULL`, []any{c.Group}},
		{`UPDATE group_claims SET state='expired' WHERE group_id=$1 AND state='pending' AND onboarding_challenge_id IS DISTINCT FROM $2::uuid`, []any{c.Group, c.ID}},
		{`DELETE FROM member_groups WHERE group_id=$1`, []any{c.Group}},
		{`DELETE FROM group_chat_preferences WHERE group_id=$1`, []any{c.Group}},
		{`DELETE FROM heat_chat_consumers WHERE group_id=$1`, []any{c.Group}},
		{`DELETE FROM business_rule_consumers WHERE group_id=$1`, []any{c.Group}},
		{`UPDATE group_bindings SET tenant_id=$2,primary_account_id=$3,ai_account_id=NULL,active=true,
			management_enabled=false,ai_enabled=false,ai_requested=false,ai_request_state='disabled',
			config='{}',epoch=epoch+1,version=version+1,data_since=clock_timestamp() WHERE group_id=$1`,
			[]any{c.Group, c.Tenant, c.Account}},
		{`UPDATE users SET access_version=access_version+1 WHERE tenant_id IN ($1::uuid,$2::uuid)`, []any{source, c.Tenant}},
	} {
		if _, err = tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.group_binding_challenge','',true)`); err != nil {
		return err
	}
	return onboardingAudit(ctx, tx, actor, source, "group_binding_transferred_out", c.Group,
		map[string]any{"transfer_id": id, "platform": c.Platform})
}
