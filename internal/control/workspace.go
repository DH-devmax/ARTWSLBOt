package control

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// ResourceWorkspace is ownership metadata, never a website authentication role.
type ResourceWorkspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	OwnerID string `json:"owner_id"`
}

// OperatorWorkspace only reads an already provisioned space. Provisioning is
// part of the migration/user creation transaction, not a GET side effect.
func OperatorWorkspace(ctx context.Context, q Queryer, actor string) (ResourceWorkspace, error) {
	var out ResourceWorkspace
	if !ValidID(actor) {
		return out, Denied(403, "superadmin_required")
	}
	err := q.QueryRow(ctx, `SELECT t.id::text,t.name,t.space_kind,t.operator_owner_id::text
		FROM tenants t JOIN users u ON u.id=t.operator_owner_id
		WHERE workspace_is_demo(t.id,$1::uuid) AND u.id=$1 FOR SHARE OF t,u`, actor).
		Scan(&out.ID, &out.Name, &out.Kind, &out.OwnerID)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "operator_workspace_unavailable")
	}
	return out, err
}

func ActorWorkspace(ctx context.Context, q Queryer, actor Actor) (ResourceWorkspace, error) {
	if actor.Role == "superadmin" {
		return OperatorWorkspace(ctx, q, actor.ID)
	}
	var out ResourceWorkspace
	err := q.QueryRow(ctx, `SELECT t.id::text,t.name,t.space_kind,coalesce(t.owner_id::text,'')
		FROM tenants t JOIN users u ON u.tenant_id=t.id
		WHERE u.id=$1 AND u.role='user' AND NOT u.disabled AND t.id=$2 AND t.space_kind='tenant_business'
		FOR SHARE OF t,u`, actor.ID, actor.TenantID).Scan(&out.ID, &out.Name, &out.Kind, &out.OwnerID)
	if err == pgx.ErrNoRows {
		return out, Denied(403, "workspace_access_denied")
	}
	return out, err
}
