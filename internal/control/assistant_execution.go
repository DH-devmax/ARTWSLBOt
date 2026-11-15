package control

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/database"
	"wangshangliao/internal/features"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

// Only control can construct this admission. It does not reuse test_windows or
// native_debug and is rechecked by the shared native final submission fence.
type assistantBusiness struct {
	service *AssistantService
	manual  *ManualActionService
	plan    AssistantActionPlan
}

func (b *assistantBusiness) storage() (*database.DB, *security.Vault) {
	if b.manual != nil {
		return b.manual.DB, b.manual.Vault
	}
	return b.service.DB, b.service.Vault
}

func (b *assistantBusiness) tables() (string, string) {
	if b.manual != nil {
		return "manual_action_plans", "manual_message_attempts"
	}
	return "assistant_action_plans", "assistant_message_attempts"
}

func (b *assistantBusiness) prepareAvailable(ctx context.Context, q Queryer, account string) error {
	var busy bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_rule_jobs WHERE account_id=$1
		AND status IN('executing','submitting','unknown')) OR EXISTS(SELECT 1 FROM business_rule_account_cooldowns
		WHERE account_id=$1 AND next_write_at>clock_timestamp()) OR EXISTS(
		SELECT 1 FROM native_setting_operations WHERE account_id=$1
		AND updated_at>clock_timestamp()-interval '5 seconds')`, account).Scan(&busy)
	if err != nil {
		return err
	}
	if busy {
		return Denied(409, "business_account_busy")
	}
	return nil
}

func (b *assistantBusiness) authorizeOwnership(ctx context.Context, q Queryer, req SettingRequest, proof *SettingEvidence) error {
	if err := b.plan.controlled.require(ctx, q, "", b.plan.Scope.Platform, b.plan.Scope.GroupID); err != nil {
		return err
	}
	var owner string
	var revision int64
	err := q.QueryRow(ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1 AND enabled
		AND owner<>'' AND lease_until>clock_timestamp()+interval '3 seconds'
		AND observed_at>clock_timestamp()-interval '15 seconds' AND snapshot->>'state'='connected' FOR SHARE`,
		req.Account).Scan(&owner, &revision)
	if err == pgx.ErrNoRows {
		return Denied(409, "native_text_connection_changed")
	}
	if err != nil {
		return err
	}
	if proof != nil && (proof.ReceiverOwner != owner || proof.ReceiverRevision != revision || proof.OwnerID == "" ||
		(proof.Role == "owner") != (proof.OwnerID == proof.UID)) {
		return Denied(409, "group_ownership_evidence_required")
	}
	var platform, tenant, state string
	var identity []byte
	err = q.QueryRow(ctx, `SELECT o.platform,o.tenant_id::text,o.state,o.owner_identity_hash
		FROM group_binding_ownership o JOIN group_bindings b ON b.group_id=o.group_id AND b.tenant_id=o.tenant_id
		WHERE o.group_id=$1 AND b.active AND b.primary_account_id=$2 FOR SHARE OF o,b`,
		b.plan.Scope.GroupID, req.Account).Scan(&platform, &tenant, &state, &identity)
	if err == pgx.ErrNoRows {
		if b.plan.ExecutionMode == "operator_demo" {
			return b.authorizeDemoOwnership(ctx, q, req, proof)
		}
		return Denied(409, "group_ownership_proof_required")
	}
	if err != nil {
		return err
	}
	if platform != b.plan.Scope.Platform || state != "active" || tenant == "" {
		if b.plan.ExecutionMode == "operator_demo" && platform == b.plan.Scope.Platform && tenant != "" {
			return b.authorizeDemoOwnership(ctx, q, req, proof)
		}
		return Denied(409, "group_ownership_reproof_required")
	}
	if proof != nil && !bytes.Equal(identity, bindingIdentityDigest(platform, b.plan.Scope.GroupID, proof.OwnerID)) {
		return Denied(409, "group_ownership_changed")
	}
	return nil
}

