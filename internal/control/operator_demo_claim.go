package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type OperatorDemoClaim struct {
	ID            string     `json:"id,omitempty"`
	Group         string     `json:"group_id"`
	Platform      string     `json:"platform"`
	State         string     `json:"state"`
	Version       int64      `json:"version"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RobotRole     string     `json:"robot_role,omitempty"`
	BlockedReason string     `json:"blocked_reason"`
}

type operatorClaimScope struct {
	Actor                        Actor
	Group, Tenant                string
	Native                       GroupBindingNativeScope
	ActorVersion, BindingVersion int64
	BindingEpoch                 int64
}

func operatorDemoClaimScope(ctx context.Context, q Queryer, ref SessionRef, group string) (operatorClaimScope, error) {
	var out operatorClaimScope
	if !ValidID(group) || !bindingPlatform(ref.assistantPlatform()) {
		return out, Denied(404, "group_not_found")
	}
	actor, err := sessionActor(ctx, q, ref, true)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	out.Actor, out.Group = actor, group
	err = q.QueryRow(ctx, `SELECT b.tenant_id::text,b.version,b.epoch,a.id::text,a.epoch,a.version,
		g.native_id,a.native_uid,u.access_version FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		JOIN users u ON u.id=$3 WHERE b.group_id=$1 AND b.active AND g.platform=$2
		AND a.platform=g.platform AND a.workspace=g.platform AND a.purpose='management'
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL AND a.native_uid IS NOT NULL
		FOR SHARE OF b,a,u`, group, ref.assistantPlatform(), actor.ID).
		Scan(&out.Tenant, &out.BindingVersion, &out.BindingEpoch, &out.Native.AccountID,
			&out.Native.AccountEpoch, &out.Native.AccountVersion, &out.Native.NativeGroupID,
			&out.Native.NativeSenderID, &out.ActorVersion)
	out.Native.Platform = ref.assistantPlatform()
	if err == pgx.ErrNoRows {
		return out, Denied(409, "operator_demo_claim_scope_changed")
	}
	return out, err
}

func readOperatorClaim(ctx context.Context, q Queryer, scope operatorClaimScope, id string) (OperatorDemoClaim, error) {
	out := OperatorDemoClaim{Group: scope.Group, Platform: scope.Native.Platform, State: "unverified",
		BlockedReason: "operator_demo_claim_required"}
	var epoch, version, bindingEpoch, bindingVersion, actorVersion int64
	var account string
	err := q.QueryRow(ctx, `SELECT id::text,state,version,expires_at,robot_role,blocked_reason,
		account_id::text,account_epoch,account_version,binding_epoch,binding_version,actor_version
		FROM operator_demo_group_claims WHERE actor_id=$1 AND group_id=$2 AND platform=$3
		AND ($4='' OR id::text=$4) ORDER BY created_at DESC,id LIMIT 1`,
		scope.Actor.ID, scope.Group, scope.Native.Platform, id).
		Scan(&out.ID, &out.State, &out.Version, &out.ExpiresAt, &out.RobotRole, &out.BlockedReason,
			&account, &epoch, &version, &bindingEpoch, &bindingVersion, &actorVersion)
	if err == pgx.ErrNoRows && id == "" {
		return out, nil
	}
	if err == pgx.ErrNoRows {
		return out, Denied(404, "operator_demo_claim_not_found")
	}
	if err != nil {
		return out, err
	}
	if out.State == "checking" || out.State == "active" {
		if account != scope.Native.AccountID || epoch != scope.Native.AccountEpoch || version != scope.Native.AccountVersion ||
			bindingEpoch != scope.BindingEpoch || bindingVersion != scope.BindingVersion || actorVersion != scope.ActorVersion {
			out.State, out.BlockedReason = "stale", "operator_demo_claim_scope_changed"
		} else if out.ExpiresAt == nil || !time.Now().Before(*out.ExpiresAt) {
			out.State, out.BlockedReason = "expired", "operator_demo_claim_expired"
		}
	}
	return out, nil
}

// Progress is database-only; opening or refreshing a dialog never reads upstream.
func (s *GroupBindingService) OperatorDemoClaim(ctx context.Context, ref SessionRef, group string) (OperatorDemoClaim, error) {
	scope, err := operatorDemoClaimScope(ctx, s.DB.Platform, ref, group)
	if err != nil {
		return OperatorDemoClaim{}, err
	}
	return readOperatorClaim(ctx, s.DB.Platform, scope, "")
}

// CreateOperatorDemoClaim uses the already authenticated pool account as native
// evidence. It does not claim customer ownership, issue a package or send a message.
func (s *GroupBindingService) CreateOperatorDemoClaim(ctx context.Context, ref SessionRef, group string, version int64, key string) (OperatorDemoClaim, error) {
	var out OperatorDemoClaim
	if !onboardingKey(key) || version < 1 {
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
	scope, err := operatorDemoClaimScope(ctx, tx, ref, group)
	if err != nil {
		return out, err
	}
	var old, oldGroup, oldPlatform string
	var oldVersion int64
	err = tx.QueryRow(ctx, `SELECT id::text,group_id::text,platform,binding_version
		FROM operator_demo_group_claims WHERE actor_id=$1 AND create_key=$2`, scope.Actor.ID, key).
		Scan(&old, &oldGroup, &oldPlatform, &oldVersion)
	if err == nil {
		if oldGroup != group || oldPlatform != scope.Native.Platform || oldVersion != version {
			return out, Denied(409, "idempotency_conflict")
		}
		return readOperatorClaim(ctx, tx, scope, old)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if scope.BindingVersion != version {
		return out, Denied(409, "version_conflict")
	}
	var pending bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operator_demo_group_claims
		WHERE actor_id=$1 AND group_id=$2 AND state='checking'
		AND created_at>clock_timestamp()-interval '15 seconds')`, scope.Actor.ID, group).Scan(&pending)
	if err != nil {
		return out, err
	}
	if pending {
		return out, Denied(409, "operator_demo_claim_pending")
	}
	if s.Native == nil || s.Vault == nil {
		return out, Denied(503, "group_binding_native_unavailable")
	}
	if _, err = s.connection(ctx, tx, bindingRecord{Platform: scope.Native.Platform, Account: scope.Native.AccountID,
		Epoch: scope.Native.AccountEpoch, AccountVersion: scope.Native.AccountVersion}, nil); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE operator_demo_group_claims SET state='cancelled',version=version+1
		WHERE actor_id=$1 AND group_id=$2 AND state IN ('checking','active')`, scope.Actor.ID, group); err != nil {
		return out, err
	}
	id := NewID()
	_, err = tx.Exec(ctx, `INSERT INTO operator_demo_group_claims(id,actor_id,actor_version,tenant_id,group_id,
		platform,account_id,account_epoch,account_version,binding_epoch,binding_version,create_key)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, scope.Actor.ID, scope.ActorVersion, scope.Tenant,
		group, scope.Native.Platform, scope.Native.AccountID, scope.Native.AccountEpoch, scope.Native.AccountVersion,
		scope.BindingEpoch, scope.BindingVersion, key)
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, scope.Actor, scope.Tenant, "operator_demo_group_claim_started", id,
		map[string]any{"platform": scope.Native.Platform, "group_id": group}); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, Denied(503, "operator_demo_claim_commit_unknown")
	}
	// Reserving first makes a duplicate click/query reuse this operation even
	// during the native read. No caller automatically repeats a failed read.
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	roles, readErr := s.Native.BindingRoles(readCtx, scope.Native)
	cancel()
	save, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	return s.finishOperatorDemoClaim(save, ref, scope, id, roles, readErr)
}

