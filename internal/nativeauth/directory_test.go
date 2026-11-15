package nativeauth

import (
	"encoding/json"
	"testing"
)

func TestDirectoryBoundedMapping(t *testing.T) {
	var data map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"owner":[{"groupId":"123","groupCloudId":"456","groupName":"Synthetic","ownerUserId":"7","groupAvatar":"9007199254740993"}],"member":[{"groupId":124,"groupCloudId":"457","groupAvatar":"987.jpg","me":{"role":"GROUP_ROLE_ADMIN"}}]}`), &data)
	groups, err := parseDirectory(data, 7)
	if err != nil || len(groups) != 2 || groups[0].Role != "owner" || groups[1].Role != "admin" {
		t.Fatal("directory fixture", err)
	}
	if groups[0].Avatar != "9007199254740993" || groups[1].Avatar != "987.jpg" {
		t.Fatal("exact group avatar mapping lost")
	}
	for _, raw := range []string{
		`{"owner":[]}`, `{"owner":null,"member":[]}`,
		`{"owner":[{"groupId":"1","groupCloudId":"2"}],"member":[{"groupId":"1","groupCloudId":"3"}]}`,
		`{"owner":[{"groupId":"1","groupCloudId":"2"}],"member":[{"groupId":"3","groupCloudId":"2"}]}`,
		`{"owner":[{"groupId":"01","groupCloudId":"2"}],"member":[]}`,
	} {
		data = nil
		_ = json.Unmarshal([]byte(raw), &data)
		if _, err := parseDirectory(data, 7); err == nil {
			t.Fatal("invalid directory accepted")
		}
	}
}

func TestDirectoryAvatarOnlyKeepsReviewedReferences(t *testing.T) {
	for raw, want := range map[string]string{
		`"18446744073709551615"`: "18446744073709551615",
		`9007199254740993`:       "9007199254740993",
		`"123.jpg"`:              "123.jpg",
		`"0"`:                    "", `"01"`: "", `"../1"`: "", `"https://example.com/123"`: "",
		`"18446744073709551616"`: "", `null`: "", `{}`: "", `"-1"`: "", `""`: "",
	} {
		if got := directoryAvatar(json.RawMessage(raw)); got != want {
			t.Fatalf("avatar reference %s: got %q, want %q", raw, got, want)
		}
	}
}
