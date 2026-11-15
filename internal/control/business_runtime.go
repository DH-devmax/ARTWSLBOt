package control

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

type BusinessRuntime struct {
	Group             string    `json:"group_id"`
	Tenant            string    `json:"tenant_id"`
	Enabled           bool      `json:"enabled"`
	ManagementEnabled bool      `json:"management_enabled"`
	Version           int64     `json:"version"`
	BindingVersion    int64     `json:"binding_version"`
	BindingEpoch      int64     `json:"binding_epoch"`
	CanManage         bool      `json:"can_manage"`
	BlockedReason     string    `json:"blocked_reason"`
	SampledAt         time.Time `json:"sampled_at"`
}

type businessRuntimeScope struct {
	BusinessRuntime
	controlledExecutionFence
	account, nativeGroup string
	accountEpoch         int64
	accountVersion       int64
}

func (s *GroupBindingService) businessRuntimeScope(ctx context.Context, tx pgx.Tx, ref SessionRef,
	group string, enabled bool) (Actor, businessRuntimeScope, error) {
	var scope businessRuntimeScope
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return a, scope, err
	}
	if err = RequireGroup(ctx, tx, a, group); err != nil {
		return a, scope, err
	}
	if a.Role != "superadmin" {
		if err = RequireTenantOwner(ctx, tx, a); err != nil {
			return a, scope, err
		}
	}
	permissions, err := LiveActorPermissions(ctx, tx, a, s.Catalog)
	if err != nil {
		return a, scope, err
	}
	if !slices.Contains(permissions, "groups:configure") {
		return a, scope, Denied(403, "permission_denied")
	}
	// Native writers lock the account before the binding. Keep the same order
	// while changing runtime, including when a paused account is being stopped.
	var account string
	err = tx.QueryRow(ctx, `SELECT a.id::text FROM accounts a JOIN group_bindings b ON b.primary_account_id=a.id
		WHERE b.group_id=$1 AND b.active AND ($2 OR b.tenant_id=nullif($3,'')::uuid) FOR SHARE OF a`,
		group, a.Role == "superadmin", a.TenantID).Scan(&account)
	if err == pgx.ErrNoRows {
		return a, scope, Denied(404, "group_not_found")
	}
	if err != nil {
		return a, scope, err
	}
	err = tx.QueryRow(ctx, `SELECT b.group_id::text,b.tenant_id::text,b.management_enabled,b.version,b.epoch,
		a.id::text,a.epoch,a.version,g.native_id,r.version FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		JOIN group_business_runtime r ON r.group_id=b.group_id AND r.tenant_id=b.tenant_id
		WHERE b.group_id=$1 AND b.active AND g.platform=$2 AND a.platform=g.platform AND a.purpose='management'
		AND a.id=$3 AND ($4 OR b.tenant_id=nullif($5,'')::uuid)
		FOR UPDATE OF b,r`, group, ref.assistantPlatform(), account, a.Role == "superadmin", a.TenantID).
		Scan(&scope.Group, &scope.Tenant, &scope.ManagementEnabled, &scope.BindingVersion, &scope.BindingEpoch,
			&scope.account, &scope.accountEpoch, &scope.accountVersion, &scope.nativeGroup, &scope.Version)
	if err == pgx.ErrNoRows {
		return a, scope, Denied(404, "group_not_found")
	}
	if err != nil {
		return a, scope, err
	}
	scope.controlledExecutionFence, err = controlledExecutionScope(ctx, tx, a.ID, ref.assistantPlatform(), group)
	if err != nil {
		return a, scope, err
	}
	service, err := WorkspaceServiceEntitlement(ctx, tx, scope.Tenant)
	if err != nil {
		return a, scope, err
	}
	if service.Demo {
		if !service.humanOwner(a) {
			return a, scope, Denied(403, "workspace_owner_required")
		}
	} else if err = RequireTenantCapability(ctx, tx, scope.Tenant, "groups:configure"); err != nil {
		return a, scope, err
	}
	if enabled && !service.Demo {
		var entitled bool
		err = tx.QueryRow(ctx, `SELECT p.modules && ARRAY['moderation','replies','schedules','rankings','cards','friends','ai']
			FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
			WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
			AND $2=ANY(p.allowed_platforms) FOR SHARE OF sub,p`, scope.Tenant, ref.assistantPlatform()).Scan(&entitled)
		if err == pgx.ErrNoRows || err == nil && !entitled {
			return a, scope, Denied(403, "active_subscription_required")
		}
	}
	return a, scope, err
}

