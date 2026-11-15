package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"wangshangliao/internal/control"
)

func operatorClaimGroup(t *testing.T, f *onboardingFixture, platform string) string {
	t.Helper()
	var group string
	ctx := context.Background()
	err := f.db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)
		VALUES($1,'synthetic-demo-group','Synthetic demo group') RETURNING id::text`, platform).Scan(&group)
	if err == nil {
		_, err = f.db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,
			management_enabled,config) VALUES($1,$2,$3,false,'{"blocked_words":["keep"]}')`,
			group, f.tenantA, f.accounts[platform])
	}
	if err == nil {
		_, err = f.db.Platform.Exec(ctx, `DELETE FROM subscriptions WHERE tenant_id=$1`, f.tenantA)
	}
	if err != nil {
		t.Fatal(err)
	}
	return group
}

func operatorClaimRead(t *testing.T, raw []byte) control.OperatorDemoClaim {
	t.Helper()
	var out control.OperatorDemoClaim
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPostgresOperatorDemoClaimWithoutRegistrationOrPackage(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			group := operatorClaimGroup(t, f, platform)
			admin, customer := f.admin, f.alice
			admin.workspace, customer.workspace = platform, platform
			path := "/user-center/groups/" + group + "/demo-claim"
			body := map[string]any{"binding_version": 1}
			for _, c := range []testClient{customer, f.member, f.bob} {
				c.workspace = platform
				onboardingAPI(t, c, "GET", path, "", nil, 403)
				onboardingAPI(t, c, "POST", path, "synthetic-customer-denied", body, 403)
			}
			for _, field := range []string{"tenant_id", "actor_id", "account_id", "platform", "role", "execution_mode"} {
				onboardingAPI(t, admin, "POST", path, "synthetic-forged-claim-"+field,
					map[string]any{"binding_version": 1, field: "operator_demo"}, 400)
			}
			out := operatorClaimRead(t, onboardingAPI(t, admin, "GET", path, "", nil, 200))
			if out.State != "unverified" || f.native.roles.Load() != 0 {
				t.Fatal("viewing a claim performed a native read", out)
			}
			wrong := admin
			wrong.workspace = "wsl"
			if platform == "wsl" {
				wrong.workspace = "lajiaoliao"
			}
			onboardingAPI(t, wrong, "POST", path, "synthetic-wrong-platform", body, 409)
			onboardingAPI(t, admin, "POST", path, "synthetic-stale-version", map[string]any{"binding_version": 2}, 409)
			out = operatorClaimRead(t, onboardingAPI(t, admin, "POST", path, "synthetic-demo-claim", body, 200))
			if out.State != "active" || out.Group != group || out.Platform != platform || out.RobotRole != "admin" ||
				out.ID == "" || out.Version != 2 || out.ExpiresAt == nil || f.native.roles.Load() != 1 {
				t.Fatal("operator claim did not use the existing native account", out)
			}
			again := operatorClaimRead(t, onboardingAPI(t, admin, "POST", path, "synthetic-demo-claim", body, 200))
			if again.ID != out.ID || again.State != "active" || f.native.roles.Load() != 1 {
				t.Fatal("duplicate claim read upstream twice", again)
			}
			for i := 0; i < 2; i++ {
				onboardingAPI(t, admin, "GET", path, "", nil, 200)
			}
			var page struct {
				Items []struct {
					Ownership string                    `json:"ownership_state"`
					Claim     control.OperatorDemoClaim `json:"demo_claim"`
				} `json:"items"`
			}
			if err := json.Unmarshal(onboardingAPI(t, admin, "GET", "/user-center/groups?workspace=all&platform="+platform, "", nil, 200), &page); err != nil ||
				len(page.Items) != 1 || page.Items[0].Ownership != "unverified" || page.Items[0].Claim.ID != out.ID {
				t.Fatal("demo claim replaced customer ownership", page, err)
			}
			_, err := f.db.Platform.Exec(ctx, `INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
				VALUES($1,'mute_group','manual','open',ARRAY['acceptance:SYNTHETIC-DEMO-CLAIM'])`, platform)
			if err != nil {
				t.Fatal(err)
			}
			actor := control.Actor{ID: f.adminID, Role: "superadmin"}
			err = control.RequireHumanBusinessAdmission(ctx, f.db.Platform, actor, control.BusinessPermissionCatalog(), platform, group, "mute_group", "manual")
			if err != nil {
				t.Fatal("demo still required a customer login or package", err)
			}
			if err = control.RequireBusinessAdmission(ctx, f.db.Platform, platform, group, "mute_group", "automatic"); err == nil {
				t.Fatal("automatic worker inherited the demo claim")
			}
			cancelPath := path + "/" + out.ID + "/cancel"
			onboardingAPI(t, customer, "POST", cancelPath, "", map[string]any{"version": out.Version}, 403)
			onboardingAPI(t, admin, "POST", cancelPath, "", map[string]any{"version": out.Version + 1}, 409)
			out = operatorClaimRead(t, onboardingAPI(t, admin, "POST", cancelPath, "", map[string]any{"version": out.Version}, 200))
			if out.State != "cancelled" || out.Version != 3 {
				t.Fatal("demo claim did not cancel", out)
			}
			if err = control.RequireHumanBusinessAdmission(ctx, f.db.Platform, actor, control.BusinessPermissionCatalog(), platform, group, "mute_group", "manual"); err == nil {
				t.Fatal("cancelled claim still admitted an action")
			}
			var unchanged bool
			err = f.db.Platform.QueryRow(ctx, `SELECT b.active AND b.version=1 AND b.epoch=1 AND NOT b.management_enabled
				AND b.config->'blocked_words'='["keep"]'::jsonb AND NOT r.enabled
				AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=b.tenant_id)
				AND NOT EXISTS(SELECT 1 FROM group_binding_ownership WHERE group_id=b.group_id)
				AND NOT EXISTS(SELECT 1 FROM group_binding_challenges)
				AND NOT EXISTS(SELECT 1 FROM test_windows)
				AND NOT EXISTS(SELECT 1 FROM native_setting_operations)
				FROM group_bindings b JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`, group).Scan(&unchanged)
			if err != nil || !unchanged || f.native.sends.Load() != 0 || f.native.roles.Load() != 1 {
				t.Fatal("demo changed customer or native state", unchanged, err)
			}
		})
	}
}

