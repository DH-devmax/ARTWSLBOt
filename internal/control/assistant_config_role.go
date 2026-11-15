package control

import (
	"bytes"
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type assistantConfigRoleProof struct {
	scope GroupBindingNativeScope
	roles GroupBindingRoleEvidence
}

func assistantConfigRoleScope(ctx context.Context, q Queryer, p AssistantPlan) (GroupBindingNativeScope, error) {
	var scope GroupBindingNativeScope
	err := q.QueryRow(ctx, `SELECT g.platform,g.native_id,a.id::text,a.epoch,a.version,a.native_uid
		FROM group_bindings b JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		WHERE b.group_id=$1 AND b.active AND b.version=$2 AND b.epoch=$3 AND a.id=$4 AND a.epoch=$5
		AND g.platform=$6 AND a.platform=$6 AND a.workspace=$6 AND a.purpose='management'
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL AND a.native_uid IS NOT NULL
		FOR SHARE OF b,a`, p.GroupID, p.BaseVersion, p.BindingEpoch, p.AccountID, p.AccountEpoch, p.Platform).
		Scan(&scope.Platform, &scope.NativeGroupID, &scope.AccountID, &scope.AccountEpoch,
			&scope.AccountVersion, &scope.NativeSenderID)
	if err == pgx.ErrNoRows {
		return scope, Denied(409, "assistant_scope_changed")
	}
	return scope, err
}

func (s *AssistantService) verifyConfigRole(ctx context.Context, tx pgx.Tx, p AssistantPlan, proof assistantConfigRoleProof) error {
	if err := p.controlled.require(ctx, tx, "", p.Platform, p.GroupID); err != nil {
		return err
	}
	current, err := assistantConfigRoleScope(ctx, tx, p)
	if err != nil {
		return err
	}
	roles := proof.roles
	if current != proof.scope || !roles.Present || roles.NativeGroupID != current.NativeGroupID ||
		roles.NativeSenderID != current.NativeSenderID || roles.NativeOwnerID == "" ||
		roles.RobotRole != "admin" && roles.RobotRole != "owner" || roles.SenderRole != roles.RobotRole ||
		(roles.RobotRole == "owner") != (roles.NativeOwnerID == current.NativeSenderID) {
		return Denied(409, "fresh_native_role_required")
	}
	bindings := GroupBindingService{DB: s.DB, Vault: s.Vault}
	if _, err = bindings.connection(ctx, tx, bindingRecord{
		Platform: current.Platform, Account: current.AccountID, Epoch: current.AccountEpoch,
		AccountVersion: current.AccountVersion,
	}, &roles.GroupBindingConnectionEvidence); err != nil {
		return err
	}
	var latestRole, source string
	var observed time.Time
	err = tx.QueryRow(ctx, `SELECT role,observation_source,observed_at FROM account_groups
		WHERE account_id=$1 AND group_id=$2 FOR SHARE`, p.AccountID, p.GroupID).
		Scan(&latestRole, &source, &observed)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil && source == "native" && (observed.After(roles.ObservedAt) || latestRole != roles.RobotRole) {
		return Denied(409, "assistant_scope_changed")
	}
	var customerOwner []byte
	err = tx.QueryRow(ctx, `SELECT owner_identity_hash FROM group_binding_ownership
		WHERE group_id=$1 AND platform=$2 AND state='active' FOR SHARE`, p.GroupID, p.Platform).Scan(&customerOwner)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil && !bytes.Equal(customerOwner, bindingIdentityDigest(p.Platform, p.GroupID, roles.NativeOwnerID)) {
		return Denied(409, "group_ownership_changed")
	}
	// A new roster cannot silently replace the owner used for the current demo proof.
	if p.ExecutionMode == "operator_demo" && p.controlled.ControlledScopeID == "" {
		var owner []byte
		err = tx.QueryRow(ctx, `SELECT owner_identity_hash FROM operator_demo_group_claims
			WHERE actor_id=(SELECT actor_id FROM assistant_plans WHERE id=$1) AND group_id=$2
			AND platform=$3 AND state='active' AND expires_at>clock_timestamp()
			ORDER BY created_at DESC,id LIMIT 1 FOR SHARE`, p.ID, p.GroupID, p.Platform).Scan(&owner)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == nil && !bytes.Equal(owner, bindingIdentityDigest(p.Platform, p.GroupID, roles.NativeOwnerID)) {
			return Denied(409, "group_ownership_changed")
		}
	}
	return nil
}
