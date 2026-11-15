package control

import (
	"bytes"
	"context"

	"github.com/jackc/pgx/v5"
)

func (s *GroupBindingService) CloseControlledScope(ctx context.Context, ref SessionRef, id string, version int64) (ControlledOnboardingScope, error) {
	var out ControlledOnboardingScope
	if !ValidID(id) || version < 1 {
		return out, Denied(400, "controlled_onboarding_scope_invalid")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return out, err
	}
	a, err := s.controlledOwner(ctx, tx, ref)
	if err != nil {
		return out, err
	}
	out, err = s.readControlledScope(ctx, tx, a.ID, ref.assistantPlatform(), id)
	if err != nil {
		return out, err
	}
	if out.Version != version {
		return out, Denied(409, "version_conflict")
	}
	if err = s.closeControlledScopeTx(ctx, tx, id, "closed", "controlled_onboarding_scope_closed"); err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, a, out.TenantID, "controlled_onboarding_scope_closed", id,
		map[string]any{"platform": out.Platform, "group_id": out.GroupID}); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return s.ReadControlledScope(ctx, ref, id)
}

func (s *GroupBindingService) closeControlledScopeTx(ctx context.Context, tx pgx.Tx, id, state, reason string) error {
	tag, err := tx.Exec(ctx, `UPDATE onboarding_test_scopes SET state=$2,blocked_reason=$3,
		closed_at=clock_timestamp(),version=version+1 WHERE id=$1 AND state IN ('active','preparing','unknown')`, id, state, reason)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	// Prepared records never reached the native fence. Submitting and unknown
	// records retain their original identifier and are only reconciled.
	for _, query := range []string{
		`UPDATE group_binding_friend_attempts a SET status='failed',attempts_used=1,result_code=$2,updated_at=clock_timestamp()
			FROM group_binding_challenges c WHERE c.id=a.challenge_id AND c.controlled_scope_id=$1 AND a.status='prepared'`,
		`UPDATE customer_group_accept_attempts a SET status='failed',result_code=$2,updated_at=clock_timestamp()
			FROM group_binding_challenges c WHERE c.id=a.challenge_id AND c.controlled_scope_id=$1 AND a.status='prepared'`,
		`UPDATE group_binding_outbound_attempts a SET status='failed',result_code=$2,updated_at=clock_timestamp()
			FROM group_binding_challenges c WHERE c.id=a.challenge_id AND c.controlled_scope_id=$1 AND a.status='prepared'`,
		`UPDATE group_binding_challenges c SET state=CASE WHEN
			EXISTS(SELECT 1 FROM group_binding_friend_attempts a WHERE a.challenge_id=c.id
				AND a.status IN ('submitting','unknown')) OR
			EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id
				AND a.status IN ('submitting','unknown'))
			OR EXISTS(SELECT 1 FROM group_binding_outbound_attempts a WHERE a.challenge_id=c.id
				AND a.status IN ('submitting','unknown'))
			THEN c.state ELSE 'cancelled' END,blocked_reason=$2,version=version+1
			WHERE c.controlled_scope_id=$1 AND c.state IN ('waiting','proven','review')`,
	} {
		if _, err = tx.Exec(ctx, query, id, reason); err != nil {
			return err
		}
	}
	var group string
	err = tx.QueryRow(ctx, `UPDATE group_binding_ownership SET state='paused',version=version+1,
		observed_at=clock_timestamp() WHERE controlled_scope_id=$1 AND state='active' RETURNING group_id::text`, id).Scan(&group)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE group_bindings SET management_enabled=false,ai_enabled=false,ai_requested=false,
		ai_request_state='disabled',version=version+1,epoch=epoch+1 WHERE group_id=$1`, group); err != nil {
		return err
	}
	return pauseBindingWork(ctx, tx, group, reason)
}

func (s *GroupBindingService) expireControlledScopesTx(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT id::text,expires_at<=clock_timestamp() FROM onboarding_test_scopes
		WHERE state IN ('active','preparing') AND NOT onboarding_test_scope_active(id)
		ORDER BY expires_at,id LIMIT 20 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	type item struct {
		id      string
		expired bool
	}
	items := []item{}
	for rows.Next() {
		var current item
		if err = rows.Scan(&current.id, &current.expired); err != nil {
			rows.Close()
			return err
		}
		items = append(items, current)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, current := range items {
		state, reason := "closed", "controlled_onboarding_scope_changed"
		if current.expired {
			state, reason = "expired", "controlled_onboarding_scope_expired"
		}
		if err = s.closeControlledScopeTx(ctx, tx, current.id, state, reason); err != nil {
			return err
		}
	}
	return nil
}

// SweepControlledScopes reuses the existing onboarding scanner. Submission
// fences use live SQL checks and do not wait for this cleanup pass.
func (s *GroupBindingService) SweepControlledScopes(ctx context.Context) error {
	var stale bool
	if err := s.DB.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM onboarding_test_scopes
		WHERE state IN ('active','preparing') AND NOT onboarding_test_scope_active(id))`).Scan(&stale); err != nil || !stale {
		return err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	if err = s.expireControlledScopesTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func requireControlledChallenge(ctx context.Context, q Queryer, c bindingRecord) error {
	if c.ControlledScope == "" {
		return nil
	}
	var live bool
	err := q.QueryRow(ctx, `SELECT onboarding_test_scope_active(id) AND expires_at>clock_timestamp()+interval '3 seconds' FROM onboarding_test_scopes
		WHERE id=$1 AND actor_id=$2 AND tenant_id=$3 AND platform=$4 AND account_id=$5 AND group_id=$6
		AND account_epoch=$7 AND account_version=$8 FOR SHARE`, c.ControlledScope, c.Applicant, c.Tenant,
		c.Platform, c.Account, c.Group, c.Epoch, c.AccountVersion).Scan(&live)
	if err == pgx.ErrNoRows || err == nil && !live {
		return Denied(409, "controlled_onboarding_scope_inactive")
	}
	return err
}

func bindingChallengeSender(ctx context.Context, q Queryer, c bindingRecord, sender string) error {
	if c.ControlledScope == "" {
		return bindingHumanSender(ctx, q, c.Platform, sender)
	}
	if err := requireControlledChallenge(ctx, q, c); err != nil {
		return err
	}
	var matches bool
	err := q.QueryRow(ctx, `SELECT claimant_native_id=$2 FROM onboarding_test_scopes WHERE id=$1`,
		c.ControlledScope, sender).Scan(&matches)
	if err == nil && !matches {
		return Denied(409, "controlled_onboarding_claimant_mismatch")
	}
	return err
}

// An ordinary customer's authenticated-message proof and a temporary demo test
// proof are never interchangeable, even if the native sender is a group owner.
func RequireControlledBinding(ctx context.Context, q Queryer, actor, platform, group string) error {
	var proofKind, scopeActor, tenant string
	var live, own bool
	err := q.QueryRow(ctx, `SELECT o.proof_kind,coalesce(s.actor_id::text,''),o.tenant_id::text,
		CASE WHEN o.proof_kind='controlled_test' THEN onboarding_test_scope_active(s.id) ELSE true END,
		CASE WHEN o.proof_kind='controlled_test' THEN workspace_is_demo(o.tenant_id,s.actor_id) ELSE true END
		FROM group_binding_ownership o LEFT JOIN onboarding_test_scopes s ON s.id=o.controlled_scope_id
		WHERE o.group_id=$1 AND o.platform=$2`, group, platform).Scan(&proofKind, &scopeActor, &tenant, &live, &own)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil || proofKind != "controlled_test" {
		return err
	}
	if !live || !own || actor != "" && actor != scopeActor {
		return Denied(409, "controlled_onboarding_scope_inactive")
	}
	return nil
}

func (s *GroupBindingService) controlledRoleProof(ctx context.Context, q Queryer, c bindingRecord, proof bindingProof) error {
	if err := bindingChallengeSender(ctx, q, c, proof.Sender); err != nil || c.ControlledScope == "" {
		return err
	}
	var group, claimantRole, robotRole string
	var ownerHash []byte
	err := q.QueryRow(ctx, `SELECT g.native_id,s.claimant_role,s.robot_role,s.owner_identity_hash
		FROM onboarding_test_scopes s JOIN native_groups g ON g.id=s.group_id WHERE s.id=$1`, c.ControlledScope).
		Scan(&group, &claimantRole, &robotRole, &ownerHash)
	if err != nil {
		return err
	}
	if proof.Group != group || len(ownerHash) != 0 &&
		(proof.SenderRole != claimantRole || proof.RobotRole != robotRole ||
			!bytes.Equal(ownerHash, bindingIdentityDigest(c.Platform, c.Group, proof.Owner))) {
		return Denied(409, "controlled_onboarding_role_changed")
	}
	return nil
}

func bindingProofKind(c bindingRecord) string {
	if c.ControlledScope != "" {
		return "controlled_test"
	}
	return "authenticated_message"
}
