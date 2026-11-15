package nativeauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"wangshangliao/internal/nativewire"
)

// AccountSettings deliberately excludes the app response's contacts, identity
// documents, phone numbers, session preferences and arbitrary upstream fields.
type AccountSettings struct {
	UID               string          `json:"uid"`
	Nickname          string          `json:"nickname"`
	Avatar            string          `json:"avatar"`
	SearchableAccount *bool           `json:"searchable_account"`
	SearchableMobile  *bool           `json:"searchable_mobile"`
	FriendPolicy      string          `json:"friend_policy"`
	GroupPolicy       string          `json:"group_policy"`
	AudioVideo        *bool           `json:"audio_video"`
	MaskedMobile      string          `json:"masked_mobile"`
	ProfileDetails    json.RawMessage `json:"profile_details,omitempty"`
}

// SettingChange admits one reviewed desktop operation. It cannot carry a URL,
// trusted actor, native credentials or a batch of unrelated settings.
type SettingChange struct {
	Kind          string `json:"kind"`
	Group         string `json:"group_id,omitempty"`
	Target        string `json:"target_id,omitempty"`
	Text          string `json:"text,omitempty"`
	Enabled       bool   `json:"enabled,omitempty"`
	SearchAccount bool   `json:"searchable_account,omitempty"`
	SearchMobile  bool   `json:"searchable_mobile,omitempty"`
	Minutes       int    `json:"minutes,omitempty"`
}

func ValidNativeName(name string, empty bool) bool {
	return utf8.ValidString(name) && (empty || strings.TrimSpace(name) != "") &&
		name == strings.TrimSpace(name) && len(utf16.Encode([]rune(name))) <= 20 &&
		strings.IndexFunc(name, unicode.IsControl) < 0
}

func (c SettingChange) Validate() error {
	canonical := SettingChange{Kind: c.Kind}
	switch c.Kind {
	case "group_info":
		if !settingID(c.Group) || !ValidNativeName(c.Text, false) ||
			strings.IndexFunc(c.Text, unicode.IsSpace) >= 0 {
			return rejection("native_setting_name_invalid")
		}
		canonical.Group, canonical.Text = c.Group, c.Text
	case "send_text":
		if !settingID(c.Group) || !validAssistantText(c.Text) {
			return rejection("native_setting_fields_invalid")
		}
		canonical.Group, canonical.Text = c.Group, c.Text
	case "recall_message":
		if !settingID(c.Group) || !ValidMessageReference(c.Text, "wsl", c.Group) {
			return rejection("native_setting_fields_invalid")
		}
		canonical.Group, canonical.Text = c.Group, c.Text
	case "mute_member", "unmute_member", "kick_member", "grant_admin", "revoke_admin":
		if !settingID(c.Group) || !settingID(c.Target) || c.Kind == "mute_member" && (c.Minutes < 1 || c.Minutes > 1440) {
			return rejection("native_moderation_request_invalid")
		}
		canonical.Group, canonical.Target = c.Group, c.Target
		if c.Kind == "mute_member" {
			canonical.Minutes = c.Minutes
		}
	case "mute_group", "unmute_group", "publish_notice":
		if !settingID(c.Group) {
			return rejection("native_moderation_request_invalid")
		}
		canonical.Group = c.Group
		if c.Kind == "publish_notice" {
			if !utf8.ValidString(c.Text) || strings.TrimSpace(c.Text) == "" || utf8.RuneCountInString(c.Text) > 600 ||
				strings.IndexFunc(c.Text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
				return rejection("native_moderation_request_invalid")
			}
			canonical.Text = c.Text
		}
	case "friend_approve", "friend_reject":
		if !settingID(c.Target) || !socialLogID(c.Text) || c.Group != "" && !settingID(c.Group) {
			return rejection("native_social_request_invalid")
		}
		canonical.Group, canonical.Target, canonical.Text = c.Group, c.Target, c.Text
	case "group_invite", "friend_delete":
		if !settingID(c.Group) || !settingID(c.Target) {
			return rejection("native_social_request_invalid")
		}
		canonical.Group, canonical.Target = c.Group, c.Target
	case "group_leave":
		if !settingID(c.Group) {
			return rejection("native_social_request_invalid")
		}
		canonical.Group = c.Group
	case "group_accept":
		if !settingID(c.Group) || !settingID(c.Target) || !socialLogID(c.Text) {
			return rejection("native_social_request_invalid")
		}
		canonical.Group, canonical.Target, canonical.Text = c.Group, c.Target, c.Text
	case "nickname":
		if !ValidNativeName(c.Text, false) {
			return rejection("native_setting_name_invalid")
		}
		canonical.Text = c.Text
	case "member_nickname":
		if !settingID(c.Group) || !settingID(c.Target) || !ValidNativeName(c.Text, true) {
			return rejection("native_setting_name_invalid")
		}
		canonical.Group, canonical.Target, canonical.Text = c.Group, c.Target, c.Text
	case "search":
		canonical.SearchAccount, canonical.SearchMobile = c.SearchAccount, c.SearchMobile
	case "friend_policy":
		// Question/answer policies require additional data, so they are read-only.
		if c.Text != "FRIEND_NEED_APPROVAL" && c.Text != "FRIEND_ANY_ONE" {
			return rejection("native_setting_policy_invalid")
		}
		canonical.Text = c.Text
	case "group_policy":
		if c.Text != "GROUP_REJECT_JOIN" && c.Text != "GROUP_APPROVAL_JOIN" && c.Text != "GROUP_ASK_JOIN" {
			return rejection("native_setting_policy_invalid")
		}
		canonical.Text = c.Text
	case "audio_video":
		canonical.Enabled = c.Enabled
	case "avatar", "password_sms", "password_verify", "password", "phone_old_sms", "phone_old_verify", "phone_new_sms", "phone":
		if len(c.Text) != 36 || strings.IndexFunc(c.Text, func(r rune) bool {
			return r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'f')
		}) >= 0 {
			return rejection("native_setting_fields_invalid")
		}
		canonical.Text = c.Text
	default:
		return rejection("native_setting_unsupported")
	}
	if canonical != c {
		return rejection("native_setting_fields_invalid")
	}
	return nil
}

