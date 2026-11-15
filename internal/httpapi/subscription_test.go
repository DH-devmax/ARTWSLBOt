package httpapi

import (
	"testing"
	"time"
)

func TestPostgresDisabledSubscriptionRestoration(t *testing.T) {
	f := newCommercialFixture(t)
	row := expect(t, f.admin, "POST", "/users", map[string]string{
		"username": "restoration-owner", "password": "synthetic-commercial-password",
		"tenant_name": "restoration-tenant",
	}, 201)
	tenant := field(t, row, "tenant_id")
	plan := field(t, expect(t, f.admin, "POST", "/plans", map[string]any{
		"name": "synthetic-restoration", "group_limit": 1, "modules": []string{"moderation"},
	}, 201), "id")
	path := "/subscriptions/" + tenant
	expired := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Microsecond)
	body := func(enabled bool, expires time.Time, version int) map[string]any {
		return map[string]any{"plan_id": plan, "enabled": enabled, "expires_at": expires, "version": version}
	}
	expect(t, f.admin, "PUT", path, body(false, time.Time{}, 0), 400)
	expect(t, f.admin, "PUT", path, body(true, expired, 0), 400)
	expect(t, f.admin, "PUT", path, body(false, expired, 0), 200)
	owner := f.login("restoration-owner")
	expect(t, owner, "PUT", path, body(true, time.Now().Add(time.Hour), 1), 403)
	expect(t, f.admin, "PUT", path, body(true, time.Now().Add(time.Hour), 1), 200)
	expect(t, f.admin, "PUT", path, body(false, expired, 1), 409)
	expect(t, f.admin, "PUT", path, body(false, expired, 2), 200)
	var restored bool
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT NOT enabled AND plan_id=$2
		AND expires_at=$3 AND version=3 FROM subscriptions WHERE tenant_id=$1`,
		tenant, plan, expired).Scan(&restored); err != nil || !restored {
		t.Fatalf("disabled subscription was not restored exactly: %v", err)
	}
	expect(t, f.admin, "PUT", path, body(true, expired, 3), 400)
	var audits, periods int
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM audit_events WHERE action='subscription_updated' AND tenant_id=$1),
		(SELECT count(*) FROM subscription_periods WHERE tenant_id=$1)`, tenant).Scan(&audits, &periods); err != nil || audits != 3 || periods != 0 {
		t.Fatalf("restoration changed commercial billing or lost its audit: audits=%d periods=%d err=%v", audits, periods, err)
	}
	var commercialPlan string
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT plan_id::text FROM commercial_plans LIMIT 1`).Scan(&commercialPlan); err != nil {
		t.Fatal(err)
	}
	commercial := body(false, expired, 3)
	commercial["plan_id"] = commercialPlan
	expect(t, f.admin, "PUT", path, commercial, 409)
	paid := f.owner("paid-restoration-owner", "PLUS")
	expect(t, f.admin, "PUT", "/subscriptions/"+paid.tenant, body(false, expired, 1), 409)
}
