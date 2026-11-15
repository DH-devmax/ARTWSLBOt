package nativeauth

import (
	"strings"
	"testing"
)

func TestFriendDeleteSettingContract(t *testing.T) {
	good := SettingChange{Kind: "friend_delete", Group: "123", Target: "8"}
	if err := good.Validate(); err != nil || !SocialKind(good.Kind) || GroupMembershipKind(good.Kind) {
		t.Fatal("friend deletion must be a distinct scoped social action", err)
	}
	for _, id := range []string{"1", "1073741823"} {
		change := good
		change.Group, change.Target = id, id
		if err := change.Validate(); err != nil {
			t.Fatal("valid native ID rejected", id, err)
		}
	}
	for _, id := range []string{"", "0", "08", "+8", "-8", "8.0", " 8", "8 ", "1073741824",
		"9007199254740993", strings.Repeat("1", 129), "\u516b", "8\x00", string([]byte{0xff})} {
		for _, field := range []string{"group", "target"} {
			change := good
			if field == "group" {
				change.Group = id
			} else {
				change.Target = id
			}
			if err := change.Validate(); err == nil {
				t.Fatalf("invalid %s accepted: %q", field, id)
			}
		}
	}
	for name, mutate := range map[string]func(*SettingChange){
		"text":           func(c *SettingChange) { c.Text = "unrelated" },
		"enabled":        func(c *SettingChange) { c.Enabled = true },
		"search_account": func(c *SettingChange) { c.SearchAccount = true },
		"search_mobile":  func(c *SettingChange) { c.SearchMobile = true },
		"minutes":        func(c *SettingChange) { c.Minutes = 1 },
		"kind":           func(c *SettingChange) { c.Kind = "friend_delete_batch" },
	} {
		t.Run(name, func(t *testing.T) {
			change := good
			mutate(&change)
			if err := change.Validate(); err == nil {
				t.Fatal("friend deletion accepted unrelated fields")
			}
		})
	}
}