func settingID(s string) bool {
	id, err := strconv.ParseUint(s, 10, 30)
	return err == nil && id > 0 && strconv.FormatUint(id, 10) == s
}

func SettingsDigest(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (c *Client) Settings(ctx context.Context) (AccountSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := c.request(ctx, "/v1/settings/query-app-settings", map[string]any{"v": "0"}, nil)
	if err != nil {
		return AccountSettings{}, err
	}
	out, err := parseAccountSettings(data)
	if err == nil && out.UID != strconv.FormatUint(c.fields[3], 10) {
		err = rejection("native_setting_identity")
	}
	return out, err
}

func parseAccountSettings(data map[string]json.RawMessage) (AccountSettings, error) {
	out := AccountSettings{UID: directoryID(data["uid"])}
	if !settingID(out.UID) {
		return out, rejection("native_setting_identity")
	}
	for key, target := range map[string]*string{
		"nickName": &out.Nickname, "avatar": &out.Avatar,
		"friendPolicy": &out.FriendPolicy, "groupInvitationPolicy": &out.GroupPolicy,
	} {
		if raw := data[key]; len(raw) > 0 && string(raw) != "null" {
			if json.Unmarshal(raw, target) != nil || !utf8.ValidString(*target) || len(*target) > 1024 {
				return out, rejection("native_setting_shape")
			}
		}
	}
	for key, target := range map[string]**bool{
		"searchableAccount": &out.SearchableAccount, "searchableMobile": &out.SearchableMobile,
		"audioVideoOpen": &out.AudioVideo,
	} {
		if raw := data[key]; len(raw) > 0 && string(raw) != "null" {
			if json.Unmarshal(raw, target) != nil {
				return out, rejection("native_setting_shape")
			}
		}
	}
	if len(out.Nickname) > 1024 || strings.IndexFunc(out.Nickname, unicode.IsControl) >= 0 {
		return out, rejection("native_setting_shape")
	}
	var phone struct {
		NationalNumber string `json:"nationalNumber"`
	}
	if raw := data["phone"]; len(raw) != 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &phone) != nil {
			return out, rejection("native_setting_shape")
		}
		if phoneDigits(phone.NationalNumber) {
			out.MaskedMobile = phone.NationalNumber[:3] + "****" + phone.NationalNumber[7:]
		}
	}
	return out, nil
}

