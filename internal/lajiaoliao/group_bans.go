package lajiaoliao

import (
	"context"
	"encoding/json"
)

// Native group admission bans are separate from friend and business blacklists.
type GroupBan struct {
	Group     string `json:"groupID"`
	User      string `json:"userID"`
	Name      string `json:"nickname"`
	BlockedBy string `json:"blockedBy"`
	BlockedAt int64  `json:"blockedAt"`
}

func BanAction(kind string) bool {
	return kind == "kick_member_ban" || kind == "remove_group_ban"
}

func (c *Client) GroupBans(ctx context.Context, s Session, group string, page int) ([]GroupBan, int, error) {
	if !ValidID(group) || page < 1 || page > 100 {
		return nil, 0, &Failure{Code: "lajiaoliao_invalid_page"}
	}
	raw, err := c.request(ctx, "/group/get_group_permanent_ban_list", map[string]any{
		"groupID": group, "pagination": map[string]int{"pageNumber": page, "showNumber": 100},
	}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Total int        `json:"total"`
		Bans  []GroupBan `json:"bans"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Total < 0 || result.Total > 10000 ||
		len(result.Bans) > 100 || len(result.Bans) > result.Total {
		return nil, 0, &Failure{Code: "lajiaoliao_group_ban_shape"}
	}
	seen := map[string]bool{}
	for _, row := range result.Bans {
		if row.Group != group || !ValidID(row.User) || seen[row.User] ||
			!validText(row.Name, 1000, true) || !ValidID(row.BlockedBy) || row.BlockedAt <= 0 {
			return nil, 0, &Failure{Code: "lajiaoliao_group_ban_shape"}
		}
		seen[row.User] = true
	}
	return append([]GroupBan{}, result.Bans...), result.Total, nil
}

func (c *Client) hasGroupBan(ctx context.Context, s Session, group, user string) (bool, error) {
	seen := map[string]bool{}
	for page := 1; page <= 100; page++ {
		rows, total, err := c.GroupBans(ctx, s, group, page)
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if seen[row.User] {
				return false, &Failure{Code: "lajiaoliao_group_ban_shape"}
			}
			seen[row.User] = true
		}
		if len(seen) == total {
			return seen[user], nil
		}
		if len(rows) < 100 {
			break
		}
	}
	return false, &Failure{Code: "lajiaoliao_group_ban_incomplete"}
}
