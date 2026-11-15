package control

import (
	"context"
	"github.com/jackc/pgx/v5"
	"slices"
	"time"

	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
)

type AssistantExposure struct {
	Platform       string                         `json:"platform"`
	Capabilities   []string                       `json:"capabilities"`
	Version        int64                          `json:"version"`
	UpdatedAt      time.Time                      `json:"updated_at"`
	CatalogVersion string                         `json:"catalog_version"`
	Catalog        []features.AssistantCapability `json:"catalog"`
}

func assistantExposure(ctx context.Context, q Queryer, platform string) (AssistantExposure, error) {
	out := AssistantExposure{Platform: platform, CatalogVersion: features.AssistantCatalogVersion, Catalog: features.AssistantCapabilities(platform)}
	err := q.QueryRow(ctx, `SELECT capabilities,version,updated_at FROM assistant_module_exposure WHERE platform=$1 FOR SHARE`, platform).
		Scan(&out.Capabilities, &out.Version, &out.UpdatedAt)
	return out, err
}

func (s *AssistantService) ModuleExposure(ctx context.Context, ref SessionRef) (AssistantExposure, error) {
	a, err := s.actor(ctx, s.DB.Platform, ref, false)
	if err != nil {
		return AssistantExposure{}, err
	}
	if a.Role != "superadmin" {
		return AssistantExposure{}, Denied(403, "superadmin_required")
	}
	return assistantExposure(ctx, s.DB.Platform, ref.assistantPlatform())
}

