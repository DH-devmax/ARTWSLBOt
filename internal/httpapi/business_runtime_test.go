package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"wangshangliao/internal/control"
)

type runtimeNativeFixture struct {
	*onboardingNativeFixture
	afterRead func()
}

func (n runtimeNativeFixture) BindingRoles(ctx context.Context, scope control.GroupBindingNativeScope) (control.GroupBindingRoleEvidence, error) {
	out, err := n.onboardingNativeFixture.BindingRoles(ctx, scope)
	if n.afterRead != nil {
		n.afterRead()
	}
	return out, err
}

func TestPostgresBusinessRuntimeEnablesAuthenticatedManagement(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			client, admin := f.alice, f.admin
			client.workspace, admin.workspace = platform, platform
			c := f.createChallenge(t, client, platform, "runtime-bind-"+platform)
			c = f.prove(t, client, platform, c, f.native.owner, "native-runtime-"+platform)
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "runtime-confirm-"+platform,
				map[string]any{"version": c.Version}, 200)
			group := c.Group.ID
			path := "/groups/" + group + "/business-runtime"
			read := func() control.BusinessRuntime {
				t.Helper()
				var out control.BusinessRuntime
				if err := json.Unmarshal(expect(t, client, "GET", path, nil, 200), &out); err != nil {
					t.Fatal(err)
				}
				return out
			}
			before := read()
			var config []byte
			if err := f.db.Platform.QueryRow(ctx, `SELECT config FROM group_bindings WHERE group_id=$1`, group).Scan(&config); err != nil {
				t.Fatal(err)
			}
			calls := f.native.roles.Load()
			other, member := f.bob, f.member
			other.workspace, member.workspace = platform, platform
			expect(t, other, "PUT", path, map[string]any{"enabled": true, "version": before.Version}, 404)
			expect(t, member, "PUT", path, map[string]any{"enabled": true, "version": before.Version}, 404)
			if f.native.roles.Load() != calls {
				t.Fatal("unauthorized runtime update read native roles")
			}
			raw := expect(t, client, "PUT", path, map[string]any{"enabled": true, "version": before.Version}, 200)
			var running control.BusinessRuntime
			if err := json.Unmarshal(raw, &running); err != nil || !running.Enabled || !running.ManagementEnabled ||
				running.Version <= before.Version || running.BindingVersion < 2 || running.BindingEpoch < 2 {
				t.Fatal("runtime did not enable bound management", string(raw), err)
			}
			if now := read(); !now.Enabled || !now.ManagementEnabled || now.BlockedReason != "" {
				t.Fatal("saved runtime projection differs from live management", now)
			}
			var management, runtime, legacyFlags bool
			var saved []byte
			if err := f.db.Platform.QueryRow(ctx, `SELECT b.management_enabled,r.enabled,
				a.connection_enabled OR a.testing_enabled,b.config FROM group_bindings b
				JOIN group_business_runtime r ON r.group_id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
				WHERE b.group_id=$1`, group).Scan(&management, &runtime, &legacyFlags, &saved); err != nil ||
				!management || !runtime || legacyFlags || !bytes.Equal(config, saved) {
				t.Fatal("runtime enable altered feature config or borrowed test flags", err)
			}
			if err := control.RequireBusinessAdmission(ctx, f.db.Platform, platform, group, "mute_member", "automatic"); err == nil {
				t.Fatal("human runtime enable opened production admission")
			}
			expect(t, admin, "PUT", "/capabilities/gates/mute_member/automatic",
				map[string]any{"state": "pilot", "version": 0, "pilot_group_ids": []string{group},
					"evidence_refs": []string{"acceptance:SYNTHETIC-RUNTIME"}}, 200)
			if err := control.RequireBusinessAdmission(ctx, f.db.Platform, platform, group, "mute_member", "automatic"); err != nil {
				t.Fatal("explicit runtime enable remained blocked by management flag", err)
			}
			expect(t, client, "PUT", path, map[string]any{"enabled": false, "version": before.Version}, 409)
			var stopped control.BusinessRuntime
			if err := json.Unmarshal(expect(t, client, "PUT", path,
				map[string]any{"enabled": false, "version": running.Version}, 200), &stopped); err != nil ||
				stopped.Enabled || stopped.ManagementEnabled {
				t.Fatal("runtime stop left management enabled", stopped, err)
			}
			// Superadmins use the same target package, ceiling and native proof.
			if err := json.Unmarshal(expect(t, admin, "PUT", path,
				map[string]any{"enabled": true, "version": stopped.Version}, 200), &running); err != nil || !running.ManagementEnabled {
				t.Fatal("scoped superadmin could not enable management", running, err)
			}
			f.native.owner = "changed-native-owner"
			expect(t, client, "PUT", path, map[string]any{"enabled": true, "version": running.Version}, 409)
			var paused bool
			if err := f.db.Platform.QueryRow(ctx, `SELECT o.state='paused' AND NOT b.management_enabled AND NOT r.enabled
				FROM group_binding_ownership o JOIN group_bindings b ON b.group_id=o.group_id
				JOIN group_business_runtime r ON r.group_id=o.group_id WHERE o.group_id=$1`, group).Scan(&paused); err != nil || !paused {
				t.Fatal("failed enable rolled back ownership pause", err)
			}
		})
	}
}

