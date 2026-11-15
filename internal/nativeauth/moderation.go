package nativeauth

import (
	"context"
	"encoding/json"
	"strconv"

	"wangshangliao/internal/nativewire"
)

func ModerationKind(kind string) bool {
	switch kind {
	case "mute_member", "unmute_member", "mute_group", "unmute_group", "kick_member", "publish_notice",
		"grant_admin", "revoke_admin", "group_info":
		return true
	}
	return false
}

type GroupPolicy struct {
	Group string `json:"group_id"`
	Muted bool   `json:"muted"`
}

func (c *Client) GroupPolicy(ctx context.Context, group string) (GroupPolicy, error) {
	if !settingID(group) {
		return GroupPolicy{}, rejection("native_moderation_request_invalid")
	}
	id, _ := strconv.ParseUint(group, 10, 30)
	data, err := c.Request(ctx, "/v1/group/get-group-info", map[string]any{"groupId": id, "v": "0"})
	if err != nil {
		return GroupPolicy{}, err
	}
	info, err := groupInfoFields(data, group)
	if err != nil {
		return GroupPolicy{}, err
	}
	mode := socialEnum(info["groupMuteMode"], []string{"MUTE_NO", "MUTE_MEMBER"})
	if mode == "" {
		return GroupPolicy{}, rejection("native_moderation_shape")
	}
	return GroupPolicy{Group: group, Muted: mode == "MUTE_MEMBER"}, nil
}

type GroupNotice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (c *Client) GroupNotices(ctx context.Context, group string) ([]GroupNotice, error) {
	if !settingID(group) {
		return nil, rejection("native_moderation_request_invalid")
	}
	id, _ := strconv.ParseUint(group, 10, 30)
	data, err := c.Request(ctx, "/v1/group/notice-list", map[string]any{"groupId": id, "v": "0"})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID      json.RawMessage `json:"noticeId"`
		Group   json.RawMessage `json:"groupId"`
		Text    string          `json:"noticeContent"`
		Deleted bool            `json:"delete"`
	}
	if json.Unmarshal(data["noticeInfoList"], &rows) != nil || rows == nil || len(rows) > 500 {
		return nil, rejection("native_moderation_shape")
	}
	out, seen := []GroupNotice{}, map[string]bool{}
	for _, row := range rows {
		id := directoryID(row.ID)
		if !socialLogID(id) || seen[id] || directoryID(row.Group) != group ||
			(SettingChange{Kind: "publish_notice", Group: group, Text: row.Text}).Validate() != nil {
			return nil, rejection("native_moderation_shape")
		}
		seen[id] = true
		if !row.Deleted {
			out = append(out, GroupNotice{ID: id, Text: row.Text})
		}
	}
	return out, nil
}

// ChangeModeration performs exactly one desktop business mutation. Success
// denotes an upstream acknowledgement, not a claim that clients applied it.
func (c *Client) ChangeModeration(ctx context.Context, change SettingChange, fence nativewire.SubmissionFence) error {
	if !ModerationKind(change.Kind) || change.Validate() != nil || fence == nil {
		return rejection("native_moderation_request_invalid")
	}
	group, _ := strconv.ParseUint(change.Group, 10, 30)
	target, _ := strconv.ParseUint(change.Target, 10, 30)
	params := map[string]any{"groupId": group}
	var route string
	switch change.Kind {
	case "mute_member":
		route = "/v1/group/set-member-mute"
		params["userId"], params["min"] = target, change.Minutes
	case "unmute_member":
		route = "/v1/group/member-mute-cancel"
		params["userId"] = target
	case "mute_group", "unmute_group":
		route = "/v1/group/set-group-mute"
		params["muteMode"] = "MUTE_NO"
		if change.Kind == "mute_group" {
			params["muteMode"] = "MUTE_MEMBER"
		}
	case "kick_member":
		route = "/v1/group/remove-group-member"
		params["groupMemberIds"] = []uint64{target}
	case "grant_admin":
		route = "/v1/group/add-group-manage"
		params["groupManagerIds"] = []uint64{target}
	case "revoke_admin":
		route = "/v1/group/del-group-manage"
		params["userId"] = target
	case "publish_notice":
		route = "/v1/group/add-notice"
		params["noticeContent"], params["noticeMode"] = change.Text, "COMMON_NOTICE"
	case "group_info":
		route, params["groupName"] = "/v1/group/set-group-name", change.Text
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := c.request(context.WithValue(ctx, settingFenceKey{}, fence), route, params, nil)
	if err != nil {
		return err
	}
	if change.Kind == "mute_member" || change.Kind == "unmute_member" {
		if directoryID(data["groupId"]) != change.Group || directoryID(data["userId"]) != change.Target {
			return &Error{Code: "native_response_invalid", Submitted: true}
		}
		if change.Kind == "mute_member" {
			var minutes int
			if json.Unmarshal(data["min"], &minutes) != nil || minutes != change.Minutes {
				return &Error{Code: "native_response_invalid", Submitted: true}
			}
		}
	}
	if change.Kind == "publish_notice" {
		var text string
		if directoryID(data["groupId"]) != change.Group || !socialLogID(directoryID(data["noticeId"])) ||
			json.Unmarshal(data["noticeContent"], &text) != nil || text != change.Text ||
			socialEnum(data["noticeMode"], []string{"COMMON_NOTICE", "POP_NOTICE", "NEWCOMER_NOTICE", "TOP_NOTICE"}) != "COMMON_NOTICE" {
			return &Error{Code: "native_response_invalid", Submitted: true}
		}
	}
	if change.Kind == "group_info" {
		var name string
		if directoryID(data["groupId"]) != change.Group || json.Unmarshal(data["groupName"], &name) != nil || name != change.Text {
			return &Error{Code: "native_response_invalid", Submitted: true}
		}
	}
	return nil
}
