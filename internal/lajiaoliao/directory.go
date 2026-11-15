// SPDX-License-Identifier: AGPL-3.0-or-later
package lajiaoliao

import (
	"context"
	"encoding/json"
	"strings"
)

type Group struct {
	ID          string `json:"groupID"`
	Name        string `json:"groupName"`
	Status      int    `json:"status"`
	MemberCount int    `json:"memberCount"`
}
type Member struct {
	GroupID string `json:"groupID"`
	UserID  string `json:"userID"`
	Name    string `json:"nickname"`
	Role    int    `json:"roleLevel"`
	MuteEnd int64  `json:"muteEndTime"`
}

func validID(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n")
}

// Page bounds prevent an unexpected directory from monopolizing an account's
// serial read lane. A full last page is explicitly incomplete.
func (c *Client) Groups(ctx context.Context, s Session, page int) ([]Group, bool, error) {
	if page < 1 || page > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_invalid_page"}
	}
	raw, err := c.request(ctx, "/group/get_joined_group_list", map[string]any{"userID": s.UserID,
		"pagination": map[string]int{"pageNumber": page, "showNumber": 100}}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nil, false, err
	}
	var result struct {
		Groups *[]Group `json:"groups"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Groups == nil || len(*result.Groups) > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_directory_shape"}
	}
	seen := map[string]bool{}
	for _, g := range *result.Groups {
		if !validID(g.ID) || len(g.Name) > 1000 || g.MemberCount < 0 || seen[g.ID] {
			return nil, false, &Failure{Code: "lajiaoliao_directory_shape"}
		}
		seen[g.ID] = true
	}
	return *result.Groups, len(*result.Groups) == 100, nil
}

func (c *Client) Members(ctx context.Context, s Session, group string, page int) ([]Member, bool, error) {
	if !validID(group) || page < 1 || page > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_invalid_page"}
	}
	raw, err := c.request(ctx, "/group/get_group_member_list", map[string]any{"groupID": group,
		"userID": s.UserID, "filter": 0, "pagination": map[string]int{"pageNumber": page, "showNumber": 100}},
		s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nil, false, err
	}
	var result struct {
		Members *[]Member `json:"members"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Members == nil || len(*result.Members) > 100 {
		return nil, false, &Failure{Code: "lajiaoliao_directory_shape"}
	}
	seen := map[string]bool{}
	for _, m := range *result.Members {
		if m.GroupID != group || !validID(m.UserID) || len(m.Name) > 1000 || seen[m.UserID] ||
			(m.Role != 20 && m.Role != 60 && m.Role != 100) {
			return nil, false, &Failure{Code: "lajiaoliao_membership_unverified"}
		}
		seen[m.UserID] = true
	}
	return *result.Members, len(*result.Members) == 100, nil
}

func (c *Client) Member(ctx context.Context, s Session, group, user string) (Member, error) {
	if !validID(group) || !validID(user) {
		return Member{}, &Failure{Code: "lajiaoliao_invalid_identity"}
	}
	raw, err := c.request(ctx, "/group/get_group_members_info", map[string]any{"groupID": group, "userIDs": []string{user}},
		s.IMToken, "5", s.DeviceID)
	if err != nil {
		return Member{}, err
	}
	var result struct {
		Members []Member `json:"members"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Members) != 1 || result.Members[0].GroupID != group ||
		result.Members[0].UserID != user || (result.Members[0].Role != 20 && result.Members[0].Role != 60 && result.Members[0].Role != 100) {
		return Member{}, &Failure{Code: "lajiaoliao_membership_unverified"}
	}
	return result.Members[0], nil
}

// The caller owns durable intent, live SaaS authorization and the final fence.
// This adapter cannot select arbitrary routes or automatically retry mutations.
func (c *Client) Mute(ctx context.Context, s Session, group, user string, seconds int,
	fence func(context.Context, func(context.Context) error) error) error {
	if fence == nil || user == s.UserID || seconds < 0 || seconds > 86400 || (seconds != 0 && (seconds < 60 || seconds%60 != 0)) {
		return &Failure{Code: "lajiaoliao_invalid_action"}
	}
	self, err := c.Member(ctx, s, group, s.UserID)
	if err != nil {
		return err
	}
	target, err := c.Member(ctx, s, group, user)
	if err != nil {
		return err
	}
	if (self.Role != 60 && self.Role != 100) || target.Role != 20 {
		return &Failure{Code: "lajiaoliao_native_role_required"}
	}
	route := "/group/cancel_mute_group_member"
	payload := map[string]any{"groupID": group, "userID": user}
	if seconds > 0 {
		route = "/group/mute_group_member"
		payload["mutedSeconds"] = seconds
	}
	return fence(ctx, func(write context.Context) error {
		_, err := c.request(write, route, payload, s.IMToken, "5", s.DeviceID)
		return err
	})
}
