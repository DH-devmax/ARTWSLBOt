package control

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
)

type AssistantPlan struct {
	Platform         string                    `json:"platform"`
	ID               string                    `json:"id"`
	SessionID        string                    `json:"session_id"`
	AccountID        string                    `json:"account_id"`
	GroupID          string                    `json:"group_id"`
	BaseVersion      int64                     `json:"base_version"`
	AccountEpoch     int64                     `json:"-"`
	BindingEpoch     int64                     `json:"-"`
	Digest           string                    `json:"digest"`
	Version          int64                     `json:"version"`
	Status           string                    `json:"status"`
	ExpiresAt        time.Time                 `json:"expires_at"`
	Changes          FeatureChanges            `json:"changes,omitempty"`
	Before           map[string]map[string]any `json:"before,omitempty"`
	ExecutionID      string                    `json:"execution_id,omitempty"`
	CommittedVersion int64                     `json:"committed_version,omitempty"`
	Applied          bool                      `json:"applied"`
	Message          string                    `json:"message"`
	ExecutionMode    string                    `json:"execution_mode"`
	GroupName        string                    `json:"group_name"`
	Summary          string                    `json:"summary"`
	PolicyVersion    int64                     `json:"policy_version,omitempty"`
	Capabilities     []string                  `json:"capabilities,omitempty"`
	controlled       controlledExecutionFence
}
type planContent struct {
	controlledExecutionFence
	Changes       FeatureChanges            `json:"changes"`
	Before        map[string]map[string]any `json:"before"`
	PolicyVersion int64                     `json:"policy_version,omitempty"`
	Capabilities  []string                  `json:"capabilities,omitempty"`
}

