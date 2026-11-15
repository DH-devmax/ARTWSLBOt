package control

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// These aliases are shared with the bounded assistant directory projection.
// A demo claim belongs to one website actor and the exact current binding.
const operatorDemoClaimVisibleSQL = `EXISTS(SELECT 1 FROM operator_demo_group_claims c
	JOIN users cu ON cu.id=c.actor_id
	WHERE c.actor_id=$4::uuid AND c.group_id=b.group_id AND c.tenant_id=b.tenant_id
	AND c.platform=a.platform AND c.account_id=a.id AND c.state='active'
	AND c.expires_at>clock_timestamp()+interval '3 seconds'
	AND cu.role='superadmin' AND NOT cu.disabled AND cu.access_version=c.actor_version
	AND c.binding_epoch=b.epoch AND c.binding_version=b.version
	AND c.account_epoch=a.epoch AND c.account_version=a.version
	AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL)`

type HumanGroupAccess struct {
	OwnershipState string `json:"ownership_state"`
	AccessMode     string `json:"access_mode"`
	BlockedReason  string `json:"blocked_reason"`
}

func humanGroupAccess(state string, super, demo bool) HumanGroupAccess {
	out := HumanGroupAccess{OwnershipState: state, AccessMode: "read_only"}
	if state == "" {
		out.OwnershipState = "unverified"
	}
	if state == "active" || super && demo {
		out.AccessMode = "manage"
		return out
	}
	if super {
		out.BlockedReason = "operator_demo_claim_required"
	} else if state == "paused" {
		out.BlockedReason = "group_ownership_reproof_required"
	} else {
		out.BlockedReason = "group_ownership_proof_required"
	}
	return out
}

func ResolveHumanGroupAccess(ctx context.Context, q Queryer, actor Actor, platform, group string) (HumanGroupAccess, error) {
	var out HumanGroupAccess
	if err := RequireGroup(ctx, q, actor, group); err != nil {
		return out, err
	}
	var state string
	var demo bool
	claimSQL := "false"
	if actor.Role == "superadmin" {
		claimSQL = operatorDemoClaimVisibleSQL
	}
	err := q.QueryRow(ctx, `SELECT coalesce(o.state,'unverified'),`+claimSQL+`
		FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
		JOIN accounts a ON a.id=b.primary_account_id
		LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id AND o.tenant_id=b.tenant_id AND o.platform=g.platform
		WHERE g.platform=$1 AND b.active AND b.group_id=$5 AND $4::uuid IS NOT NULL
		AND ($2 OR b.tenant_id=nullif($3,'')::uuid)`,
		platform, actor.Role == "superadmin", actor.TenantID, actor.ID, group).Scan(&state, &demo)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "group_not_found")
	}
	return humanGroupAccess(state, actor.Role == "superadmin", demo), err
}

// Read access to an old binding is distinct from permission to change it.
// Every configuration commit and human native admission uses this fence.
func RequireHumanGroupOwnership(ctx context.Context, q Queryer, actor Actor, platform, group string) error {
	if err := RequireGroup(ctx, q, actor, group); err != nil {
		return err
	}
	if actor.Role == "superadmin" {
		return requireOperatorHumanOwnership(ctx, q, actor.ID, platform, group)
	}
	active, err := activeCustomerOwnership(ctx, q, platform, group)
	if err != nil || active {
		return err
	}
	return Denied(409, "group_ownership_proof_required")
}
