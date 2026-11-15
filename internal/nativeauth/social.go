package nativeauth

import (
	"context"
	"encoding/json"
	"strconv"
	"unicode"
	"unicode/utf8"

	"wangshangliao/internal/nativewire"
)

// Social records contain no phone, transport credentials or arbitrary profile.
type Friend struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FriendApplication struct {
	ID        string `json:"id"`
	User      Friend `json:"user"`
	Direction string `json:"direction"`
	State     string `json:"state"`
	NotSeeMe  bool   `json:"-"`
	NotSeeHim bool   `json:"-"`
}

func GroupMembershipKind(kind string) bool {
	return kind == "group_invite" || kind == "group_leave" || kind == "group_accept"
}

func SocialKind(kind string) bool {
	return kind == "friend_approve" || kind == "friend_reject" || kind == "friend_delete" || GroupMembershipKind(kind)
}

func socialLogID(id string) bool {
	n, err := strconv.ParseUint(id, 10, 63)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == id
}

func socialName(raw json.RawMessage) (string, error) {
	var s string
	if len(raw) > 0 && json.Unmarshal(raw, &s) != nil || !utf8.ValidString(s) || len(s) > 1024 ||
		stringsContainControl(s) {
		return "", rejection("native_social_shape")
	}
	return s, nil
}

func stringsContainControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func socialEnum(raw json.RawMessage, names []string) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		for _, name := range names {
			if name == text {
				return text
			}
		}
	}
	var n int
	if json.Unmarshal(raw, &n) == nil && n >= 0 && n < len(names) {
		return names[n]
	}
	return ""
}

// The desktop requests one server window at lastId=0. lastId is its maximum
// event ID, not a documented pagination cursor; never invent continuation.
func (c *Client) FriendApplications(ctx context.Context) ([]FriendApplication, error) {
	data, err := c.Request(ctx, "/v1/friend/friend-apply-list", map[string]any{"lastId": "0"})
	if err != nil {
		return nil, err
	}
	return parseFriendApplications(data)
}

func parseFriendApplications(data map[string]json.RawMessage) ([]FriendApplication, error) {
	var rows []struct {
		ID   json.RawMessage `json:"flId"`
		User struct {
			ID   json.RawMessage `json:"uid"`
			Name json.RawMessage `json:"nickName"`
		} `json:"user"`
		Direction json.RawMessage `json:"direction"`
		State     json.RawMessage `json:"state"`
		Circle    struct {
			NotSeeMe  bool `json:"notSeeMe"`
			NotSeeHim bool `json:"notSeeHim"`
		} `json:"friendCircle"`
	}
	if json.Unmarshal(data["list"], &rows) != nil || rows == nil || len(rows) > 500 {
		return nil, rejection("native_social_limit_or_shape")
	}
	out, seen := []FriendApplication{}, map[string]bool{}
	states := []string{"FRIENDLOG_STATE_UNKNOWN", "FRIENDLOG_STATE_AGREE", "FRIENDLOG_STATE_REFUSE",
		"FRIENDLOG_STATE_PROCESSING", "FRIENDLOG_STATE_DELETE", "FRIENDLOG_STATE_EXPIRE"}
	for _, row := range rows {
		id, uid := directoryID(row.ID), directoryID(row.User.ID)
		name, err := socialName(row.User.Name)
		direction := socialEnum(row.Direction, []string{"FROM_ME", "FROM_HIM"})
		state := socialEnum(row.State, states)
		if err != nil || !socialLogID(id) || !settingID(uid) || seen[id] || direction == "" || state == "" {
			return nil, rejection("native_social_shape")
		}
		seen[id] = true
		out = append(out, FriendApplication{ID: id, User: Friend{uid, name}, Direction: direction,
			State: state, NotSeeMe: row.Circle.NotSeeMe, NotSeeHim: row.Circle.NotSeeHim})
	}
	return out, nil
}

func (c *Client) Friends(ctx context.Context) ([]Friend, error) {
	data, err := c.Request(ctx, "/v1/friend/get-friend-list", map[string]any{"v": "0"})
	if err != nil {
		return nil, err
	}
	return parseFriends(data)
}

