package control

import "testing"

func TestOnboardingPeerOnlyDerivedForAuthenticatedWSLClaimant(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		_, p, _, _ := proofWindowFixture(t, platform, "private")
		c := bindingRecord{ID: p.Challenge, Applicant: p.Actor, Tenant: p.Tenant,
			Platform: platform, Account: p.Receiver, Epoch: p.ReceiverEpoch,
			AccountVersion: p.ReceiverVersion, Version: p.ChallengeVersion,
			ControlledScope: p.Scope, FriendFlow: "incoming_contact", Mode: "operator_demo"}
		a := &onboardingControlledAuthorization{Challenge: c.ID, Actor: c.Applicant, Tenant: c.Tenant,
			Window: NewID(), Group: p.Group, NativeGroup: p.NativeGroup, Platform: platform,
			Account: c.Account, AddAccount: p.Recipient, Target: p.Recipient,
			Epoch: c.Epoch, AccountVersion: c.AccountVersion, Version: c.Version,
			BindingEpoch: p.BindingEpoch, Claimant: &onboardingControlledClaimant{
				Scope: c.ControlledScope, Account: p.Account, Recipient: p.Recipient,
				ScopeVersion: p.ScopeVersion, Epoch: p.Epoch, Version: p.AccountVersion,
				Binding: p.Binding, BindingVersion: p.BindingVersion}}
		if !a.matches(c) {
			t.Fatal("invalid synthetic authorization")
		}
		peer := onboardingOutboundPeer(c, a)
		if platform == "lajiaoliao" {
			if peer != nil {
				t.Fatal("WSL mapping leaked into Chili transport")
			}
			continue
		}
		if peer == nil || peer.GroupBindingNativeScope != c.scope("", "") || peer.NativeID != p.Recipient ||
			!validOnboardingOutboundPeer(c, a, peer) {
			t.Fatal("peer was not derived from the receiving robot's sealed scope")
		}
		for _, change := range []func(*GroupBindingOutboundPeer){
			func(peer *GroupBindingOutboundPeer) { peer.AccountID = p.Account },
			func(peer *GroupBindingOutboundPeer) { peer.AccountEpoch++ },
			func(peer *GroupBindingOutboundPeer) { peer.AccountVersion++ },
			func(peer *GroupBindingOutboundPeer) { peer.NativeID = p.Sender },
			func(peer *GroupBindingOutboundPeer) { peer.NativeGroupID = p.NativeGroup },
			func(peer *GroupBindingOutboundPeer) { peer.NativeSenderID = p.Sender },
		} {
			bad := *peer
			change(&bad)
			if validOnboardingOutboundPeer(c, a, &bad) {
				t.Fatal("caller-provided peer escaped the persisted authorization")
			}
		}
		for _, change := range []func(){
			func() { c.Applicant = NewID() },
			func() { a.Claimant = nil },
		} {
			oldC, oldA := c, *a
			change()
			if onboardingOutboundPeer(c, a) != nil || validOnboardingOutboundPeer(c, a, peer) {
				t.Fatal("ordinary or changed authorization obtained a controlled peer")
			}
			c, *a = oldC, oldA
		}
		if !validOnboardingOutboundPeer(c, a, nil) || onboardingOutboundPeer(c, nil) != nil {
			t.Fatal("retained or ordinary transport changed")
		}
	}
}
