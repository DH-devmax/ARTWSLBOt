package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"wangshangliao/internal/nativeauth"
)

// Fixed desktop 1.0.3 and bundled OpenIM contracts, independently of WSL.
const (
	GroupStatusNormal = 0
	GroupStatusMuted  = 3
)

type GroupInfo struct {
	ID           string `json:"groupID"`
	Name         string `json:"groupName"`
	Status       *int   `json:"status"`
	Notification string `json:"notification"`
}

type Friend struct {
	Owner string `json:"ownerUserID"`
	ID    string `json:"friendUserID"`
	Name  string `json:"nickname"`
}

type FriendRequest struct {
	From    string `json:"fromUserID"`
	To      string `json:"toUserID"`
	Name    string `json:"fromNickname"`
	Created int64  `json:"createTime"`
	Result  *int   `json:"handleResult"`
}

type GroupRequest struct {
	Group   string `json:"groupID"`
	User    string `json:"userID"`
	Name    string `json:"nickname"`
	Created int64  `json:"reqTime"`
	Result  *int   `json:"handleResult"`
}

type Change struct {
	Kind, Group, Target, Text            string
	Minutes                              int
	Enabled, SearchAccount, SearchMobile bool
}

func ValidID(value string) bool { return validID(value) && utf8.ValidString(value) }

func validText(value string, limit int, empty bool) bool {
	return utf8.ValidString(value) && len(value) <= limit && (empty || strings.TrimSpace(value) != "") &&
		strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0
}

