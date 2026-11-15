package gateway

import (
	"testing"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func TestModerationTargetsAndReadbackAreConservative(t *testing.T) {
	view := ModerationSnapshot{SettingSnapshot: SettingSnapshot{SelfID: "7", SelfRole: "admin"},
		Group: "123", Present: true, Policy: nativeauth.GroupPolicy{Group: "123"},
		Members: []nativeauth.GroupMember{{ID: "7", Role: "admin"}, {ID: "8", Role: "member"},
			{ID: "9", Role: "owner"}, {ID: "10", Role: "admin"}}}
	change := nativeauth.SettingChange{Kind: "mute_member", Group: "123", Target: "8", Minutes: 2}
	if err := view.ValidateChange(change); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"7", "9", "10", "11"} {
		bad := change
		bad.Target = target
		if view.ValidateChange(bad) == nil {
			t.Fatal("protected or unknown member admitted", target)
		}
	}
	view.MutesKnown, view.MutedIDs = true, []string{"8"}
	if view.Matches(change) {
		t.Fatal("mute boolean falsely verified requested duration")
	}
	unmute := nativeauth.SettingChange{Kind: "unmute_member", Group: "123", Target: "8"}
	if view.Matches(unmute) {
		t.Fatal("muted member counted as unmuted")
	}
	view.MutedIDs = nil
	if !view.Matches(unmute) {
		t.Fatal("known complete mute list failed reconciliation")
	}
	view.MutesKnown = false
	if view.Matches(unmute) {
		t.Fatal("unknown mute list silently became empty")
	}
	view.SelfRole = "owner"
	change.Target = "10"
	if view.ValidateChange(change) != nil {
		t.Fatal("owner cannot moderate administrator")
	}
}

func TestChiliPermanentBanNeedsIndependentCompleteEvidence(t *testing.T) {
	view := ModerationSnapshot{
		SettingSnapshot: SettingSnapshot{Platform: "lajiaoliao", SelfID: "self-string", SelfRole: "owner"},
		Group:           "group-string", Present: true, BansKnown: true,
		Members: []nativeauth.GroupMember{{ID: "self-string", Role: "owner"},
			{ID: "target-string", Role: "member"}},
	}
	kick := nativeauth.SettingChange{Kind: "kick_member_ban", Group: view.Group, Target: "target-string"}
	if err := view.ValidateChange(kick); err != nil || view.Matches(kick) {
		t.Fatal("initial kick evidence invalid", err)
	}
	view.BansKnown = false
	if view.ValidateChange(kick) == nil {
		t.Fatal("unknown bans admitted a permanent kick")
	}
	view.BansKnown = true
	view.Members = view.Members[:1]
	if view.Matches(kick) {
		t.Fatal("absence from roster falsely proved permanent ban")
	}
	view.GroupBans = []lajiaoliao.GroupBan{{Group: view.Group, User: kick.Target}}
	if !view.Matches(kick) {
		t.Fatal("complete roster and exact ban failed reconciliation")
	}
	remove := kick
	remove.Kind = "remove_group_ban"
	if view.ValidateChange(remove) != nil || view.Matches(remove) {
		t.Fatal("ban removal ignored native ban state")
	}
	view.GroupBans = nil
	if view.ValidateChange(remove) == nil || !view.Matches(remove) {
		t.Fatal("known absence did not reconcile removal")
	}
	for _, edit := range []func(*ModerationSnapshot){
		func(s *ModerationSnapshot) { s.Platform = "wsl" },
		func(s *ModerationSnapshot) { s.Present = false },
		func(s *ModerationSnapshot) { s.SelfRole = "member" },
		func(s *ModerationSnapshot) { s.SelfID = kick.Target },
		func(s *ModerationSnapshot) { s.BansKnown = false },
		func(s *ModerationSnapshot) { s.Group = "foreign-group" },
	} {
		bad := view
		bad.GroupBans = []lajiaoliao.GroupBan{{Group: view.Group, User: kick.Target}}
		edit(&bad)
		if bad.ValidateChange(remove) == nil {
			t.Fatal("ban removal escaped scope, role or known-state check")
		}
	}
}

func TestAdministratorChangeRequiresOwnerAndExactCurrentRole(t *testing.T) {
	view := ModerationSnapshot{SettingSnapshot: SettingSnapshot{SelfID: "7"},
		Group: "123", Present: true,
		Members: []nativeauth.GroupMember{{ID: "7", Role: "owner"}, {ID: "8", Role: "member"},
			{ID: "9", Role: "owner"}, {ID: "10", Role: "admin"}, {ID: "11", Role: "unknown"}}}
	for _, kind := range []string{"grant_admin", "revoke_admin"} {
		for _, selfRole := range []string{"owner", "admin", "member", "unknown", ""} {
			for _, target := range []string{"7", "8", "9", "10", "11", "12", ""} {
				view.SelfRole = selfRole
				change := nativeauth.SettingChange{Kind: kind, Group: "123", Target: target}
				allowed := selfRole == "owner" &&
					(kind == "grant_admin" && target == "8" || kind == "revoke_admin" && target == "10")
				if (view.ValidateChange(change) == nil) != allowed {
					t.Fatal("role mutation crossed ownership/target boundary", kind, selfRole, target)
				}
			}
		}
		change := nativeauth.SettingChange{Kind: kind, Group: "123", Target: "8"}
		for _, role := range []string{"member", "admin", "owner", "unknown", ""} {
			view.Members[1].Role = role
			want := kind == "grant_admin" && role == "admin" || kind == "revoke_admin" && role == "member"
			if view.Matches(change) != want {
				t.Fatal("role readback inferred a different state", kind, role)
			}
		}
		view.Members[1].Role = "member"
		for _, bad := range []nativeauth.SettingChange{
			{Kind: kind, Group: "123", Target: "8", Minutes: 1},
			{Kind: kind, Group: "123", Target: "8", Text: "unrelated"},
			{Kind: kind, Group: "123", Target: "08"},
			{Kind: kind, Group: "123", Target: "8,10"},
		} {
			if bad.Validate() == nil {
				t.Fatal("role action admitted extra fields or batch", bad)
			}
		}
		view.Group = "124"
		if view.Matches(change) {
			t.Fatal("another group reconciled role mutation")
		}
		view.Group = "123"
	}
}