// A failed final transaction cannot undo the ownership pause. The onboarding
// service independently rechecks the live account/connection before pausing.
func (b *assistantBusiness) observeOwnership(ctx context.Context, req SettingRequest, proof SettingEvidence) error {
	if b.plan.ExecutionMode == "operator_demo" && b.plan.controlled.ControlledScopeID == "" {
		db, _ := b.storage()
		proven, err := activeCustomerOwnership(ctx, db.Platform, b.plan.Scope.Platform, b.plan.Scope.GroupID)
		if err != nil {
			return err
		}
		if !proven {
			return b.revokeDemoOwnership(ctx)
		}
	}
	db, vault := b.storage()
	bindings := GroupBindingService{DB: db, Vault: vault}
	return bindings.ObserveGroupOwnership(ctx, GroupBindingNativeScope{
		Platform: req.Workspace, AccountID: req.Account, AccountEpoch: req.Epoch,
		AccountVersion: req.Version, NativeGroupID: req.Change.Group, NativeSenderID: proof.UID,
	}, GroupBindingRoleEvidence{
		GroupBindingConnectionEvidence: GroupBindingConnectionEvidence{
			Platform: req.Workspace, AccountID: req.Account, AccountEpoch: req.Epoch, AccountVersion: req.Version,
			ReceiverOwner: proof.ReceiverOwner, ReceiverRevision: proof.ReceiverRevision,
			DeploymentVersion: proof.DeploymentVersion, CredentialDigest: proof.CredentialDigest, ObservedAt: proof.ObservedAt,
		},
		NativeGroupID: req.Change.Group, NativeSenderID: proof.UID, NativeOwnerID: proof.OwnerID,
		SenderRole: proof.Role, RobotRole: proof.Role, Present: true,
	})
}

type AssistantActionExecutor func(context.Context, SessionRef, AssistantActionPlan) (SettingOperation, error)

func (b *assistantBusiness) authorize(ctx context.Context, tx pgx.Tx, ref SessionRef, req SettingRequest) error {
	p := b.plan
	if req.TestWindowID != "" || req.Change.Kind != p.Action || req.Account != p.Scope.AccountID || req.Workspace != p.Scope.Platform || req.Epoch != p.AccountEpoch {
		return Denied(403, "assistant_scope_mismatch")
	}
	if b.manual != nil {
		return b.authorizeManual(ctx, tx, ref)
	}
	a, err := b.service.authorize(ctx, tx, ref, p.Scope, "assistant:action_approve", false, true)
	if err != nil {
		return err
	}
	if err = p.controlled.require(ctx, tx, a.ID, p.Scope.Platform, p.Scope.GroupID); err != nil {
		return err
	}
	if p.PolicyVersion == 0 {
		return Denied(409, "assistant_module_policy_changed")
	}
	if _, err = b.service.assistantPolicyFor(ctx, tx, a, p.Scope, "action_propose", "", p.Action, p.PolicyVersion); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT singleton FROM assistant_settings FOR SHARE`); err != nil {
		return err
	}
	settings, _, err := b.service.settings(ctx, tx)
	if err != nil {
		return err
	}
	if !settings.Enabled || !settings.ApprovalsEnabled {
		return Denied(409, "assistant_approvals_disabled")
	}
	grants, err := EffectivePermissions(ctx, tx, a, b.service.Catalog)
	if err != nil {
		return err
	}
	d, ok := features.FindAssistantAction(p.Scope.Platform, p.Action)
	if !ok || !d.Implemented {
		return Denied(409, "assistant_action_production_blocked")
	}
	e, err := ResolveHumanEntitlement(ctx, tx, a, b.service.Catalog, p.Scope.Platform, p.Scope.GroupID)
	if err != nil {
		return err
	}
	if p.ExecutionMode != "legacy" && p.ExecutionMode != e.Mode {
		return Denied(409, "assistant_scope_changed")
	}
	if err = RequireHumanBusinessAdmission(ctx, tx, a, b.service.Catalog, p.Scope.Platform, p.Scope.GroupID, p.Action, "assistant"); err != nil {
		return err
	}
	if !slices.Contains(grants, "assistant:action_propose") || !slices.Contains(grants, d.Permission) {
		return Denied(403, "permission_denied")
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT true FROM assistant_action_plans ap
		JOIN group_bindings b ON b.group_id=ap.group_id JOIN accounts a ON a.id=ap.account_id
		WHERE ap.id=$1 AND ap.actor_id=$2 AND ap.digest=$3 AND ap.status IN('approved','submitting')
		AND ap.expires_at>clock_timestamp()+interval '3 seconds'
		AND b.active AND b.primary_account_id=a.id AND b.epoch=ap.binding_epoch
		AND b.version=ap.base_version AND a.epoch=ap.account_epoch
		AND ap.domain=a.platform AND ap.execution_mode=$4
		FOR SHARE OF ap,b,a`, p.ID, a.ID, p.Digest, p.ExecutionMode).Scan(&allowed)
	if err == pgx.ErrNoRows {
		return Denied(409, "assistant_scope_changed")
	}
	if err != nil {
		return err
	}
	if !allowed {
		return Denied(409, "assistant_scope_changed")
	}
	if err = RequireGroup(ctx, tx, a, p.Scope.GroupID); err != nil {
		return err
	}
	return nil
}