func settingRoute(c SettingChange) (string, map[string]any, error) {
	if err := c.Validate(); err != nil {
		return "", nil, err
	}
	switch c.Kind {
	case "nickname":
		return "/v1/settings/self-nick-name", map[string]any{"nickName": c.Text}, nil
	case "member_nickname":
		group, _ := strconv.ParseUint(c.Group, 10, 30)
		target, _ := strconv.ParseUint(c.Target, 10, 30)
		return "/v1/group/set-member-nickname", map[string]any{"groupId": group, "userId": target, "nick": c.Text}, nil
	case "search":
		return "/v1/settings/p2p-search", map[string]any{"searchableAccount": c.SearchAccount, "searchableMobile": c.SearchMobile}, nil
	case "friend_policy":
		return "/v1/settings/p2p-verify", map[string]any{"policy": c.Text}, nil
	case "group_policy":
		return "/v1/settings/group-verify", map[string]any{"policy": c.Text}, nil
	case "audio_video":
		return "/v1/settings/audio-video-chat", map[string]any{"audioVideoOpen": c.Enabled}, nil
	default:
		return "", nil, rejection("native_security_flow_required")
	}
}

// ChangeSetting uses the existing desktop business signing/nonce machinery.
// The fence runs after encoding and durable nonce allocation, immediately at
// the single HTTP submission. Neither this method nor the HTTP client retries.
func (c *Client) ChangeSetting(ctx context.Context, change SettingChange, fence nativewire.SubmissionFence) error {
	route, params, err := settingRoute(change)
	if err != nil {
		return err
	}
	if fence == nil {
		return rejection("native_setting_fence_required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.request(context.WithValue(ctx, settingFenceKey{}, fence), route, params, nil)
	return err
}

type settingFenceKey struct{}

func nativeSettingsRoute(route string) bool {
	switch route {
	case "/v1/settings/query-app-settings", "/v1/group/get-group-info",
		"/v1/friend/friend-apply-list", "/v1/friend/get-friend-list",
		"/v1/group/notice-list", "/v1/group/get-apply-logs":
		return true
	}
	return false
}

func nativeSettingMutationRoute(route string) bool {
	switch route {
	case "/v1/settings/self-nick-name", "/v1/group/set-member-nickname", "/v1/group/set-group-name",
		"/v1/settings/p2p-search", "/v1/settings/p2p-verify",
		"/v1/settings/group-verify", "/v1/settings/audio-video-chat",
		"/v1/settings/avatar", "/v1/user/image-check", "/v1/verify/sms",
		"/v1/verify/verify", "/v1/settings/password", "/v1/settings/changePhone",
		"/v1/friend/friend-apply-handler", "/v1/friend/del-friend",
		"/v1/group/add-group-member", "/v1/group/leave-group", "/v1/group/group-member-invite",
		"/v1/group/set-member-mute", "/v1/group/member-mute-cancel", "/v1/group/set-group-mute",
		"/v1/group/remove-group-member", "/v1/group/add-notice",
		"/v1/group/add-group-manage", "/v1/group/del-group-manage":
		return true
	}
	return false
}

func (c *Client) NicknameLocked(ctx context.Context, group string) (bool, error) {
	if !settingID(group) {
		return false, rejection("native_member_group")
	}
	id, _ := strconv.ParseUint(group, 10, 30)
	data, err := c.Request(ctx, "/v1/group/get-group-info", map[string]any{"groupId": id, "v": "0"})
	if err != nil {
		return false, err
	}
	data, err = groupInfoFields(data, group)
	if err != nil {
		return false, err
	}
	var locked bool
	if json.Unmarshal(data["nicknameMode"], &locked) != nil {
		return false, rejection("native_setting_role_unverified")
	}
	return locked, nil
}

// The official response wraps GroupInfo; retain the reviewed flat shape while
// refusing ambiguous responses that supply two independent group identities.
func groupInfoFields(data map[string]json.RawMessage, group string) (map[string]json.RawMessage, error) {
	if raw, ok := data["groupInfo"]; ok {
		if len(data["groupId"]) != 0 {
			return nil, rejection("native_setting_identity")
		}
		var info map[string]json.RawMessage
		if json.Unmarshal(raw, &info) != nil || info == nil {
			return nil, rejection("native_setting_identity")
		}
		data = info
	}
	if directoryID(data["groupId"]) != group {
		return nil, rejection("native_setting_identity")
	}
	return data, nil
}
