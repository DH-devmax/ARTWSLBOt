package control

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func (s *GroupBindingService) saveOwnership(ctx context.Context, tx pgx.Tx, c bindingRecord, proof bindingProof) error {
	if c.ProofEvent == nil || *c.ProofEvent != proof.Event {
		return Denied(409, "group_binding_native_proof_required")
	}
	owner, err := s.Vault.Seal("group-binding-owner:"+c.Platform+":"+c.Group, []byte(proof.Owner))
	if err != nil {
		return err
	}
	applicant, err := s.Vault.Seal("group-binding-applicant:"+c.Platform+":"+c.Group, []byte(proof.Sender))
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `INSERT INTO group_binding_ownership(group_id,tenant_id,platform,owner_identity_hash,
		owner_identity_sealed,applicant_identity_sealed,applicant_role,robot_role,proof_challenge_id,proof_event_id,proof_kind,controlled_scope_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,nullif($12,'')::uuid) ON CONFLICT(group_id) DO UPDATE SET
		owner_identity_hash=excluded.owner_identity_hash,owner_identity_sealed=excluded.owner_identity_sealed,
		applicant_identity_sealed=excluded.applicant_identity_sealed,applicant_role=excluded.applicant_role,
		robot_role=excluded.robot_role,tenant_id=excluded.tenant_id,proof_challenge_id=excluded.proof_challenge_id,proof_event_id=excluded.proof_event_id,
		proof_kind=excluded.proof_kind,controlled_scope_id=excluded.controlled_scope_id,
		state='active',version=group_binding_ownership.version+1,verified_at=clock_timestamp(),observed_at=clock_timestamp()
		WHERE group_binding_ownership.platform=excluded.platform AND (group_binding_ownership.tenant_id=excluded.tenant_id
			OR EXISTS(SELECT 1 FROM customer_group_transfers tr WHERE tr.challenge_id=excluded.proof_challenge_id
				AND tr.group_id=excluded.group_id AND tr.source_tenant_id=group_binding_ownership.tenant_id
				AND tr.target_tenant_id=excluded.tenant_id))`,
		c.Group, c.Tenant, c.Platform, bindingIdentityDigest(c.Platform, c.Group, proof.Owner), owner, applicant,
		proof.SenderRole, proof.RobotRole, c.ID, proof.Event, bindingProofKind(c), c.ControlledScope)
	if err == nil && result.RowsAffected() != 1 {
		return Denied(409, "group_binding_ownership_scope_changed")
	}
	return err
}

// OwnershipScope is an internal read contract for fresh roster adapters. The
// applicant identity is never returned by an HTTP progress or audit endpoint.
// A missing anchor remains missing for legacy manually reviewed bindings.
func (s *GroupBindingService) OwnershipScope(ctx context.Context, platform, group string) (GroupBindingNativeScope, error) {
	var out GroupBindingNativeScope
	if !bindingPlatform(platform) || !ValidID(group) {
		return out, Denied(400, "group_binding_request_invalid")
	}
	if err := RequireControlledBinding(ctx, s.DB.Platform, "", platform, group); err != nil {
		return out, err
	}
	var sealed []byte
	err := s.DB.Platform.QueryRow(ctx, `SELECT g.platform,g.native_id,a.id::text,a.epoch,a.version,o.applicant_identity_sealed
		FROM group_binding_ownership o JOIN group_bindings b ON b.group_id=o.group_id AND b.tenant_id=o.tenant_id
		JOIN native_groups g ON g.id=o.group_id AND g.platform=o.platform JOIN accounts a ON a.id=b.primary_account_id
		WHERE o.group_id=$1 AND o.platform=$2 AND a.platform=g.platform AND a.purpose='management'`,
		group, platform).Scan(&out.Platform, &out.NativeGroupID, &out.AccountID, &out.AccountEpoch, &out.AccountVersion, &sealed)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "group_ownership_proof_required")
	}
	if err != nil {
		return out, err
	}
	plain, err := s.Vault.Open("group-binding-applicant:"+platform+":"+group, sealed)
	if err != nil {
		return out, err
	}
	out.NativeSenderID = string(plain)
	clear(plain)
	return out, nil
}

