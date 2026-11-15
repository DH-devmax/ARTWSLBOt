package nativeauth

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

type InvitationRecord struct {
	ID        string     `json:"id"`
	Group     string     `json:"group_id"`
	Type      string     `json:"type"`
	State     string     `json:"state"`
	Applicant Friend     `json:"applicant"`
	Inviter   *Friend    `json:"inviter,omitempty"`
	At        *time.Time `json:"at,omitempty"`
}

// The account-wide route rejects groupId/v. One returned server window is not
// a complete history; its lastId is retained without inventing pagination.
func (c *Client) InvitationRecords(ctx context.Context) ([]InvitationRecord, string, error) {
	data, err := c.Request(ctx, "/v1/group/get-apply-logs", map[string]any{"lastId": "0"})
	if err != nil {
		return nil, "", err
	}
	return parseInvitationRecords(data)
}

func parseInvitationRecords(data map[string]json.RawMessage) ([]InvitationRecord, string, error) {
	type member struct {
		ID   json.RawMessage `json:"userId"`
		Name json.RawMessage `json:"nick"`
	}
	var rows []struct {
		ID        json.RawMessage `json:"applyId"`
		Group     json.RawMessage `json:"groupId"`
		Type      json.RawMessage `json:"groupApplyType"`
		State     json.RawMessage `json:"state"`
		Applicant member          `json:"applicant"`
		Inviter   *member         `json:"inviter"`
		At        json.RawMessage `json:"applyTime"`
	}
	cursor, validCursor := invitationCursor(data["lastId"])
	if !validCursor ||
		json.Unmarshal(data["list"], &rows) != nil || rows == nil || len(rows) > 4096 {
		return nil, "", rejection("native_invitation_shape")
	}
	out, seen := []InvitationRecord{}, map[string]bool{}
	for _, row := range rows {
		id, group, uid := directoryID(row.ID), directoryID(row.Group), directoryID(row.Applicant.ID)
		name, err := socialName(row.Applicant.Name)
		kind := socialEnum(row.Type, []string{"APPLY", "INVITATION", "KICK_MEMBER"})
		state := socialEnum(row.State, []string{"MEMBER_STATE_NONE", "MEMBER_STATE_INVITED", "MEMBER_STATE_APPLIED",
			"MEMBER_STATE_REJECTED", "MEMBER_STATE_GOOD", "MEMBER_STATE_MANAGER_REJECT",
			"MEMBER_STATE_KICKED", "MEMBER_STATE_QUIT", "MEMBER_STATE_DELETE"})
		if err != nil || !socialLogID(id) || !settingID(group) || !settingID(uid) || seen[id] || kind == "" || state == "" {
			return nil, "", rejection("native_invitation_shape")
		}
		item := InvitationRecord{ID: id, Group: group, Applicant: Friend{ID: uid, Name: name}, Type: kind, State: state}
		if row.Inviter != nil {
			inviter, err := socialName(row.Inviter.Name)
			id := directoryID(row.Inviter.ID)
			if err != nil || !settingID(id) {
				return nil, "", rejection("native_invitation_shape")
			}
			item.Inviter = &Friend{ID: id, Name: inviter}
		}
		var stamp string
		if len(row.At) != 0 && string(row.At) != "null" && json.Unmarshal(row.At, &stamp) != nil {
			return nil, "", rejection("native_invitation_shape")
		}
		if stamp != "" {
			at, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil {
				return nil, "", rejection("native_invitation_shape")
			}
			item.At = &at
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out, cursor, nil
}

func invitationCursor(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return "", false
		}
		value = number.String()
	}
	id, err := strconv.ParseUint(value, 10, 63)
	return value, err == nil && strconv.FormatUint(id, 10) == value
}