func (b *assistantBusiness) authorizeManual(ctx context.Context, tx pgx.Tx, ref SessionRef) error {
	p := b.plan
	a, scope, e, err := b.manual.scope(ctx, tx, ref, p.Scope.GroupID, p.Action)
	if err != nil {
		return err
	}
	if err = p.controlled.require(ctx, tx, a.ID, scope.Platform, scope.GroupID); err != nil {
		return err
	}
	if scope != p.Scope || e.Mode != p.ExecutionMode {
		return Denied(409, "assistant_scope_changed")
	}
	if err = RequireHumanBusinessAdmission(ctx, tx, a, b.manual.Catalog, scope.Platform, scope.GroupID, p.Action, "manual"); err != nil {
		return err
	}
	fingerprint, err := humanActionHash(ctx, tx, a, scope, e)
	if err != nil {
		return err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT true FROM manual_action_plans ap
		JOIN group_bindings b ON b.group_id=ap.group_id JOIN accounts a ON a.id=ap.account_id
		WHERE ap.id=$1 AND ap.actor_id=$2 AND ap.digest=$3 AND ap.authorization_hash=$4
		AND ap.execution_mode=$5 AND ap.status IN('approved','submitting')
		AND ap.expires_at>clock_timestamp()+interval '3 seconds'
		AND b.active AND b.primary_account_id=a.id AND b.version=ap.base_version
		AND b.epoch=ap.binding_epoch AND a.epoch=ap.account_epoch AND a.platform=ap.domain
		FOR SHARE OF ap,b,a`, p.ID, a.ID, p.Digest, fingerprint, e.Mode).Scan(&valid)
	if err == pgx.ErrNoRows {
		return Denied(409, "assistant_scope_changed")
	}
	return err
}

func (b *assistantBusiness) roleFence(req SettingRequest, proof *SettingEvidence) (string, int64, error) {
	if proof != nil {
		if req.Change.Kind == "member_nickname" {
			if proof.Target != req.Change.Target || !settingRoleAllowed(proof.UID, req.Change.Target, proof.Role, proof.TargetRole, proof.Locked) {
				return "", 0, Denied(403, "native_setting_role_required")
			}
		} else {
			if !proof.ModerationAllowed || proof.Role != "owner" && proof.Role != "admin" {
				return "", 0, Denied(403, "native_moderation_role_required")
			}
			if req.Change.Target != "" && (proof.Target != req.Change.Target || req.Change.Target == proof.UID ||
				proof.TargetRole != "member" && !(proof.Role == "owner" && proof.TargetRole == "admin")) {
				return "", 0, Denied(403, "native_moderation_target_protected")
			}
		}
	}
	return b.plan.Scope.GroupID, b.plan.BindingEpoch, nil
}

// ExecuteAssistant shares durable native operations, transport checks and fresh
// role proof with manual Tools, while keeping their admission modes separate.
func (s *NativeSettingsService) ExecuteAssistant(ctx context.Context, ref SessionRef, service *AssistantService, planID string, req SettingRequest, port SettingPort) (SettingOperation, error) {
	plan, err := service.ActionPlan(ctx, ref, planID)
	if err != nil {
		return SettingOperation{}, err
	}
	if plan.Status != "approved" || req.TestWindowID != "" {
		return SettingOperation{}, Denied(409, "assistant_plan_not_pending")
	}
	var nativeGroup string
	err = s.DB.Platform.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1 AND platform=$2`, plan.Scope.GroupID, plan.Scope.Platform).Scan(&nativeGroup)
	if err != nil {
		return SettingOperation{}, err
	}
	expected := plan.settingChange(nativeGroup)
	if req.Change != expected || req.Key != plan.ID {
		return SettingOperation{}, Denied(403, "assistant_scope_mismatch")
	}
	req.business = &assistantBusiness{service: service, plan: plan}
	return s.execute(ctx, ref, req, port, nil)
}

