package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"wangshangliao/internal/control"
)

func TestOnboardingControlledClaimantPostgresLegacyBindingBeforeOwnership(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := onboardingClaimantFixtureWithPriorProof(t, platform, "owner", false)
			ctx := context.Background()
			if _, err := f.db.Platform.Exec(ctx, `UPDATE group_bindings
				SET tenant_id=$2,primary_account_id=$3 WHERE group_id=$1`, f.group, f.tenantA, f.claimant); err != nil {
				t.Fatal(err)
			}
			f.openWindow(t)
			f.prepare(t)
			sent := f.send(t, "prebinding-outbound-confirm")
			if sent.FriendOutbound == nil || sent.FriendOutbound.Status != "submitted" {
				t.Fatal("unowned legacy fixture could not submit its single contact sample", sent)
			}
			f.assertUsage(t, 1)
			f.assertUnproven(t, sent)
			if friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings
				WHERE group_id=$1 AND tenant_id=$2 AND primary_account_id=$3`, f.group, f.tenantA, f.claimant) != 1 {
				t.Fatal("contact sampling moved the legacy binding before proof")
			}
			onboardingAPI(t, f.admin, http.MethodPost,
				"/test-tools/onboarding-friend/challenges/"+f.challenge.ID+"/proof-windows", "prebinding-private-window",
				map[string]any{"version": f.challenge.Version, "kind": "private",
					"text": f.challenge.PrivateChallenge, "seconds": 300}, http.StatusCreated)
			proven := f.prove(t, f.admin, platform, f.challenge, f.nativeClaimant, f.nativeGroup)
			raw := onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+f.challenge.ID+"/confirm",
				"prebinding-confirm", map[string]int64{"version": proven.Version}, http.StatusOK)
			var review control.GroupBindingChallenge
			if err := json.Unmarshal(raw, &review); err != nil || review.State != "review" || !review.TransferRequired || review.Transfer == nil {
				t.Fatal("legacy binding skipped authenticated owner transfer review", review, err)
			}
			raw = onboardingAPI(t, f.admin, http.MethodPost, "/group-binding/challenges/"+f.challenge.ID+"/transfer-review",
				"prebinding-transfer-review", map[string]any{"version": review.Version, "approve": true,
					"source_tenant_id": review.Transfer.SourceTenantID, "binding_version": review.Transfer.BindingVersion}, http.StatusOK)
			if field(t, raw, "state") != "bound" ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_bindings
					WHERE group_id=$1 AND tenant_id=$2 AND primary_account_id=$3 AND NOT management_enabled AND NOT ai_enabled`,
					f.group, f.scope.TenantID, f.outbound.recipient) != 1 {
				t.Fatal("independent proofs and explicit review did not produce a disabled demo binding")
			}
		})
	}
}

func TestOnboardingControlledClaimantPostgresLegacyBindingChangeBeforeLookup(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := onboardingClaimantFixtureWithPriorProof(t, platform, "owner", false)
			f.openWindow(t)
			if _, err := f.db.Platform.Exec(context.Background(),
				`UPDATE group_bindings SET tenant_id=$2 WHERE group_id=$1`, f.group, f.tenantA); err != nil {
				t.Fatal(err)
			}
			before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
			_, err := f.app.GroupBinding.PrepareControlledOutboundFriend(context.Background(), f.ref, f.challenge.ID,
				control.OnboardingControlledTargetInput{Window: f.window.ID, Version: f.challenge.Version,
					Account: f.outbound.target.AddAccount, Key: "prebinding-changed-tenant"})
			if err == nil || outboundCalls(f.outbound.bindingOutboundNativeFixture) != before ||
				friendCount(t, f.onboardingFixture, `SELECT count(*) FROM group_binding_outbound_attempts`) != 0 {
				t.Fatal("a changed legacy binding retained its old lookup grant", err)
			}
		})
	}
}
