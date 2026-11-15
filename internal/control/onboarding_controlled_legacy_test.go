package control

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestOnboardingControlledLegacyClaimantReadOnly(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			vault, proof, _, _ := proofWindowFixture(t, platform, "private")
			c := bindingRecord{ID: proof.Challenge, Applicant: proof.Actor, Tenant: proof.Tenant,
				Platform: platform, Account: proof.Receiver, Epoch: proof.ReceiverEpoch,
				AccountVersion: proof.ReceiverVersion, ApplicantVersion: 3, Version: 4,
				ControlledScope: proof.Scope, FriendFlow: "incoming_contact", Mode: "operator_demo"}
			expires := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
			auth := &onboardingControlledAuthorization{Challenge: c.ID, Actor: c.Applicant, Tenant: c.Tenant,
				Window: NewID(), Group: proof.Group, NativeGroup: proof.NativeGroup,
				Platform: platform, Account: c.Account, AddAccount: proof.Recipient, Target: proof.Recipient,
				Epoch: c.Epoch, AccountVersion: c.AccountVersion, Version: c.Version,
				BindingEpoch: proof.BindingEpoch, ExpiresAt: expires,
				Claimant: &onboardingControlledClaimant{Scope: c.ControlledScope, Account: proof.Account,
					Recipient: proof.Recipient, ScopeVersion: 2, Epoch: proof.Epoch, Version: proof.AccountVersion,
					BindingVersion: proof.BindingVersion, ExpiresAt: expires}}
			record := bindingOutboundRecord{ID: NewID(), Challenge: c.ID, Actor: c.Applicant, Tenant: c.Tenant,
				Platform: platform, Account: proof.Account, Epoch: proof.Epoch, AccountVersion: proof.AccountVersion,
				ApplicantVersion: c.ApplicantVersion, Version: c.Version, Window: auth.Window, ExpiresAt: expires}
			source := onboardingOutboundSource(c, auth)
			payload := bindingOutboundPayload{Challenge: c.ID, Actor: c.Applicant, Tenant: c.Tenant,
				Version: c.Version, ApplicantVersion: c.ApplicantVersion, Controlled: auth,
				Request: GroupBindingOutboundRequest{GroupBindingNativeScope: source.scope("", ""),
					OperationID: record.ID, ExpiresAt: expires,
					Target: GroupBindingFriendTarget{AddAccount: auth.AddAccount, NativeID: auth.Target, Name: "Synthetic robot"}}}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var old map[string]json.RawMessage
			var authorization map[string]json.RawMessage
			var claimant map[string]json.RawMessage
			if json.Unmarshal(raw, &old) != nil || json.Unmarshal(old["Controlled"], &authorization) != nil ||
				json.Unmarshal(authorization["Claimant"], &claimant) != nil {
				t.Fatal("invalid synthetic legacy fixture")
			}
			delete(claimant, "Binding")
			authorization["Claimant"], _ = json.Marshal(claimant)
			old["Controlled"], _ = json.Marshal(authorization)
			raw, err = json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			record.Payload, err = vault.Seal("group-binding-outbound:"+record.ID, raw)
			clear(raw)
			if err != nil {
				t.Fatal(err)
			}
			original := bytes.Clone(record.Payload)
			service := &GroupBindingService{Vault: vault}
			for _, state := range []string{"prepared", "submitted", "unknown", "friends", "failed"} {
				record.Status = state
				retained, err := service.openBindingOutbound(record, c)
				if err != nil || retained.Controlled == nil || retained.Controlled.Claimant == nil {
					t.Fatal("legacy record became unreadable", state, err)
				}
				if retained.Controlled.matches(c) || retained.Controlled.Claimant.Binding != (onboardingControlledBinding{}) ||
					!bytes.Equal(record.Payload, original) || retained.Request != payload.Request {
					t.Fatal("legacy recovery changed its sealed scope or became executable", state)
				}
				capabilityError(t, service.requireOnboardingClaimant(context.Background(), nil, c, retained.Controlled),
					"onboarding_controlled_target_changed")
			}
			for _, change := range []func(*bindingRecord){
				func(c *bindingRecord) { c.Applicant = NewID() },
				func(c *bindingRecord) { c.Tenant = NewID() },
				func(c *bindingRecord) { c.ControlledScope = NewID() },
				func(c *bindingRecord) { c.Platform = "other" },
				func(c *bindingRecord) { c.Epoch++ },
			} {
				bad := c
				change(&bad)
				_, err := service.openBindingOutbound(record, bad)
				capabilityError(t, err, "group_binding_outbound_scope_changed")
			}
		})
	}
}
