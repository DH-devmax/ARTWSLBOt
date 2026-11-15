package control

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

var heatEvidence = regexp.MustCompile(`^[a-f0-9]{64}$`)

// HeatToggleEntitlement admits a human switch operation, not background
// execution. The workspace owner is checked independently of the website role.
func HeatToggleEntitlement(ctx context.Context, tx pgx.Tx, actor Actor, catalog []PermissionSpec,
	platform, group string) (WorkspaceServiceContext, error) {
	var out WorkspaceServiceContext
	permissions, err := LiveActorPermissions(ctx, tx, actor, catalog)
	if err != nil {
		return out, err
	}
	if !slices.Contains(permissions, "groups:ai") {
		return out, Denied(403, "permission_denied")
	}
	if err = RequireGroup(ctx, tx, actor, group); err != nil {
		return out, err
	}
	var tenant string
	err = tx.QueryRow(ctx, `SELECT b.tenant_id::text FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id WHERE b.group_id=$1 AND b.active AND g.platform=$2
		FOR UPDATE OF b`, group, platform).Scan(&tenant)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "group_not_found")
	}
	if err != nil {
		return out, err
	}
	out, err = WorkspaceServiceEntitlement(ctx, tx, tenant)
	if err != nil {
		return out, err
	}
	if out.Demo {
		if !out.humanOwner(actor) {
			return WorkspaceServiceContext{}, Denied(403, "workspace_owner_required")
		}
		return out, nil
	}
	var entitled bool
	err = tx.QueryRow(ctx, `SELECT 'ai'=ANY(p.modules) FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
		WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()
		AND $2=ANY(p.allowed_platforms) FOR SHARE OF sub,p`, tenant, platform).Scan(&entitled)
	if err == pgx.ErrNoRows {
		return out, Denied(403, "active_subscription_required")
	}
	if err != nil {
		return out, err
	}
	if !entitled {
		return out, Denied(403, "module_not_in_plan")
	}
	return out, nil
}

// SetHeatAdmission is an explicit administrator decision. It does not change
// readiness, authenticate accounts, create test grants or enable any group.
func (s *HeatChatService) SetHeatAdmission(ctx context.Context, ref SessionRef, platform string, enabled bool, version int64, evidence string) (int64, error) {
	if platform != "wsl" && platform != "lajiaoliao" || version < 1 || enabled && !heatEvidence.MatchString(evidence) {
		return 0, Denied(400, "heat_chat_acceptance_evidence_required")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return 0, err
	}
	if actor.Role != "superadmin" {
		return 0, Denied(403, "superadmin_required")
	}
	var next int64
	err = tx.QueryRow(ctx, `UPDATE heat_chat_admission SET enabled=$2,version=version+1,updated_at=clock_timestamp()
		WHERE platform=$1 AND version=$3 RETURNING version`, platform, enabled, version).Scan(&next)
	if err == pgx.ErrNoRows {
		return 0, Denied(409, "version_conflict")
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,tenant_id,action,resource_id,detail)
		VALUES($1,NULL,'heat_chat_admission_changed',$2,$3)`, actor.ID, platform,
		map[string]any{"enabled": enabled, "version": next, "evidence_sha256": evidence}); err != nil {
		return 0, err
	}
	if !enabled {
		if _, err = tx.Exec(ctx, `UPDATE heat_chat_jobs SET status='cancelled',result_code='platform_admission_revoked',
			updated_at=clock_timestamp() WHERE platform=$1 AND status IN('queued','generated')`, platform); err != nil {
			return 0, err
		}
	}
	return next, tx.Commit(ctx)
}

func (s *HeatChatService) ActivateRequested(ctx context.Context, tx pgx.Tx, group string) (bool, string, error) {
	scope, err := s.heatScope(ctx, tx, group, false)
	if err != nil {
		return false, heatErrorCode(err), nil
	}
	if !scope.Effective.Enabled {
		return false, "heat_chat_account_disabled", nil
	}
	if err = RequireGroupRuntime(ctx, tx, group); err != nil {
		return false, heatErrorCode(err), nil
	}
	if err = requireBusinessGate(ctx, tx, scope.Platform, group, "send_text", "heatbot"); err != nil {
		return false, heatErrorCode(err), nil
	}
	if err = scope.Service.requireGroup(ctx, tx, group); err != nil {
		return false, heatErrorCode(err), nil
	}
	if s.Gateway == nil {
		return false, "heat_chat_gateway_unavailable", nil
	}
	if err = scope.Service.requirePermission(ctx, tx, s.Gateway.Catalog, "groups:ai"); err != nil {
		return false, heatErrorCode(err), nil
	}
	if !scope.Service.Demo {
		if err = RequireTenantCapability(ctx, tx, scope.Tenant, "groups:ai"); err != nil {
			return false, heatErrorCode(err), nil
		}
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM heat_chat_admission WHERE platform=$1 AND enabled)
		AND EXISTS(SELECT 1 FROM account_groups WHERE account_id=$2 AND group_id=$3
			AND role IN('member','admin','owner') AND observation_source='native'
			AND observed_at>clock_timestamp()-interval '60 seconds' AND observed_at<=clock_timestamp())`,
		scope.Platform, scope.Account, scope.Group).Scan(&allowed)
	if err != nil {
		return false, "", err
	}
	if !allowed {
		return false, "heat_chat_platform_or_role_not_admitted", nil
	}
	tag, err := tx.Exec(ctx, `UPDATE group_bindings SET ai_enabled=true,ai_request_state='ready'
		WHERE group_id=$1 AND active AND ai_requested AND ai_account_id=$2 AND epoch=$3`,
		group, scope.Account, scope.BindingEpoch)
	if err != nil {
		return false, "", err
	}
	return tag.RowsAffected() == 1, "", nil
}

// Reconcile only observes the existing job's native echo. It cannot mint a
// model credential, allocate another client ID, or invoke the send port.
func (s *HeatChatService) Reconcile(ctx context.Context, id string) error {
	var group, client string
	var digest, sealed []byte
	var at time.Time
	err := s.DB.Platform.QueryRow(ctx, `SELECT j.group_id::text,a.client_id::text,j.scope_digest,j.response_sealed,a.message_at
		FROM heat_chat_jobs j JOIN heat_chat_send_attempts a ON a.job_id=j.id
		WHERE j.id=$1 AND j.status='unknown' AND j.response_sealed IS NOT NULL`, id).Scan(&group, &client, &digest, &sealed, &at)
	if err != nil {
		return err
	}
	scope, err := s.heatScope(ctx, s.DB.Platform, group, true)
	if err != nil || !bytes.Equal(scope.digest(), digest) {
		return Denied(409, "heat_chat_scope_changed")
	}
	raw, err := s.Vault.Open("heat-chat-response:"+id, sealed)
	if err != nil {
		return err
	}
	var text string
	err = json.Unmarshal(raw, &text)
	clear(raw)
	if err != nil {
		return err
	}
	messages, err := s.messages(ctx, scope, at.Add(-5*time.Second), true)
	if err != nil {
		return err
	}
	for _, m := range messages {
		if m.Group == scope.NativeGroup && m.Sender == scope.NativeSelf && m.ClientID == client && m.Text == text {
			_, err = s.DB.Platform.Exec(ctx, `UPDATE heat_chat_jobs SET status='sent',result_code='heat_chat_echo_confirmed',
				updated_at=clock_timestamp() WHERE id=$1 AND status='unknown'`, id)
			return err
		}
	}
	return Denied(409, "heat_chat_send_unconfirmed")
}
