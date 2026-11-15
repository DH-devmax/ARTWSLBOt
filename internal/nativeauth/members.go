package nativeauth

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MemberPageLimit = 100
	MemberLimit     = 10000
	MemberByteLimit = 4 * 1024 * 1024
)

// GroupMember is a safe projection, not the upstream record. Unknown roles and
// missing transport IDs remain explicit and cannot authorize native actions.
type GroupMember struct {
	ID        string `json:"id"`
	Transport string `json:"transport"`
	Name      string `json:"name"`
	Card      string `json:"card"`
	Role      string `json:"role"`
}

type MemberDirectory struct {
	Group           string        `json:"group_id"`
	Members         []GroupMember `json:"members"`
	Pages           int           `json:"pages"`
	Complete        bool          `json:"complete"`
	MappingComplete bool          `json:"mapping_complete"`
}

type memberRequester func(context.Context, string, map[string]any) (map[string]json.RawMessage, error)

// Members performs bounded, serial reads of one business group. No partial
// roster is returned on errors, cursor loops, identity conflicts or cancellation.
func (c *Client) Members(ctx context.Context, group string) (MemberDirectory, error) {
	return readMembers(ctx, group, c.Request)
}

func readMembers(ctx context.Context, group string, request memberRequester) (MemberDirectory, error) {
	if directoryID(json.RawMessage(strconv.Quote(group))) != group || group == "" {
		return MemberDirectory{}, rejection("native_member_group")
	}
	id, _ := strconv.ParseUint(group, 10, 64)
	out := MemberDirectory{Group: group, Members: []GroupMember{}, MappingComplete: true}
	members, peers, cursors := map[string]GroupMember{}, map[string]string{}, map[string]bool{}
	cursor := ""
	size := 0
	for range MemberPageLimit {
		if err := ctx.Err(); err != nil {
			return MemberDirectory{}, err
		}
		params := map[string]any{"groupId": id, "v": "0"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		data, err := request(ctx, "/v1/group/get-group-members", params)
		if err != nil {
			return MemberDirectory{}, err
		}
		if raw := data["groupId"]; len(raw) != 0 && directoryID(raw) != group {
			return MemberDirectory{}, rejection("native_member_identity")
		}
		page, next, err := parseMemberPage(data)
		if err != nil {
			return MemberDirectory{}, err
		}
		for _, member := range page {
			if old, ok := members[member.ID]; ok {
				// Even a role/name change during pagination requires a new
				// snapshot; do not publish a mixed-version roster.
				if old != member {
					return MemberDirectory{}, rejection("native_member_identity")
				}
				continue
			}
			if member.Transport != "" && peers[member.Transport] != "" && peers[member.Transport] != member.ID {
				return MemberDirectory{}, rejection("native_member_identity")
			}
			members[member.ID] = member
			if member.Transport != "" {
				peers[member.Transport] = member.ID
			} else {
				out.MappingComplete = false
			}
			if len(members) > MemberLimit {
				return MemberDirectory{}, rejection("native_member_limit")
			}
			size += len(member.ID) + len(member.Transport) + len(member.Name) + len(member.Card) + 128
			if size > MemberByteLimit {
				return MemberDirectory{}, rejection("native_member_limit")
			}
			out.Members = append(out.Members, member)
		}
		out.Pages++
		if next == "" {
			if err := ctx.Err(); err != nil {
				return MemberDirectory{}, err
			}
			out.Complete = true
			return out, nil
		}
		if cursors[next] {
			return MemberDirectory{}, rejection("native_member_cursor")
		}
		cursors[next], cursor = true, next
	}
	return MemberDirectory{}, rejection("native_member_limit")
}

func parseMemberPage(data map[string]json.RawMessage) ([]GroupMember, string, error) {
	var rows []map[string]json.RawMessage
	if json.Unmarshal(data["groupMemberInfo"], &rows) != nil || rows == nil || len(rows) > MemberLimit {
		return nil, "", rejection("native_member_shape")
	}
	members := make([]GroupMember, 0, len(rows))
	for _, row := range rows {
		id := directoryID(row["userId"])
		if id == "" {
			return nil, "", rejection("native_member_identity")
		}
		peer := ""
		if raw := row["nimId"]; len(raw) != 0 && string(raw) != "null" {
			if json.Unmarshal(raw, &peer) != nil {
				peer = directoryID(raw)
				if peer == "" {
					return nil, "", rejection("native_member_identity")
				}
			}
			if len(peer) > 1024 || !utf8.ValidString(peer) || strings.IndexFunc(peer, func(r rune) bool {
				return unicode.IsSpace(r) || unicode.IsControl(r)
			}) >= 0 {
				return nil, "", rejection("native_member_identity")
			}
		}
		role := "unknown"
		for _, key := range []string{"groupRole", "role"} {
			raw := row[key]
			if len(raw) == 0 {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return nil, "", rejection("native_member_role")
			}
			// Do not guess numeric/SDK-role aliases from another protocol.
			next := map[string]string{"GROUP_ROLE_OWNER": "owner", "GROUP_ROLE_ADMIN": "admin", "GROUP_ROLE_MEMBER": "member"}[value]
			if next == "" {
				next = "unknown"
			}
			if role != "unknown" && role != next {
				return nil, "", rejection("native_member_role")
			}
			if key == "role" && len(row["groupRole"]) != 0 && role != next {
				return nil, "", rejection("native_member_role")
			}
			role = next
		}
		members = append(members, GroupMember{ID: id, Transport: peer, Role: role,
			Name: memberLabel(row, "userNick", "nickName", "nickname"),
			Card: memberLabel(row, "groupMemberNick", "nick", "groupNick", "cardName")})
	}
	var info map[string]json.RawMessage
	if raw := data["pageInfo"]; len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &info) != nil || info == nil {
			return nil, "", rejection("native_member_cursor")
		}
	}
	cursor, more := "", false
	for _, object := range []map[string]json.RawMessage{data, info} {
		for _, key := range []string{"nextCursor", "next_cursor", "nextPageToken"} {
			raw := object[key]
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) != nil || len(value) > 8192 || !utf8.ValidString(value) {
				return nil, "", rejection("native_member_cursor")
			}
			if value != "" {
				if cursor != "" && cursor != value {
					return nil, "", rejection("native_member_cursor")
				}
				cursor = value
			}
		}
		if raw := object["hasMore"]; len(raw) > 0 {
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return nil, "", rejection("native_member_cursor")
			}
			more = more || value
		}
	}
	if more && cursor == "" {
		return nil, "", rejection("native_member_incomplete")
	}
	return members, cursor, nil
}

func memberLabel(row map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var value string
		if json.Unmarshal(row[key], &value) == nil && value != "" {
			value = strings.Join(strings.Fields(value), " ")
			runes := []rune(value)
			return string(runes[:min(len(runes), 256)])
		}
	}
	return ""
}
