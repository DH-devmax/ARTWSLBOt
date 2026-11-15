//go:build sodium && cgo

package nativeauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModerationDesktopContractsFenceAndUnknown(t *testing.T) {
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	c := base.(*Client)
	defer c.Close()
	c.jwt, c.fields[3] = "synthetic-moderation-jwt", 7
	writes, mismatch := 0, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, p := desktopRequest(t, r, c.bodyKey)
		data := map[string]any{}
		if string(p["groupId"]) != "123" {
			t.Error("lost group scope")
		}
		switch r.URL.Path {
		case "/v1/group/get-group-info":
			data["groupInfo"] = map[string]any{"groupId": 123, "groupMuteMode": "MUTE_NO", "nicknameMode": false}
		case "/v1/group/notice-list":
			data["noticeInfoList"] = []any{
				map[string]any{"noticeId": "9007199254740993", "groupId": 123, "noticeContent": "existing"},
				map[string]any{"noticeId": "9007199254740994", "groupId": 123, "noticeContent": "deleted", "delete": true},
			}
		case "/v1/group/set-member-mute":
			writes++
			if len(p) != 3 || string(p["userId"]) != "8" || string(p["min"]) != "2" {
				t.Error("mute duration must be minutes, not seconds")
			}
			data["groupId"], data["userId"], data["min"] = 123, 8, 2
			if mismatch {
				data["userId"] = 9
			}
		case "/v1/group/member-mute-cancel":
			writes++
			if len(p) != 2 || string(p["userId"]) != "8" {
				t.Error("invalid unmute contract")
			}
			data["groupId"], data["userId"] = 123, 8
		case "/v1/group/set-group-mute":
			writes++
			if len(p) != 2 || string(p["muteMode"]) != `"MUTE_MEMBER"` && string(p["muteMode"]) != `"MUTE_NO"` {
				t.Error("invalid group mute mode")
			}
		case "/v1/group/remove-group-member":
			writes++
			if len(p) != 2 || string(p["groupMemberIds"]) != "[8]" {
				t.Error("kick expanded beyond one target")
			}
		case "/v1/group/add-group-manage":
			writes++
			if len(p) != 2 || string(p["groupManagerIds"]) != "[8]" {
				t.Error("administrator grant expanded beyond one target")
			}
		case "/v1/group/del-group-manage":
			writes++
			if len(p) != 2 || string(p["userId"]) != "8" {
				t.Error("invalid administrator revoke contract")
			}
		case "/v1/group/add-notice":
			writes++
			if len(p) != 3 || string(p["noticeMode"]) != `"COMMON_NOTICE"` {
				t.Error("notice kind expanded")
			}
			data["groupId"], data["noticeContent"], data["noticeMode"], data["noticeId"] = 123, "test", "COMMON_NOTICE", "9007199254740993"
		case "/v1/group/set-group-name":
			writes++
			if len(p) != 2 || string(p["groupName"]) != `"test"` {
				t.Error("group name scope changed")
			}
			data["groupId"], data["groupName"] = 123, "test"
		default:
			t.Error("unreviewed route", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer server.Close()
	c.config.Origin, c.http = server.URL, server.Client()
	if policy, err := c.GroupPolicy(context.Background(), "123"); err != nil || policy.Muted {
		t.Fatal(policy, err)
	}
	if locked, err := c.NicknameLocked(context.Background(), "123"); err != nil || locked {
		t.Fatal("official nested GroupInfo nickname policy unreadable", locked, err)
	}
	if notices, err := c.GroupNotices(context.Background(), "123"); err != nil || len(notices) != 1 || notices[0].ID != "9007199254740993" {
		t.Fatal("notice ID precision", notices, err)
	}
	for _, route := range []string{"/v1/group/set-member-mute", "/v1/group/member-mute-cancel",
		"/v1/group/set-group-mute", "/v1/group/remove-group-member", "/v1/group/add-notice",
		"/v1/group/add-group-manage", "/v1/group/del-group-manage", "/v1/group/set-group-name"} {
		if _, err := c.Request(context.Background(), route, map[string]any{}); err == nil {
			t.Fatal("unfenced mutation", route)
		}
	}
	change := SettingChange{Kind: "mute_member", Group: "123", Target: "8", Minutes: 2}
	denied := errors.New("revoked")
	if err := c.ChangeModeration(context.Background(), change,
		func(context.Context, func(context.Context) error) error { return denied }); !errors.Is(err, denied) || writes != 0 {
		t.Fatal("revocation reached native request", err)
	}
	fence := func(ctx context.Context, submit func(context.Context) error) error { return submit(ctx) }
	for _, item := range []SettingChange{change, {Kind: "unmute_member", Group: "123", Target: "8"},
		{Kind: "mute_group", Group: "123"}, {Kind: "unmute_group", Group: "123"},
		{Kind: "kick_member", Group: "123", Target: "8"}, {Kind: "publish_notice", Group: "123", Text: "test"},
		{Kind: "grant_admin", Group: "123", Target: "8"}, {Kind: "revoke_admin", Group: "123", Target: "8"},
		{Kind: "group_info", Group: "123", Text: "test"}} {
		if err := c.ChangeModeration(context.Background(), item, fence); err != nil {
			t.Fatal(item.Kind, err)
		}
	}
	mismatch = true
	var native *Error
	if err := c.ChangeModeration(context.Background(), change, fence); !errors.As(err, &native) || !native.Submitted || native.Code != "native_response_invalid" || writes != 10 {
		t.Fatal("ambiguous result retried or treated as success", writes, err)
	}
}
