package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wangshangliao/internal/control"
)

func TestGroupBindingPostgresReproveActiveLegacyBinding(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			client, other := f.alice, f.bob
			client.workspace, other.workspace = platform, platform
			var group string
			if err := f.db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)
				VALUES($1,'legacy-native-group','Legacy group') RETURNING id::text`, platform).Scan(&group); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,
				management_enabled,config) VALUES($1,$2,$3,true,
				'{"blocked_words":["keep-this-config"],"feature_options":{"blocked_words":{"enabled":true}}}')`,
				group, f.tenantA, f.accounts[platform]); err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"platform": platform, "restore_group_id": group, "binding_version": 1}
			onboardingAPI(t, other, "POST", "/group-binding/challenges", "wrong-tenant-reproof", body, 409)
			body["binding_version"] = 2
			onboardingAPI(t, client, "POST", "/group-binding/challenges", "wrong-version-reproof", body, 409)
			body["binding_version"] = 1
			// A legacy manual review without a new authenticated challenge cannot
			// turn an already active binding into ownership evidence.
			tx, err := f.db.Platform.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = control.BindCustomerGroupTx(ctx, tx, f.tenantA, platform, f.accounts[platform], group, 1, "")
			_ = tx.Rollback(ctx)
			if err == nil {
				t.Fatal("manual review rebound an active legacy binding without proof")
			}
			list := onboardingAPI(t, client, "GET", "/user-center/groups?platform="+platform, "", nil, 200)
			var groups struct {
				Items []struct {
					ID        string `json:"group_id"`
					Ownership string `json:"ownership_state"`
				} `json:"items"`
			}
			if err = json.Unmarshal(list, &groups); err != nil || len(groups.Items) != 1 ||
				groups.Items[0].ID != group || groups.Items[0].Ownership != "unverified" {
				t.Fatal("legacy ownership status not available to its customer", err)
			}
			raw := onboardingAPI(t, client, "POST", "/group-binding/challenges", "legacy-reproof", body, 201)
			var c control.GroupBindingChallenge
			if err = json.Unmarshal(raw, &c); err != nil || c.Group == nil || c.Group.ID != group ||
				c.Robot.ID != f.accounts[platform] {
				t.Fatal("legacy reproof did not retain its group and robot", err)
			}
			var unchanged bool
			if err = f.db.Platform.QueryRow(ctx, `SELECT active AND management_enabled AND version=1 AND epoch=1
				AND NOT EXISTS(SELECT 1 FROM group_binding_ownership WHERE group_id=$1)
				FROM group_bindings WHERE group_id=$1`, group).Scan(&unchanged); err != nil || !unchanged {
				t.Fatal("creating a challenge changed the live binding", err)
			}
			c = f.prove(t, client, platform, c, f.native.owner, "legacy-native-group")
			body = map[string]any{"version": c.Version}
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "legacy-reproof-confirm", body, 200)
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "legacy-reproof-confirm", body, 200)
			var valid bool
			var version int64
			if err = f.db.Platform.QueryRow(ctx, `SELECT b.version,b.active AND NOT b.management_enabled
				AND NOT b.ai_enabled AND NOT r.enabled AND b.epoch=2 AND b.version=2
				AND b.config->'blocked_words'='["keep-this-config"]'::jsonb
				AND b.config#>>'{feature_options,blocked_words,enabled}'='false'
				AND o.state='active' AND o.tenant_id=b.tenant_id
				FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id
				JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`, group).
				Scan(&version, &valid); err != nil || !valid {
				t.Fatal("reproof lost config, replayed, or activated business", err)
			}
			onboardingAPI(t, client, "POST", "/group-binding/challenges", "already-verified-reproof",
				map[string]any{"platform": platform, "restore_group_id": group, "binding_version": version}, 409)
			if f.native.sends.Load() != 0 {
				t.Fatal("ownership reproof performed a native mutation")
			}
		})
	}
}

func TestGroupBindingPostgresProofDiagnosticsAndBoundedRetry(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			client := f.alice
			client.workspace = platform
			c := f.createChallenge(t, client, platform, "proof-diagnostics")
			ref := onboardingRef(t, client, platform)
			readReason := func(want string, calls int32) {
				t.Helper()
				if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
					t.Fatal(err)
				}
				current, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
				if err != nil || current.BlockedReason != want || current.State != "waiting" ||
					current.CanConfirm || f.native.roles.Load() != calls {
					t.Fatalf("wrong bounded proof diagnostic: got %+v calls=%d want=%s/%d err=%v",
						current, f.native.roles.Load(), want, calls, err)
				}
			}
			// A pool identity is rejected before reading native roles. Polling
			// cannot silently retry the same event or spend upstream requests.
			var poolSender string
			if err := f.db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,platform,workspace,purpose,native_uid)
				VALUES('Other pool robot',$1,$1,'management','synthetic-other-robot') RETURNING native_uid`, platform).
				Scan(&poolSender); err != nil {
				t.Fatal(err)
			}
			f.ingress(t, platform, control.OnboardingMessage{Group: "diagnostic-group", Sender: poolSender, Text: c.GroupChallenge}, nil)
			readReason("group_binding_pool_identity", 0)
			readReason("group_binding_pool_identity", 0)
			f.ingress(t, platform, control.OnboardingMessage{Group: "diagnostic-group", Sender: "synthetic-ordinary-member", Text: c.GroupChallenge}, nil)
			readReason("group_binding_native_role_required", 1)
			readReason("group_binding_native_role_required", 1)
			f.native.roleErr = errors.New("untrusted upstream body with a synthetic credential")
			f.ingress(t, platform, control.OnboardingMessage{Group: "diagnostic-group", Sender: f.native.owner, Text: c.GroupChallenge}, nil)
			readReason("group_binding_native_proof_unavailable", 2)
			f.native.roleErr = nil
			readReason("group_binding_native_proof_unavailable", 2)
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: f.native.owner, Text: c.PrivateChallenge}, nil)
			if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: "synthetic-native-admin", Text: c.PrivateChallenge}, nil)
			readReason("group_binding_identity_changed", 2)
			readReason("group_binding_identity_changed", 2)
			f.ingress(t, platform, control.OnboardingMessage{Group: "diagnostic-group", Sender: "synthetic-native-admin", Text: c.GroupChallenge}, nil)
			readReason("group_binding_identity_changed", 3)
			f.ingress(t, platform, control.OnboardingMessage{Group: "diagnostic-group", Sender: f.native.owner, Text: c.GroupChallenge}, nil)
			if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
			if err != nil || current.State != "proven" || current.BlockedReason != "" || !current.CanConfirm ||
				f.native.roles.Load() != 4 || f.native.sends.Load() != 0 {
				t.Fatal("a fresh valid human proof did not recover the same challenge", current, err)
			}
		})
	}
}