func (c Change) Validate() error {
	expected := Change{Kind: c.Kind}
	switch c.Kind {
	case "group_info", "send_text", "recall_message":
		if !ValidID(c.Group) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		if c.Kind == "group_info" && (!validText(c.Text, 128, false) || utf8.RuneCountInString(c.Text) > 20 ||
			strings.IndexFunc(c.Text, unicode.IsSpace) >= 0) ||
			c.Kind == "send_text" && (!validText(c.Text, 8000, false) || utf8.RuneCountInString(c.Text) > 2000) ||
			c.Kind == "recall_message" && !nativeauth.ValidMessageReference(c.Text, "lajiaoliao", c.Group) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Group, expected.Text = c.Group, c.Text
	case "nickname":
		if !validText(c.Text, 128, false) || utf8.RuneCountInString(c.Text) > 20 || c.Text != strings.TrimSpace(c.Text) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Text = c.Text
	case "search":
		expected.SearchAccount, expected.SearchMobile = c.SearchAccount, c.SearchMobile
	case "friend_policy":
		if c.Text != "FRIEND_NEED_APPROVAL" && c.Text != "FRIEND_ANY_ONE" && c.Text != "FRIEND_REJECT_ALL" {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Text = c.Text
	case "group_policy":
		if c.Text != "GROUP_REJECT_JOIN" && c.Text != "GROUP_APPROVAL_JOIN" {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Text = c.Text
	case "audio_video":
		expected.Enabled = c.Enabled
	case "profile_gender":
		if c.Text != "0" && c.Text != "1" && c.Text != "2" {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Text = c.Text
	case "profile_birth":
		if !ValidBirth(c.Text) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Text = c.Text
	case "profile_signature":
		if !validText(c.Text, 320, true) || utf8.RuneCountInString(c.Text) > 80 {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Text = c.Text
	case "push_notifications":
		var push Notifications
		d := json.NewDecoder(bytes.NewBufferString(c.Text))
		d.DisallowUnknownFields()
		if len(c.Text) > 512 || d.Decode(&push) != nil || d.Decode(new(any)) != io.EOF || !push.Complete() {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Text = c.Text
	case "mute_member", "unmute_member", "kick_member", "kick_member_ban", "remove_group_ban", "grant_admin", "revoke_admin", "member_nickname":
		if !ValidID(c.Group) || !ValidID(c.Target) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Group, expected.Target = c.Group, c.Target
		if c.Kind == "mute_member" {
			if c.Minutes < 1 || c.Minutes > 1440 {
				return &Failure{Code: "lajiaoliao_invalid_action"}
			}
			expected.Minutes = c.Minutes
		}
		if c.Kind == "member_nickname" {
			if !validText(c.Text, 128, true) || utf8.RuneCountInString(c.Text) > 15 || c.Text != strings.TrimSpace(c.Text) {
				return &Failure{Code: "lajiaoliao_invalid_action"}
			}
			expected.Text = c.Text
		}
	case "mute_group", "unmute_group", "publish_notice", "group_invite", "group_leave":
		if !ValidID(c.Group) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Group = c.Group
		if c.Kind == "group_invite" {
			if !ValidID(c.Target) {
				return &Failure{Code: "lajiaoliao_invalid_action"}
			}
			expected.Target = c.Target
		}
		if c.Kind == "publish_notice" {
			if !validText(c.Text, 2400, false) || utf8.RuneCountInString(c.Text) > 600 {
				return &Failure{Code: "lajiaoliao_invalid_action"}
			}
			expected.Text = c.Text
		}
	case "friend_approve", "friend_reject":
		n, err := strconv.ParseInt(c.Text, 10, 64)
		if !ValidID(c.Target) || err != nil || n <= 0 || strconv.FormatInt(n, 10) != c.Text ||
			c.Group != "" && !ValidID(c.Group) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Group, expected.Target, expected.Text = c.Group, c.Target, c.Text
	case "friend_delete":
		if !ValidID(c.Group) || !ValidID(c.Target) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Group, expected.Target = c.Group, c.Target
	case "group_accept":
		if !ValidID(c.Group) || !ValidID(c.Target) || !validInvitationID(c.Text) {
			return &Failure{Code: "lajiaoliao_invalid_action"}
		}
		expected.Group, expected.Target, expected.Text = c.Group, c.Target, c.Text
	default:
		return &Failure{Code: "lajiaoliao_action_unavailable"}
	}
	if c != expected {
		return &Failure{Code: "lajiaoliao_invalid_action"}
	}
	return nil
}

func (c *Client) GroupInfo(ctx context.Context, s Session, group string) (GroupInfo, error) {
	if !ValidID(group) {
		return GroupInfo{}, &Failure{Code: "lajiaoliao_invalid_identity"}
	}
	raw, err := c.request(ctx, "/group/get_groups_info", map[string]any{"groupIDs": []string{group}}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return GroupInfo{}, err
	}
	var result struct {
		Groups []GroupInfo `json:"groupInfos"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Groups) != 1 || result.Groups[0].ID != group ||
		!validText(result.Groups[0].Notification, 65536, true) {
		return GroupInfo{}, &Failure{Code: "lajiaoliao_group_shape"}
	}
	// The SDK's proto3 JSON omits normal status (zero).
	if result.Groups[0].Status == nil {
		normal := GroupStatusNormal
		result.Groups[0].Status = &normal
	}
	if *result.Groups[0].Status != GroupStatusNormal && *result.Groups[0].Status != GroupStatusMuted {
		return GroupInfo{}, &Failure{Code: "lajiaoliao_group_shape"}
	}
	return result.Groups[0], nil
}

func (c *Client) Friends(ctx context.Context, s Session, page int) ([]Friend, bool, error) {
	if page < 1 || page > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_invalid_page"}
	}
	raw, err := c.request(ctx, "/friend/get_friend_list", map[string]any{"userID": s.UserID,
		"pagination": map[string]int{"pageNumber": page, "showNumber": 100}}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nil, false, err
	}
	var result struct {
		Friends *[]struct {
			Owner string `json:"ownerUserID"`
			User  struct {
				ID   string `json:"userID"`
				Name string `json:"nickname"`
			} `json:"friendUser"`
		} `json:"friendsInfo"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Friends == nil || len(*result.Friends) > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_friend_shape"}
	}
	out := []Friend{}
	seen := map[string]bool{}
	for _, row := range *result.Friends {
		f := Friend{Owner: row.Owner, ID: row.User.ID, Name: row.User.Name}
		if f.Owner != s.UserID || !ValidID(f.ID) || f.ID == s.UserID || seen[f.ID] || !validText(f.Name, 1000, true) {
			return nil, false, &Failure{Code: "lajiaoliao_friend_shape"}
		}
		seen[f.ID] = true
		out = append(out, f)
	}
	return out, len(*result.Friends) == 100, nil
}

func (c *Client) FriendRequests(ctx context.Context, s Session, page int) ([]FriendRequest, bool, error) {
	if page < 1 || page > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_invalid_page"}
	}
	raw, err := c.request(ctx, "/friend/get_friend_apply_list", map[string]any{"userID": s.UserID,
		"pagination": map[string]int{"pageNumber": page, "showNumber": 100}}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nil, false, err
	}
	var result struct {
		Total    *int             `json:"total"`
		Requests *[]FriendRequest `json:"friendRequests"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Requests == nil || len(*result.Requests) > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_friend_shape"}
	}
	more := len(*result.Requests) == 100
	if result.Total != nil {
		offset := (page - 1) * 100
		if *result.Total < offset || *result.Total > 10000 ||
			len(*result.Requests) != min(100, *result.Total-offset) {
			return nil, false, &Failure{Code: "lajiaoliao_friend_shape"}
		}
		more = offset+len(*result.Requests) < *result.Total
	}
	for _, row := range *result.Requests {
		if row.Result == nil {
			pending := 0
			row.Result = &pending
		}
		if row.To != s.UserID || !ValidID(row.From) || row.From == s.UserID || row.Created <= 0 ||
			row.Result == nil || (*row.Result != -1 && *row.Result != 0 && *row.Result != 1) || !validText(row.Name, 1000, true) {
			return nil, false, &Failure{Code: "lajiaoliao_friend_shape"}
		}
	}
	out := *result.Requests
	for i := range out {
		if out[i].Result == nil {
			pending := 0
			out[i].Result = &pending
		}
	}
	return out, more, nil
}

func (c *Client) GroupRequests(ctx context.Context, s Session, group string, page int) ([]GroupRequest, bool, error) {
	if !ValidID(group) || page < 1 || page > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_invalid_page"}
	}
	raw, err := c.request(ctx, "/group/get_recv_group_applicationList", map[string]any{"fromUserID": s.UserID,
		"groupIDs": []string{group}, "pagination": map[string]int{"pageNumber": page, "showNumber": 100}},
		s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nil, false, err
	}
	var result struct {
		Total    *int `json:"total"`
		Requests *[]struct {
			User struct {
				ID   string `json:"userID"`
				Name string `json:"nickname"`
			} `json:"userInfo"`
			Group struct {
				ID string `json:"groupID"`
			} `json:"groupInfo"`
			Created int64 `json:"reqTime"`
			Result  *int  `json:"handleResult"`
		} `json:"groupRequests"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Total != nil && (*result.Total < 0 || *result.Total > 10000) {
		return nil, false, &Failure{Code: "lajiaoliao_group_request_shape"}
	}
	// The pinned service returns groupRequests:null with an explicit total:0.
	// A missing collection without that evidence must remain unknown.
	if result.Requests == nil && result.Total != nil && *result.Total == 0 {
		return []GroupRequest{}, false, nil
	}
	if result.Requests == nil || len(*result.Requests) > 100 ||
		result.Total != nil && len(*result.Requests) > *result.Total {
		return nil, false, &Failure{Code: "lajiaoliao_group_request_shape"}
	}
	out := []GroupRequest{}
	for _, item := range *result.Requests {
		row := GroupRequest{Group: item.Group.ID, User: item.User.ID, Name: item.User.Name, Created: item.Created, Result: item.Result}
		if row.Result == nil {
			pending := 0
			row.Result = &pending
		}
		if row.Group != group || !ValidID(row.User) || row.Created <= 0 || row.Result == nil ||
			(*row.Result != -1 && *row.Result != 0 && *row.Result != 1) || !validText(row.Name, 1000, true) {
			return nil, false, &Failure{Code: "lajiaoliao_group_request_shape"}
		}
		out = append(out, row)
	}
	return out, len(*result.Requests) == 100, nil
}

// The caller owns durable intent and the final SaaS authorization fence.
// Native role reads precede that fence. No mutation is retried.
func (c *Client) Execute(ctx context.Context, s Session, change Change,
	fence func(context.Context, func(context.Context) error) error) error {
	if err := change.Validate(); err != nil {
		return err
	}
	if fence == nil {
		return &Failure{Code: "lajiaoliao_fence_required"}
	}
	if change.Kind == "friend_delete" {
		return c.deleteFriend(ctx, s, change, fence)
	}
	if change.Kind == "group_accept" {
		return c.acceptSelfInvitation(ctx, s, change, fence)
	}
	route, payload := "", map[string]any{}
	if change.Group != "" {
		self, err := c.Member(ctx, s, change.Group, s.UserID)
		if err != nil {
			return err
		}
		if change.Kind == "group_leave" {
			if self.Role != 20 && self.Role != 60 {
				return &Failure{Code: "lajiaoliao_native_role_required"}
			}
		} else if change.Kind != "member_nickname" || change.Target != s.UserID {
			if self.Role != 60 && self.Role != 100 {
				return &Failure{Code: "lajiaoliao_native_role_required"}
			}
		}
		switch change.Kind {
		case "remove_group_ban":
			banned, err := c.hasGroupBan(ctx, s, change.Group, change.Target)
			if err != nil {
				return err
			}
			if change.Target == s.UserID || !banned {
				return &Failure{Code: "lajiaoliao_group_ban_required"}
			}
		case "mute_member", "unmute_member", "kick_member", "kick_member_ban", "grant_admin", "revoke_admin", "member_nickname":
			target, err := c.Member(ctx, s, change.Group, change.Target)
			if err != nil {
				return err
			}
			if change.Kind == "grant_admin" || change.Kind == "revoke_admin" {
				expected := 20
				if change.Kind == "revoke_admin" {
					expected = 60
				}
				if self.Role != 100 || target.Role != expected || change.Target == s.UserID {
					return &Failure{Code: "lajiaoliao_native_role_required"}
				}
			} else if change.Target != s.UserID || change.Kind != "member_nickname" {
				if change.Target == s.UserID || target.Role == 100 || target.Role != 20 && self.Role != 100 {
					return &Failure{Code: "lajiaoliao_target_protected"}
				}
			}
		}
		payload["groupID"] = change.Group
	}
	switch change.Kind {
	case "mute_member":
		route = "/group/mute_group_member"
		payload["userID"], payload["mutedSeconds"] = change.Target, change.Minutes*60
	case "unmute_member":
		route, payload["userID"] = "/group/cancel_mute_group_member", change.Target
	case "mute_group":
		route = "/group/mute_group"
	case "unmute_group":
		route = "/group/cancel_mute_group"
	case "kick_member", "kick_member_ban":
		route, payload["kickedUserIDs"], payload["reason"] = "/group/kick_group", []string{change.Target}, ""
		payload["permanentBan"] = change.Kind == "kick_member_ban"
	case "remove_group_ban":
		route, payload["userID"] = "/group/remove_group_permanent_ban", change.Target
	case "publish_notice":
		route, payload["notification"] = "/group/set_group_info_ex", change.Text
	case "group_info":
		route, payload["groupName"] = "/group/set_group_info_ex", change.Text
	case "grant_admin", "revoke_admin", "member_nickname":
		route = "/group/set_group_member_info"
		member := map[string]any{"groupID": change.Group, "userID": change.Target}
		if change.Kind == "member_nickname" {
			member["nickname"] = change.Text
		} else if change.Kind == "grant_admin" {
			member["roleLevel"] = 60
		} else {
			member["roleLevel"] = 20
		}
		payload = map[string]any{"members": []any{member}}
	case "group_invite":
		route, payload["invitedUserIDs"], payload["reason"] = "/group/invite_user_to_group", []string{change.Target}, ""
	case "group_leave":
		route, payload["userID"] = "/group/quit_group", s.UserID
	case "friend_approve", "friend_reject":
		route = "/friend/add_friend_response"
		result := 1
		if change.Kind == "friend_reject" {
			result = -1
		}
		payload = map[string]any{"fromUserID": change.Target, "toUserID": s.UserID, "handleResult": result, "handleMsg": ""}
	}
	if route == "" {
		return &Failure{Code: "lajiaoliao_action_unavailable"}
	}
	return fence(ctx, func(work context.Context) error {
		_, err := c.request(work, route, payload, s.IMToken, "5", s.DeviceID)
		return err
	})
}