func (s *GroupBindingService) finishOperatorDemoClaim(ctx context.Context, ref SessionRef, initial operatorClaimScope,
	id string, roles GroupBindingRoleEvidence, readErr error) (OperatorDemoClaim, error) {
	var out OperatorDemoClaim
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	current, err := operatorDemoClaimScope(ctx, tx, ref, initial.Group)
	if err != nil {
		return out, err
	}
	state, reason := "active", ""
	if current != initial {
		state, reason = "failed", "operator_demo_claim_scope_changed"
	} else if readErr != nil {
		state, reason = "failed", "operator_demo_claim_native_unavailable"
	} else if !roles.Present || roles.NativeGroupID != current.Native.NativeGroupID ||
		roles.NativeSenderID != current.Native.NativeSenderID || roles.NativeOwnerID == "" ||
		roles.RobotRole != "owner" && roles.RobotRole != "admin" ||
		roles.SenderRole != roles.RobotRole || (roles.RobotRole == "owner") != (roles.NativeOwnerID == current.Native.NativeSenderID) {
		state, reason = "failed", "operator_demo_claim_role_required"
	} else if _, err = s.connection(ctx, tx, bindingRecord{Platform: current.Native.Platform, Account: current.Native.AccountID,
		Epoch: current.Native.AccountEpoch, AccountVersion: current.Native.AccountVersion}, &roles.GroupBindingConnectionEvidence); err != nil {
		state, reason = "failed", "operator_demo_claim_scope_changed"
	}
	var ownerHash []byte
	role := ""
	if state == "active" {
		role, ownerHash = roles.RobotRole, bindingIdentityDigest(current.Native.Platform, current.Group, roles.NativeOwnerID)
	}
	tag, err := tx.Exec(ctx, `UPDATE operator_demo_group_claims SET state=$2,blocked_reason=$3,robot_role=$4,
		owner_identity_hash=$5,version=version+1 WHERE id=$1 AND state='checking' AND expires_at>clock_timestamp()`,
		id, state, reason, role, ownerHash)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() == 1 {
		if err = onboardingAudit(ctx, tx, current.Actor, current.Tenant, "operator_demo_group_claim_verified", id,
			map[string]any{"platform": current.Native.Platform, "group_id": current.Group, "state": state, "reason": reason}); err != nil {
			return out, err
		}
	}
	out, err = readOperatorClaim(ctx, tx, current, id)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *GroupBindingService) CancelOperatorDemoClaim(ctx context.Context, ref SessionRef, group, id string, version int64) (OperatorDemoClaim, error) {
	var out OperatorDemoClaim
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
	scope, err := operatorDemoClaimScope(ctx, tx, ref, group)
	if err != nil {
		return out, err
	}
	out, err = readOperatorClaim(ctx, tx, scope, id)
	if err != nil {
		return out, err
	}
	if out.State == "cancelled" {
		var newer bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operator_demo_group_claims
			WHERE actor_id=$1 AND group_id=$2 AND platform=$3 AND id<>$4
			AND state IN ('checking','active') AND expires_at>clock_timestamp())`,
			scope.Actor.ID, group, scope.Native.Platform, id).Scan(&newer)
		if err != nil {
			return out, err
		}
		if newer {
			return out, Denied(409, "operator_demo_claim_scope_changed")
		}
		return out, nil
	}
	if out.Version != version {
		return out, Denied(409, "version_conflict")
	}
	tag, err := tx.Exec(ctx, `UPDATE operator_demo_group_claims SET state='cancelled',version=version+1
		WHERE id=$1 AND actor_id=$2 AND version=$3`, id, scope.Actor.ID, version)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() != 1 {
		return out, Denied(409, "version_conflict")
	}
	if err = onboardingAudit(ctx, tx, scope.Actor, scope.Tenant, "operator_demo_group_claim_cancelled", id,
		map[string]any{"platform": scope.Native.Platform, "group_id": group}); err != nil {
		return out, err
	}
	out, err = readOperatorClaim(ctx, tx, scope, id)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// A deliberate configuration edit can continue its author's current test scope.
// The old proof stays immutable and the successor keeps its original deadline.
func continueOperatorDemoClaimAfterConfig(ctx context.Context, tx pgx.Tx, actor Actor, tenant, platform, group string,
	previousVersion, version int64) error {
	_, err := tx.Exec(ctx, `WITH previous AS (
		UPDATE operator_demo_group_claims c SET state='cancelled',version=c.version+1
		FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
		JOIN accounts a ON a.id=b.primary_account_id JOIN users u ON u.id=$1
		WHERE c.actor_id=u.id AND u.role='superadmin' AND NOT u.disabled
		AND c.actor_version=u.access_version AND c.group_id=$2 AND c.tenant_id=$3
		AND c.platform=$4 AND c.state='active' AND c.expires_at>clock_timestamp()+interval '3 seconds'
		AND b.group_id=c.group_id AND b.tenant_id=c.tenant_id AND b.active
		AND c.binding_version=$5 AND b.version=$6 AND b.epoch=c.binding_epoch+1
		AND a.id=c.account_id AND a.platform=c.platform AND g.platform=c.platform
		AND a.workspace=c.platform AND a.purpose='management'
		AND a.epoch=c.account_epoch AND a.version=c.account_version
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND NOT EXISTS(SELECT 1 FROM group_binding_ownership o
			WHERE o.group_id=b.group_id AND o.tenant_id=b.tenant_id AND o.platform=$4 AND o.state='active')
		RETURNING c.*
	), successor AS (
		INSERT INTO operator_demo_group_claims(id,actor_id,actor_version,tenant_id,group_id,platform,
			account_id,account_epoch,account_version,binding_epoch,binding_version,create_key,state,
			robot_role,owner_identity_hash,expires_at)
		SELECT $7,u.actor_id,u.actor_version,u.tenant_id,u.group_id,u.platform,u.account_id,
			u.account_epoch,u.account_version,u.binding_epoch+1,$6,
			'config:'||u.id::text||':'||$6::text,'active',u.robot_role,u.owner_identity_hash,u.expires_at
		FROM previous u RETURNING id
	)
	INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,detail)
	SELECT p.actor_id,p.tenant_id,'operator_demo_group_claim_config_continued',s.id::text,
		jsonb_build_object('platform',p.platform,'group_id',p.group_id,'previous_claim_id',p.id,
			'binding_version',$6,'expires_at',p.expires_at)
	FROM previous p CROSS JOIN successor s`, actor.ID, group, tenant, platform, previousVersion, version, NewID())
	return err
}