func (s *AssistantService) SaveModuleExposure(ctx context.Context, ref SessionRef, version int64, ids []string) (AssistantExposure, error) {
	if len(ids) > 128 {
		return AssistantExposure{}, Denied(400, "assistant_invalid_module_policy")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		d, ok := features.FindAssistantCapability(ref.assistantPlatform(), id)
		if !ok || !d.Implemented || seen[id] {
			return AssistantExposure{}, Denied(400, "assistant_invalid_module_policy")
		}
		seen[id] = true
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return AssistantExposure{}, err
	}
	defer tx.Rollback(ctx)
	// Keep the same lifecycle lock ordering as approvals and revocation.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return AssistantExposure{}, err
	}
	a, err := s.actor(ctx, tx, ref, true)
	if err != nil {
		return AssistantExposure{}, err
	}
	if a.Role != "superadmin" {
		return AssistantExposure{}, Denied(403, "superadmin_required")
	}
	tag, err := tx.Exec(ctx, `UPDATE assistant_module_exposure SET capabilities=$3,version=version+1,updated_at=clock_timestamp(),updated_by=$4
  WHERE platform=$1 AND version=$2`, ref.assistantPlatform(), version, ids, a.ID)
	if err != nil {
		return AssistantExposure{}, err
	}
	if tag.RowsAffected() != 1 {
		return AssistantExposure{}, Denied(409, "version_conflict")
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail) VALUES($1,'assistant_module_exposure_updated',$2,
  jsonb_build_object('version',$3::bigint,'capability_count',$4::int))`, a.ID, ref.assistantPlatform(), version+1, len(ids))
	if err != nil {
		return AssistantExposure{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AssistantExposure{}, Denied(503, "assistant_commit_unknown")
	}
	return s.ModuleExposure(ctx, ref)
}

func (s *AssistantService) requireAssistantCapability(ctx context.Context, q Queryer, a Actor, scope assistant.Scope, policy AssistantExposure, d features.AssistantCapability) error {
	access, err := s.assistantModuleAccess(ctx, q, a, scope)
	if err != nil {
		return err
	}
	return access.check(scope.Platform, policy, d)
}

type assistantModuleAccess struct {
	super                     bool
	grants, ceilings, modules []string
	ownershipBlocked          string
}

func (s *AssistantService) assistantModuleAccess(ctx context.Context, q Queryer, a Actor, scope assistant.Scope) (assistantModuleAccess, error) {
	e, err := ResolveHumanEntitlement(ctx, q, a, s.Catalog, scope.Platform, scope.GroupID)
	access := assistantModuleAccess{super: a.Role == "superadmin", grants: e.Permissions, modules: e.Modules}
	if err != nil {
		return access, err
	}
	if !e.Active {
		return access, Denied(403, "active_subscription_required")
	}
	if scope.Kind == "group" {
		groupAccess, e := ResolveHumanGroupAccess(ctx, q, a, scope.Platform, scope.GroupID)
		if e != nil {
			return access, e
		}
		access.ownershipBlocked = groupAccess.BlockedReason
	}
	if !access.super {
		err = q.QueryRow(ctx, `SELECT permissions FROM tenant_capability_ceilings WHERE tenant_id=$1 FOR SHARE`, a.TenantID).Scan(&access.ceilings)
		if err == pgx.ErrNoRows {
			err = nil
		}
	}
	return access, err
}
func (access assistantModuleAccess) check(platform string, policy AssistantExposure, d features.AssistantCapability) error {
	if d.Category != "query" && access.ownershipBlocked != "" {
		return Denied(409, access.ownershipBlocked)
	}
	if !d.Implemented || !slices.Contains(policy.Capabilities, d.ID) {
		return Denied(409, "assistant_module_closed")
	}
	grants := access.grants
	base := "assistant:use"
	if d.Category == "config_propose" {
		base = "assistant:propose"
	}
	if d.Category == "action_propose" {
		base = "assistant:action_propose"
	}
	if !slices.Contains(grants, base) {
		return Denied(403, "permission_denied")
	}
	if !slices.Contains(grants, d.Permission) || !slices.Contains(grants, d.BusinessPermission) {
		return Denied(403, "permission_denied")
	}
	if !access.super {
		for _, code := range []string{base, d.Permission, d.BusinessPermission} {
			if !slices.Contains(access.ceilings, code) {
				return Denied(403, "permission_denied")
			}
		}
	}
	if !access.super && d.Feature != "" {
		for _, f := range features.CatalogForPlatform(platform) {
			if f.ID == d.Feature && !slices.Contains(access.modules, f.Module) {
				return Denied(403, "module_not_in_plan")
			}
		}
	}
	if !access.super && d.Action != "" {
		def, ok := features.FindAssistantAction(platform, d.Action)
		if !ok || !slices.Contains(access.modules, def.Module) {
			return Denied(403, "module_not_in_plan")
		}
	}
	return nil
}

func (s *AssistantService) assistantPolicyFor(ctx context.Context, q Queryer, a Actor, scope assistant.Scope, kind, feature, action string, expected int64) (int64, error) {
	p, err := assistantExposure(ctx, q, scope.Platform)
	if err != nil {
		return 0, err
	}
	if expected != 0 && p.Version != expected {
		return 0, Denied(409, "assistant_module_policy_changed")
	}
	for _, d := range p.Catalog {
		if d.Category == kind && d.Feature == feature && d.Action == action {
			if err = s.requireAssistantCapability(ctx, q, a, scope, p, d); err != nil {
				return 0, err
			}
			return p.Version, nil
		}
	}
	return 0, Denied(409, "assistant_module_closed")
}

func (s *AssistantService) checkConfigExposure(ctx context.Context, q Queryer, a Actor, scope assistant.Scope, changes FeatureChanges, expected int64) (int64, error) {
	if len(changes) == 0 {
		return 0, Denied(400, "assistant_invalid_input")
	}
	var version int64
	for id := range changes {
		v, err := s.assistantPolicyFor(ctx, q, a, scope, "config_propose", id, "", expected)
		if err != nil {
			return 0, err
		}
		if version != 0 && version != v {
			return 0, Denied(409, "assistant_module_policy_changed")
		}
		version = v
	}
	return version, nil
}

// The whitelist is a projection of registered adapters intersected with the
// current platform policy, commercial rights, tenant ceiling and actor grants.
func (s *AssistantService) effectiveAssistantTools(ctx context.Context, a Actor, scope assistant.Scope) ([]assistant.Tool, int64, error) {
	policy, err := assistantExposure(ctx, s.DB.Platform, scope.Platform)
	if err != nil {
		return nil, 0, err
	}
	allowed := map[string]bool{}
	access, err := s.assistantModuleAccess(ctx, s.DB.Platform, a, scope)
	if err != nil {
		return nil, 0, err
	}
	actions := []string{}
	members := false
	for _, d := range policy.Catalog {
		if e := access.check(scope.Platform, policy, d); e != nil {
			if _, ok := e.(*Failure); ok {
				continue
			}
			return nil, 0, e
		}
		allowed[d.Tool] = true
		if d.Action != "" {
			actions = append(actions, d.Action)
			if slices.Contains([]string{"mute_member", "unmute_member", "kick_member", "member_nickname"}, d.Action) {
				members = true
			}
			if d.Action == "recall_message" {
				allowed["find_group_messages"] = true
			}
		}
	}
	all := assistant.Tools(allowed["propose_group_config_change"])
	if scope.Kind == "group" {
		all = assistant.GroupTools(allowed["propose_group_config_change"])
	}
	all = append(all, assistant.ActionTools(actions, members && s.NativeMembers != nil)...)
	allowed["find_group_members"] = members && s.NativeMembers != nil
	out := []assistant.Tool{}
	for _, tool := range all {
		if allowed[tool.Function.Name] {
			out = append(out, tool)
		}
	}
	return out, policy.Version, nil
}
