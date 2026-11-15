package control

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
)

// BusinessEntitlement describes the human actor, separately from the resource
// owner. Background consumers must use RequireBusinessAdmission instead.
type BusinessEntitlement struct {
	Mode          string   `json:"mode"`
	Name          string   `json:"name"`
	BillingSource string   `json:"billing_source"`
	TenantID      string   `json:"tenant_id,omitempty"`
	OperatorDemo  bool     `json:"unlimited"`
	Active        bool     `json:"active"`
	Modules       []string `json:"modules"`
	Permissions   []string `json:"permissions"`
}

func ResolveHumanEntitlement(ctx context.Context, q Queryer, actor Actor, catalog []PermissionSpec, platform, group string) (BusinessEntitlement, error) {
	out := BusinessEntitlement{Mode: "tenant_business", Name: "客户套餐", BillingSource: "tenant_wallet",
		TenantID: actor.TenantID, Modules: []string{}, Permissions: []string{}}
	if platform != "wsl" && platform != "lajiaoliao" || !ValidID(actor.ID) {
		return out, Denied(403, "permission_denied")
	}
	var live bool
	err := q.QueryRow(ctx, `SELECT true FROM users WHERE id=$1 AND role=$2 AND NOT disabled
		AND coalesce(tenant_id::text,'')=$3 FOR SHARE`, actor.ID, actor.Role, actor.TenantID).Scan(&live)
	if err == pgx.ErrNoRows {
		return out, Denied(403, "permission_denied")
	}
	if err != nil {
		return out, err
	}
	if group != "" {
		if err = RequireGroup(ctx, q, actor, group); err != nil {
			return out, err
		}
		err = q.QueryRow(ctx, `SELECT b.tenant_id::text FROM group_bindings b
			JOIN native_groups g ON g.id=b.group_id WHERE b.group_id=$1 AND b.active AND g.platform=$2
			FOR SHARE OF b`, group, platform).Scan(&out.TenantID)
		if err == pgx.ErrNoRows {
			return out, Denied(404, "group_not_found")
		}
		if err != nil {
			return out, err
		}
	}
	out.Permissions, err = EffectivePermissions(ctx, q, actor, catalog)
	if err != nil {
		return out, err
	}
	if actor.Role == "superadmin" {
		out.Mode, out.Name, out.BillingSource = "operator_demo", "超管专属 · 不限额", "platform_demo"
		out.OperatorDemo, out.Active = true, true
		out.Modules = []string{"management", "management_assistant", "ai"}
		for _, definition := range features.CatalogForPlatform(platform) {
			if !slices.Contains(out.Modules, definition.Module) {
				out.Modules = append(out.Modules, definition.Module)
			}
		}
		return out, nil
	}
	err = q.QueryRow(ctx, `SELECT p.modules FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
		WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()
		AND $2=ANY(p.allowed_platforms) FOR SHARE OF sub,p`, out.TenantID, platform).Scan(&out.Modules)
	if err == pgx.ErrNoRows {
		return out, nil
	}
	out.Active = err == nil
	return out, err
}

func (e BusinessEntitlement) RequireModule(module string) error {
	if !e.Active {
		return Denied(403, "active_subscription_required")
	}
	if !moduleEntitled(e.Modules, module) {
		return Denied(403, "module_not_in_plan")
	}
	return nil
}

// Human admission retains the same global gate. Demo only replaces commercial
// entitlement and automatic runtime; it never grants protocol or native roles.
func RequireHumanBusinessAdmission(ctx context.Context, q Queryer, actor Actor, catalog []PermissionSpec, platform, group, action, source string) error {
	if source != "assistant" && source != "manual" {
		return Denied(403, "invalid_business_source")
	}
	entitlement, err := ResolveHumanEntitlement(ctx, q, actor, catalog, platform, group)
	if err != nil {
		return err
	}
	d, ok := features.FindBusinessAction(platform, action, source)
	if !ok || !d.Implemented {
		return Denied(409, "business_capability_unavailable")
	}
	if err = requireBusinessGate(ctx, q, platform, group, action, source); err != nil {
		return err
	}
	if !slices.Contains(entitlement.Permissions, d.Permission) {
		return Denied(403, "permission_denied")
	}
	if entitlement.OperatorDemo {
		return requireOperatorHumanOwnership(ctx, q, actor.ID, platform, group)
	}
	return RequireBusinessAdmission(ctx, q, platform, group, action, source)
}
