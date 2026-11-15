package httpapi

import (
	"context"
	"net/http"
	"testing"

	"wangshangliao/internal/control"
)

func TestGroupBindingContactPostgresRequiresIndependentPrivateIngress(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, decision := range []string{"prepared", "approved", "failed"} {
			t.Run(platform+"/"+decision, func(t *testing.T) {
				f, n, client, ref := friendFixture(t, platform)
				friendOpenGate(t, f, platform)
				// Public Create still defaults to incoming_contact for both
				// existing members and new friend rounds.
				created := f.createChallenge(t, client, platform, "private-round-create")
				c := contactPrepare(t, f, client, created)
				if decision != "prepared" {
					n.noFence = decision == "failed"
					c = contactApprove(t, f, client, c)
					want := "succeeded"
					if decision == "failed" {
						want = "failed"
					}
					if c.FriendContact.Status != want {
						t.Fatal("friend decision fixture did not reach its intended state", c)
					}
				}
				ctx := context.Background()
				f.ingress(t, platform, control.OnboardingMessage{Group: "private-round-group",
					Sender: n.owner, Text: created.GroupChallenge}, nil)
				if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
					t.Fatal(err)
				}
				blocked, err := f.app.GroupBinding.Read(ctx, ref, created.ID)
				if err != nil || blocked.State != "waiting" || blocked.IdentityProven || blocked.CanConfirm ||
					blocked.BlockedReason != "group_binding_identity_required" || blocked.Version != c.Version ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_challenges
						WHERE id=$1 AND proof_event_id IS NOT NULL`, created.ID) != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 {
					t.Fatal("friend round accepted group proof without independent private ingress", blocked, err)
				}
				onboardingAPI(t, client, http.MethodPost, "/group-binding/challenges/"+created.ID+"/confirm",
					"private-round-early-confirm", map[string]int64{"version": blocked.Version}, http.StatusConflict)
				// No challenge, identity seal or ownership is seeded with SQL.
				proven := f.prove(t, client, platform, created, n.owner, "private-round-group")
				if !proven.IdentityProven || friendCount(t, f, `SELECT count(*) FROM group_binding_challenges
					WHERE id=$1 AND identity_event_id IS NOT NULL AND identity_friend_proof_id IS NULL
					AND proof_event_id IS NOT NULL AND identity_event_id<>proof_event_id`, created.ID) != 1 {
					t.Fatal("new friend round did not retain two independent received events", proven)
				}
				raw := onboardingAPI(t, client, http.MethodPost, "/group-binding/challenges/"+created.ID+"/confirm",
					"private-round-confirm", map[string]int64{"version": proven.Version}, http.StatusOK)
				if field(t, raw, "state") != "bound" {
					t.Fatal("real private and group ingress could not complete the friend round")
				}
			})
		}
	}
}

func TestGroupBindingContactPostgresFinalConfirmationRechecksPrivateIngress(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := friendFixture(t, platform)
			friendOpenGate(t, f, platform)
			created := f.createChallenge(t, client, platform, "private-final-create")
			contactApprove(t, f, client, contactPrepare(t, f, client, created))
			proven := f.prove(t, client, platform, created, n.owner, "private-final-group")
			// Model retention after proof, during the final native role read.
			f.native.beforeRoles = func() {
				f.native.beforeRoles = nil
				if _, err := f.db.Platform.Exec(context.Background(), `UPDATE native_received_events SET payload_sealed=NULL
					WHERE id=(SELECT identity_event_id FROM group_binding_challenges WHERE id=$1)`, created.ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err := f.app.GroupBinding.Confirm(context.Background(), ref, created.ID,
				control.GroupBindingConfirmation{Version: proven.Version, Key: "private-final-confirm"})
			friendFailure(t, err, http.StatusConflict, "group_binding_source_expired")
			if friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_ownership`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND state='proven'`, created.ID) != 1 {
				t.Fatal("confirmation used a stale private identity seal after its source disappeared")
			}
		})
	}
}

func TestOnboardingControlledClaimantPostgresRequiresIndependentPrivateIngress(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, role := range []string{"owner", "admin"} {
			t.Run(platform+"/"+role, func(t *testing.T) {
				f := newOnboardingClaimantFixture(t, platform, role)
				f.openWindow(t)
				f.prepare(t)
				sent := f.send(t, "claimant-private-round-send")
				if sent.FriendOutbound == nil || sent.FriendOutbound.Status != "submitted" {
					t.Fatal("claimant fixture did not finish its friend send", sent)
				}
				f.scan(t, control.OnboardingMessage{Group: f.nativeGroup, Sender: f.nativeClaimant,
					Text: f.challenge.GroupChallenge})
				blocked := f.readChallenge(t, f.challenge)
				f.assertUnproven(t, blocked)
				if blocked.State != "waiting" || blocked.BlockedReason != "group_binding_identity_required" {
					t.Fatal("claimant send or paused old ownership replaced independent private ingress", blocked)
				}
				proven := f.prove(t, f.admin, platform, f.challenge, f.nativeClaimant, f.nativeGroup)
				raw := onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+f.challenge.ID+"/confirm",
					"claimant-private-round-confirm", map[string]int64{"version": proven.Version}, http.StatusOK)
				want := "bound"
				if role == "admin" {
					want = "review"
				}
				if field(t, raw, "state") != want || f.outbound.outboundSends.Load() != 1 {
					t.Fatal("independent ingress changed role review or resent the friend request")
				}
				f.assertUsage(t, 1)
			})
		}
	}
}

func TestOnboardingControlledClaimantPostgresRejectsUnsupportedOwnershipBeforeWindows(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, ownership := range []string{"inactive_binding", "active_controlled"} {
			t.Run(platform+"/"+ownership, func(t *testing.T) {
				f := onboardingClaimantFixtureWithPriorProof(t, platform, "owner", ownership != "inactive_binding")
				if ownership == "inactive_binding" {
					if _, err := f.db.Platform.Exec(context.Background(), `UPDATE group_bindings SET active=false WHERE group_id=$1`, f.group); err != nil {
						t.Fatal(err)
					}
				}
				if ownership == "active_controlled" {
					proven := f.prove(t, f.admin, platform, f.challenge, f.nativeClaimant, f.nativeGroup)
					onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+f.challenge.ID+"/confirm",
						"active-ownership-confirm", map[string]int64{"version": proven.Version}, http.StatusOK)
					f.challenge = f.controlledOnboardingFixture.challenge(t, f.scope.ID, "active-ownership-parent")
				}
				before := f.native.roles.Load()
				onboardingAPI(t, f.admin, http.MethodPost, "/test-tools/onboarding-friend/windows", "",
					f.windowInput(), http.StatusConflict)
				onboardingAPI(t, f.admin, http.MethodPost,
					"/test-tools/onboarding-friend/challenges/"+f.challenge.ID+"/proof-windows", "unsupported-proof-ownership",
					map[string]any{"version": f.challenge.Version, "kind": "private",
						"text": f.challenge.PrivateChallenge, "seconds": 300}, http.StatusConflict)
				if f.native.roles.Load() != before || f.outbound.outboundLookups.Load() != 0 ||
					f.outbound.outboundSends.Load() != 0 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM test_windows`) != 0 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM controlled_onboarding_usage`) != 0 ||
					friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_ownership
						WHERE proof_challenge_id=$1`, f.challenge.ID) != 0 {
					t.Fatal("unsupported ownership created a window, manufactured proof or reached native work")
				}
			})
		}
	}
}
