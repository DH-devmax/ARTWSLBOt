package gateway

import (
	"testing"

	"wangshangliao/internal/nativeauth"
)

func TestFriendDeleteRequiresExactCurrentFriendAndCompleteReadback(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			self, group, target := "7", "123", "8"
			if platform == "lajiaoliao" {
				self, group, target = "self-string", "group-string", "friend-string"
			}
			change := nativeauth.SettingChange{Kind: "friend_delete", Group: group, Target: target}
			view := SocialSnapshot{SettingSnapshot: SettingSnapshot{
				Platform: platform, SelfID: self, SelfRole: "admin"},
				Group: group, Present: true, DirectoryKnown: true, MembersKnown: true,
				FriendsKnown: true, MemberIDs: []string{self, target},
				Friends: []nativeauth.Friend{{ID: target, Name: "owned contact"}}}
			if _, err := view.ValidateChange(change); err != nil || view.Matches(change) {
				t.Fatal("existing group member friend could not be selected, or was already absent", err)
			}
			for _, mutate := range []func(*SocialSnapshot){
				func(s *SocialSnapshot) { s.SelfID = target },
				func(s *SocialSnapshot) { s.FriendsKnown = false },
				func(s *SocialSnapshot) { s.DirectoryKnown = false },
				func(s *SocialSnapshot) { s.MembersKnown = false },
				func(s *SocialSnapshot) { s.Present = false },
				func(s *SocialSnapshot) { s.SelfRole = "member" },
				func(s *SocialSnapshot) { s.Friends = nil },
				func(s *SocialSnapshot) { s.Group = "999" },
			} {
				bad := view
				mutate(&bad)
				if _, err := bad.ValidateChange(change); err == nil {
					t.Fatal("unverified friend deletion admitted", bad)
				}
			}
			view.Friends = []nativeauth.Friend{}
			if !view.Matches(change) {
				t.Fatal("complete fresh absence was not reconciled")
			}
			view.FriendsKnown = false
			if view.Matches(change) {
				t.Fatal("partial friend directory proved deletion")
			}
		})
	}
}
