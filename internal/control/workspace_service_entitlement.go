package control

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
)

// WorkspaceServiceContext is attribution for control-owned background work.
// It carries neither a website session nor an operator's human permissions.
type WorkspaceServiceContext struct {
	TenantID      string
	OwnerID       string
	OwnerVersion  int64
	ExecutionMode string
	BillingSource string
	Demo          bool
	ownerRole     string
}

func WorkspaceServiceEntitlement(ctx context.Context, q Queryer, tenant string) (WorkspaceServiceContext, error) {
	var out WorkspaceServiceContext
	if !ValidID(tenant) {
		return out, Denied(403, "workspace_service_unavailable")
	}
	var kind, ownerTenant, tenantRole string
	var demo bool
	err := q.QueryRow(ctx, `SELECT t.id::text,u.id::text,u.access_version,t.space_kind,u.role,
		coalesce(u.tenant_id::text,''),u.tenant_role,workspace_is_demo(t.id,u.id)
		FROM tenants t JOIN users u ON u.id=t.owner_id
		WHERE t.id=$1 AND NOT u.disabled FOR SHARE OF t,u`, tenant).Scan(
		&out.TenantID, &out.OwnerID, &out.OwnerVersion, &kind, &out.ownerRole, &ownerTenant, &tenantRole, &demo)
	if err == pgx.ErrNoRows {
		return WorkspaceServiceContext{}, Denied(403, "workspace_service_unavailable")
	}
	if err != nil {
		return WorkspaceServiceContext{}, err
	}
	if out.TenantID != tenant || !ValidID(out.OwnerID) || out.OwnerVersion < 1 {
		return WorkspaceServiceContext{}, Denied(403, "workspace_service_unavailable")
	}
	switch {
	case kind == "operator_demo" && demo && out.ownerRole == "superadmin" && ownerTenant == "":
		out.Demo = true
		out.ExecutionMode, out.BillingSource = "workspace_demo", "platform_workspace"
	case kind == "tenant_business" && !demo && out.ownerRole == "user" && ownerTenant == tenant && tenantRole == "owner":
		out.ExecutionMode, out.BillingSource = "tenant_business", "tenant_wallet"
	default:
		return WorkspaceServiceContext{}, Denied(403, "workspace_service_unavailable")
	}
	return out, nil
}

func (s WorkspaceServiceContext) revalidate(ctx context.Context, q Queryer) error {
	current, err := WorkspaceServiceEntitlement(ctx, q, s.TenantID)
	if err != nil {
		return err
	}
	if current != s {
		return Denied(409, "workspace_service_changed")
	}
	return nil
}

func (s WorkspaceServiceContext) permissions(ctx context.Context, q Queryer, catalog []PermissionSpec) ([]string, error) {
	if s.Demo {
		out := make([]string, 0, len(catalog))
		for _, permission := range catalog {
			out = append(out, permission.Code)
		}
		return out, nil
	}
	// The role is read from the actual customer owner, never synthesized for a
	// superadmin whose website identity has no tenant membership.
	return EffectivePermissions(ctx, q, Actor{ID: s.OwnerID, TenantID: s.TenantID, Role: s.ownerRole}, catalog)
}

func (s WorkspaceServiceContext) requireGroup(ctx context.Context, q Queryer, group string) error {
	if !ValidID(group) {
		return Denied(404, "group_not_found")
	}
	if err := s.revalidate(ctx, q); err != nil {
		return err
	}
	var id, platform string
	err := q.QueryRow(ctx, `SELECT b.group_id::text,g.platform FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		WHERE b.group_id=$1 AND b.tenant_id=$2 AND b.active
		AND a.purpose='management' AND a.platform=g.platform AND a.workspace=g.platform
		AND (workspace_is_demo(b.tenant_id,$3) OR member_group_visible(b.tenant_id,b.group_id,$3))
		FOR SHARE OF b,a`, group, s.TenantID, s.OwnerID).Scan(&id, &platform)
	if err == pgx.ErrNoRows {
		return Denied(404, "group_not_found")
	}
	if err != nil {
		return err
	}
	return RequireControlledBinding(ctx, q, "", platform, group)
}

func (s WorkspaceServiceContext) requirePermission(ctx context.Context, q Queryer, catalog []PermissionSpec, permission string) error {
	permissions, err := s.permissions(ctx, q, catalog)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, permission) {
		return Denied(403, "permission_denied")
	}
	return nil
}

func (s WorkspaceServiceContext) humanOwner(actor Actor) bool {
	return s.Demo && actor.ID == s.OwnerID && actor.Role == "superadmin" && actor.TenantID == ""
}
