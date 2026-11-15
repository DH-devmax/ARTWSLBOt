package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"wangshangliao/internal/control"
)

func operatorBindingChallenge(t *testing.T, f *onboardingFixture, client testClient, platform, tenant, key string) control.GroupBindingChallenge {
	t.Helper()
	client.workspace = platform
	raw := onboardingAPI(t, client, "POST", "/group-binding/challenges", key,
		map[string]any{"platform": platform, "target_tenant_id": tenant}, 201)
	var out control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &out); err != nil || out.Mode != "operator_demo" ||
		out.Customer.TenantID != tenant || out.Customer.Name == "" || out.Scope.TenantID != tenant ||
		out.Scope.Platform != platform || out.Scope.ActorID == "" {
		t.Fatal("operator response lost its server-owned applicant/resource scope", out, err)
	}
	return out
}

func TestGroupBindingPostgresOperatorContextAndInputIsolation(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		admin, alice, member := f.admin, f.alice, f.member
		admin.workspace, alice.workspace, member.workspace = platform, platform, platform
		var first, second control.GroupBindingContext
		raw := onboardingAPI(t, admin, "GET", "/group-binding/context?platform="+platform+"&limit=1", "", nil, 200)
		if err := json.Unmarshal(raw, &first); err != nil || first.Mode != "operator_demo" ||
			len(first.Customers) != 1 || first.Customers[0].TenantID != f.tenantA || first.NextOffset != 1 ||
			first.Scope.Platform != platform || first.Scope.ActorID != f.adminID ||
			first.Scope.TenantID != first.Workspace.ID || first.Workspace.Kind != "operator_demo" ||
			first.DefaultTargetTenantID != first.Workspace.ID || !first.CanOnboardForCustomer {
			t.Fatal("bounded context returned a different role/resource", first, err)
		}
		raw = onboardingAPI(t, admin, "GET", "/group-binding/context?platform="+platform+"&limit=1&offset=1", "", nil, 200)
		if err := json.Unmarshal(raw, &second); err != nil || len(second.Customers) != 1 ||
			second.Customers[0].TenantID != f.tenantB || second.NextOffset != -1 {
			t.Fatal("context pagination did not retain its customer boundary", second, err)
		}
		raw = onboardingAPI(t, alice, "GET", "/group-binding/context?platform="+platform, "", nil, 200)
		var own control.GroupBindingContext
		if err := json.Unmarshal(raw, &own); err != nil || own.Mode != "tenant_business" ||
			len(own.Customers) != 1 || own.Customers[0].TenantID != f.tenantA || own.Scope.TenantID != f.tenantA ||
			own.Scope.ActorID != f.aliceID || own.NextOffset != -1 {
			t.Fatal("customer context disclosed another tenant", own, err)
		}
		onboardingAPI(t, member, "GET", "/group-binding/context?platform="+platform, "", nil, 403)
		onboardingAPI(t, admin, "GET", "/group-binding/context?platform="+platform+"&limit=51", "", nil, 400)
		onboardingAPI(t, admin, "GET", "/group-binding/context?platform="+platform+"&offset=-1", "", nil, 400)
		onboardingAPI(t, admin, "GET", "/group-binding/context?platform="+platform+"&limit=bad", "", nil, 400)
		other := "wsl"
		if platform == "wsl" {
			other = "lajiaoliao"
		}
		onboardingAPI(t, admin, "GET", "/group-binding/context?platform="+other, "", nil, 400)
		onboardingAPI(t, alice, "POST", "/group-binding/challenges", "customer-target-forbidden-"+platform,
			map[string]any{"platform": platform, "target_tenant_id": f.tenantA}, 403)
		ownChallenge := f.createChallenge(t, admin, platform, "operator-own-default-"+platform)
		if ownChallenge.Customer.TenantID != first.Workspace.ID || ownChallenge.Mode != "operator_demo" {
			t.Fatal("new operator challenge did not use its own space", ownChallenge)
		}
		onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+ownChallenge.ID+"/cancel", "",
			map[string]any{"version": ownChallenge.Version}, 200)
		onboardingAPI(t, admin, "POST", "/group-binding/challenges", "operator-invalid-customer-"+platform,
			map[string]any{"platform": platform, "target_tenant_id": f.aliceID}, 404)
		onboardingAPI(t, admin, "POST", "/group-binding/challenges", "browser-mode-forbidden-"+platform,
			map[string]any{"platform": platform, "target_tenant_id": f.tenantA, "mode": "operator_demo"}, 400)
		onboardingAPI(t, admin, "POST", "/group-binding/challenges", "browser-name-forbidden-"+platform,
			map[string]any{"platform": platform, "target_tenant_id": f.tenantA, "customer": map[string]string{"name": "Forged"}}, 400)
	}
	// An empty tenant and a tenant pointing at an ordinary member are not
	// customers. No synthetic customer/account is created by opening the list.
	var orphan, memberTenant string
	if err := f.db.Platform.QueryRow(ctx, `INSERT INTO tenants(name) VALUES('Orphan') RETURNING id::text`).Scan(&orphan); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Platform.QueryRow(ctx, `INSERT INTO tenants(name,owner_id) SELECT 'Member-only',id FROM users
		WHERE username='customer-member' RETURNING id::text`).Scan(&memberTenant); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{orphan, memberTenant} {
		onboardingAPI(t, f.admin, "POST", "/group-binding/challenges", "not-a-real-owner-"+tenant,
			map[string]string{"platform": "wsl", "target_tenant_id": tenant}, 404)
	}
	var filtered control.GroupBindingContext
	raw := onboardingAPI(t, f.admin, "GET", "/group-binding/context?platform=wsl&search=customer-b", "", nil, 200)
	if err := json.Unmarshal(raw, &filtered); err != nil || len(filtered.Customers) != 1 || filtered.Customers[0].TenantID != f.tenantB {
		t.Fatal("customer search did not filter server-owned names", filtered, err)
	}
	raw = onboardingAPI(t, f.admin, "GET", "/group-binding/context?platform=wsl", "", nil, 200)
	if err := json.Unmarshal(raw, &filtered); err != nil || len(filtered.Customers) != 2 {
		t.Fatal("ownerless/member-only tenant appeared in onboarding customers", filtered, err)
	}
	c := operatorBindingChallenge(t, f, f.admin, "wsl", f.tenantA, "operator-only-applicant")
	onboardingAPI(t, f.alice, "GET", "/group-binding/challenges/"+c.ID, "", nil, 404)
	onboardingAPI(t, f.alice, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "customer-cannot-impersonate-operator",
		map[string]any{"version": c.Version}, 404)
	otherID := control.NewID()
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO users(id,username,password_hash,role)
		VALUES($1,$1::uuid::text,'synthetic-unused-password','superadmin')`, otherID); err != nil {
		t.Fatal(err)
	}
	otherOperator := f.client(t, f.admin.base, otherID)
	onboardingAPI(t, otherOperator, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "other-operator-cannot-confirm",
		map[string]any{"version": c.Version}, 404)
	onboardingAPI(t, otherOperator, "POST", "/group-binding/challenges/"+c.ID+"/cancel", "",
		map[string]any{"version": c.Version}, 404)
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "target-cannot-change-on-confirm",
		map[string]any{"version": c.Version, "target_tenant_id": f.tenantB}, 400)
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+c.ID+"/cancel", "",
		map[string]any{"version": c.Version}, 200)
	if f.native.roles.Load() != 0 || f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
		t.Fatal("context reads or rejected input called a native port")
	}
}

func TestGroupBindingPostgresOperatorBindRestoreAndReviewWithoutPackage(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			admin, customer := f.admin, f.alice
			admin.workspace, customer.workspace = platform, platform
			if _, err := f.db.Platform.Exec(ctx, `DELETE FROM subscriptions WHERE tenant_id=$1`, f.tenantA); err != nil {
				t.Fatal(err)
			}
			resourceCounts := func() [5]int {
				t.Helper()
				var out [5]int
				if err := f.db.Platform.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenants),(SELECT count(*) FROM users),
					(SELECT count(*) FROM plans),(SELECT count(*) FROM subscriptions),(SELECT count(*) FROM subscription_orders)`).
					Scan(&out[0], &out[1], &out[2], &out[3], &out[4]); err != nil {
					t.Fatal(err)
				}
				return out
			}
			before := resourceCounts()
			// The operator reserves the other platform too. The binding
			// trigger must preserve this reservation without requiring a package.
			otherPlatform := "wsl"
			if platform == "wsl" {
				otherPlatform = "lajiaoliao"
			}
			operatorBindingChallenge(t, f, admin, otherPlatform, f.tenantA, "other-platform-reservation")
			c := operatorBindingChallenge(t, f, admin, platform, f.tenantA, "operator-bind-challenge")
			if c.Scope.ActorID != f.adminID || c.Customer.Name != "customer-a" {
				t.Fatal("operator/customer metadata was supplied by the browser")
			}
			for _, statement := range []string{
				`UPDATE group_binding_challenges SET applicant_mode='tenant_business' WHERE id=$1`,
				`UPDATE group_binding_challenges SET tenant_id='` + f.tenantB + `'::uuid WHERE id=$1`,
			} {
				if _, err := f.db.Platform.Exec(ctx, statement, c.ID); err == nil {
					t.Fatal("persisted applicant mode/resource could be replaced")
				}
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges", "operator-bind-challenge",
				map[string]string{"platform": platform, "target_tenant_id": f.tenantB}, 409)
			onboardingAPI(t, customer, "POST", "/group-binding/challenges", "customer-still-needs-package",
				map[string]string{"platform": platform}, 409)
			c = f.prove(t, admin, platform, c, f.native.owner, "operator-native-group")
			bound := onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "operator-bind-confirm",
				map[string]any{"version": c.Version}, 200)
			if field(t, bound, "state") != "bound" {
				t.Fatal("authenticated native owner did not bind for the selected customer")
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "operator-bind-confirm",
				map[string]any{"version": c.Version}, 200)
			var isolated bool
			err := f.db.Platform.QueryRow(ctx, `SELECT b.tenant_id=$2 AND o.tenant_id=$2 AND c.applicant_id=$3
				AND c.applicant_mode='operator_demo' AND o.proof_challenge_id=c.id AND o.state='active'
				AND NOT b.management_enabled AND NOT b.ai_enabled AND NOT b.ai_requested AND NOT r.enabled
				AND NOT EXISTS(SELECT 1 FROM operator_demo_group_claims)
				AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$2)
				FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id
				JOIN group_binding_challenges c ON c.id=o.proof_challenge_id
				JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`,
				c.Group.ID, f.tenantA, f.adminID).Scan(&isolated)
			if err != nil || !isolated {
				t.Fatal("operator impersonated the customer, enabled business, or reused demo-claim evidence", err)
			}
			entitlement, err := control.ResolveHumanEntitlement(ctx, f.db.Platform,
				control.Actor{ID: f.aliceID, TenantID: f.tenantA, Role: "user"}, nil, platform, c.Group.ID)
			if err != nil || entitlement.Active || entitlement.OperatorDemo || entitlement.BillingSource != "tenant_wallet" {
				t.Fatal("customer inherited the human operator's commercial exemption", entitlement, err)
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges", "cannot-replace-active-binding",
				map[string]any{"platform": platform, "target_tenant_id": f.tenantA, "restore_group_id": c.Group.ID, "binding_version": 1}, 409)
			if _, err = f.db.Platform.Exec(ctx, `UPDATE group_bindings SET active=false,management_enabled=false,ai_enabled=false,
				config='{"blocked_words":["retain"],"feature_options":{"blocked_words":{"enabled":true}}}' WHERE group_id=$1`,
				c.Group.ID); err != nil {
				t.Fatal(err)
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges", "restore-target-mismatch",
				map[string]any{"platform": platform, "target_tenant_id": f.tenantB, "restore_group_id": c.Group.ID, "binding_version": 1}, 409)
			raw := onboardingAPI(t, admin, "POST", "/group-binding/challenges", "operator-restore",
				map[string]any{"platform": platform, "target_tenant_id": f.tenantA, "restore_group_id": c.Group.ID, "binding_version": 1}, 201)
			var restored control.GroupBindingChallenge
			if err = json.Unmarshal(raw, &restored); err != nil || restored.Robot.ID != c.Robot.ID {
				t.Fatal("restore replaced its original robot", err)
			}
			restored = f.prove(t, admin, platform, restored, f.native.owner, "operator-native-group")
			onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+restored.ID+"/confirm", "operator-restore-confirm",
				map[string]any{"version": restored.Version}, 200)
			if err = f.db.Platform.QueryRow(ctx, `SELECT b.active AND b.epoch=2 AND b.version=2
				AND b.config->'blocked_words'='["retain"]'::jsonb
				AND b.config#>>'{feature_options,blocked_words,enabled}'='false'
				AND NOT b.management_enabled AND NOT b.ai_enabled AND NOT r.enabled
				FROM group_bindings b JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`,
				c.Group.ID).Scan(&isolated); err != nil || !isolated {
				t.Fatal("operator restore lost config, changed history, or activated business", err)
			}
			adminProof := operatorBindingChallenge(t, f, admin, platform, f.tenantA, "operator-native-admin-proof")
			adminProof = f.prove(t, admin, platform, adminProof, "synthetic-native-admin", "operator-admin-group")
			raw = onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+adminProof.ID+"/confirm", "operator-native-admin-confirm",
				map[string]any{"version": adminProof.Version}, 200)
			var review control.GroupBindingChallenge
			if err = json.Unmarshal(raw, &review); err != nil || review.State != "review" {
				t.Fatal("operator website role skipped native-administrator review", err)
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+adminProof.ID+"/review", "",
				map[string]any{"version": review.Version, "approve": true}, 200)
			if after := resourceCounts(); after != before {
				t.Fatal("operator onboarding created a customer, subscription, or order", before, after)
			}
			if f.native.sends.Load() != 0 || f.native.invites.Load() != 0 {
				t.Fatal("already-joined group proof unexpectedly accepted an invitation")
			}
		})
	}
}