func (s *AssistantService) propose(ctx context.Context, ref SessionRef, session AssistantSession, call assistant.ToolCall) (assistant.ToolResult, error) {
	out := assistant.ToolResult{Type: "propose_group_config_change", Scope: session.Scope, SampledAt: time.Now().UTC(), Verification: "suggestion_only"}
	var args struct {
		GroupID     string         `json:"group_id"`
		Changes     FeatureChanges `json:"changes"`
		BaseVersion int64          `json:"base_version"`
	}
	if err := strictJSON(call.Function.Arguments, &args); err != nil {
		return out, err
	}
	if args.GroupID == "" && session.Scope.Kind == "group" && ValidID(session.Scope.AccountID) && ValidID(session.Scope.GroupID) {
		args.GroupID = session.Scope.GroupID
	}
	if !ValidID(args.GroupID) || session.Scope.Kind != "group" || args.GroupID != session.Scope.GroupID {
		return out, Denied(403, "assistant_group_scope_required")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := s.authorize(ctx, tx, ref, session.Scope, "assistant:propose", true, true)
	if err != nil {
		return out, err
	}
	var plan AssistantPlan
	plan.Platform = session.Scope.Platform
	plan.ID, plan.SessionID, plan.AccountID, plan.GroupID, plan.Status, plan.Version = NewID(), session.ID,
		session.Scope.AccountID, args.GroupID, "pending_approval", 1
	var tenant string
	var raw []byte
	entitlement, err := ResolveHumanEntitlement(ctx, tx, a, s.Catalog, session.Scope.Platform, args.GroupID)
	if err != nil {
		return out, err
	}
	plan.ExecutionMode = entitlement.Mode
	err = tx.QueryRow(ctx, `SELECT b.tenant_id::text,b.config,b.version,b.epoch,a.epoch,g.name
        FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
        JOIN native_groups g ON g.id=b.group_id
        WHERE b.group_id=$1 AND b.primary_account_id=$2 AND b.active AND a.workspace=$3
        FOR SHARE OF b,a`, args.GroupID, plan.AccountID, session.Scope.Platform).
		Scan(&tenant, &raw, &plan.BaseVersion, &plan.BindingEpoch, &plan.AccountEpoch, &plan.GroupName)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "assistant_scope_not_found")
	}
	if err != nil {
		return out, err
	}
	if a.Role != "superadmin" {
		for _, code := range []string{"assistant:use", "assistant:propose"} {
			if err = RequireTenantCapability(ctx, tx, tenant, code); err != nil {
				return out, err
			}
		}
		if err = requireFeatureCeilings(ctx, tx, tenant, args.Changes); err != nil {
			return out, err
		}
	}
	if args.BaseVersion != plan.BaseVersion {
		return out, Denied(409, "version_conflict")
	}
	if len(raw) > assistant.MaxResultBytes-2048 {
		return out, Denied(409, "assistant_truncated_config")
	}
	grants, err := EffectivePermissions(ctx, tx, a, s.Catalog)
	if err != nil {
		return out, err
	}
	var config features.Config
	if err = json.Unmarshal(raw, &config); err != nil {
		return out, err
	}
	_, _, err = ValidateFeatures(config, args.Changes, a, grants, entitlement.Modules, true)
	if err != nil {
		return out, err
	}
	content := planContent{Changes: args.Changes, Before: map[string]map[string]any{}}
	content.controlledExecutionFence, err = controlledExecutionScope(ctx, tx, a.ID, plan.Platform, plan.GroupID)
	if err != nil {
		return out, err
	}
	plan.controlled = content.controlledExecutionFence
	content.PolicyVersion, err = s.checkConfigExposure(ctx, tx, a, session.Scope, args.Changes, 0)
	if err != nil {
		return out, err
	}
	plan.PolicyVersion = content.PolicyVersion
	for _, id := range sortedAssistantKeys(args.Changes) {
		content.Capabilities = append(content.Capabilities, id+".config_propose")
	}
	plan.Capabilities = content.Capabilities
	for id, fields := range args.Changes {
		content.Before[id] = map[string]any{}
		for field := range fields {
			content.Before[id][field] = features.Values(config, id)[field]
		}
	}
	blob, _ := json.Marshal(content)
	if len(blob) > assistant.MaxResultBytes-2048 {
		return out, assistant.ErrLimit
	}
	plan.Digest = digest(struct {
		Plan    AssistantPlan
		Content planContent
	}{plan, content})
	sealed, err := s.Vault.Seal("assistant:plan:"+plan.ID, blob)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO assistant_plans(id,session_id,actor_id,tenant_id,account_id,group_id,base_version,
        account_epoch,binding_epoch,digest,payload_sealed,domain,execution_mode) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING expires_at`,
		plan.ID, session.ID, a.ID, tenant, plan.AccountID, plan.GroupID, plan.BaseVersion, plan.AccountEpoch, plan.BindingEpoch, plan.Digest, sealed, session.Scope.Platform, plan.ExecutionMode).Scan(&plan.ExpiresAt)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        VALUES($1,$2,'assistant_plan_proposed',$3,jsonb_build_object('base_version',$4::bigint))`, tenant, a.ID, plan.ID, plan.BaseVersion); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	plan.Changes, plan.Before = content.Changes, content.Before
	plan.Message = "仅为建议；尚未保存配置"
	plan.Summary = assistantConfigSummary(plan)
	out.Data = plan
	return out, nil
}

