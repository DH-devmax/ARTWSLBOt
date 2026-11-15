package control

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
)

// RequireTenantCapability checks the operator's explicit ceiling, not a member's
// grant or package. Callers must retain those independent checks.
func RequireTenantCapability(ctx context.Context, q Queryer, tenant, permission string) error {
	if !ValidID(tenant) || !slices.ContainsFunc(BusinessPermissionCatalog(), func(p PermissionSpec) bool { return p.Code == permission }) {
		return Denied(403, "tenant_capability_denied")
	}
	var permissions []string
	err := q.QueryRow(ctx, `SELECT permissions FROM tenant_capability_ceilings WHERE tenant_id=$1 FOR SHARE`, tenant).Scan(&permissions)
	if err == pgx.ErrNoRows || err == nil && !ceilingAllows(permissions, permission) {
		return Denied(403, "tenant_capability_denied")
	}
	return err
}

// A legacy write alias cannot restore a field withheld by the tenant ceiling.
func ceilingAllows(ceiling []string, permission string) bool {
	if !slices.Contains(ceiling, permission) {
		return false
	}
	for _, d := range features.Catalog() {
		if d.Legacy == permission {
			for _, field := range d.Fields {
				if features.CanEdit([]string{permission}, d, field.Key) &&
					!slices.Contains(ceiling, features.Permission(d.ID, field.Key)) {
					return false
				}
			}
		}
	}
	return true
}

// RequireBusinessAdmission accepts internal native_groups.id, including a group
// discovered before binding for onboarding/group_accept. Final callers pass the
// submission transaction so SHARE locks fence concurrent admission changes.
func RequireBusinessAdmission(ctx context.Context, q Queryer, platform, group, action, source string) error {
	d, ok := features.FindBusinessAction(platform, action, source)
	if !ok || !d.Implemented || !ValidID(group) {
		return Denied(409, "business_capability_unavailable")
	}
	if err := requireBusinessGate(ctx, q, platform, group, action, source); err != nil {
		return err
	}
	return requireBusinessEntitlement(ctx, q, platform, group, action, source, d)
}

func requireBusinessGate(ctx context.Context, q Queryer, platform, group, action, source string) error {
	var state string
	var pilot bool
	err := q.QueryRow(ctx, `SELECT c.state,EXISTS(SELECT 1 FROM business_capability_pilots p
		WHERE p.platform=c.platform AND p.action=c.action AND p.source=c.source
		AND p.group_id=g.id AND p.gate_version=c.version)
		FROM business_capability_gates c JOIN native_groups g ON g.id=$2 AND g.platform=c.platform
		WHERE c.platform=$1 AND c.action=$3 AND c.source=$4 FOR SHARE OF c`,
		platform, group, action, source).Scan(&state, &pilot)
	if err == pgx.ErrNoRows {
		return Denied(409, "business_admission_closed")
	}
	if err != nil {
		return err
	}
	if state != "open" && !(state == "pilot" && pilot) {
		return Denied(409, "business_admission_closed")
	}
	return nil
}

