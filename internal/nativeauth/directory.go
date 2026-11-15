package nativeauth

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

type DirectoryGroup struct {
	ID     string `json:"id"`
	Team   string `json:"team"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Avatar string `json:"avatar,omitempty"`
}

func directoryID(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		var n json.Number
		if json.Unmarshal(raw, &n) != nil {
			return ""
		}
		value = string(n)
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != value {
		return ""
	}
	return value
}

// Directory performs one fixed read. It does not read member rosters, infer
// administrator status from membership, grant SaaS ownership or mutate groups.
func (c *Client) Directory(ctx context.Context, uid uint64) ([]DirectoryGroup, error) {
	data, err := c.Request(ctx, "/v1/group/get-group-list", map[string]any{"v": "0"})
	if err != nil {
		return nil, err
	}
	return parseDirectory(data, uid)
}

func parseDirectory(data map[string]json.RawMessage, uid uint64) ([]DirectoryGroup, error) {
	groups := []DirectoryGroup{}
	seen, teams := map[string]string{}, map[string]string{}
	for _, kind := range []string{"owner", "member"} {
		var entries []map[string]json.RawMessage
		if len(data[kind]) == 0 || json.Unmarshal(data[kind], &entries) != nil || entries == nil || len(entries) > 10000 {
			return nil, rejection("native_directory_shape")
		}
		for _, row := range entries {
			id, team := directoryID(row["groupId"]), directoryID(row["groupCloudId"])
			if id == "" || team == "" || seen[id] != "" && seen[id] != team || teams[team] != "" && teams[team] != id {
				return nil, rejection("native_directory_identity")
			}
			if seen[id] != "" {
				continue
			}
			name := ""
			_ = json.Unmarshal(row["groupName"], &name)
			if name == "" {
				_ = json.Unmarshal(row["name"], &name)
			}
			name = strings.Join(strings.Fields(name), " ")
			if len([]rune(name)) > 256 {
				name = string([]rune(name)[:256])
			}
			if name == "" {
				name = id
			}
			role := "unknown"
			var me struct {
				Role string `json:"role"`
			}
			if json.Unmarshal(row["me"], &me) == nil {
				role = map[string]string{"GROUP_ROLE_OWNER": "owner", "GROUP_ROLE_ADMIN": "admin", "GROUP_ROLE_MEMBER": "member"}[me.Role]
			}
			if role == "" {
				role = "unknown"
			}
			owner := ""
			for _, key := range []string{"ownerUserId", "groupOwnerId", "ownerId"} {
				if owner = directoryID(row[key]); owner != "" {
					break
				}
			}
			if owner == strconv.FormatUint(uid, 10) {
				if role == "unknown" {
					role = "owner"
				} else if role != "owner" {
					role = "unknown"
				}
			}
			seen[id], teams[team] = team, id
			avatar := directoryAvatar(row["groupAvatar"])
			groups = append(groups, DirectoryGroup{ID: id, Team: team, Name: name, Role: role, Avatar: avatar})
		}
	}
	return groups, nil
}

func directoryAvatar(raw json.RawMessage) string {
	var avatar string
	if json.Unmarshal(raw, &avatar) != nil {
		avatar = directoryID(raw)
	}
	asset := strings.TrimSuffix(avatar, ".jpg")
	n, err := strconv.ParseUint(asset, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != asset {
		return ""
	}
	return avatar
}
