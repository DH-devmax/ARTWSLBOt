package control

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// A legacy isolated fixture is not customer ownership. Pin its existing binding
// without moving it; only the normal authenticated proof/review may do that.
type onboardingControlledBinding struct {
	Tenant, Account string
	Epoch, Version  int64
}

func (b onboardingControlledBinding) marker() string {
	return strconv.FormatInt(b.Epoch, 10) + ":" + strconv.FormatInt(b.Version, 10) +
		":" + b.Tenant + ":" + b.Account
}

func loadOnboardingControlledBinding(ctx context.Context, q Queryer, group, tenant, claimant, receiver, platform string) (onboardingControlledBinding, error) {
	var b onboardingControlledBinding
	var allowed bool
	err := q.QueryRow(ctx, `SELECT b.tenant_id::text,coalesce(b.primary_account_id::text,''),b.epoch,b.version,
		b.active AND (b.tenant_id=$2 AND b.primary_account_id=$4
			AND o.tenant_id=$2 AND o.platform=$5 AND o.proof_kind='controlled_test' AND o.state='paused'
			OR o.group_id IS NULL AND b.primary_account_id IN($3::uuid,$4::uuid))
		FROM group_bindings b
		LEFT JOIN LATERAL (SELECT o.group_id,o.tenant_id,o.platform,o.proof_kind,o.state
			FROM group_binding_ownership o WHERE o.group_id=b.group_id FOR SHARE) o ON true
		WHERE b.group_id=$1 FOR SHARE OF b`, group, tenant, claimant, receiver, platform).
		Scan(&b.Tenant, &b.Account, &b.Epoch, &b.Version, &allowed)
	if err == pgx.ErrNoRows || err == nil && (!allowed || !ValidID(b.Tenant) || !ValidID(b.Account) || b.Epoch < 1 || b.Version < 1) {
		return b, Denied(409, "controlled_onboarding_scope_inactive")
	}
	return b, err
}
