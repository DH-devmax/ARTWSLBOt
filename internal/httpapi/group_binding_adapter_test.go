package httpapi

import (
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

func TestBindingAbsenceRequiresKnownCurrentGroup(t *testing.T) {
	scope := control.GroupBindingNativeScope{NativeGroupID: "group"}
	snapshot := gateway.SocialSnapshot{SettingSnapshot: gateway.SettingSnapshot{SelfID: "robot"},
		Group: "group", DirectoryKnown: true}
	if !bindingAbsent(snapshot, scope) {
		t.Fatal("fresh complete directory absence rejected")
	}
	for _, edit := range []func(*gateway.SocialSnapshot){
		func(s *gateway.SocialSnapshot) { s.DirectoryKnown = false },
		func(s *gateway.SocialSnapshot) { s.Present = true },
		func(s *gateway.SocialSnapshot) { s.Group = "other" },
		func(s *gateway.SocialSnapshot) { s.SelfID = "" },
	} {
		bad := snapshot
		edit(&bad)
		if bindingAbsent(bad, scope) {
			t.Fatal("unverified absence accepted")
		}
	}
}

func TestBindingInvitationRequiresOneProvenInviterAndRecipient(t *testing.T) {
	scope := control.GroupBindingNativeScope{Platform: "lajiaoliao", NativeGroupID: "group-string", NativeSenderID: "customer"}
	row := nativeauth.InvitationRecord{ID: "invitation", Group: scope.NativeGroupID,
		Type: "INVITATION", State: "MEMBER_STATE_INVITED",
		Applicant: nativeauth.Friend{ID: "robot"}, Inviter: &nativeauth.Friend{ID: "customer"}}
	snapshot := gateway.SocialSnapshot{SettingSnapshot: gateway.SettingSnapshot{Platform: scope.Platform, SelfID: "robot"},
		Group: scope.NativeGroupID, DirectoryKnown: true, InvitesKnown: true, Invitations: []nativeauth.InvitationRecord{row}}
	deadline := time.Now().Add(time.Minute)
	got, err := bindingInvitation(snapshot, scope, "", deadline)
	if err != nil || got.RecipientID != "robot" || got.InviterID != "customer" || !got.ExpiresAt.Equal(deadline) {
		t.Fatalf("valid evidence rejected: %+v %v", got, err)
	}
	cases := []struct {
		name string
		edit func(*gateway.SocialSnapshot)
	}{
		{"already_joined", func(s *gateway.SocialSnapshot) { s.Present = true }},
		{"unknown_directory", func(s *gateway.SocialSnapshot) { s.DirectoryKnown = false }},
		{"unknown_invites", func(s *gateway.SocialSnapshot) { s.InvitesKnown = false }},
		{"wrong_group", func(s *gateway.SocialSnapshot) { s.Group = "other" }},
		{"wrong_inviter", func(s *gateway.SocialSnapshot) { s.Invitations[0].Inviter = &nativeauth.Friend{ID: "other"} }},
		{"missing_inviter", func(s *gateway.SocialSnapshot) { s.Invitations[0].Inviter = nil }},
		{"wrong_recipient", func(s *gateway.SocialSnapshot) { s.Invitations[0].Applicant.ID = "other" }},
		{"completed", func(s *gateway.SocialSnapshot) { s.Invitations[0].State = "MEMBER_STATE_NORMAL" }},
		{"ambiguous", func(s *gateway.SocialSnapshot) { s.Invitations = append(s.Invitations, row) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := snapshot
			copy.Invitations = append([]nativeauth.InvitationRecord(nil), snapshot.Invitations...)
			tc.edit(&copy)
			if _, err := bindingInvitation(copy, scope, "", deadline); err == nil {
				t.Fatal("unproven invitation accepted")
			}
		})
	}
	if _, err := bindingInvitation(snapshot, scope, "another-fingerprint", deadline); err == nil {
		t.Fatal("substituted invitation accepted")
	}
}
