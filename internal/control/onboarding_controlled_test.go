package control

import (
	"context"
	"testing"
	"time"
)

func TestOnboardingControlledContextCannotBeInheritedOrForged(t *testing.T) {
	c := bindingRecord{ID: NewID(), Applicant: NewID(), Tenant: NewID(), Platform: "wsl", Account: NewID(),
		Epoch: 3, AccountVersion: 4, Version: 2, Mode: "operator_demo", FriendFlow: "incoming_contact",
		State: "waiting", ExpiresAt: time.Now().Add(time.Minute)}
	a := &onboardingControlledAuthorization{Challenge: c.ID, Actor: c.Applicant, Tenant: c.Tenant,
		Platform: c.Platform, Account: c.Account, Epoch: c.Epoch, AccountVersion: c.AccountVersion,
		Version: c.Version, Window: NewID(), Group: NewID(), NativeGroup: "synthetic-group",
		BindingEpoch: 1, AddAccount: "synthetic-account", Target: "synthetic-target", ExpiresAt: c.ExpiresAt}
	if err := outboundPendingContext(context.Background(), c, c.Version); err == nil {
		t.Fatal("incoming contact admitted an ordinary direct outgoing call")
	}
	type untrustedKey string
	forged := context.WithValue(context.Background(), untrustedKey("native_debug"), a)
	if err := outboundPendingContext(forged, c, c.Version); err == nil {
		t.Fatal("untrusted context manufactured controlled authority")
	}
	for _, window := range []string{"", "not-a-window-id"} {
		invalid := *a
		invalid.Window = window
		work := context.WithValue(context.Background(), onboardingControlledKey{}, &invalid)
		if err := outboundPendingContext(work, c, c.Version); err == nil {
			t.Fatal("private context accepted an empty or invalid window ID", window)
		}
	}
	trusted := context.WithValue(context.Background(), onboardingControlledKey{}, a)
	if err := outboundPendingContext(trusted, c, c.Version); err != nil {
		t.Fatal("private verified context did not retain the outgoing implementation", err)
	}
	for _, field := range []string{"actor", "tenant", "platform", "account", "epoch", "account_version", "purpose", "flow"} {
		other := c
		switch field {
		case "actor":
			other.Applicant = NewID()
		case "tenant":
			other.Tenant = NewID()
		case "platform":
			other.Platform = "lajiaoliao"
		case "account":
			other.Account = NewID()
		case "epoch":
			other.Epoch++
		case "account_version":
			other.AccountVersion++
		case "purpose":
			other.Mode = "tenant_business"
		case "flow":
			other.FriendFlow = "legacy"
		}
		if err := outboundPendingContext(trusted, other, other.Version); err == nil {
			t.Fatal("controlled authority crossed immutable scope", field)
		}
	}
	c.FriendFlow = "outbound"
	if err := outboundPendingContext(context.Background(), c, c.Version); err == nil {
		t.Fatal("historical outbound row retained an ordinary execution path")
	}
	if _, err := (&GroupBindingService{}).authorizeOutboundLookup(context.Background(), SessionRef{}, c); err == nil {
		t.Fatal("historical outbound lookup bypassed the controlled context")
	}
}

func TestOnboardingControlledPublicExecutionRequiresPrivateContextBeforeDatabase(t *testing.T) {
	s := &GroupBindingService{}
	id := NewID()
	_, err := s.PrepareOutboundFriend(context.Background(), SessionRef{}, id,
		GroupBindingFriendTargetInput{Version: 1, Account: "synthetic-account", Key: "synthetic-prepare"})
	if err == nil {
		t.Fatal("public preparation accepted an ordinary context")
	}
	_, err = s.SendOutboundFriend(context.Background(), SessionRef{}, id,
		GroupBindingConfirmation{Version: 1, Key: "synthetic-confirm"})
	if err == nil {
		t.Fatal("public submission accepted an ordinary context")
	}
}
