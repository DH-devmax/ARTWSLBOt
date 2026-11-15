package control

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type GroupBindingCustomer struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

type GroupBindingViewScope struct {
	Platform string `json:"platform"`
	ActorID  string `json:"actor_id"`
	TenantID string `json:"tenant_id,omitempty"`
}

type GroupBindingContext struct {
	Mode                  string                 `json:"mode"`
	Customers             []GroupBindingCustomer `json:"customers"`
	NextOffset            int                    `json:"next_offset"`
	Scope                 GroupBindingViewScope  `json:"scope"`
	Workspace             ResourceWorkspace      `json:"workspace"`
	DefaultTargetTenantID string                 `json:"default_target_tenant_id"`
	CanOnboardForCustomer bool                   `json:"can_onboard_for_customer"`
}

func bindingCustomer(ctx context.Context, tx pgx.Tx, tenant string, lock bool) (GroupBindingCustomer, error) {
	out := GroupBindingCustomer{}
	sql := `SELECT t.id::text,t.name FROM tenants t JOIN users owner ON owner.id=t.owner_id
		AND owner.tenant_id=t.id AND owner.role='user' AND owner.tenant_role='owner' AND NOT owner.disabled
		WHERE t.id=$1 AND t.space_kind='tenant_business'`
	if lock {
		sql += ` FOR SHARE OF t,owner`
	}
	err := tx.QueryRow(ctx, sql, tenant).Scan(&out.TenantID, &out.Name)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "group_binding_customer_not_found")
	}
	return out, err
}

// Context is website/database-only. Selecting a customer or paging this list
// neither reserves capacity nor consults native directories.
func (s *GroupBindingService) Context(ctx context.Context, ref SessionRef, platform, search string, offset, limit int) (GroupBindingContext, error) {
	out := GroupBindingContext{Customers: []GroupBindingCustomer{}, NextOffset: -1}
	if !bindingPlatform(platform) || platform != ref.assistantPlatform() || offset < 0 || offset > 5000000 ||
		limit < 1 || limit > 50 || len(search) > 240 || !utf8.ValidString(search) ||
		strings.IndexFunc(search, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return out, Denied(400, "group_binding_context_invalid")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := s.owner(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	out.Scope = GroupBindingViewScope{Platform: platform, ActorID: a.ID}
	out.Workspace, err = ActorWorkspace(ctx, tx, a)
	if err != nil {
		return out, err
	}
	out.DefaultTargetTenantID = out.Workspace.ID
	out.Scope.TenantID = out.Workspace.ID
	out.Mode = "tenant_business"
	if a.Role != "superadmin" {
		customer, err := bindingCustomer(ctx, tx, a.TenantID, true)
		if err != nil {
			return out, err
		}
		out.Scope.TenantID = customer.TenantID
		if offset == 0 && (search == "" || strings.Contains(strings.ToLower(customer.Name), strings.ToLower(search))) {
			out.Customers = append(out.Customers, customer)
		}
		return out, nil
	}
	out.Mode = "operator_demo"
	out.CanOnboardForCustomer = true
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.name FROM tenants t JOIN users owner ON owner.id=t.owner_id
		AND owner.tenant_id=t.id AND owner.role='user' AND owner.tenant_role='owner' AND NOT owner.disabled
		WHERE t.space_kind='tenant_business' AND ($1='' OR strpos(lower(t.name),lower($1))>0)
		ORDER BY t.name,t.id LIMIT $2 OFFSET $3`, strings.TrimSpace(search), limit+1, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var customer GroupBindingCustomer
		if err = rows.Scan(&customer.TenantID, &customer.Name); err != nil {
			return out, err
		}
		if len(out.Customers) == limit {
			out.NextOffset = offset + limit
			continue
		}
		out.Customers = append(out.Customers, customer)
	}
	return out, rows.Err()
}

func bindingCreateTarget(actor Actor, target string) (tenant, mode string, err error) {
	if actor.Role == "superadmin" {
		if !ValidID(target) {
			return "", "", Denied(400, "group_binding_customer_required")
		}
		return target, "operator_demo", nil
	}
	if target != "" {
		return "", "", Denied(403, "group_binding_target_forbidden")
	}
	if actor.Role != "user" || !ValidID(actor.TenantID) {
		return "", "", Denied(403, "tenant_owner_required")
	}
	return actor.TenantID, "tenant_business", nil
}

func bindingRecordMatches(c bindingRecord, actor Actor, platform string) bool {
	return c.Applicant == actor.ID && c.Platform == platform &&
		(c.Mode == "operator_demo" && actor.Role == "superadmin" ||
			c.Mode == "tenant_business" && actor.Role == "user" && c.Tenant == actor.TenantID)
}