func TestPostgresOperatorDemoClaimFailureIsolationAndRecovery(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			group := operatorClaimGroup(t, f, platform)
			admin := f.admin
			admin.workspace = platform
			path := "/user-center/groups/" + group + "/demo-claim"
			body := map[string]any{"binding_version": 1}
			f.native.robotRole = "member"
			failed := operatorClaimRead(t, onboardingAPI(t, admin, "POST", path, "synthetic-member-claim", body, 200))
			if failed.State != "failed" || failed.BlockedReason != "operator_demo_claim_role_required" {
				t.Fatal("ordinary pool member admitted", failed)
			}
			f.native.robotRole = "admin"
			f.native.roleErr = errors.New("synthetic sensitive password=not-for-output")
			failed = operatorClaimRead(t, onboardingAPI(t, admin, "POST", path, "synthetic-failed-read", body, 200))
			if failed.State != "failed" || failed.BlockedReason != "operator_demo_claim_native_unavailable" {
				t.Fatal("native failure was hidden or exposed", failed)
			}
			raw := onboardingAPI(t, admin, "POST", path, "synthetic-failed-read", body, 200)
			if bytes.Contains(raw, []byte("sensitive")) || f.native.roles.Load() != 2 {
				t.Fatal("duplicate failure retried or leaked upstream")
			}
			f.native.roleErr = nil
			started, finish := make(chan struct{}), make(chan struct{})
			f.native.beforeRoles = func() { close(started); <-finish }
			result := make(chan error, 1)
			ref := onboardingRef(t, admin, platform)
			go func() {
				_, err := f.app.GroupBinding.CreateOperatorDemoClaim(ctx, ref, group, 1, "synthetic-concurrent")
				result <- err
			}()
			<-started
			pending := operatorClaimRead(t, onboardingAPI(t, admin, "POST", path, "synthetic-concurrent", body, 200))
			if pending.State != "checking" || f.native.roles.Load() != 3 {
				t.Fatal("concurrent duplicate started another read", pending)
			}
			close(finish)
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			f.native.beforeRoles = nil
			active := operatorClaimRead(t, onboardingAPI(t, admin, "GET", path, "", nil, 200))
			if active.State != "active" || f.native.roles.Load() != 3 {
				t.Fatal("completed read was not persisted", active)
			}
			if _, err := f.db.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.accounts[platform]); err != nil {
				t.Fatal(err)
			}
			stale := operatorClaimRead(t, onboardingAPI(t, admin, "GET", path, "", nil, 200))
			if stale.State != "stale" || f.native.roles.Load() != 3 {
				t.Fatal("old account epoch retained demo admission", stale)
			}
			if f.native.sends.Load() != 0 {
				t.Fatal("claim failure performed a native mutation")
			}
			var audit string
			if err := f.db.Platform.QueryRow(ctx, `SELECT coalesce(string_agg(detail::text,''),'') FROM audit_events
				WHERE action LIKE 'operator_demo_group_claim_%'`).Scan(&audit); err != nil || strings.Contains(audit, "sensitive") {
				t.Fatal("audit exposed arbitrary upstream text", err)
			}
		})
	}
}

