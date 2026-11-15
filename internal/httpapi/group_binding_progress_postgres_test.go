package httpapi

import (
	"testing"

	"wangshangliao/internal/control"
)

func TestPostgresGroupBindingProgressDoesNotRefreshCommercialPeriod(t *testing.T) {
	f := newCommercialFixture(t)
	owner := f.owner("binding-progress-owner", "PRO")
	f.group(owner, "binding-progress-group", false)
	var plan string
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM commercial_plans
		WHERE code='PRO' AND version=1`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	expect(t, f.admin, "POST", "/billing/confirm", control.PaymentConfirmation{
		TenantID: owner.tenant, PlanVersionID: plan, IdempotencyKey: "binding-progress-renewal",
		PaymentReference: "synthetic-progress-renewal", AmountMicroUSDT: 49_000_000,
	}, 200)
	if _, err := f.db.Platform.Exec(f.ctx, `UPDATE subscription_periods
		SET starts_at=starts_at-interval '30 days 1 second',ends_at=ends_at-interval '30 days 1 second'
		WHERE tenant_id=$1`, owner.tenant); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		err := f.db.Platform.QueryRow(f.ctx, `SELECT jsonb_build_object(
			'periods',(SELECT jsonb_agg(jsonb_build_array(id,state) ORDER BY id)
				FROM subscription_periods WHERE tenant_id=$1),
			'grants',(SELECT count(*) FROM credit_entries WHERE tenant_id=$1),
			'wallets',(SELECT count(*) FROM ai_wallets WHERE tenant_id=$1),
			'bindings',(SELECT jsonb_agg(jsonb_build_array(group_id,version,epoch) ORDER BY group_id)
				FROM group_bindings WHERE tenant_id=$1),
			'friends',(SELECT count(*) FROM group_binding_friend_attempts WHERE tenant_id=$1)
		)::text`, owner.tenant).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	expect(t, owner.client, "GET", "/group-binding/context?platform=wsl", nil, 200)
	missing := control.NewID()
	expect(t, owner.client, "GET", "/group-binding/challenges/"+missing, nil, 404)
	expect(t, owner.client, "GET", "/group-binding/challenges/"+missing+"/friend-verify", nil, 404)
	if after := snapshot(); after != before {
		t.Fatal("reading onboarding progress changed billing, bindings, or approval state", before, after)
	}
	expect(t, owner.client, "GET", "/me", nil, 200)
	if after := snapshot(); after == before {
		t.Fatal("ordinary account access no longer activates a due commercial period")
	}
}