// ObserveGroupOwnership takes evidence from the authenticated roster adapter,
// never browser fields. It pauses independently of the caller's submission
// transaction so an authorization failure cannot roll the pause back.
func (s *GroupBindingService) ObserveGroupOwnership(ctx context.Context, scope GroupBindingNativeScope, roles GroupBindingRoleEvidence) error {
	if !bindingPlatform(scope.Platform) || !ValidID(scope.AccountID) || scope.NativeGroupID == "" ||
		roles.NativeGroupID != scope.NativeGroupID || roles.Present && roles.NativeOwnerID == "" ||
		!roles.Present && !roles.AbsenceVerified {
		return Denied(409, "group_ownership_evidence_required")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	var group, tenant, state, applicantRole, robotRole, controlledScope string
	var ownerHash, applicantSealed []byte
	err = tx.QueryRow(ctx, `SELECT o.group_id::text,o.tenant_id::text,o.state,o.owner_identity_hash,
		o.applicant_identity_sealed,o.applicant_role,o.robot_role,coalesce(o.controlled_scope_id::text,'') FROM group_binding_ownership o
		JOIN native_groups g ON g.id=o.group_id AND g.platform=o.platform
		JOIN group_bindings b ON b.group_id=o.group_id AND b.tenant_id=o.tenant_id
		WHERE o.platform=$1 AND g.native_id=$2 AND b.primary_account_id=$3
		FOR UPDATE OF o,b`, scope.Platform, scope.NativeGroupID, scope.AccountID).
		Scan(&group, &tenant, &state, &ownerHash, &applicantSealed, &applicantRole, &robotRole, &controlledScope)
	if err == pgx.ErrNoRows {
		return Denied(409, "group_ownership_proof_required")
	}
	if err != nil {
		return err
	}
	c := bindingRecord{Platform: scope.Platform, Account: scope.AccountID, Epoch: scope.AccountEpoch, AccountVersion: scope.AccountVersion}
	if _, err = s.connection(ctx, tx, c, &roles.GroupBindingConnectionEvidence); err != nil {
		return err
	}
	plain, err := s.Vault.Open("group-binding-applicant:"+scope.Platform+":"+group, applicantSealed)
	if err != nil {
		return err
	}
	applicant := string(plain)
	clear(plain)
	ownerChanged := roles.Present && !bytes.Equal(ownerHash, bindingIdentityDigest(scope.Platform, group, roles.NativeOwnerID))
	if roles.Present && !ownerChanged && roles.NativeSenderID != applicant {
		return Denied(409, "group_ownership_applicant_evidence_required")
	}
	changed := ownerChanged || !roles.Present || roles.SenderRole != applicantRole || roles.RobotRole != robotRole
	if changed && state == "active" {
		if controlledScope != "" {
			if err = s.closeControlledScopeTx(ctx, tx, controlledScope, "closed", "controlled_onboarding_role_changed"); err != nil {
				return err
			}
		} else {
			if _, err = tx.Exec(ctx, `UPDATE group_binding_ownership SET state='paused',version=version+1,
			observed_at=clock_timestamp() WHERE group_id=$1`, group); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE group_bindings SET management_enabled=false,ai_enabled=false,ai_requested=false,
			ai_request_state='disabled',version=version+1,epoch=epoch+1 WHERE group_id=$1`, group); err != nil {
				return err
			}
			if err = pauseBindingWork(ctx, tx, group, "group_ownership_changed"); err != nil {
				return err
			}
		}
		raw, _ := json.Marshal(map[string]any{"owner_changed": ownerChanged, "role_changed": !ownerChanged})
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,action,resource_id,detail)
			VALUES($1,'group_ownership_paused',$2,$3)`, tenant, group, raw); err != nil {
			return err
		}
	} else if _, err = tx.Exec(ctx, `UPDATE group_binding_ownership SET observed_at=clock_timestamp() WHERE group_id=$1`, group); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if changed || state == "paused" {
		return Denied(409, "group_ownership_reproof_required")
	}
	return nil
}