func parseFriends(data map[string]json.RawMessage) ([]Friend, error) {
	var rows []struct {
		ID    json.RawMessage `json:"uid"`
		Name  json.RawMessage `json:"nickName"`
		State json.RawMessage `json:"state"`
	}
	if json.Unmarshal(data["friendList"], &rows) != nil || rows == nil || len(rows) > 2000 {
		return nil, rejection("native_social_limit_or_shape")
	}
	out, seen := []Friend{}, map[string]bool{}
	for _, row := range rows {
		id := directoryID(row.ID)
		name, err := socialName(row.Name)
		state := socialEnum(row.State, []string{"FRIEND_STATE_NONE", "FRIEND_STATE_APPLIED",
			"FRIEND_STATE_PENDING", "FRIEND_STATE_FAILED", "FRIEND_STATE_REJECTED", "FRIEND_STATE_GOOD",
			"FRIEND_STATE_DELETED", "FRIEND_STATE_BLACKLIST"})
		if err != nil || !settingID(id) || seen[id] || state == "" {
			return nil, rejection("native_social_shape")
		}
		seen[id] = true
		if state == "FRIEND_STATE_GOOD" {
			out = append(out, Friend{id, name})
		}
	}
	return out, nil
}

// ChangeSocial is one explicitly fenced operation. The approved application is
// supplied by the gateway's fresh read, never by browser-provided profile data.
func (c *Client) ChangeSocial(ctx context.Context, change SettingChange, application *FriendApplication, fence nativewire.SubmissionFence) error {
	if !SocialKind(change.Kind) || change.Validate() != nil || fence == nil {
		return rejection("native_social_request_invalid")
	}
	var route string
	var params map[string]any
	switch change.Kind {
	case "friend_delete":
		if application != nil {
			return rejection("native_social_request_invalid")
		}
		target, _ := strconv.ParseUint(change.Target, 10, 30)
		route, params = "/v1/friend/del-friend", map[string]any{"uid": target}
		fence = friendDeleteFence(fence)
	case "friend_approve", "friend_reject":
		if application == nil || application.ID != change.Text || application.User.ID != change.Target ||
			application.Direction != "FROM_HIM" || application.State != "FRIENDLOG_STATE_PROCESSING" {
			return rejection("native_social_application_changed")
		}
		state := "FRIENDLOG_STATE_AGREE"
		if change.Kind == "friend_reject" {
			state = "FRIENDLOG_STATE_REFUSE"
		}
		route = "/v1/friend/friend-apply-handler"
		params = map[string]any{"flId": application.ID, "state": state, "nickName": application.User.Name,
			"classId": 0, "friendCircle": map[string]bool{"notSeeMe": application.NotSeeMe, "notSeeHim": application.NotSeeHim}}
	case "group_invite":
		group, _ := strconv.ParseUint(change.Group, 10, 30)
		target, _ := strconv.ParseUint(change.Target, 10, 30)
		route, params = "/v1/group/add-group-member", map[string]any{"groupId": group, "groupMemberIds": []uint64{target}}
	case "group_leave":
		group, _ := strconv.ParseUint(change.Group, 10, 30)
		route, params = "/v1/group/leave-group", map[string]any{"groupId": group}
	case "group_accept":
		route, params = "/v1/group/group-member-invite", map[string]any{"applyId": change.Text, "agree": true}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if change.Kind == "friend_delete" && (!settingID(strconv.FormatUint(c.fields[3], 10)) ||
		change.Target == strconv.FormatUint(c.fields[3], 10)) {
		return rejection("native_social_target_invalid")
	}
	if change.Kind == "group_accept" && change.Target != strconv.FormatUint(c.fields[3], 10) {
		return rejection("native_invitation_self_required")
	}
	data, err := c.request(context.WithValue(ctx, settingFenceKey{}, fence), route, params, nil)
	if err != nil {
		return err
	}
	if change.Kind == "friend_delete" && len(data) != 0 {
		return &Error{Code: "native_response_invalid", Submitted: true}
	}
	if application != nil && (directoryID(data["flId"]) != application.ID || socialEnum(data["state"],
		[]string{"FRIENDLOG_STATE_UNKNOWN", "FRIENDLOG_STATE_AGREE", "FRIENDLOG_STATE_REFUSE"}) != params["state"]) {
		return &Error{Code: "native_response_invalid", Submitted: true}
	}
	if change.Kind == "group_accept" {
		var added bool
		state := socialEnum(data["state"], []string{"MEMBER_STATE_NONE", "MEMBER_STATE_INVITED", "MEMBER_STATE_APPLIED",
			"MEMBER_STATE_REJECTED", "MEMBER_STATE_GOOD", "MEMBER_STATE_MANAGER_REJECT",
			"MEMBER_STATE_KICKED", "MEMBER_STATE_QUIT", "MEMBER_STATE_DELETE"})
		if json.Unmarshal(data["newAdd"], &added) != nil || state != "MEMBER_STATE_GOOD" && state != "MEMBER_STATE_APPLIED" ||
			added && (state != "MEMBER_STATE_GOOD" || !socialLogID(directoryID(data["groupCloudId"]))) {
			return &Error{Code: "native_response_invalid", Submitted: true}
		}
	}
	return nil
}
