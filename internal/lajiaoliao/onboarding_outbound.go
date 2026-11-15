package lajiaoliao

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// OnboardingOutboundTarget is contact information, never website identity proof.
type OnboardingOutboundTarget struct {
	ID            string `json:"id"`
	Account       string `json:"account"`
	Name          string `json:"name"`
	IsFriend      bool   `json:"is_friend"`
	CanRequest    bool   `json:"can_request"`
	BlockedReason string `json:"blocked_reason,omitempty"`
}

type OnboardingOutboundEvidence struct {
	OperationID   string `json:"operation_id"`
	ApplicationID string `json:"application_id,omitempty"`
	SenderID      string `json:"sender_id"`
	RecipientID   string `json:"recipient_id"`
	State         string `json:"state"`
	IsFriend      bool   `json:"is_friend"`
}

type OnboardingOutboundRequest struct {
	OperationID string
	SelfID      string
	Target      OnboardingOutboundTarget
	ExpiresAt   time.Time
}

type OnboardingOutboundFence func(context.Context, OnboardingOutboundTarget, func(context.Context) error) error

type OnboardingLookupError struct {
	Stage string
	Cause error
}

func (e *OnboardingLookupError) Error() string { return "lajiaoliao_onboarding_lookup_failed" }
func (e *OnboardingLookupError) Unwrap() error { return e.Cause }

func ValidOnboardingOutboundAccount(account string) bool {
	return ValidID(account) && strings.IndexFunc(account, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) < 0
}

// LookupOnboardingOutboundFriend follows the desktop's account search, then
// reads only the selected native relationship. Phone/profile fields stay local.
func (c *Client) LookupOnboardingOutboundFriend(ctx context.Context, s Session, account string) (OnboardingOutboundTarget, error) {
	if !ValidID(s.UserID) || !ValidOnboardingOutboundAccount(account) {
		return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_invalid"}
	}
	raw, err := c.request(ctx, "/user/search/full", map[string]any{
		"keyword": account, "normal": 0,
		"pagination": map[string]int{"pageNumber": 1, "showNumber": 1},
	}, s.ChatToken, "3", s.DeviceID)
	if err != nil {
		return OnboardingOutboundTarget{}, &OnboardingLookupError{Stage: "search", Cause: err}
	}
	defer clear(raw)
	target, err := parseOnboardingOutboundSearch(raw, s.UserID, account)
	if err != nil {
		return OnboardingOutboundTarget{}, &OnboardingLookupError{Stage: "search", Cause: err}
	}
	target.IsFriend, err = c.onboardingOutboundIsFriend(ctx, s, target.ID)
	if err != nil {
		return OnboardingOutboundTarget{}, &OnboardingLookupError{Stage: "friend_info", Cause: err}
	}
	target.CanRequest = !target.IsFriend
	return target, nil
}

func parseOnboardingOutboundSearch(raw json.RawMessage, self, account string) (OnboardingOutboundTarget, error) {
	var result struct {
		Total *int `json:"total"`
		Users *[]struct {
			ID      string          `json:"userID"`
			Account string          `json:"account"`
			Name    string          `json:"nickname"`
			Banned  json.RawMessage `json:"isBanned"`
			Status  json.RawMessage `json:"accountStatus"`
		} `json:"users"`
	}
	if !ValidID(self) || !ValidOnboardingOutboundAccount(account) ||
		json.Unmarshal(raw, &result) != nil || result.Total == nil || *result.Total < 0 ||
		result.Users == nil || len(*result.Users) > 1 || len(*result.Users) > *result.Total {
		return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
	if *result.Total == 0 {
		return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_not_found"}
	}
	if *result.Total != 1 || len(*result.Users) != 1 {
		return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_ambiguous"}
	}
	user := (*result.Users)[0]
	if !ValidID(user.ID) || user.Account != "" && !ValidOnboardingOutboundAccount(user.Account) ||
		!validText(user.Name, 1000, true) {
		return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
	// The desktop also searches phone/nickname. Only an exact account or native
	// string userID match may become this single onboarding candidate.
	if user.Account != account && user.ID != account {
		return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_not_found"}
	}
	if user.ID == self {
		return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_self"}
	}
	if len(user.Status) != 0 {
		var status int
		if json.Unmarshal(user.Status, &status) != nil {
			var text string
			if json.Unmarshal(user.Status, &text) != nil {
				return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
			}
			var err error
			status, err = strconv.Atoi(text)
			if err != nil || strconv.Itoa(status) != text {
				return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
			}
		}
		if status != 0 {
			return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_unavailable"}
		}
	}
	banned, err := onboardingOutboundBanned(user.Banned)
	if err != nil {
		return OnboardingOutboundTarget{}, err
	}
	if banned {
		return OnboardingOutboundTarget{}, &Failure{Code: "lajiaoliao_onboarding_outbound_unavailable"}
	}
	return OnboardingOutboundTarget{ID: user.ID, Account: account, Name: user.Name}, nil
}

func onboardingOutboundBanned(raw json.RawMessage) (bool, error) {
	// The pinned desktop normalizes only these explicit boolean representations.
	switch strings.TrimSpace(string(raw)) {
	case "", "null", "false", "0", `"false"`, `"0"`:
		return false, nil
	case "true", "1", `"true"`, `"1"`:
		return true, nil
	default:
		return false, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
}

func (c *Client) onboardingOutboundIsFriend(ctx context.Context, s Session, target string) (bool, error) {
	if !ValidID(s.UserID) || !ValidID(target) || target == s.UserID {
		return false, &Failure{Code: "lajiaoliao_onboarding_outbound_invalid"}
	}
	raw, err := c.request(ctx, "/friend/get_designated_friends", map[string]any{
		"ownerUserID": s.UserID, "friendUserIDs": []string{target},
	}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return false, err
	}
	defer clear(raw)
	var result struct {
		Friends json.RawMessage `json:"friendsInfo"`
	}
	var friends []struct {
		Owner string `json:"ownerUserID"`
		User  struct {
			ID   string `json:"userID"`
			Name string `json:"nickname"`
		} `json:"friendUser"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return false, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
	if len(result.Friends) == 0 && onboardingOutboundEmptyFriends(raw) {
		return false, nil
	}
	if len(result.Friends) == 0 ||
		json.Unmarshal(result.Friends, &friends) != nil || len(friends) > 1 {
		return false, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
	if len(friends) == 0 {
		return false, nil
	}
	friend := friends[0]
	if friend.Owner != s.UserID || friend.User.ID != target || !validText(friend.User.Name, 1000, true) {
		return false, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
	return true, nil
}

func onboardingOutboundEmptyFriends(raw json.RawMessage) bool {
	// request has already verified this fixed route's successful envelope.
	// The SDK accepts an empty object or absent/null data as no relationships.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	if len(fields) == 0 {
		return true
	}
	for name := range fields {
		switch name {
		case "errCode", "errMsg", "errDlt", "data":
		default:
			return false
		}
	}
	var code *int
	if json.Unmarshal(fields["errCode"], &code) != nil || code == nil || *code != 0 {
		return false
	}
	data, present := fields["data"]
	return !present || strings.TrimSpace(string(data)) == "null"
}