func TestGroupBindingPostgresOperatorConfirmRechecksScope(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			for index, change := range []string{"actor_version", "actor_role", "customer_owner", "account_epoch", "pool_sender"} {
				t.Run(change, func(t *testing.T) {
					var actor string
					if err := f.db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role)
						VALUES($1,'synthetic-unused-password','superadmin') RETURNING id::text`, fmt.Sprintf("operator-scope-%d", index)).
						Scan(&actor); err != nil {
						t.Fatal(err)
					}
					client := f.client(t, f.admin.base, actor)
					client.workspace = platform
					c := operatorBindingChallenge(t, f, client, platform, f.tenantA, "operator-scope-"+change)
					c = f.prove(t, client, platform, c, f.native.owner, "scope-"+change)
					ref := onboardingRef(t, client, platform)
					f.native.beforeRoles = func() {
						f.native.beforeRoles = nil
						var err error
						switch change {
						case "actor_version":
							_, err = f.db.Platform.Exec(ctx, `UPDATE users SET access_version=access_version+1 WHERE id=$1`, actor)
						case "actor_role":
							_, err = f.db.Platform.Exec(ctx, `UPDATE users SET role='user',tenant_id=$2,tenant_role='viewer' WHERE id=$1`, actor, f.tenantA)
						case "customer_owner":
							_, err = f.db.Platform.Exec(ctx, `UPDATE tenants SET owner_id=NULL WHERE id=$1`, f.tenantA)
						case "account_epoch":
							_, err = f.db.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.accounts[platform])
						case "pool_sender":
							_, err = f.db.Platform.Exec(ctx, `INSERT INTO accounts(name,platform,workspace,purpose,native_uid)
								VALUES('Synthetic now-pooled owner',$1,$1,'management',$2)`, platform, f.native.owner)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					_, err := f.app.GroupBinding.Confirm(ctx, ref, c.ID,
						control.GroupBindingConfirmation{Version: c.Version, Key: "operator-scope-confirm-" + change})
					if err == nil {
						t.Fatal("changed scope was accepted at the final binding fence")
					}
					var persisted bool
					if err = f.db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_bindings WHERE group_id=$1)
						OR EXISTS(SELECT 1 FROM group_binding_ownership WHERE proof_challenge_id=$2)`,
						c.Group.ID, c.ID).Scan(&persisted); err != nil || persisted {
						t.Fatal("scope change partially committed a binding or ownership proof", err)
					}
					if change == "customer_owner" {
						if _, err = f.db.Platform.Exec(ctx, `UPDATE tenants SET owner_id=$2 WHERE id=$1`, f.tenantA, f.aliceID); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
			if f.native.sends.Load() != 0 {
				t.Fatal("binding scope checks submitted a native mutation")
			}
		})
	}
}

