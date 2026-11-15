package control

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SeedTenantRoles creates explicit grants once. Adding a permission to the catalog
// never silently expands an existing tenant's roles.
func SeedTenantRoles(ctx context.Context, tx pgx.Tx, tenant string, catalog []PermissionSpec) error {
	for _, role := range []string{"owner", "manager", "operator", "viewer", "custom"} {
		grants := RolePermissions(role, catalog)
		if role == "owner" {
			grants = []string{}
			for _, spec := range catalog {
				grants = append(grants, spec.Code)
			}
		}
		if role != "owner" {
			grants = slices.DeleteFunc(grants, func(code string) bool { return code == "members:manage" || code == "billing:read" })
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tenant_roles(tenant_id,code,name,permissions) VALUES($1,$2,$2,$3)
            ON CONFLICT DO NOTHING`, tenant, role, grants); err != nil {
			return err
		}
	}
	return nil
}

func RequireMemberManager(ctx context.Context, tx pgx.Tx, a Actor, catalog []PermissionSpec) error {
	if err := RequireTenantOwner(ctx, tx, a); err != nil {
		return err
	}
	grants, err := LiveActorPermissions(ctx, tx, a, catalog)
	if err != nil {
		return err
	}
	if !slices.Contains(grants, "members:manage") {
		return Denied(403, "permission_denied")
	}
	return nil
}

// The role label and a legacy members:manage grant cannot establish ownership.
func RequireTenantOwner(ctx context.Context, q Queryer, a Actor) error {
	if a.Role != "user" || !ValidID(a.ID) || !ValidID(a.TenantID) {
		return Denied(403, "tenant_owner_required")
	}
	var owner bool
	err := q.QueryRow(ctx, `SELECT true FROM tenants t JOIN users u ON u.id=t.owner_id
		WHERE t.id=$1 AND u.id=$2 AND u.tenant_id=t.id AND u.tenant_role='owner'
		AND u.role='user' AND NOT u.disabled FOR SHARE OF t,u`, a.TenantID, a.ID).Scan(&owner)
	if err == pgx.ErrNoRows || err == nil && !owner {
		return Denied(403, "tenant_owner_required")
	}
	return err
}

func CheckMemberSeats(ctx context.Context, tx pgx.Tx, tenant string) error {
	var count, limit int
	err := tx.QueryRow(ctx, `SELECT count(*),(SELECT p.member_limit FROM subscriptions s JOIN plans p ON p.id=s.plan_id
        WHERE s.tenant_id=$1 AND s.enabled AND s.expires_at>clock_timestamp()) FROM users WHERE tenant_id=$1 AND NOT disabled`,
		tenant).Scan(&count, &limit)
	if err != nil {
		return Denied(409, "active_subscription_required")
	}
	if count >= limit {
		return Denied(409, "member_limit_reached")
	}
	return nil
}

func ValidateGrants(grants []string, catalog []PermissionSpec) bool {
	if len(grants) > len(catalog) {
		return false
	}
	seen := map[string]bool{}
	for _, code := range grants {
		if seen[code] || !slices.ContainsFunc(catalog, func(s PermissionSpec) bool { return s.Code == code }) {
			return false
		}
		seen[code] = true
	}
	return true
}

func ValidRoleCode(code string) bool {
	return len(code) > 0 && len(code) <= 40 && strings.IndexFunc(code, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
	}) < 0
}
