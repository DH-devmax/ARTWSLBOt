package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"wangshangliao/internal/control"
)

func customerBindingNativeCalls(n *bindingFriendNativeFixture) [7]int32 {
	return [7]int32{n.roles.Load(), n.invites.Load(), n.sends.Load(), n.lookups.Load(),
		n.approvals.Load(), n.submits.Load(), n.reconciles.Load()}
}

func customerBindingProgress(t *testing.T, client testClient, n *bindingFriendNativeFixture, id string) control.GroupBindingChallenge {
	t.Helper()
	before := customerBindingNativeCalls(n)
	path := "/group-binding/challenges/" + id
	var current control.GroupBindingChallenge
	raw := onboardingAPI(t, client, "GET", path, "", nil, 200)
	if err := json.Unmarshal(raw, &current); err != nil || current.ID != id {
		t.Fatal("customer progress lost its challenge", err)
	}
	var friend control.GroupBindingFriendState
	raw = onboardingAPI(t, client, "GET", path+"/friend-verify", "", nil, 200)
	if err := json.Unmarshal(raw, &friend); err != nil || friend.ChallengeID != id ||
		friend.Version != current.Version || friend.IdentityProven != current.IdentityProven || friend.CanVerify {
		t.Fatal("customer progress endpoints disagree or expose retired consent", err)
	}
	if after := customerBindingNativeCalls(n); after != before {
		t.Fatalf("GET progress called a native port: before=%v after=%v", before, after)
	}
	return current
}

func customerContactProgress(t *testing.T, client testClient, n *bindingFriendNativeFixture, id string) control.GroupBindingChallenge {
	t.Helper()
	return customerBindingProgress(t, client, n, id)
}