func TestGroupBindingPostgresOperatorInvitationUnknownAndFinalRevoke(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			admin := f.admin
			admin.workspace = platform
			if _, err := f.db.Platform.Exec(ctx, `DELETE FROM subscriptions WHERE tenant_id=$1`, f.tenantA); err != nil {
				t.Fatal(err)
			}
			c := operatorBindingChallenge(t, f, admin, platform, f.tenantA, "operator-invitation")
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: f.native.owner, Text: c.PrivateChallenge}, nil)
			if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			ref := onboardingRef(t, admin, platform)
			current, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
			if err != nil || !current.CanAcceptInvitation {
				t.Fatal("operator private challenge did not prove its inviter", err)
			}
			in := control.GroupBindingAcceptInput{NativeGroupID: "operator-invited-group", Version: current.Version, Key: "operator-invitation-once"}
			if _, err = f.app.GroupBinding.ConfirmAccept(ctx, ref, c.ID, in); err == nil || f.native.sends.Load() != 0 {
				t.Fatal("operator bypassed the closed onboarding source gate")
			}
			if _, err = f.db.Platform.Exec(ctx, `INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
				VALUES($1,'group_accept','onboarding','open',ARRAY['synthetic-operator-onboarding'])`, platform); err != nil {
				t.Fatal(err)
			}
			f.native.unknown = true
			attempt, err := f.app.GroupBinding.ConfirmAccept(ctx, ref, c.ID, in)
			if err != nil || attempt.Status != "unknown" || f.native.sends.Load() != 1 {
				t.Fatal("unknown operator invitation was not retained", attempt, err)
			}
			again, err := f.app.GroupBinding.ConfirmAccept(ctx, ref, c.ID, in)
			if err != nil || again.ID != attempt.ID || f.native.sends.Load() != 1 {
				t.Fatal("unknown operator invitation was replayed", err)
			}
			if err = f.app.GroupBinding.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			resolved, err := f.app.GroupBinding.ReconcileAccept(ctx, ref, c.ID, attempt.ID)
			if err != nil || resolved.Status != "succeeded" || f.native.sends.Load() != 1 {
				t.Fatal("original operator invitation could not reconcile without a replay", resolved, err)
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+c.ID+"/cancel", "",
				map[string]any{"version": current.Version}, 200)
			next := operatorBindingChallenge(t, f, admin, platform, f.tenantA, "operator-final-revoke")
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: f.native.owner, Text: next.PrivateChallenge}, nil)
			if err = f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			next, err = f.app.GroupBinding.Read(ctx, ref, next.ID)
			if err != nil {
				t.Fatal(err)
			}
			f.native.unknown = false
			f.native.beforeSubmit = func(context.Context, control.GroupBindingAcceptRequest) error {
				_, err := f.db.Platform.Exec(ctx, `UPDATE users SET access_version=access_version+1 WHERE id=$1`, f.adminID)
				return err
			}
			rejected, err := f.app.GroupBinding.ConfirmAccept(ctx, ref, next.ID, control.GroupBindingAcceptInput{
				NativeGroupID: "operator-revoked-invite", Version: next.Version, Key: "operator-final-revoke-once"})
			if err != nil || rejected.Status != "failed" || f.native.sends.Load() != 1 {
				t.Fatal("revoked operator crossed the native final fence", rejected, err)
			}
		})
	}
}