func requireBusinessEntitlement(ctx context.Context, q Queryer, platform, group, action, source string, d features.AssistantAction) error {
	// The onboarding owner verifies identity and its own durable invitation.
	// No tenant may be invented from an invitation or a native roster.
	if action == "group_accept" && source == "onboarding" {
		return nil
	}
	if err := RequireGroupRuntime(ctx, q, group); err != nil {
		return err
	}
	if source == "automatic" || source == "heatbot" {
		var demo, management, chat bool
		err := q.QueryRow(ctx, `SELECT workspace_is_demo(b.tenant_id),b.management_enabled,
			b.ai_requested AND b.ai_enabled FROM group_bindings b
			JOIN native_groups g ON g.id=b.group_id JOIN tenants t ON t.id=b.tenant_id
			JOIN users u ON u.id=t.owner_id
			WHERE b.group_id=$1 AND b.active AND g.platform=$2 FOR SHARE OF b,t,u`,
			group, platform).Scan(&demo, &management, &chat)
		if err == pgx.ErrNoRows {
			return Denied(403, "active_subscription_required")
		}
		if err != nil {
			return err
		}
		if demo {
			if source == "heatbot" && !chat {
				return Denied(409, "group_chat_disabled")
			}
			if source == "automatic" && !management {
				return Denied(409, "group_management_disabled")
			}
			return nil
		}
	}
	var tenant string
	var modules []string
	var management, chat bool
	var aiLimit int
	err := q.QueryRow(ctx, `SELECT b.tenant_id::text,p.modules,b.management_enabled,
		b.ai_enabled AND b.ai_requested,p.ai_group_limit FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id JOIN subscriptions sub ON sub.tenant_id=b.tenant_id
		JOIN plans p ON p.id=sub.plan_id WHERE b.group_id=$1 AND b.active AND g.platform=$2
		AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
		AND $2=ANY(p.allowed_platforms) FOR SHARE OF b,sub,p`, group, platform).
		Scan(&tenant, &modules, &management, &chat, &aiLimit)
	if err == pgx.ErrNoRows {
		return Denied(403, "active_subscription_required")
	}
	if err != nil {
		return err
	}
	permission, module := d.Permission, d.Module
	if source == "heatbot" {
		permission, module = "groups:ai", "ai"
		if !chat || aiLimit < 1 {
			return Denied(409, "group_chat_disabled")
		}
	} else if !management {
		return Denied(409, "group_management_disabled")
	}
	if !slices.Contains(modules, module) || source == "assistant" && !slices.Contains(modules, "management_assistant") {
		return Denied(403, "module_not_in_plan")
	}
	if err = RequireTenantCapability(ctx, q, tenant, permission); err != nil {
		return err
	}
	if source == "assistant" {
		for _, code := range []string{"assistant:use", "assistant:action_propose", "assistant:action_approve"} {
			if err = RequireTenantCapability(ctx, q, tenant, code); err != nil {
				return err
			}
		}
	}
	return nil
}

func ScopedHumanAction(ctx context.Context, q Queryer, actor Actor, catalog []PermissionSpec, platform, group, action, source string) (features.AssistantAction, error) {
	d, ok := features.FindBusinessAction(platform, action, source)
	if !ok {
		return d, Denied(409, "assistant_action_unavailable")
	}
	err := RequireHumanBusinessAdmission(ctx, q, actor, catalog, platform, group, action, source)
	d.Production = err == nil
	d.BlockReason = ""
	if err == nil {
		return d, nil
	}
	var failure *Failure
	if errors.As(err, &failure) {
		d.BlockReason = failure.Code
		return d, nil
	}
	return d, err
}

// ScopedAssistantAction resolves production admission without changing global
// catalog/readiness. A known closed gate is displayable; database failures fail.
func ScopedAssistantAction(ctx context.Context, q Queryer, platform, group, action string) (features.AssistantAction, error) {
	d, ok := features.FindAssistantAction(platform, action)
	if !ok {
		return d, Denied(409, "assistant_action_unavailable")
	}
	err := RequireBusinessAdmission(ctx, q, platform, group, action, "assistant")
	d.Production = err == nil
	if err == nil {
		d.BlockReason = ""
		return d, nil
	}
	var failure *Failure
	if errors.As(err, &failure) {
		d.BlockReason = failure.Code
		return d, nil
	}
	return d, err
}

func moduleEntitled(modules []string, module string) bool {
	if module == "" {
		return true
	}
	if module == "management" {
		return slices.ContainsFunc(modules, func(m string) bool {
			return slices.Contains([]string{"moderation", "replies", "schedules", "rankings", "cards", "friends"}, m)
		})
	}
	return slices.Contains(modules, module)
}

func requireFeatureCeilings(ctx context.Context, q Queryer, tenant string, changes FeatureChanges) error {
	for id, fields := range changes {
		for field := range fields {
			if err := RequireTenantCapability(ctx, q, tenant, features.Permission(id, field)); err != nil {
				return err
			}
		}
	}
	return nil
}