func (s *AssistantService) Plan(ctx context.Context, ref SessionRef, id string) (AssistantPlan, error) {
	var out AssistantPlan
	if !ValidID(id) {
		return out, Denied(404, "assistant_plan_not_found")
	}
	a, err := s.actor(ctx, s.DB.Platform, ref, false)
	if err != nil {
		return out, err
	}
	var sealed []byte
	if batch := cachedAssistantPlans(ctx, a.ID, ref.assistantPlatform()); batch != nil {
		record, ok := batch.configs[id]
		if !ok {
			err = pgx.ErrNoRows
		} else {
			out, sealed = record.AssistantPlan, record.Sealed
		}
	} else {
		err = s.read(ctx, a, ref.assistantPlatform(), func(q Queryer) error {
			return q.QueryRow(ctx, `SELECT id::text,session_id::text,account_id::text,
        group_id::text,base_version,account_epoch,binding_epoch,digest,version,status,expires_at,payload_sealed,
        coalesce(execution_id::text,''),coalesce(committed_version,0),execution_mode,
        coalesce((SELECT g.name FROM native_groups g WHERE g.id=p.group_id AND g.platform=p.domain),'')
        FROM assistant_plans p
        WHERE id=$1 AND actor_id=$2 AND domain=$3`, id, a.ID, ref.assistantPlatform()).Scan(&out.ID, &out.SessionID, &out.AccountID, &out.GroupID,
				&out.BaseVersion, &out.AccountEpoch, &out.BindingEpoch, &out.Digest, &out.Version, &out.Status, &out.ExpiresAt, &sealed,
				&out.ExecutionID, &out.CommittedVersion, &out.ExecutionMode, &out.GroupName)
		})
	}
	out.Platform = ref.assistantPlatform()
	if err == pgx.ErrNoRows {
		return out, Denied(404, "assistant_plan_not_found")
	}
	if err != nil {
		return out, err
	}
	if _, err = s.Session(ctx, ref, out.SessionID, false); err != nil {
		return out, err
	}
	_, err = s.authorize(ctx, s.DB.Platform, ref, assistant.Scope{Kind: "group", AccountID: out.AccountID, GroupID: out.GroupID},
		"assistant:use", false, false)
	if err != nil {
		return out, err
	}
	if len(sealed) > 0 {
		raw, e := s.Vault.Open("assistant:plan:"+id, sealed)
		if e != nil {
			return out, e
		}
		defer clear(raw)
		var content planContent
		if err = json.Unmarshal(raw, &content); err != nil {
			return out, err
		}
		out.Changes, out.Before = content.Changes, content.Before
		out.PolicyVersion = content.PolicyVersion
		out.Capabilities = content.Capabilities
		out.controlled = content.controlledExecutionFence
	}
	if err = out.controlled.require(ctx, s.DB.Platform, a.ID, out.Platform, out.GroupID); err != nil {
		return out, err
	}
	// Stored diffs are resource data too: withdrawing a field grant withdraws its history.
	entitlement, err := ResolveHumanEntitlement(ctx, s.DB.Platform, a, s.Catalog, out.Platform, out.GroupID)
	if err != nil {
		return out, err
	}
	if out.ExecutionMode != "legacy" && out.ExecutionMode != entitlement.Mode {
		return out, Denied(403, "permission_denied")
	}
	if a.Role != "superadmin" {
		if err = requireFeatureCeilings(ctx, s.DB.Platform, entitlement.TenantID, out.Changes); err != nil {
			return out, err
		}
	}
	grants, err := EffectivePermissions(ctx, s.DB.Platform, a, s.Catalog)
	if err != nil {
		return out, err
	}
	if !slices.Contains(grants, "assistant:propose") {
		return out, Denied(403, "permission_denied")
	}
	if len(out.Changes) > 0 {
		if _, _, err = ValidateFeatures(features.Config{}, out.Changes, a, grants, entitlement.Modules, true); err != nil {
			return out, err
		}
	}
	if out.Status == "pending_approval" && !time.Now().Before(out.ExpiresAt) {
		out.Status = "expired"
	}
	if out.Status == "pending_approval" {
		_, e := s.checkConfigExposure(ctx, s.DB.Platform, a, assistant.Scope{Platform: out.Platform, Kind: "group", AccountID: out.AccountID, GroupID: out.GroupID}, out.Changes, out.PolicyVersion)
		if out.PolicyVersion == 0 || e != nil {
			if e != nil {
				if _, ok := e.(*Failure); !ok {
					return out, e
				}
			}
			out.Status = "invalidated"
		}
	}
	out.Message = "仅为建议；尚未保存配置"
	if out.Status == "invalidated" {
		out.Message = "模块开放策略或权限已变化，此建议失效；请重新提出需求"
	}
	if out.Status == "committed" {
		out.Message = "配置已保存，实际生效尚未验证"
	}
	out.Summary = assistantConfigSummary(out)
	return out, nil
}