func TestGroupBindingPostgresOperatorCrossTenantNeedsTransferReview(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			source, admin := f.alice, f.admin
			source.workspace, admin.workspace = platform, platform
			first := f.createChallenge(t, source, platform, "source-customer-group")
			first = f.prove(t, source, platform, first, f.native.owner, "shared-native-group")
			onboardingAPI(t, source, "POST", "/group-binding/challenges/"+first.ID+"/confirm", "source-customer-confirm",
				map[string]any{"version": first.Version}, 200)
			if _, err := f.db.Platform.Exec(ctx, `DELETE FROM subscriptions WHERE tenant_id=$1`, f.tenantB); err != nil {
				t.Fatal(err)
			}
			next := operatorBindingChallenge(t, f, admin, platform, f.tenantB, "operator-transfer-challenge")
			next = f.prove(t, admin, platform, next, f.native.owner, "shared-native-group")
			raw := onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+next.ID+"/confirm", "operator-transfer-confirm",
				map[string]any{"version": next.Version}, 200)
			var review control.GroupBindingChallenge
			if err := json.Unmarshal(raw, &review); err != nil || review.State != "review" ||
				!review.TransferRequired || review.Transfer == nil || review.Transfer.SourceTenantID != f.tenantA ||
				review.Customer.TenantID != f.tenantB || review.Mode != "operator_demo" {
				t.Fatal("operator bypassed the dedicated foreign-tenant review", review, err)
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+next.ID+"/review", "",
				map[string]any{"version": review.Version, "approve": true}, 409)
			var before string
			if err := f.db.Platform.QueryRow(ctx, `SELECT tenant_id::text FROM group_bindings WHERE group_id=$1`,
				first.Group.ID).Scan(&before); err != nil || before != f.tenantA {
				t.Fatal("operator confirmation silently replaced the previous resource customer", err)
			}
			body := map[string]any{"version": review.Version, "approve": true,
				"source_tenant_id": f.tenantA, "binding_version": review.Transfer.BindingVersion}
			approved := onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review",
				"operator-explicit-transfer", body, 200)
			if field(t, approved, "state") != "bound" {
				t.Fatal("explicit operator transfer failed without a target package")
			}
			onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review",
				"operator-explicit-transfer", body, 200)
			var valid bool
			if err := f.db.Platform.QueryRow(ctx, `SELECT b.tenant_id=$2 AND o.tenant_id=$2
				AND o.proof_challenge_id=$3 AND b.version=2 AND b.epoch=2
				AND NOT b.management_enabled AND NOT b.ai_enabled AND NOT runtime.enabled
				AND (SELECT count(*) FROM customer_group_transfers WHERE challenge_id=$3)=1
				AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$2)
				FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id
				JOIN group_business_runtime runtime ON runtime.group_id=b.group_id WHERE b.group_id=$1`,
				first.Group.ID, f.tenantB, next.ID).Scan(&valid); err != nil || !valid {
				t.Fatal("operator transfer replayed, lost ownership, or enabled customer business", err)
			}
			if f.native.sends.Load() != 0 {
				t.Fatal("website transfer unexpectedly submitted a native mutation")
			}
		})
	}
}
