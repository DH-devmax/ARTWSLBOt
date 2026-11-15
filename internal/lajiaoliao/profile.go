package lajiaoliao

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type Notifications struct {
	Detail    *bool  `json:"showMessageDetail"`
	Receive   *bool  `json:"receiveNewMessage"`
	Group     *bool  `json:"groupMessageNotification"`
	Sound     string `json:"notificationSound"`
	Vibration *bool  `json:"vibration"`
}

func (n Notifications) Complete() bool {
	return n.Detail != nil && n.Receive != nil && n.Group != nil && n.Vibration != nil &&
		validText(n.Sound, 64, false) && n.Sound == strings.TrimSpace(n.Sound)
}

type ProfileDetails struct {
	Gender    *int           `json:"gender,omitempty"`
	Birth     *int64         `json:"birth,omitempty"`
	Signature *string        `json:"signature,omitempty"`
	Region    string         `json:"region,omitempty"`
	Push      *Notifications `json:"notifications,omitempty"`
}

type Profile struct {
	ID            string `json:"userID"`
	Name          string `json:"nickname"`
	Avatar        string `json:"faceURL"`
	SearchAccount *bool
	SearchPhone   *bool
	FriendPolicy  string
	GroupPolicy   string
	AudioVideo    *bool
	Details       *ProfileDetails
	MaskedPhone   string
	ex            map[string]json.RawMessage
}

func (c *Client) BasicProfile(ctx context.Context, s Session) (Profile, error) {
	raw, err := c.request(ctx, "/user/get_users_info", map[string]any{"userIDs": []string{s.UserID}}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return Profile{}, err
	}
	defer clear(raw)
	var result struct {
		Users []struct {
			ID     string `json:"userID"`
			Name   string `json:"nickname"`
			Avatar string `json:"faceURL"`
		} `json:"usersInfo"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Users) != 1 ||
		result.Users[0].ID != s.UserID || !validText(result.Users[0].Name, 1000, true) ||
		len(result.Users[0].Avatar) > 2048 {
		return Profile{}, &Failure{Code: "lajiaoliao_profile_shape"}
	}
	user := result.Users[0]
	return Profile{ID: user.ID, Name: user.Name, Avatar: user.Avatar}, nil
}

func (c *Client) Profile(ctx context.Context, s Session) (Profile, error) {
	var out Profile
	raw, err := c.request(ctx, "/user/get_users_info", map[string]any{"userIDs": []string{s.UserID}}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return out, err
	}
	var result struct {
		Users []struct {
			ID     string `json:"userID"`
			Name   string `json:"nickname"`
			Avatar string `json:"faceURL"`
			Ex     string `json:"ex"`
		} `json:"usersInfo"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Users) != 1 || result.Users[0].ID != s.UserID {
		return out, &Failure{Code: "lajiaoliao_profile_shape"}
	}
	user := result.Users[0]
	if !validText(user.Name, 1000, true) || len(user.Avatar) > 2048 || len(user.Ex) > 8192 {
		return out, &Failure{Code: "lajiaoliao_profile_shape"}
	}
	out.ID, out.Name, out.Avatar = user.ID, user.Name, user.Avatar
	out.ex = map[string]json.RawMessage{}
	if user.Ex != "" && (json.Unmarshal([]byte(user.Ex), &out.ex) != nil || out.ex == nil || len(out.ex) > 64) {
		return out, &Failure{Code: "lajiaoliao_profile_shape"}
	}
	if v, ok := out.ex["allowGroupInvite"]; ok {
		var mode int
		if json.Unmarshal(v, &mode) != nil || (mode != 0 && mode != 1) {
			return out, &Failure{Code: "lajiaoliao_profile_shape"}
		}
		out.GroupPolicy = map[int]string{0: "GROUP_REJECT_JOIN", 1: "GROUP_APPROVAL_JOIN"}[mode]
	}
	if v, ok := out.ex["disableAudioVideoCalls"]; ok {
		var disabled bool
		if json.Unmarshal(v, &disabled) != nil {
			return out, &Failure{Code: "lajiaoliao_profile_shape"}
		}
		enabled := !disabled
		out.AudioVideo = &enabled
	}
	raw, err = c.request(ctx, "/friend/privacy/get", struct{}{}, s.ChatToken, "3", s.DeviceID)
	if err != nil {
		return out, err
	}
	var privacy struct {
		Policy  *int            `json:"addFriendPolicy"`
		Account *bool           `json:"findByLajiaoID"`
		Phone   *bool           `json:"findByPhone"`
		Privacy json.RawMessage `json:"privacy"`
	}
	if json.Unmarshal(raw, &privacy) != nil {
		return out, &Failure{Code: "lajiaoliao_profile_shape"}
	}
	if len(privacy.Privacy) > 0 {
		if json.Unmarshal(privacy.Privacy, &privacy) != nil {
			return out, &Failure{Code: "lajiaoliao_profile_shape"}
		}
	}
	out.SearchAccount, out.SearchPhone = privacy.Account, privacy.Phone
	if privacy.Policy != nil {
		out.FriendPolicy = map[int]string{1: "FRIEND_NEED_APPROVAL", 2: "FRIEND_ANY_ONE", 3: "FRIEND_REJECT_ALL"}[*privacy.Policy]
		if out.FriendPolicy == "" {
			return out, &Failure{Code: "lajiaoliao_profile_shape"}
		}
	}
	return out, nil
}