// These flows use ordinary customer sessions and production HTTP routes. Only
// authenticated ingress and the native port are synthetic; integrationDB confines
// fixture mutations to wsl_test and its schema resets must remain serial.
func TestGroupBindingCustomerFlowPostgres(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, contact := range []string{"incoming_request", "already_friends", "authenticated_private_proof"} {
			t.Run(platform+"/"+contact, func(t *testing.T) {
				f, n, client, _ := friendFixture(t, platform)
				ctx := context.Background()
				preparedContact := contact == "incoming_request"
				if preparedContact {
					friendOpenGate(t, f, platform)
				}
				onboardingAPI(t, client, "POST", "/group-binding/challenges", "customer-cannot-select-tenant",
					map[string]any{"platform": platform, "target_tenant_id": f.tenantB}, 403)
				created := f.createChallenge(t, client, platform, "customer-flow-create")
				if created.Mode != "tenant_business" || created.Customer.TenantID != f.tenantA ||
					created.Scope.TenantID != f.tenantA || created.Scope.ActorID != f.aliceID ||
					created.Scope.Platform != platform || created.Platform != platform ||
					created.Robot.ID != f.accounts[platform] || created.State != "waiting" ||
					created.IdentityProven || created.CanConfirm || created.Group != nil ||
					created.GroupChallenge == "" || created.PrivateChallenge == "" ||
					created.FriendFlow != "incoming_contact" || created.FriendChallenge != "" || created.CanVerifyFriend {
					t.Fatal("customer did not create its own unproven incoming challenge", created)
				}
				path := "/group-binding/challenges/" + created.ID
				customerContactProgress(t, client, n, created.ID)
				onboardingAPI(t, client, "POST", path+"/confirm", "customer-unproven-confirm",
					map[string]any{"version": created.Version}, 409)
				onboardingAPI(t, client, "POST", path+"/friend-verify", "customer-retired-friend",
					map[string]any{"version": created.Version}, 410)
				if preparedContact {
					raw := onboardingAPI(t, client, "POST", path+"/friend-candidate", "customer-target-prepare",
						map[string]any{"version": created.Version, "nickname": "Synthetic customer"}, 200)
					var prepared control.GroupBindingChallenge
					if err := json.Unmarshal(raw, &prepared); err != nil || prepared.FriendContact == nil ||
						prepared.FriendContact.Name != "Synthetic customer" || prepared.IdentityProven ||
						prepared.Version != created.Version || n.submits.Load() != 0 {
						t.Fatal("customer target preparation changed scope or performed a native mutation", prepared, err)
					}
					if contact == "incoming_request" {
						raw = onboardingAPI(t, client, "POST", path+"/friend-approve", "customer-friend-confirm",
							map[string]any{"version": created.Version, "operation_id": prepared.FriendContact.ID}, 200)
						var submitted control.GroupBindingChallenge
						if err := json.Unmarshal(raw, &submitted); err != nil || submitted.FriendContact == nil ||
							submitted.FriendContact.ID != prepared.FriendContact.ID ||
							submitted.FriendContact.Status != "succeeded" || submitted.IdentityProven ||
							submitted.CanConfirm || n.submits.Load() != 1 {
							t.Fatal("customer request bypassed independent private proof", submitted, err)
						}
						before := customerBindingNativeCalls(n)
						onboardingAPI(t, client, "POST", path+"/friend-approve", "customer-friend-confirm",
							map[string]any{"version": created.Version, "operation_id": prepared.FriendContact.ID}, 200)
						if customerBindingNativeCalls(n) != before {
							t.Fatal("customer retry resent the native friend request")
						}
					}
					f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: "synthetic-other-human",
						Text: created.PrivateChallenge}, nil)
					if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
						t.Fatal(err)
					}
					if current := customerContactProgress(t, client, n, created.ID); current.IdentityProven || current.Version != created.Version {
						t.Fatal("unpinned native sender obtained customer identity", current)
					}
				}
				f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: n.owner,
					Text: created.PrivateChallenge}, nil)
				pending := customerContactProgress(t, client, n, created.ID)
				if pending.IdentityProven || pending.Version != created.Version {
					t.Fatal("GET consumed a pending private proof")
				}
				if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
					t.Fatal(err)
				}
				identity := customerContactProgress(t, client, n, created.ID)
				if !identity.IdentityProven || identity.State != "waiting" || identity.Group != nil ||
					identity.CanConfirm || identity.Version != created.Version+1 || n.roles.Load() != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_ownership`) != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE id=$1
						AND identity_sealed IS NOT NULL AND identity_event_id IS NOT NULL
						AND identity_friend_proof_id IS NULL AND proof_event_id IS NULL`, created.ID) != 1 {
					t.Fatal("private proof granted ownership or lost authenticated ingress", identity)
				}
				var identityEvent int64
				if err := f.db.Platform.QueryRow(ctx, `SELECT identity_event_id FROM group_binding_challenges
					WHERE id=$1`, created.ID).Scan(&identityEvent); err != nil {
					t.Fatal(err)
				}
				nativeGroup := "synthetic-customer-flow-" + platform + "-" + contact
				f.ingress(t, platform, control.OnboardingMessage{Group: nativeGroup, Sender: "synthetic-other-human",
					Text: created.GroupChallenge}, nil)
				if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
					t.Fatal(err)
				}
				if current := customerContactProgress(t, client, n, created.ID); current.State != "waiting" ||
					current.Group != nil || !current.IdentityProven || current.CanConfirm ||
					current.Version != identity.Version || current.BlockedReason != "group_binding_native_role_required" ||
					n.roles.Load() != 1 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE id=$1
						AND identity_event_id=$2 AND identity_sealed IS NOT NULL AND proof_event_id IS NULL
						AND proof_sealed IS NULL AND group_id IS NULL`, created.ID, identityEvent) != 1 ||
					friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_ownership`) != 0 {
					t.Fatal("another sender's group proof inherited the pinned customer identity", current)
				}
				if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
					t.Fatal(err)
				}
				if n.roles.Load() != 1 {
					t.Fatal("polling repeated the rejected event's native role lookup")
				}
				f.ingress(t, platform, control.OnboardingMessage{Group: nativeGroup, Sender: n.owner,
					Text: created.GroupChallenge}, nil)
				pending = customerContactProgress(t, client, n, created.ID)
				if pending.State != "waiting" || pending.Group != nil || pending.Version != identity.Version {
					t.Fatal("GET consumed the group proof")
				}
				if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
					t.Fatal(err)
				}
				proven := customerContactProgress(t, client, n, created.ID)
				if proven.State != "proven" || !proven.IdentityProven || !proven.CanConfirm ||
					proven.ProofRole != "owner" || proven.Group == nil || proven.Group.NativeID != nativeGroup ||
					proven.Version != identity.Version+1 || n.roles.Load() != 2 {
					t.Fatal("authenticated customer owner did not prove the selected native group", proven)
				}
				before := customerBindingNativeCalls(n)
				for _, other := range []struct {
					client testClient
					status int
				}{{f.admin, 404}, {f.bob, 404}, {f.member, 403}} {
					other.client.workspace = platform
					onboardingAPI(t, other.client, "POST", path+"/confirm", "customer-proxy-confirm",
						map[string]any{"version": proven.Version}, other.status)
				}
				bob := f.bob
				bob.workspace = platform
				onboardingAPI(t, bob, "GET", path, "", nil, 404)
				onboardingAPI(t, bob, "GET", path+"/friend-verify", "", nil, 404)
				if customerBindingNativeCalls(n) != before || friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 {
					t.Fatal("proxy confirmation or history read committed a binding")
				}
				raw := onboardingAPI(t, client, "POST", path+"/confirm", "customer-owner-confirm",
					map[string]any{"version": proven.Version}, 200)
				var bound control.GroupBindingChallenge
				if err := json.Unmarshal(raw, &bound); err != nil || bound.State != "bound" ||
					bound.Group == nil || bound.Group.ID != proven.Group.ID || bound.Version != proven.Version+1 {
					t.Fatal("customer could not confirm its ordinary production binding", bound, err)
				}
				before = customerBindingNativeCalls(n)
				onboardingAPI(t, client, "POST", path+"/confirm", "customer-owner-confirm",
					map[string]any{"version": proven.Version}, 200)
				onboardingAPI(t, client, "POST", path+"/confirm", "customer-owner-another-key",
					map[string]any{"version": proven.Version}, 409)
				if current := customerContactProgress(t, client, n, created.ID); current.State != "bound" ||
					customerBindingNativeCalls(n) != before {
					t.Fatal("bound replay repeated native work or lost its result")
				}
				var anchoredAndOff bool
				err := f.db.Platform.QueryRow(ctx, `SELECT b.active AND b.tenant_id=$2 AND b.primary_account_id=$3
					AND b.version=1 AND b.epoch=1 AND NOT b.management_enabled AND NOT b.ai_enabled
					AND NOT b.ai_requested AND NOT r.enabled AND o.tenant_id=b.tenant_id AND o.platform=$4
					AND o.state='active' AND o.applicant_role='owner' AND o.version=1
					AND o.proof_event_id=c.proof_event_id AND c.state='bound' AND c.id=$5
					AND c.applicant_id=$6 AND c.applicant_mode='tenant_business'
					AND e.account_id=b.primary_account_id AND e.kind='group' AND d.state='decoded'
					AND d.scene='group' AND NOT EXISTS(SELECT 1 FROM operator_demo_group_claims)
					FROM group_bindings b JOIN group_business_runtime r ON r.group_id=b.group_id
					JOIN group_binding_ownership o ON o.group_id=b.group_id
					JOIN group_binding_challenges c ON c.id=o.proof_challenge_id
					JOIN native_received_events e ON e.id=o.proof_event_id
					JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
					WHERE b.group_id=$1`, proven.Group.ID, f.tenantA, f.accounts[platform], platform, created.ID, f.aliceID).
					Scan(&anchoredAndOff)
				if err != nil || !anchoredAndOff {
					t.Fatal("customer binding lost ownership evidence or enabled background business", err)
				}
				wantPrepare, wantEffects := int32(0), int32(0)
				if preparedContact {
					wantPrepare = 1
				}
				if contact == "incoming_request" {
					wantEffects = 1
				}
				if n.lookups.Load() != wantPrepare || n.approvals.Load() != wantEffects ||
					n.reconciles.Load() != 0 || n.submits.Load() != wantEffects || n.invites.Load() != 0 || n.sends.Load() != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 1 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts`) != int(wantPrepare) ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 {
					t.Fatal("customer flow repeated requests or borrowed the legacy approval path")
				}
			})
		}
	}
}

func TestGroupBindingCustomerFlowPostgresRejectsPoolIdentity(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := friendFixture(t, platform)
			friendOpenGate(t, f, platform)
			c := f.createChallenge(t, client, platform, "customer-pool-identity")
			if _, err := f.db.Platform.Exec(context.Background(), `INSERT INTO accounts(name,platform,workspace,purpose,native_uid)
				VALUES('Synthetic pooled customer owner',$1,$1,'management',$2)`, platform, n.owner); err != nil {
				t.Fatal(err)
			}
			if _, err := f.app.GroupBinding.PrepareContact(context.Background(), ref, c.ID,
				control.GroupBindingContactInput{Version: c.Version, Nickname: "Synthetic customer", Key: "customer-pool-target"}); err == nil {
				t.Fatal("pooled identity became a customer contact target")
			}
			for _, private := range []bool{true, false} {
				message := control.OnboardingMessage{Private: private, Sender: n.owner, Text: c.PrivateChallenge}
				if !private {
					message.Group, message.Text = "synthetic-pool-owner-group", c.GroupChallenge
				}
				f.ingress(t, platform, message, nil)
				if err := f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				current := customerContactProgress(t, client, n, c.ID)
				if current.State != "waiting" || current.Version != c.Version || current.IdentityProven ||
					current.Group != nil || current.CanConfirm || current.BlockedReason != "group_binding_pool_identity" ||
					n.roles.Load() != 0 {
					t.Fatal("pool identity became customer identity or ownership", current)
				}
			}
			if n.lookups.Load() != 1 || n.submits.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_ownership`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 {
				t.Fatal("rejected pool proofs created authorization or native side effects")
			}
		})
	}
}
