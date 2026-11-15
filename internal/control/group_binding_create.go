package control

import (
	"bytes"
	"context"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/security"
)

func (s *GroupBindingService) Create(ctx context.Context, ref SessionRef, in GroupBindingCreate) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !bindingPlatform(in.Platform) || ref.assistantPlatform() != in.Platform || !onboardingKey(in.Key) ||
		in.TargetTenantID != "" && !ValidID(in.TargetTenantID) ||
		in.ControlledScopeID != "" && !ValidID(in.ControlledScopeID) ||
		in.RestoreGroupID != "" && (!ValidID(in.RestoreGroupID) || in.BindingVersion < 1) ||
		in.RestoreGroupID == "" && in.BindingVersion != 0 {
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
	a, err := s.owner(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	target := in.TargetTenantID
	ownSpace := false
	if a.Role == "superadmin" {
		space, spaceErr := OperatorWorkspace(ctx, tx, a.ID)
		if spaceErr != nil {
			return out, spaceErr
		}
		if target == "" {
			target = space.ID
		}
		ownSpace = target == space.ID
	}
	tenant, mode, err := bindingCreateTarget(a, target)
	if err != nil {
		return out, err
	}
	if !ownSpace {
		if _, err = bindingCustomer(ctx, tx, tenant, true); err != nil {
			return out, err
		}
	}
	var controlled *ControlledOnboardingScope
	if in.ControlledScopeID != "" {
		if a.Role != "superadmin" || !ownSpace || in.RestoreGroupID != "" {
			return out, Denied(403, "controlled_onboarding_own_workspace_required")
		}
		scope, scopeErr := s.readControlledScope(ctx, tx, a.ID, in.Platform, in.ControlledScopeID)
		if scopeErr != nil {
			return out, scopeErr
		}
		if scope.State != "active" || scope.TenantID != tenant {
			return out, Denied(409, "controlled_onboarding_scope_inactive")
		}
		controlled = &scope
	}
	digest := bindingCreateDigest(in)
	var oldID string
	var oldHash []byte
	err = tx.QueryRow(ctx, `SELECT id::text,request_hash FROM group_binding_challenges
		WHERE applicant_id=$1 AND create_key=$2`, a.ID, in.Key).Scan(&oldID, &oldHash)
	if err == nil {
		if !bytes.Equal(oldHash, digest) {
			return out, Denied(409, "idempotency_conflict")
		}
		c, err := loadBindingRecord(ctx, tx, oldID, false)
		if err != nil {
			return out, err
		}
		if _, err = s.checkRecord(ctx, tx, ref, c, true); err != nil {
			return out, err
		}
		return s.readTx(ctx, tx, ref, a, oldID)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if err = expireBindingChallenges(ctx, tx); err != nil {
		return out, err
	}
	var live bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_challenges WHERE applicant_id=$1
		AND platform=$2 AND state IN ('waiting','proven','review'))`, a.ID, in.Platform).Scan(&live); err != nil {
		return out, err
	}
	if live {
		return out, Denied(409, "group_binding_challenge_pending")
	}
	var selected string
	group := in.RestoreGroupID
	if controlled != nil {
		selected, group = controlled.AccountID, controlled.GroupID
	}
	if in.RestoreGroupID != "" {
		var currentTenant, state string
		var active bool
		var version int64
		err = tx.QueryRow(ctx, `SELECT b.tenant_id::text,coalesce(b.primary_account_id::text,''),b.active,b.version,
			coalesce(o.state,'') FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
			LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id
			WHERE b.group_id=$1 AND g.platform=$2 FOR UPDATE OF b`, in.RestoreGroupID, in.Platform).
			Scan(&currentTenant, &selected, &active, &version, &state)
		if err == pgx.ErrNoRows || err == nil && (currentTenant != tenant || version != in.BindingVersion ||
			active && state != "paused" && state != "") {
			return out, Denied(409, "group_binding_restore_not_allowed")
		}
		if err != nil {
			return out, err
		}
	}
	if err = bindingCapacityMode(ctx, tx, tenant, in.Platform, selected, group, "", mode); err != nil {
		return out, err
	}
	var epoch, version, actorVersion int64
	// A management pool contains at most ten accounts. Selection neither opens
	// another connection nor copies native identity into a second account.
	err = tx.QueryRow(ctx, `SELECT a.id::text,a.epoch,a.version FROM accounts a JOIN account_receivers r ON r.account_id=a.id
		WHERE a.platform=$1 AND a.workspace=$1 AND a.purpose='management'
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL AND a.native_uid IS NOT NULL
		AND r.enabled AND r.owner<>'' AND r.lease_until>clock_timestamp()
		AND r.observed_at>clock_timestamp()-interval '15 seconds' AND r.snapshot->>'state'='connected'
		AND ($2='' OR a.id::text=$2)
		AND (SELECT count(*) FROM group_bindings b WHERE b.active
			AND (b.primary_account_id=a.id OR b.ai_account_id=a.id) AND b.group_id::text<>$3)
			+(SELECT count(*) FROM group_binding_challenges c WHERE c.account_id=a.id
			AND c.state IN ('waiting','proven','review') AND c.reservation_until>clock_timestamp()
			AND NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id
				AND (b.primary_account_id=a.id OR b.ai_account_id=a.id)))<30
		AND NOT EXISTS(SELECT 1 FROM customer_group_accept_attempts ca WHERE ca.account_id=a.id
			AND ca.status IN ('prepared','submitting','unknown'))
		ORDER BY (SELECT count(*) FROM group_bindings b WHERE b.active AND b.primary_account_id=a.id),a.id
		LIMIT 1 FOR SHARE OF a,r`, in.Platform, selected, group).
		Scan(&out.Robot.ID, &epoch, &version)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "group_binding_robot_unavailable")
	}
	if err != nil {
		return out, err
	}
	if err = bindingCapacityMode(ctx, tx, tenant, in.Platform, out.Robot.ID, group, "", mode); err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, `SELECT access_version FROM users WHERE id=$1 FOR SHARE`, a.ID).Scan(&actorVersion); err != nil {
		return out, err
	}
	token, err := security.Token()
	if err != nil {
		return out, err
	}
	privateToken, err := security.Token()
	if err != nil {
		return out, err
	}
	id := NewID()
	_, err = tx.Exec(ctx, `INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,platform,account_id,
		account_epoch,account_version,applicant_version,token_hash,create_key,request_hash,group_id,binding_version,private_token_hash,applicant_mode,
		controlled_scope_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,nullif($12,'')::uuid,$13,$14,$15,nullif($16,'')::uuid)`,
		id, tenant, a.ID, in.Platform, out.Robot.ID, epoch, version, actorVersion, bindingTokenDigest(token),
		in.Key, digest, group, in.BindingVersion, bindingTokenDigest(privateToken), mode, in.ControlledScopeID)
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, a, tenant, "group_binding_challenge_created", id,
		map[string]any{"platform": in.Platform, "mode": mode, "restoring": in.RestoreGroupID != ""}); err != nil {
		return out, err
	}
	out, err = s.readTx(ctx, tx, ref, a, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GroupBindingChallenge{}, err
	}
	out.GroupChallenge, out.PrivateChallenge = "群绑定 "+token, "群接入 "+privateToken
	if port, ok := s.Native.(GroupBindingFriendNativePort); ok {
		projection, readErr := port.BindingRobot(ctx, GroupBindingNativeScope{Platform: in.Platform, AccountID: out.Robot.ID,
			AccountEpoch: epoch, AccountVersion: version})
		if readErr == nil && projection.ID == out.Robot.ID && projection.NativeID == out.Robot.NativeID {
			out.Robot = projection
		}
	}
	return out, nil
}
