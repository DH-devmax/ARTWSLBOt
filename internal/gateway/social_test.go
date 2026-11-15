package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func TestChiliSelfInvitationMillisecondTimeCanBeDelivered(t *testing.T) {
	invite := lajiaoliao.SelfInvitation{Group: "group-string", User: "self-string",
		Inviter: "owner-string", Created: 1700000000123}
	at := invite.Time()
	view := SocialSnapshot{SettingSnapshot: SettingSnapshot{Platform: "lajiaoliao", SelfID: invite.User},
		Group: invite.Group, DirectoryKnown: true, InvitesKnown: true,
		Invitations: []nativeauth.InvitationRecord{{ID: invite.ID(), Group: invite.Group,
			Type: "INVITATION", State: "MEMBER_STATE_INVITED", At: &at,
			Applicant: nativeauth.Friend{ID: invite.User}, Inviter: &nativeauth.Friend{ID: invite.Inviter}}}}
	raw, err := json.Marshal(map[string]any{"snapshot": view})
	if err != nil || !strings.Contains(string(raw), `"2023-11-14T22:13:20.123Z"`) {
		t.Fatal("millisecond invitation could not be delivered as JSON", err)
	}
	if _, err := view.ValidateChange(nativeauth.SettingChange{Kind: "group_accept", Group: invite.Group,
		Target: invite.User, Text: invite.ID()}); err != nil {
		t.Fatal("timestamp conversion changed invitation authorization", err)
	}
}

func TestChiliSelfInvitationScopeAndReadOnlyResult(t *testing.T) {
	change := nativeauth.SettingChange{Kind: "group_accept", Group: "group-string",
		Target: "self-string", Text: strings.Repeat("a", 64)}
	record := nativeauth.InvitationRecord{ID: change.Text, Group: change.Group, Type: "INVITATION",
		State: "MEMBER_STATE_INVITED", Applicant: nativeauth.Friend{ID: change.Target},
		Inviter: &nativeauth.Friend{ID: "owner-string"}}
	view := SocialSnapshot{SettingSnapshot: SettingSnapshot{Platform: "lajiaoliao", SelfID: change.Target},
		Group: change.Group, DirectoryKnown: true, InvitesKnown: true, Invitations: []nativeauth.InvitationRecord{record}}
	if _, err := view.ValidateChange(change); err != nil || view.Matches(change) {
		t.Fatal("Chili string-scoped invitation was rejected or implied membership", err)
	}
	view.Invitations = append(view.Invitations, record)
	if _, err := view.ValidateChange(change); err == nil {
		t.Fatal("ambiguous invitation admitted by a group/user-only wire response")
	}
	view.Invitations = view.Invitations[:1]
	bad := change
	bad.Target = "other-user"
	if _, err := view.ValidateChange(bad); err == nil {
		t.Fatal("another account could accept the invitation")
	}
	view.Present, view.MembersKnown, view.MemberIDs = true, true, []string{change.Target}
	if !view.Matches(change) {
		t.Fatal("complete Chili string-ID membership was not observed")
	}
	view.MembersKnown = false
	if view.Matches(change) {
		t.Fatal("partial member directory proved acceptance")
	}
	view.MembersKnown, view.Platform = true, "wsl"
	if view.Matches(change) {
		t.Fatal("Chili fingerprint or string IDs were reused as WSL evidence")
	}
}

