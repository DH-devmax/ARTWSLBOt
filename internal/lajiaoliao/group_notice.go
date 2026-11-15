package lajiaoliao

import (
	"encoding/json"
	"strings"
)

type GroupNotice struct {
	Event   string          `json:"event"`
	Group   string          `json:"group_id"`
	Actor   *MessagePerson  `json:"actor,omitempty"`
	Targets []MessagePerson `json:"targets,omitempty"`
	Seconds int64           `json:"seconds,omitempty"`
}

// Only bounded display fields are projected. Full group/user ex and face URLs
// are intentionally ignored; notifications never become authorization evidence.
func readGroupNotice(kind int32, raw []byte, group string) (*MessageView, error) {
	event := map[int32]string{
		1502: "group_updated", 1504: "member_left", 1508: "member_kicked",
		1509: "member_invited", 1510: "member_joined", 1512: "member_muted",
		1513: "member_unmuted", 1514: "group_muted", 1515: "group_unmuted",
		1516: "member_updated", 1519: "announcement_updated", 1520: "name_updated",
		1521: "message_pinned", 1522: "message_unpinned",
	}[kind]
	if event == "" {
		return &MessageView{Kind: "unsupported", ContentType: kind}, nil
	}
	object, ok := messageObject(raw)
	if !ok || !messageFields(object, "detail") {
		return nil, &Failure{Code: "lajiaoliao_group_notice_shape"}
	}
	var detail string
	if json.Unmarshal(object["detail"], &detail) != nil {
		return nil, &Failure{Code: "lajiaoliao_group_notice_shape"}
	}
	fields, ok := messageObject([]byte(detail))
	if !ok {
		return nil, &Failure{Code: "lajiaoliao_group_notice_shape"}
	}
	var info struct {
		ID           string `json:"groupID"`
		Name         string `json:"groupName"`
		Notification string `json:"notification"`
	}
	if json.Unmarshal(fields["group"], &info) != nil || info.ID != group || !ValidID(info.ID) ||
		!validText(info.Name, 1000, true) || !validText(info.Notification, 65536, true) {
		return nil, &Failure{Code: "lajiaoliao_group_notice_shape"}
	}
	notice := &GroupNotice{Event: event, Group: group}
	person := func(raw json.RawMessage) (*MessagePerson, error) {
		var p struct {
			Group string `json:"groupID"`
			ID    string `json:"userID"`
			Name  string `json:"nickname"`
		}
		if json.Unmarshal(raw, &p) != nil || !ValidID(p.ID) || p.Group != "" && p.Group != group ||
			!validText(p.Name, 1000, true) {
			return nil, &Failure{Code: "lajiaoliao_group_notice_shape"}
		}
		return &MessagePerson{ID: p.ID, Name: p.Name}, nil
	}
	if value := fields["opUser"]; value != nil {
		var err error
		notice.Actor, err = person(value)
		if err != nil {
			return nil, err
		}
	}
	key := map[int32]string{1504: "quitUser", 1510: "entrantUser",
		1512: "mutedUser", 1513: "mutedUser", 1516: "changedUser"}[kind]
	if key != "" {
		p, err := person(fields[key])
		if err != nil {
			return nil, err
		}
		notice.Targets = append(notice.Targets, *p)
	}
	if kind == 1508 || kind == 1509 {
		key = "kickedUserList"
		if kind == 1509 {
			key = "invitedUserList"
		}
		var list []json.RawMessage
		if json.Unmarshal(fields[key], &list) != nil || len(list) == 0 || len(list) > 100 {
			return nil, &Failure{Code: "lajiaoliao_group_notice_shape"}
		}
		for _, value := range list {
			p, err := person(value)
			if err != nil {
				return nil, err
			}
			notice.Targets = append(notice.Targets, *p)
		}
	}
	if kind == 1512 && fields["mutedSeconds"] != nil {
		if json.Unmarshal(fields["mutedSeconds"], &notice.Seconds) != nil || notice.Seconds < 0 {
			return nil, &Failure{Code: "lajiaoliao_group_notice_shape"}
		}
	}
	view := &MessageView{Kind: "notice", ContentType: kind, Notice: notice}
	if kind == 1519 {
		view.Text = info.Notification
	} else if kind == 1520 {
		view.Text = info.Name
	}
	if len(notice.Targets) > 32 {
		notice.Targets, view.Truncated = notice.Targets[:32], true
	}
	view.Text = strings.TrimSpace(view.Text)
	return view, nil
}
