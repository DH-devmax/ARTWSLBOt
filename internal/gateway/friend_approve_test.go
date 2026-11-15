package gateway

import (
	"testing"

	"wangshangliao/internal/nativeauth"
)

func TestFriendApprovalExactApplicationAndChiliAmbiguity(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			change := nativeauth.SettingChange{Kind: "friend_approve", Group: "123", Target: "8", Text: "11"}
			view := SocialSnapshot{SettingSnapshot: SettingSnapshot{Platform: platform, SelfID: "7"},
				Group: "123", Applications: []nativeauth.FriendApplication{
					{ID: "11", User: nativeauth.Friend{ID: "8"}, Direction: "FROM_HIM", State: "FRIENDLOG_STATE_PROCESSING"},
				}}
			if _, err := view.ValidateChange(change); err != nil {
				t.Fatal("group-bound approval rejected", err)
			}
			bad := change
			bad.Text = "12"
			if _, err := view.ValidateChange(bad); err == nil {
				t.Fatal("application ID ignored")
			}
			if platform == "lajiaoliao" {
				duplicate := view.Applications[0]
				duplicate.ID = "12"
				view.Applications = append(view.Applications, duplicate)
				if _, err := view.ValidateChange(change); err == nil {
					t.Fatal("ambiguous applicant-only native route admitted")
				}
				view.Applications = view.Applications[:1]
			}
			view.Applications[0].State = "FRIENDLOG_STATE_AGREE"
			if _, err := view.ValidateChange(change); err == nil || !view.Matches(change) {
				t.Fatal("processed application replayed or its outcome lost", err)
			}
		})
	}
}