// Only the sealed plan can supply a native message reference.
func (p AssistantActionPlan) settingChange(group string) nativeauth.SettingChange {
	change := nativeauth.SettingChange{Kind: p.Action, Group: group, Text: p.Text, Minutes: p.Minutes}
	if p.Target != nil {
		change.Target = p.Target.ID
	}
	if p.Action == "recall_message" {
		change.Target = ""
		if p.message != nil {
			raw, _ := json.Marshal(p.message)
			change.Text = string(raw)
		}
	}
	return change
}

func (s *NativeSettingsService) AssistantChange(ctx context.Context, ref SessionRef, service *AssistantService, id string) (nativeauth.SettingChange, error) {
	p, err := service.ActionPlan(ctx, ref, id)
	if err != nil || p.Status != "approved" {
		return nativeauth.SettingChange{}, Denied(409, "assistant_plan_not_pending")
	}
	var group string
	err = s.DB.Platform.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1 AND platform=$2`, p.Scope.GroupID, p.Scope.Platform).Scan(&group)
	return p.settingChange(group), err
}

func (s *NativeSettingsService) ManualChange(ctx context.Context, ref SessionRef, service *ManualActionService, group, id string) (nativeauth.SettingChange, error) {
	p, err := service.Plan(ctx, ref, group, id)
	if err != nil || p.Status != "approved" {
		return nativeauth.SettingChange{}, Denied(409, "assistant_plan_not_pending")
	}
	var native string
	err = s.DB.Platform.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1 AND platform=$2`, group, p.Scope.Platform).Scan(&native)
	return p.settingChange(native), err
}

func (s *NativeSettingsService) ExecuteManual(ctx context.Context, ref SessionRef, service *ManualActionService, group, id string, req SettingRequest, port SettingPort) (SettingOperation, error) {
	p, err := service.Plan(ctx, ref, group, id)
	if err != nil {
		return SettingOperation{}, err
	}
	expected, err := s.ManualChange(ctx, ref, service, group, id)
	if err != nil {
		return SettingOperation{}, err
	}
	if req.TestWindowID != "" || req.Change != expected || req.Key != p.ID {
		return SettingOperation{}, Denied(403, "assistant_scope_mismatch")
	}
	req.business = &assistantBusiness{manual: service, plan: p}
	return s.execute(ctx, ref, req, port, nil)
}