func TestSocialCurrentEvidenceProtectsTargetsAndOutcomes(t *testing.T) {
	view := SocialSnapshot{SettingSnapshot: SettingSnapshot{SelfID: "7", SelfRole: "admin"},
		Group: "123", Present: true, DirectoryKnown: true, MembersKnown: true, FriendsKnown: true,
		MemberIDs: []string{"7", "9"}, Friends: []nativeauth.Friend{{ID: "8"}, {ID: "9"}}}
	invite := nativeauth.SettingChange{Kind: "group_invite", Group: "123", Target: "8"}
	if _, err := view.ValidateChange(invite); err != nil || view.Matches(invite) {
		t.Fatal("fresh friend invitation or premature membership", err)
	}
	for _, target := range []string{"7", "9", "10"} {
		bad := invite
		bad.Target = target
		if _, err := view.ValidateChange(bad); err == nil {
			t.Fatal("unverified or present invitation target", target)
		}
	}
	view.MemberIDs = append(view.MemberIDs, "8")
	if !view.Matches(invite) {
		t.Fatal("did not observe actual membership")
	}
	leave := nativeauth.SettingChange{Kind: "group_leave", Group: "123"}
	view.SelfRole = "owner"
	if _, err := view.ValidateChange(leave); err == nil {
		t.Fatal("owner leave could disband group")
	}
	view.SelfRole = "member"
	if _, err := view.ValidateChange(leave); err != nil {
		t.Fatal(err)
	}
	view.Present = false
	if _, err := view.ValidateChange(invite); err == nil || !view.Matches(leave) {
		t.Fatal("absent account could invite or leave result was not observed")
	}
	view.DirectoryKnown = false
	if view.Matches(leave) || (SocialSnapshot{}).Matches(nativeauth.SettingChange{Kind: "group_leave"}) {
		t.Fatal("unknown or empty directory was treated as evidence of departure")
	}
	view.DirectoryKnown = true
	view.MembersKnown = false
	view.Present = true
	if view.Matches(invite) {
		t.Fatal("partial roster proved invitation outcome")
	}
	view.Group = ""
	view.Applications = []nativeauth.FriendApplication{{ID: "11", User: nativeauth.Friend{ID: "8"},
		Direction: "FROM_HIM", State: "FRIENDLOG_STATE_PROCESSING"}}
	approve := nativeauth.SettingChange{Kind: "friend_approve", Target: "8", Text: "11"}
	if _, err := view.ValidateChange(approve); err != nil {
		t.Fatal(err)
	}
	view.Applications[0].Direction = "FROM_ME"
	if _, err := view.ValidateChange(approve); err == nil {
		t.Fatal("approved outgoing request")
	}
	view.Applications[0].Direction, view.Applications[0].State = "FROM_HIM", "FRIENDLOG_STATE_AGREE"
	if _, err := view.ValidateChange(approve); err == nil || !view.Matches(approve) {
		t.Fatal("processed request resubmitted or reconciliation missed agreement")
	}
}

func TestSelfInvitationRequiresPendingOwnedGroupEvidence(t *testing.T) {
	change := nativeauth.SettingChange{Kind: "group_accept", Group: "123", Target: "7", Text: "9007199254740993"}
	record := nativeauth.InvitationRecord{ID: change.Text, Group: "123", Type: "INVITATION",
		State: "MEMBER_STATE_INVITED", Applicant: nativeauth.Friend{ID: "7"}}
	view := SocialSnapshot{SettingSnapshot: SettingSnapshot{SelfID: "7"}, Group: "123",
		DirectoryKnown: true, InvitesKnown: true, Invitations: []nativeauth.InvitationRecord{record}}
	if _, err := view.ValidateChange(change); err != nil || view.Matches(change) {
		t.Fatal("pending invitation rejected or acknowledged as membership", err)
	}
	for _, mutate := range []func(*SocialSnapshot){
		func(s *SocialSnapshot) { s.DirectoryKnown = false },
		func(s *SocialSnapshot) { s.InvitesKnown = false },
		func(s *SocialSnapshot) { s.Present = true },
		func(s *SocialSnapshot) { s.Group = "999" },
		func(s *SocialSnapshot) { s.SelfID = "8" },
		func(s *SocialSnapshot) { s.Invitations[0].Group = "999" },
		func(s *SocialSnapshot) { s.Invitations[0].Applicant.ID = "8" },
		func(s *SocialSnapshot) { s.Invitations[0].Type = "APPLY" },
		func(s *SocialSnapshot) { s.Invitations[0].State = "MEMBER_STATE_GOOD" },
		func(s *SocialSnapshot) { s.Invitations[0].ID = "1" },
	} {
		bad := view
		bad.Invitations = cloneInvitations(view.Invitations)
		mutate(&bad)
		if _, err := bad.ValidateChange(change); err == nil {
			t.Fatal("changed invitation evidence admitted self acceptance", bad)
		}
	}
	view.Present, view.MembersKnown, view.MemberIDs = true, true, []string{"7"}
	if !view.Matches(change) {
		t.Fatal("complete owned membership was not observed")
	}
	view.MembersKnown = false
	if view.Matches(change) {
		t.Fatal("partial membership proved acceptance")
	}
}
