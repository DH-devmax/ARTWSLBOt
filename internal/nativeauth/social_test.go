package nativeauth

import (
	"encoding/json"
	"testing"
)

func socialData(t *testing.T, text string) map[string]json.RawMessage {
	t.Helper()
	var data map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSocialProjectionStrictIdentityAndPrivacy(t *testing.T) {
	data := socialData(t, `{"list":[{"flId":"9007199254740993","user":{"uid":8,"nickName":"peer","phone":"excluded"},
        "direction":"FROM_HIM","state":"FRIENDLOG_STATE_PROCESSING","applyMsg":"excluded","friendCircle":{"notSeeMe":true}}]}`)
	rows, err := parseFriendApplications(data)
	if err != nil || len(rows) != 1 || rows[0].ID != "9007199254740993" || !rows[0].NotSeeMe {
		t.Fatal(rows, err)
	}
	body, _ := json.Marshal(rows)
	var public []map[string]json.RawMessage
	json.Unmarshal(body, &public)
	if len(public[0]) != 4 {
		t.Fatal("private friend fields escaped projection", string(body))
	}
	for _, invalid := range []string{
		`{}`, `{"list":null}`, `{"list":[{"flId":"01"}]}`,
		`{"list":[{"flId":"1","user":{"uid":8},"direction":"FROM_HIM","state":"unknown"}]}`,
		`{"list":[{"flId":"9223372036854775808","user":{"uid":8},"direction":1,"state":3}]}`,
		`{"list":[{"flId":"1","user":{"uid":8,"nickName":"bad\nname"},"direction":1,"state":3}]}`,
		`{"list":[{"flId":1,"user":{"uid":8},"direction":1,"state":3},{"flId":1,"user":{"uid":8},"direction":1,"state":3}]}`,
	} {
		if _, err := parseFriendApplications(socialData(t, invalid)); err == nil {
			t.Fatal("accepted malformed or ambiguous friend application", invalid)
		}
	}
	friends, err := parseFriends(socialData(t, `{"friendList":[
        {"uid":8,"nickName":"good","state":"FRIEND_STATE_GOOD"},
        {"uid":9,"nickName":"black","state":7},{"uid":10,"state":6}]}`))
	if err != nil || len(friends) != 1 || friends[0].ID != "8" {
		t.Fatal("nonfriends became invitation targets", friends, err)
	}
}

func TestSocialChangeIsSingleCanonicalTarget(t *testing.T) {
	for _, good := range []SettingChange{
		{Kind: "friend_approve", Target: "8", Text: "9007199254740993"},
		{Kind: "friend_reject", Target: "8", Text: "123"},
		{Kind: "group_invite", Group: "123", Target: "8"}, {Kind: "group_leave", Group: "123"},
	} {
		if err := good.Validate(); err != nil {
			t.Fatal(good, err)
		}
		bad := good
		bad.Enabled = true
		if bad.Validate() == nil {
			t.Fatal("accepted unrelated setting fields")
		}
	}
	for _, bad := range []SettingChange{
		{Kind: "group_invite", Group: "123", Target: "8,9"}, {Kind: "group_leave", Group: "01"},
		{Kind: "friend_approve", Target: "8", Text: "-1"}, {Kind: "friend_approve", Target: "0", Text: "1"},
		{Kind: "group_leave", Group: "123", Target: "8"},
	} {
		if bad.Validate() == nil {
			t.Fatal("accepted invalid action", bad)
		}
	}
}
