package gateway

import (
	"context"
	"testing"
)

func TestOnboardingPeerIsolationAndRetainedTransport(t *testing.T) {
	req, base, client := onboardingOutboundFixture("wsl")
	peer := OnboardingOutboundPeer{OnboardingNativeScope: OnboardingNativeScope{
		Platform: "wsl", AccountID: "synthetic-peer", AccountEpoch: 3, AccountVersion: 7},
		NativeID: req.Target.NativeID}
	req.ControlledPeer = &peer
	if !req.valid() || !sameOnboardingPeer(&peer, req.ControlledPeer) || !sameOnboardingPeer(nil, nil) {
		t.Fatal("valid pinned or retained transport rejected")
	}
	for _, change := range []func(*OnboardingOutboundPeer){
		func(p *OnboardingOutboundPeer) { p.Platform = "lajiaoliao" },
		func(p *OnboardingOutboundPeer) { p.AccountID = req.AccountID },
		func(p *OnboardingOutboundPeer) { p.AccountEpoch = 0 },
		func(p *OnboardingOutboundPeer) { p.AccountVersion = 0 },
		func(p *OnboardingOutboundPeer) { p.NativeID = "08" },
		func(p *OnboardingOutboundPeer) { p.NativeID = req.SelfID },
	} {
		bad := peer
		change(&bad)
		candidate := req
		candidate.ControlledPeer = &bad
		if candidate.valid() {
			t.Fatal("substituted peer was accepted")
		}
	}
	for _, supplied := range []*OnboardingOutboundPeer{nil, {
		OnboardingNativeScope: peer.OnboardingNativeScope, NativeID: req.SelfID,
	}} {
		_, err := readOnboardingOutbound(context.Background(), OnboardingOutboundLookup{
			OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
			ControlledPeer: supplied,
		}, &req, client, base, outboundFixtureCheck)
		if err == nil || client.reconciles != 0 || client.sends != 0 {
			t.Fatal("retained operation switched peer transport or performed a submission", err)
		}
	}
	req.ControlledPeer = nil
	if !req.valid() {
		t.Fatal("legacy nil peer transport changed")
	}
}
