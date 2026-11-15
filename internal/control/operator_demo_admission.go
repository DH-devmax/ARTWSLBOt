package control

import (
	"bytes"
	"context"

	"github.com/jackc/pgx/v5"
)

func activeCustomerOwnership(ctx context.Context, q Queryer, platform, group string) (bool, error) {
	var active bool
	err := q.QueryRow(ctx, `SELECT true FROM group_binding_ownership o
		JOIN group_bindings b ON b.group_id=o.group_id AND b.tenant_id=o.tenant_id
		WHERE o.group_id=$1 AND o.platform=$2 AND o.state='active' AND b.active
		AND o.proof_kind='authenticated_message' AND o.controlled_scope_id IS NULL
		FOR SHARE OF o,b`, group, platform).Scan(&active)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return active, err
}

func requireOperatorHumanOwnership(ctx context.Context, q Queryer, actor, platform, group string) error {
	fence, err := controlledExecutionScope(ctx, q, actor, platform, group)
	if err != nil || fence.ControlledScopeID != "" {
		return err
	}
	active, err := activeCustomerOwnership(ctx, q, platform, group)
	if err != nil || active {
		return err
	}
	return requireOperatorDemoClaim(ctx, q, actor, platform, group, "", nil)
}

// Only live human demo admission reads this record. Customer and background
// admission continue to require their authenticated-message ownership anchor.
func requireOperatorDemoClaim(ctx context.Context, q Queryer, actor, platform, group, account string, proof *SettingEvidence) error {
	fence, err := controlledExecutionScope(ctx, q, actor, platform, group)
	if err != nil {
		return err
	}
	if fence.ControlledScopeID != "" {
		return Denied(409, "controlled_onboarding_legacy_claim_forbidden")
	}
	var role string
	var identity []byte
	err = q.QueryRow(ctx, `SELECT c.robot_role,c.owner_identity_hash
		FROM operator_demo_group_claims c JOIN users u ON u.id=c.actor_id
		JOIN group_bindings b ON b.group_id=c.group_id AND b.tenant_id=c.tenant_id
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		WHERE c.actor_id=$1 AND c.platform=$2 AND c.group_id=$3
		AND ($4='' OR c.account_id::text=$4) AND c.state='active'
		AND c.expires_at>clock_timestamp()+interval '3 seconds'
		AND u.role='superadmin' AND NOT u.disabled AND u.access_version=c.actor_version
		AND b.active AND b.epoch=c.binding_epoch AND b.version=c.binding_version
		AND a.id=c.account_id AND a.epoch=c.account_epoch AND a.version=c.account_version
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND a.platform=c.platform AND a.workspace=c.platform AND a.purpose='management'
		AND g.platform=c.platform FOR SHARE OF c,u,b,a`, actor, platform, group, account).
		Scan(&role, &identity)
	if err == pgx.ErrNoRows {
		return Denied(409, "operator_demo_claim_required")
	}
	if err != nil {
		return err
	}
	if proof != nil && (proof.Role != role || !bytes.Equal(identity, bindingIdentityDigest(platform, group, proof.OwnerID))) {
		return Denied(409, "group_ownership_changed")
	}
	return nil
}

func (b *assistantBusiness) demoActor(ctx context.Context, q Queryer) (string, error) {
	plans, _ := b.tables()
	var actor string
	err := q.QueryRow(ctx, `SELECT p.actor_id::text FROM `+plans+` p JOIN users u ON u.id=p.actor_id
		WHERE p.id=$1 AND p.execution_mode='operator_demo' AND p.digest=$2
		AND u.role='superadmin' AND NOT u.disabled FOR SHARE OF p,u`, b.plan.ID, b.plan.Digest).Scan(&actor)
	if err == pgx.ErrNoRows {
		return "", Denied(403, "superadmin_required")
	}
	return actor, err
}

func (b *assistantBusiness) authorizeDemoOwnership(ctx context.Context, q Queryer, req SettingRequest, proof *SettingEvidence) error {
	actor, err := b.demoActor(ctx, q)
	if err != nil {
		return err
	}
	return requireOperatorDemoClaim(ctx, q, actor, b.plan.Scope.Platform, b.plan.Scope.GroupID, req.Account, proof)
}

func (b *assistantBusiness) revokeDemoOwnership(ctx context.Context) error {
	db, _ := b.storage()
	tx, err := db.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	actor, err := b.demoActor(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `WITH cancelled AS (
		UPDATE operator_demo_group_claims SET state='cancelled',
			blocked_reason='operator_demo_claim_scope_changed',version=version+1
		WHERE actor_id=$1 AND group_id=$2 AND platform=$3 AND state IN ('checking','active') RETURNING *)
		INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,detail)
		SELECT actor_id,tenant_id,'operator_demo_group_claim_invalidated',id::text,
			jsonb_build_object('platform',platform,'group_id',group_id) FROM cancelled`,
		actor, b.plan.Scope.GroupID, b.plan.Scope.Platform)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