func TestPostgresOperatorDemoClaimExpiryAndRestart(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			group := operatorClaimGroup(t, f, platform)
			admin := f.admin
			admin.workspace = platform
			path := "/user-center/groups/" + group + "/demo-claim"
			// Expired and interrupted records are synthetic persisted state,
			// equivalent to restarting after a previous process saved them.
			seed := func(id, key, state string, expired bool) {
				t.Helper()
				_, err := f.db.Platform.Exec(ctx, `INSERT INTO operator_demo_group_claims(id,actor_id,
					actor_version,tenant_id,group_id,platform,account_id,account_epoch,account_version,
					binding_epoch,binding_version,create_key,state,robot_role,owner_identity_hash,
					created_at,expires_at)
					SELECT $1,u.id,u.access_version,b.tenant_id,b.group_id,a.platform,a.id,a.epoch,
						a.version,b.epoch,b.version,$2,$3,
						CASE WHEN $3='active' THEN 'admin' ELSE '' END,
						CASE WHEN $3='active' THEN decode(repeat('00',32),'hex') END,
						clock_timestamp()-CASE WHEN $4 THEN interval '11 minutes' ELSE interval '1 second' END,
						clock_timestamp()+CASE WHEN $4 THEN interval '-1 minute' ELSE interval '9 minutes' END
					FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
					JOIN users u ON u.id=$5 WHERE b.group_id=$6`,
					id, key, state, expired, f.adminID, group)
				if err != nil {
					t.Fatal(err)
				}
			}
			expiredID := control.NewID()
			seed(expiredID, "synthetic-expired-claim", "active", true)
			expired := operatorClaimRead(t, onboardingAPI(t, admin, "GET", path, "", nil, 200))
			if expired.State != "expired" || expired.BlockedReason != "operator_demo_claim_expired" {
				t.Fatal("expired proof remained current", expired)
			}
			_, err := f.db.Platform.Exec(ctx, `INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
				VALUES($1,'publish_notice','manual','open',ARRAY['acceptance:SYNTHETIC-DEMO-CLAIM'])`, platform)
			if err != nil {
				t.Fatal(err)
			}
			err = control.RequireHumanBusinessAdmission(ctx, f.db.Platform, control.Actor{ID: f.adminID,
				Role: "superadmin"}, control.BusinessPermissionCatalog(), platform, group, "publish_notice", "manual")
			var denied *control.Failure
			if !errors.As(err, &denied) || denied.Code != "operator_demo_claim_required" {
				t.Fatal("expired claim admitted a human action", err)
			}
			onboardingAPI(t, admin, "POST", path+"/"+expiredID+"/cancel", "",
				map[string]any{"version": expired.Version}, 200)
			pendingID := control.NewID()
			seed(pendingID, "synthetic-interrupted-claim", "checking", false)
			if err := f.app.GroupBinding.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			recovered := operatorClaimRead(t, onboardingAPI(t, admin, "GET", path, "", nil, 200))
			replay := operatorClaimRead(t, onboardingAPI(t, admin, "POST", path,
				"synthetic-interrupted-claim", map[string]any{"binding_version": 1}, 200))
			if recovered.ID != pendingID || recovered.State != "failed" ||
				recovered.BlockedReason != "operator_demo_claim_interrupted" || replay.ID != recovered.ID ||
				replay.State != recovered.State || replay.Version != recovered.Version ||
				replay.BlockedReason != recovered.BlockedReason {
				t.Fatal("restart did not preserve an interrupted result", recovered, replay)
			}
			if f.native.roles.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("expiry, recovery or replay automatically called native transport")
			}
		})
	}
}