// FullProfile reads business fields only for the authenticated self. Neither the
// arbitrary upstream object nor its unmasked phone leaves this adapter.
func (c *Client) FullProfile(ctx context.Context, s Session) (Profile, error) {
	out, err := c.Profile(ctx, s)
	if err != nil {
		return out, err
	}
	detail, phone, area, err := c.selfDetails(ctx, s)
	if err != nil {
		return out, err
	}
	if phoneNumber(phone) && (area == "+86" || area == "86" || area == "") {
		out.MaskedPhone = "+86 " + phone[:3] + "****" + phone[7:]
	}
	raw, err := c.request(ctx, "/push_config/get", struct{}{}, s.ChatToken, "3", s.DeviceID)
	if err != nil {
		return out, err
	}
	var push Notifications
	if json.Unmarshal(raw, &push) != nil {
		return out, &Failure{Code: "lajiaoliao_profile_shape"}
	}
	detail.Push = &push
	out.Details = &detail
	return out, nil
}

func (c *Client) selfDetails(ctx context.Context, s Session) (ProfileDetails, string, string, error) {
	raw, err := c.request(ctx, "/user/find/full", map[string]any{"userIDs": []string{s.UserID}}, s.ChatToken, "3", s.DeviceID)
	if err != nil {
		return ProfileDetails{}, "", "", err
	}
	defer clear(raw)
	var result struct {
		Users []struct {
			ID string `json:"userID"`
			ProfileDetails
			Phone    string `json:"phoneNumber"`
			Area     string `json:"areaCode"`
			Country  string `json:"countryName"`
			Province string `json:"provinceName"`
			City     string `json:"cityName"`
		} `json:"users"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Users) != 1 || result.Users[0].ID != s.UserID {
		return ProfileDetails{}, "", "", &Failure{Code: "lajiaoliao_profile_shape"}
	}
	user := result.Users[0]
	if user.Gender != nil && (*user.Gender < 0 || *user.Gender > 2) ||
		user.Birth != nil && (*user.Birth < 0 || *user.Birth > time.Now().Add(24*time.Hour).UnixMilli()) ||
		user.Signature != nil && !validText(*user.Signature, 4096, true) {
		return ProfileDetails{}, "", "", &Failure{Code: "lajiaoliao_profile_shape"}
	}
	parts := []string{}
	for _, part := range []string{user.Country, user.Province, user.City} {
		if !validText(part, 128, true) {
			return ProfileDetails{}, "", "", &Failure{Code: "lajiaoliao_profile_shape"}
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	detail := user.ProfileDetails
	detail.Region = strings.Join(parts, " / ")
	// Notifications embedded in a user object are not a push-config observation.
	detail.Push = nil
	return detail, user.Phone, user.Area, nil
}

func ValidBirth(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && strconv.FormatInt(n, 10) == value && n > 0 &&
		n <= time.Now().Add(24*time.Hour).UnixMilli()
}

func (c *Client) ChangeProfile(ctx context.Context, s Session, change Change,
	fence func(context.Context, func(context.Context) error) error) error {
	if err := change.Validate(); err != nil {
		return err
	}
	if fence == nil {
		return &Failure{Code: "lajiaoliao_fence_required"}
	}
	route, token, platform := "", s.ChatToken, "3"
	payload := map[string]any{}
	switch change.Kind {
	case "nickname":
		route = "/user/update"
		payload["userID"], payload["nickname"] = s.UserID, change.Text
	case "profile_gender", "profile_birth", "profile_signature":
		route = "/user/update"
		payload["userID"] = s.UserID
		switch change.Kind {
		case "profile_gender":
			payload["gender"], _ = strconv.Atoi(change.Text)
		case "profile_birth":
			payload["birth"], _ = strconv.ParseInt(change.Text, 10, 64)
		case "profile_signature":
			payload["signature"] = change.Text
		}
	case "push_notifications":
		route = "/push_config/set"
		var push Notifications
		_ = json.Unmarshal([]byte(change.Text), &push)
		raw, err := c.request(ctx, "/push_config/get", struct{}{}, s.ChatToken, "3", s.DeviceID)
		if err != nil {
			return err
		}
		var current Notifications
		if json.Unmarshal(raw, &current) != nil || !current.Complete() || current.Sound != push.Sound {
			return &Failure{Code: "lajiaoliao_profile_shape"}
		}
		encoded, _ := json.Marshal(push)
		_ = json.Unmarshal(encoded, &payload)
	case "search":
		route = "/friend/privacy/set"
		payload["findByLajiaoID"], payload["findByPhone"] = change.SearchAccount, change.SearchMobile
	case "friend_policy":
		route = "/friend/privacy/set"
		payload["addFriendPolicy"] = map[string]int{"FRIEND_NEED_APPROVAL": 1, "FRIEND_ANY_ONE": 2, "FRIEND_REJECT_ALL": 3}[change.Text]
	case "group_policy", "audio_video":
		profile, err := c.Profile(ctx, s)
		if err != nil {
			return err
		}
		if change.Kind == "group_policy" {
			value := 0
			if change.Text == "GROUP_APPROVAL_JOIN" {
				value = 1
			}
			profile.ex["allowGroupInvite"], _ = json.Marshal(value)
		} else {
			profile.ex["disableAudioVideoCalls"], _ = json.Marshal(!change.Enabled)
		}
		ex, err := json.Marshal(profile.ex)
		if err != nil {
			return err
		}
		defer clear(ex)
		route, token, platform = "/user/update_user_info_ex", s.IMToken, "5"
		payload["userInfo"] = map[string]any{"userID": s.UserID, "ex": string(ex)}
	default:
		return &Failure{Code: "lajiaoliao_action_unavailable"}
	}
	return fence(ctx, func(work context.Context) error {
		_, err := c.request(work, route, payload, token, platform, s.DeviceID)
		return err
	})
}