func (s *AssistantService) CancelPlan(ctx context.Context, ref SessionRef, id string) (AssistantPlan, error) {
	out, err := s.Plan(ctx, ref, id)
	if err != nil {
		return out, err
	}
	a, err := s.authorize(ctx, s.DB.Platform, ref, assistant.Scope{Kind: "group", AccountID: out.AccountID, GroupID: out.GroupID},
		"assistant:propose", false, false)
	if err != nil {
		return out, err
	}
	_, err = s.DB.Platform.Exec(ctx, `UPDATE assistant_plans SET status='cancelled',version=version+1
        WHERE id=$1 AND actor_id=$2 AND status='pending_approval'`, id, a.ID)
	if err != nil {
		return out, err
	}
	return s.Plan(ctx, ref, id)
}

func (s *AssistantService) Approve(ctx context.Context, ref SessionRef, id, summary string, version int64) (AssistantPlan, error) {
	var proof *assistantConfigRoleProof
	for {
		out, err := s.Plan(ctx, ref, id)
		if err != nil {
			return out, err
		}
		scope := assistant.Scope{Kind: "group", AccountID: out.AccountID, GroupID: out.GroupID}
		tx, err := s.DB.Platform.Begin(ctx)
		if err != nil {
			return out, err
		}
		defer tx.Rollback(ctx)
		// Same lifecycle lock and lock ordering as legacy group configuration/revocation.
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
			return out, err
		}
		a, err := s.authorize(ctx, tx, ref, scope, "assistant:approve", false, true)
		if err != nil {
			return out, err
		}
		if err = out.controlled.require(ctx, tx, a.ID, out.Platform, out.GroupID); err != nil {
			return out, err
		}
		if err = liveAssistantSession(ctx, tx, out.SessionID, a.ID, out.Platform); err != nil {
			return out, err
		}
		if summary != out.Digest {
			return out, Denied(409, "assistant_plan_mismatch")
		}
		var storedStatus string
		var storedVersion int64
		var expires time.Time
		err = tx.QueryRow(ctx, `SELECT status,version,expires_at FROM assistant_plans WHERE id=$1 AND actor_id=$2 FOR UPDATE`,
			id, a.ID).Scan(&storedStatus, &storedVersion, &expires)
		if err != nil {
			return out, err
		}
		if storedStatus == "committed" {
			// Reconcile a lost COMMIT response by returning the original execution.
			if err = tx.Commit(ctx); err != nil {
				return out, err
			}
			return s.Plan(ctx, ref, id)
		}
		if storedVersion != version {
			return out, Denied(409, "version_conflict")
		}
		if storedStatus != "pending_approval" || !time.Now().Before(expires) {
			return out, Denied(409, "assistant_plan_not_pending")
		}
		if out.PolicyVersion == 0 {
			return out, Denied(409, "assistant_module_policy_changed")
		}
		if _, err = s.checkConfigExposure(ctx, tx, a, assistant.Scope{Platform: out.Platform, Kind: "group", AccountID: out.AccountID, GroupID: out.GroupID}, out.Changes, out.PolicyVersion); err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `SELECT singleton FROM assistant_settings WHERE singleton FOR SHARE`); err != nil {
			return out, err
		}
		settings, _, err := s.settings(ctx, tx)
		if err != nil {
			return out, err
		}
		if !settings.Enabled || !settings.ApprovalsEnabled {
			return out, Denied(409, "assistant_approvals_disabled")
		}
		var tenant string
		if err = tx.QueryRow(ctx, `SELECT tenant_id::text FROM group_bindings WHERE group_id=$1 AND active FOR SHARE`, out.GroupID).Scan(&tenant); err != nil {
			return out, err
		}
		if a.Role != "superadmin" {
			for _, code := range []string{"assistant:use", "assistant:propose", "assistant:approve"} {
				if err = RequireTenantCapability(ctx, tx, tenant, code); err != nil {
					return out, err
				}
			}
		}
		var accountEpoch, bindingEpoch int64
		err = tx.QueryRow(ctx, `SELECT a.epoch,b.epoch FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
        WHERE b.group_id=$1 AND b.primary_account_id=$2 AND b.active AND a.workspace=$3 FOR SHARE OF a`,
			out.GroupID, out.AccountID, out.Platform).Scan(&accountEpoch, &bindingEpoch)
		if err != nil {
			return out, Denied(409, "assistant_scope_changed")
		}
		if accountEpoch != out.AccountEpoch || bindingEpoch != out.BindingEpoch {
			return out, Denied(409, "assistant_scope_changed")
		}
		var roleValid bool
		err = tx.QueryRow(ctx, `SELECT role IN ('admin','owner') AND observation_source='native'
        AND observed_at>clock_timestamp()-interval '60 seconds' FROM account_groups
        WHERE account_id=$1 AND group_id=$2 FOR SHARE`, out.AccountID, out.GroupID).Scan(&roleValid)
		if err != nil && err != pgx.ErrNoRows {
			return out, err
		}
		if proof != nil {
			if err = s.verifyConfigRole(ctx, tx, out, *proof); err != nil {
				return out, err
			}
			roleValid = true
		}
		if !roleValid && s.ConfigRoles != nil {
			native, err := assistantConfigRoleScope(ctx, tx, out)
			if err != nil {
				return out, err
			}
			// Native reads must not hold the configuration lifecycle lock or a DB connection.
			if err = tx.Rollback(ctx); err != nil {
				return out, err
			}
			work, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			roles, err := s.ConfigRoles(work, native)
			if err != nil {
				return out, err
			}
			proof = &assistantConfigRoleProof{native, roles}
			ctx = work
			continue
		}
		if !roleValid {
			return out, Denied(409, "fresh_native_role_required")
		}
		grants, err := EffectivePermissions(ctx, tx, a, s.Catalog)
		if err != nil {
			return out, err
		}
		if !slices.Contains(grants, "assistant:propose") {
			return out, Denied(403, "permission_denied")
		}
		commit, err := SaveFeaturesTx(ctx, tx, a, s.Catalog, out.GroupID, out.BaseVersion, out.Changes, true)
		if err != nil {
			return out, err
		}
		executionID := NewID()
		_, err = tx.Exec(ctx, `INSERT INTO assistant_plan_executions(id,plan_id,actor_id,tenant_id,group_id,config_version,domain)
        SELECT $1,id,actor_id,tenant_id,group_id,$3,domain FROM assistant_plans WHERE id=$2`, executionID, id, commit.Version)
		if err != nil {
			return out, err
		}
		updated, err := tx.Exec(ctx, `UPDATE assistant_plans SET status='committed',version=version+1,execution_id=$2,
        committed_version=$3,committed_at=now() WHERE id=$1 AND status='pending_approval' AND version=$4
        AND expires_at>clock_timestamp()`, id, executionID, commit.Version, version)
		if err != nil {
			return out, err
		}
		if updated.RowsAffected() != 1 {
			return out, Denied(409, "assistant_plan_not_pending")
		}
		if _, err = s.actor(ctx, tx, ref, false); err != nil {
			return out, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        SELECT tenant_id,actor_id,'assistant_plan_committed',id::text,
        jsonb_build_object('execution_id',$2::text,'version',$3::bigint,'applied',false)
        FROM assistant_plans WHERE id=$1`, id, executionID, commit.Version)
		if err != nil {
			return out, err
		}
		if err = tx.Commit(ctx); err != nil {
			return out, Denied(503, "assistant_commit_unknown")
		}
		return s.Plan(ctx, ref, id)
	}
}