func TestPostgresBusinessRuntimeRechecksAuthorizationAfterNativeRead(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	c := f.createChallenge(t, f.alice, "wsl", "runtime-late-binding")
	c = f.prove(t, f.alice, "wsl", c, f.native.owner, "late-runtime-group")
	onboardingAPI(t, f.alice, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "runtime-late-confirm",
		map[string]any{"version": c.Version}, 200)
	group := c.Group.ID
	path := "/groups/" + group + "/business-runtime"
	var current control.BusinessRuntime
	if err := json.Unmarshal(expect(t, f.alice, "GET", path, nil, 200), &current); err != nil {
		t.Fatal(err)
	}
	calls := f.native.roles.Load()
	if _, err := f.db.Platform.Exec(ctx, `UPDATE subscriptions SET enabled=false WHERE tenant_id=$1`, f.tenantA); err != nil {
		t.Fatal(err)
	}
	expect(t, f.admin, "PUT", path, map[string]any{"enabled": true, "version": current.Version}, 403)
	if f.native.roles.Load() != calls {
		t.Fatal("missing target package reached native roles")
	}
	if _, err := f.db.Platform.Exec(ctx, `UPDATE subscriptions SET enabled=true WHERE tenant_id=$1`, f.tenantA); err != nil {
		t.Fatal(err)
	}
	f.app.GroupBinding.Native = runtimeNativeFixture{onboardingNativeFixture: f.native, afterRead: func() {
		if _, err := f.db.Platform.Exec(ctx, `UPDATE tenant_capability_ceilings SET permissions=array_remove(permissions,'groups:configure')
			WHERE tenant_id=$1`, f.tenantA); err != nil {
			t.Fatal(err)
		}
	}}
	expect(t, f.admin, "PUT", path, map[string]any{"enabled": true, "version": current.Version}, 403)
	var disabled bool
	if err := f.db.Platform.QueryRow(ctx, `SELECT NOT b.management_enabled AND NOT r.enabled
		FROM group_bindings b JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`, group).Scan(&disabled); err != nil || !disabled {
		t.Fatal("revoked tenant ceiling reached runtime enable", err)
	}
}

func TestPostgresBusinessRuntimeRefusesChangedNativeRole(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			client := f.alice
			client.workspace = platform
			c := f.createChallenge(t, client, platform, "runtime-role-"+platform)
			c = f.prove(t, client, platform, c, f.native.owner, "runtime-role-group-"+platform)
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "runtime-role-confirm-"+platform,
				map[string]any{"version": c.Version}, 200)
			group := c.Group.ID
			path := "/groups/" + group + "/business-runtime"
			var before control.BusinessRuntime
			if err := json.Unmarshal(expect(t, client, "GET", path, nil, 200), &before); err != nil {
				t.Fatal(err)
			}
			f.native.robotRole = "member"
			expect(t, client, "PUT", path, map[string]any{"enabled": true, "version": before.Version}, 409)
			var paused bool
			if err := f.db.Platform.QueryRow(context.Background(), `SELECT o.state='paused' AND NOT b.management_enabled AND NOT r.enabled
				FROM group_binding_ownership o JOIN group_bindings b ON b.group_id=o.group_id
				JOIN group_business_runtime r ON r.group_id=o.group_id WHERE o.group_id=$1`, group).Scan(&paused); err != nil || !paused {
				t.Fatal("downgraded native role enabled management", err)
			}
		})
	}
}

func TestPostgresBusinessRuntimeAllowsInitiallyProvenMemberWithoutOpeningGates(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			f.native.robotRole = "member"
			client := f.alice
			client.workspace = platform
			c := f.createChallenge(t, client, platform, "runtime-member-"+platform)
			c = f.prove(t, client, platform, c, f.native.owner, "runtime-member-group-"+platform)
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "runtime-member-confirm-"+platform,
				map[string]any{"version": c.Version}, 200)
			group := c.Group.ID
			path := "/groups/" + group + "/business-runtime"
			var before, running control.BusinessRuntime
			if err := json.Unmarshal(expect(t, client, "GET", path, nil, 200), &before); err != nil || before.Enabled || before.ManagementEnabled {
				t.Fatal("member binding implicitly enabled runtime", before, err)
			}
			if err := json.Unmarshal(expect(t, client, "PUT", path,
				map[string]any{"enabled": true, "version": before.Version}, 200), &running); err != nil ||
				!running.Enabled || !running.ManagementEnabled || running.Version <= before.Version {
				t.Fatal("proven ordinary-member robot could not enable runtime", running, err)
			}
			var bound bool
			if err := f.db.Platform.QueryRow(context.Background(), `SELECT b.management_enabled AND r.enabled
				AND o.state='active' AND o.robot_role='member'
				FROM group_bindings b JOIN group_business_runtime r ON r.group_id=b.group_id
				JOIN group_binding_ownership o ON o.group_id=b.group_id AND o.tenant_id=b.tenant_id
				WHERE b.group_id=$1`, group).Scan(&bound); err != nil || !bound {
				t.Fatal("runtime enable changed or bypassed the member ownership anchor", err)
			}
			for _, source := range []string{"assistant", "automatic"} {
				err := control.RequireBusinessAdmission(context.Background(), f.db.Platform, platform, group, "send_text", source)
				if denied, ok := err.(*control.Failure); !ok || denied.Code != "business_admission_closed" {
					t.Fatal("member runtime enable changed per-action production admission", source, err)
				}
			}
		})
	}
}