// Enabling is a human control operation. It preserves configuration and uses
// the authenticated binding anchor, never a test grant or a cached role.
func (s *GroupBindingService) SaveBusinessRuntime(ctx context.Context, ref SessionRef,
	group string, enabled bool, version int64) (BusinessRuntime, error) {
	var out BusinessRuntime
	if !ValidID(group) || !bindingPlatform(ref.assistantPlatform()) || version < 0 {
		return out, Denied(400, "invalid_business_runtime")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	_, initial, err := s.businessRuntimeScope(ctx, tx, ref, group, enabled)
	_ = tx.Rollback(ctx)
	if err != nil {
		return out, err
	}
	if initial.Version != version {
		return out, Denied(409, "version_conflict")
	}
	var nativeScope GroupBindingNativeScope
	var roles GroupBindingRoleEvidence
	if enabled {
		if s.Native == nil || s.Vault == nil {
			return out, Denied(503, "group_binding_native_unavailable")
		}
		nativeScope, err = s.OwnershipScope(ctx, ref.assistantPlatform(), group)
		if err != nil {
			return out, err
		}
		roles, err = s.Native.BindingRoles(ctx, nativeScope)
		if err != nil {
			return out, err
		}
		if err = s.ObserveGroupOwnership(ctx, nativeScope, roles); err != nil {
			return out, err
		}
	}
	tx, err = s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, current, err := s.businessRuntimeScope(ctx, tx, ref, group, enabled)
	if err != nil {
		return out, err
	}
	if current.Version != version || current.Tenant != initial.Tenant ||
		current.controlledExecutionFence != initial.controlledExecutionFence ||
		current.BindingVersion != initial.BindingVersion || current.BindingEpoch != initial.BindingEpoch ||
		current.account != initial.account || current.accountEpoch != initial.accountEpoch || current.accountVersion != initial.accountVersion {
		return out, Denied(409, "version_conflict")
	}
	if enabled {
		if nativeScope.AccountID != current.account || nativeScope.NativeGroupID != current.nativeGroup ||
			nativeScope.AccountEpoch != current.accountEpoch || nativeScope.AccountVersion != current.accountVersion {
			return out, Denied(409, "group_binding_evidence_changed")
		}
		if err = s.requireRuntimeOwnership(ctx, tx, nativeScope, group, current.Tenant, roles); err != nil {
			return out, err
		}
	}
	if current.ManagementEnabled != enabled {
		err = tx.QueryRow(ctx, `UPDATE group_bindings SET management_enabled=$2,version=version+1,epoch=epoch+1
			WHERE group_id=$1 RETURNING version,epoch`, group, enabled).Scan(&current.BindingVersion, &current.BindingEpoch)
		if err != nil {
			return out, err
		}
	}
	// Changing management_enabled closes and versions runtime through the
	// binding trigger; the caller's original version was checked above.
	err = tx.QueryRow(ctx, `UPDATE group_business_runtime SET enabled=$3,updated_by=$4,version=version+1,
		updated_at=clock_timestamp() WHERE group_id=$1 AND tenant_id=$2 RETURNING version`,
		group, current.Tenant, enabled, a.ID).Scan(&current.Version)
	if err != nil {
		return out, err
	}
	if !enabled {
		for _, query := range []string{
			`UPDATE assistant_turns SET cancel_requested=true WHERE status IN('queued','running')
				AND session_id IN(SELECT id FROM assistant_sessions WHERE group_id=$1)`,
			`UPDATE operations SET status='cancelled',result_code='group_runtime_disabled',updated_at=clock_timestamp()
				WHERE group_id=$1 AND test_window_id IS NULL AND status='queued'`,
			`UPDATE business_rule_jobs SET status='cancelled',result_code='group_runtime_disabled',updated_at=clock_timestamp()
				WHERE group_id=$1 AND status='queued'`,
			`UPDATE heat_chat_jobs SET status='cancelled',result_code='group_runtime_disabled',updated_at=clock_timestamp()
				WHERE group_id=$1 AND status IN('queued','generated')`,
		} {
			if _, err = tx.Exec(ctx, query, group); err != nil {
				return out, err
			}
		}
	}
	current.Enabled, current.ManagementEnabled, current.CanManage = enabled, enabled, true
	if !enabled {
		current.BlockedReason = "group_runtime_disabled"
	}
	detail, _ := json.Marshal(map[string]any{"enabled": enabled, "management_enabled": enabled,
		"version": current.Version, "binding_version": current.BindingVersion, "binding_epoch": current.BindingEpoch})
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,detail)
		VALUES($1,$2,'group_business_runtime_changed',$3,$4)`, a.ID, current.Tenant, group, detail); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	current.SampledAt = time.Now().UTC()
	return current.BusinessRuntime, nil
}

func (s *GroupBindingService) requireRuntimeOwnership(ctx context.Context, tx pgx.Tx, scope GroupBindingNativeScope,
	group, tenant string, roles GroupBindingRoleEvidence) error {
	if err := RequireControlledBinding(ctx, tx, "", scope.Platform, group); err != nil {
		return err
	}
	var state, applicantRole, robotRole string
	var ownerHash, applicantSealed []byte
	err := tx.QueryRow(ctx, `SELECT state,owner_identity_hash,applicant_identity_sealed,applicant_role,robot_role
		FROM group_binding_ownership WHERE group_id=$1 AND tenant_id=$2 AND platform=$3 FOR SHARE`,
		group, tenant, scope.Platform).Scan(&state, &ownerHash, &applicantSealed, &applicantRole, &robotRole)
	if err == pgx.ErrNoRows {
		return Denied(409, "group_ownership_proof_required")
	}
	if err != nil {
		return err
	}
	if state != "active" || !roles.Present || roles.NativeGroupID != scope.NativeGroupID ||
		roles.NativeSenderID != scope.NativeSenderID || !slices.Contains([]string{"owner", "admin"}, roles.SenderRole) ||
		!slices.Contains([]string{"owner", "admin", "member"}, roles.RobotRole) || roles.SenderRole != applicantRole || roles.RobotRole != robotRole ||
		!bytes.Equal(ownerHash, bindingIdentityDigest(scope.Platform, group, roles.NativeOwnerID)) {
		return Denied(409, "group_ownership_reproof_required")
	}
	plain, err := s.Vault.Open("group-binding-applicant:"+scope.Platform+":"+group, applicantSealed)
	if err != nil {
		return err
	}
	matches := string(plain) == scope.NativeSenderID
	clear(plain)
	if !matches {
		return Denied(409, "group_ownership_applicant_evidence_required")
	}
	_, err = s.connection(ctx, tx, bindingRecord{Platform: scope.Platform, Account: scope.AccountID,
		Epoch: scope.AccountEpoch, AccountVersion: scope.AccountVersion}, &roles.GroupBindingConnectionEvidence)
	return err
}

// Runtime is an explicit group switch, not proof of native support or readiness.
// Binding changes close it in the database; configuration epochs remain separate.
func RequireGroupRuntime(ctx context.Context, q Queryer, group string) error {
	if !ValidID(group) {
		return Denied(404, "group_not_found")
	}
	var enabled bool
	var platform string
	err := q.QueryRow(ctx, `SELECT r.enabled,g.platform FROM group_business_runtime r
		JOIN group_bindings b ON b.group_id=r.group_id AND b.tenant_id=r.tenant_id
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		WHERE r.group_id=$1 AND b.active AND a.platform=g.platform AND a.workspace=g.platform
		AND a.purpose='management' FOR SHARE OF r,b`, group).Scan(&enabled, &platform)
	if err == pgx.ErrNoRows || err == nil && !enabled {
		return Denied(409, "group_runtime_disabled")
	}
	if err != nil {
		return err
	}
	return RequireControlledBinding(ctx, q, "", platform, group)
}
