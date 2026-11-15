package nativeauth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSettingAllowlistAndSafeProjection(t *testing.T) {
	for _, change := range []SettingChange{
		{Kind: "nickname", Text: "机器人"},
		{Kind: "member_nickname", Group: "123", Target: "7", Text: ""},
		{Kind: "search", SearchAccount: true},
		{Kind: "friend_policy", Text: "FRIEND_NEED_APPROVAL"},
		{Kind: "group_policy", Text: "GROUP_ASK_JOIN"},
		{Kind: "audio_video"},
	} {
		if _, _, err := settingRoute(change); err != nil {
			t.Fatal(change.Kind, err)
		}
	}
	for _, change := range []SettingChange{
		{Kind: "nickname", Text: ""},
		{Kind: "nickname", Text: strings.Repeat("界", 21)},
		{Kind: "nickname", Text: strings.Repeat("🙂", 11)},
		{Kind: "nickname", Text: "a\nb"},
		{Kind: "nickname", Text: " bot "},
		{Kind: "nickname", Text: "bot", Target: "7"},
		{Kind: "member_nickname", Group: "01", Target: "7"},
		{Kind: "member_nickname", Group: "123", Target: "1073741824"},
		{Kind: "friend_policy", Text: "FRIEND_ASK_ONLY"},
		{Kind: "group_policy", Text: "OWNER"},
		{Kind: "audio_video", Text: "hidden"},
		{Kind: "password", Text: "secret"},
	} {
		if change.Validate() == nil {
			t.Fatalf("unsafe setting admitted: %#v", change)
		}
	}
	var raw map[string]json.RawMessage
	json.Unmarshal([]byte(`{"uid":7,"nickName":"机器人","avatar":"123.jpg","friendPolicy":"FRIEND_ASK_ONLY","groupInvitationPolicy":"GROUP_ASK_JOIN",
        "searchableAccount":false,"audioVideoOpen":true,"realName":"must-not-leak","cardNo":"must-not-leak","p2pNick":{"must":"not-leak"}}`), &raw)
	view, err := parseAccountSettings(raw)
	if err != nil || view.SearchableAccount == nil || *view.SearchableAccount || view.SearchableMobile != nil {
		t.Fatal("missing/false settings were confused", err)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "must") || len(SettingsDigest(view)) != 64 {
		t.Fatal("projection disclosed unrelated account information")
	}
	raw["searchableAccount"] = json.RawMessage(`"true"`)
	if _, err = parseAccountSettings(raw); err == nil {
		t.Fatal("non-boolean setting accepted")
	}
}
