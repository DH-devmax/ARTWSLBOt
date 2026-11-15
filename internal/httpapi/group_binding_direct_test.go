package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"wangshangliao/internal/control"
)

func TestGroupBindingPostgresGroupProofWithoutPrivateIdentity(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			client, admin := f.alice, f.admin
			client.workspace, admin.workspace = platform, platform
			beforeRoles := f.native.roles.Load()
			for range 3 {
				if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
					t.Fatal("idle onboarding scan", err)
				}
			}
			if f.native.roles.Load() != beforeRoles || f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("idle onboarding scan called a native port")
			}
			for _, role := range []struct{ name, sender string }{
				{"owner", f.native.owner}, {"admin", "synthetic-native-admin"},
			} {
				c := f.createChallenge(t, client, platform, "direct-group-"+role.name)
				f.ingress(t, platform, control.OnboardingMessage{Group: "direct-group-" + role.name,
					Sender: role.sender, Text: c.GroupChallenge}, nil)
				if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
					t.Fatal(err)
				}
				current, err := f.app.GroupBinding.Read(ctx, onboardingRef(t, client, platform), c.ID)
				if err != nil || current.State != "proven" || !current.CanConfirm || current.IdentityProven ||
					current.CanAcceptInvitation || current.Group == nil || current.ProofRole != role.name {
					t.Fatal("authenticated group proof incorrectly required a private challenge", role.name, current, err)
				}
				var groupOnly bool
				if err = f.db.Platform.QueryRow(ctx, `SELECT identity_event_id IS NULL AND identity_sealed IS NULL
					AND proof_event_id IS NOT NULL AND proof_sealed IS NOT NULL
					FROM group_binding_challenges WHERE id=$1`, c.ID).Scan(&groupOnly); err != nil || !groupOnly {
					t.Fatal("group proof fabricated private identity evidence", err)
				}
				result := onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm",
					"direct-confirm-"+role.name, map[string]any{"version": current.Version}, 200)
				want := "bound"
				if role.name == "admin" {
					want = "review"
				}
				if field(t, result, "state") != want {
					t.Fatal("direct native proof bypassed its confirmation/review path", role.name)
				}
				if role.name == "admin" {
					var review control.GroupBindingChallenge
					if err = json.Unmarshal(result, &review); err != nil {
						t.Fatal(err)
					}
					var bound bool
					if err = f.db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_bindings WHERE group_id=$1)`,
						current.Group.ID).Scan(&bound); err != nil || bound {
						t.Fatal("native administrator bound before explicit review", err)
					}
					onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+c.ID+"/review", "",
						map[string]any{"version": review.Version, "approve": true}, 200)
				}
				var anchoredAndDisabled bool
				if err = f.db.Platform.QueryRow(ctx, `SELECT o.tenant_id=b.tenant_id AND o.state='active'
					AND o.applicant_role=$2 AND NOT b.management_enabled AND NOT b.ai_enabled AND NOT b.ai_requested AND NOT r.enabled
					FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id
					JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`,
					current.Group.ID, role.name).Scan(&anchoredAndDisabled); err != nil || !anchoredAndDisabled {
					t.Fatal("direct group binding lost ownership or enabled business runtime", err)
				}
			}
			if f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("direct group proof performed an invitation operation")
			}
		})
	}
}

func TestGroupBindingPostgresGroupProofRespectsPrivateIdentity(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			client := f.alice
			client.workspace = platform
			c := f.createChallenge(t, client, platform, "private-identity-match")
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: f.native.owner, Text: c.PrivateChallenge}, nil)
			if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			ref := onboardingRef(t, client, platform)
			identity, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
			if err != nil || !identity.IdentityProven || identity.State != "waiting" {
				t.Fatal("private identity prerequisite missing", err)
			}
			// B is a genuine administrator in the synthetic roster. The private
			// identity check, rather than a missing role, must reject B's proof.
			f.ingress(t, platform, control.OnboardingMessage{Group: "same-identity-group", Sender: "synthetic-native-admin",
				Text: c.GroupChallenge}, nil)
			if err = f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			rejected, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
			if err != nil || rejected.State != "waiting" || rejected.Group != nil || rejected.CanConfirm ||
				!rejected.IdentityProven || rejected.Version != identity.Version || rejected.BlockedReason == "" {
				t.Fatal("a different native sender reused the proven private identity", rejected, err)
			}
			var proofs int
			if err = f.db.Platform.QueryRow(ctx, `SELECT count(*) FROM group_binding_challenges
				WHERE id=$1 AND proof_event_id IS NOT NULL`, c.ID).Scan(&proofs); err != nil || proofs != 0 {
				t.Fatal("mismatched identity persisted group ownership proof", err)
			}
			f.ingress(t, platform, control.OnboardingMessage{Group: "same-identity-group", Sender: f.native.owner,
				Text: c.GroupChallenge}, nil)
			if err = f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
			if err != nil || current.State != "proven" || !current.CanConfirm || !current.IdentityProven ||
				current.BlockedReason != "" || f.native.roles.Load() != 2 {
				t.Fatal("the original private identity could not prove its group", current, err)
			}
		})
	}
}

func TestGroupBindingPostgresInvitationRequiresPrivateIdentity(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			client := f.alice
			client.workspace = platform
			c := f.createChallenge(t, client, platform, "invitation-private-required")
			if _, err := f.db.Platform.Exec(ctx, `INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
				VALUES($1,'group_accept','onboarding','open',ARRAY['synthetic-private-identity-regression'])`, platform); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"native_group_id": "private-invitation-group", "version": c.Version}
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/invitation-prepare", "", input, 409)
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/invitation-accept",
				"missing-private-identity", input, 409)
			var attempts int
			if err := f.db.Platform.QueryRow(ctx, `SELECT count(*) FROM customer_group_accept_attempts WHERE challenge_id=$1`,
				c.ID).Scan(&attempts); err != nil || attempts != 0 {
				t.Fatal("an invitation without private identity created a durable attempt", err)
			}
			if f.native.roles.Load() != 0 || f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("an invitation without private identity reached the native port")
			}
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: f.native.owner, Text: c.PrivateChallenge}, nil)
			if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := f.app.GroupBinding.Read(ctx, onboardingRef(t, client, platform), c.ID)
			if err != nil || !current.IdentityProven || !current.CanAcceptInvitation || current.CanConfirm {
				t.Fatal("private identity did not admit invitation preparation independently", current, err)
			}
			input["version"] = current.Version
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/invitation-prepare", "", input, 200)
			if f.native.invites.Load() != 1 || f.native.sends.Load() != 0 {
				t.Fatal("preparing a privately proven invitation submitted it")
			}
		})
	}
}
