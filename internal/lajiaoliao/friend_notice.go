package lajiaoliao

import (
	"encoding/json"
	"strings"

	"github.com/openimsdk/protocol/sdkws"
)

// FriendNotice is display-only. Its parties follow the message envelope, not
// the approval/request direction, and never establish identity or friend proof.
// Friend is the separate partner snapshot on an owner-addressed self-sync.
type FriendNotice struct {
	Event     string         `json:"event"`
	Sender    MessagePerson  `json:"sender"`
	Recipient MessagePerson  `json:"recipient"`
	Friend    *MessagePerson `json:"friend,omitempty"`
}

// SystemNotice labels an informational native event. It contains no message
// status, receipt sequence, conversation scope or onboarding evidence.
type SystemNotice struct {
	Event string `json:"event"`
}

// OpenIM SDK 3.8.3 reads these Tips through NotificationElem.detail; the pinned
// protocol v0.0.73 adds the optional request snapshot on applications/approvals.
// Unknown shapes retain their native type without exposing upstream notes.
func readFriendNotice(kind int32, raw []byte, sender, recipient string) *MessageView {
	out := &MessageView{Kind: "unsupported", ContentType: kind}
	event := map[int32]string{1201: "friend_approved", 1203: "friend_request", 1204: "friend_added"}[kind]
	if event == "" {
		return out
	}
	wrapper, ok := friendNoticeObject(raw, "detail")
	var detail string
	if !ok || json.Unmarshal(wrapper["detail"], &detail) != nil {
		return out
	}
	fields, ok := messageObject([]byte(detail))
	if !ok {
		return out
	}
	notice := &FriendNotice{Event: event, Sender: MessagePerson{ID: sender}, Recipient: MessagePerson{ID: recipient}}
	var from, to, friendName string
	if kind == 1201 || kind == 1203 {
		if kind == 1201 {
			var tips sdkws.FriendApplicationApprovedTips
			if !messageFields(fields, "fromToUserID", "handleMsg", "friendVersion", "friendVersionID", "request") ||
				json.Unmarshal([]byte(detail), &tips) != nil {
				return out
			}
		} else if !messageFields(fields, "fromToUserID", "request") {
			return out
		}
		if _, ok := friendNoticeObject(fields["fromToUserID"], "fromUserID", "toUserID"); !ok {
			return out
		}
		var pair sdkws.FromToUserID
		if json.Unmarshal(fields["fromToUserID"], &pair) != nil {
			return out
		}
		from, to = pair.FromUserID, pair.ToUserID
		// Approval notifications can describe the request in reverse order.
		// Incoming applications must keep the applicant-to-recipient direction.
		if kind == 1203 && (from != sender || to != recipient) {
			return out
		}
		if !readFriendNoticeRequest(fields["request"], notice) {
			return out
		}
	} else {
		if !messageFields(fields, "friend", "operationTime", "opUser", "friendVersion", "friendVersionID") {
			return out
		}
		friend, ok := friendNoticeObject(fields["friend"], "ownerUserID", "remark", "createTime",
			"friendUser", "addSource", "operatorUserID", "ex", "isPinned", "friendGroups")
		if !ok {
			return out
		}
		// The bundled client adds repeated friendGroups. Only absent, null or
		// empty collections have a verified display contract here.
		var groups []json.RawMessage
		if raw := friend["friendGroups"]; raw != nil && (json.Unmarshal(raw, &groups) != nil || len(groups) != 0) {
			return out
		}
		if _, ok := friendNoticeObject(friend["friendUser"], "userID", "nickname", "faceURL",
			"ex", "createTime", "appMangerLevel", "globalRecvMsgOpt"); !ok {
			return out
		}
		if operator := fields["opUser"]; operator != nil && string(operator) != "null" {
			if _, ok := friendNoticeObject(operator, "userID", "nickname", "faceURL", "ex"); !ok {
				return out
			}
		}
		var tips sdkws.FriendAddedTips
		if json.Unmarshal([]byte(detail), &tips) != nil || tips.Friend == nil || tips.Friend.FriendUser == nil ||
			!validText(tips.Friend.FriendUser.Nickname, 1000, true) {
			return out
		}
		from, to = tips.Friend.OwnerUserID, tips.Friend.FriendUser.UserID
		friendName = tips.Friend.FriendUser.Nickname
		if to == sender {
			notice.Sender.Name = friendName
		} else if to == recipient {
			notice.Recipient.Name = friendName
		}
	}
	if !ValidID(from) || !ValidID(to) || from == to {
		return out
	}
	if kind == 1204 && sender == recipient && from == sender {
		// SDK relation notifications also sync the owner's own friend list.
		// Keep this partner snapshot separate from the exact envelope route.
		notice.Friend = &MessagePerson{ID: to, Name: friendName}
	} else if !(from == sender && to == recipient || from == recipient && to == sender) {
		return out
	}
	out.Kind, out.FriendNotice = "friend_notice", notice
	return out
}

// Request snapshots supply only bounded names for the same envelope parties.
// Request/handling notes, profile URLs and opaque extensions stay private.
func readFriendNoticeRequest(raw json.RawMessage, notice *FriendNotice) bool {
	if raw == nil || string(raw) == "null" {
		return true
	}
	fields, ok := friendNoticeObject(raw, "fromUserID", "fromNickname", "fromFaceURL", "toUserID",
		"toNickname", "toFaceURL", "handleResult", "reqMsg", "createTime", "handlerUserID", "handleMsg",
		"handleTime", "ex", "sourceType", "expired", "requestVersion", "personalState")
	if !ok {
		return false
	}
	// The bundled SDK adds sourceType/expired. Locally observed requestVersion
	// strings and null personalState carry no display or authorization meaning.
	for _, key := range []string{"sourceType", "requestVersion"} {
		if value := fields[key]; value != nil {
			var text *string
			if json.Unmarshal(value, &text) != nil || text == nil ||
				!validText(*text, 128, true) || strings.ContainsAny(*text, "\n\t") {
				return false
			}
		}
	}
	if value := fields["expired"]; value != nil {
		var expired *bool
		if json.Unmarshal(value, &expired) != nil || expired == nil {
			return false
		}
	}
	if value := fields["personalState"]; value != nil {
		var state any
		if json.Unmarshal(value, &state) != nil || state != nil {
			return false
		}
	}
	var request sdkws.FriendRequest
	if json.Unmarshal(raw, &request) != nil || !ValidID(request.FromUserID) || !ValidID(request.ToUserID) ||
		request.FromUserID == request.ToUserID || request.CreateTime < 0 || request.HandleTime < 0 ||
		request.HandleResult < -1 || request.HandleResult > 1 ||
		request.HandlerUserID != "" && !ValidID(request.HandlerUserID) ||
		!validText(request.FromNickname, 1000, true) || !validText(request.ToNickname, 1000, true) {
		return false
	}
	for _, text := range []string{request.FromFaceURL, request.ToFaceURL, request.ReqMsg, request.HandleMsg, request.Ex} {
		if !validText(text, 8192, true) {
			return false
		}
	}
	if request.FromUserID == notice.Sender.ID && request.ToUserID == notice.Recipient.ID {
		notice.Sender.Name, notice.Recipient.Name = request.FromNickname, request.ToNickname
		return true
	}
	if notice.Event != "friend_request" && request.FromUserID == notice.Recipient.ID && request.ToUserID == notice.Sender.ID {
		notice.Sender.Name, notice.Recipient.Name = request.ToNickname, request.FromNickname
		return true
	}
	return false
}

// SDK 3.8.3 read_drawing.go also handles receipts from the login user to itself.
// Validate bounded tips, but do not infer a read/delivery result for any message.
func readSystemReceiptNotice(raw []byte, sender, recipient string) *MessageView {
	out := &MessageView{Kind: "unsupported", ContentType: 2200}
	wrapper, ok := friendNoticeObject(raw, "detail")
	var detail string
	if !ok || json.Unmarshal(wrapper["detail"], &detail) != nil {
		return out
	}
	if _, ok := friendNoticeObject([]byte(detail), "markAsReadUserID", "conversationID", "seqs", "hasReadSeq"); !ok {
		return out
	}
	var tips sdkws.MarkAsReadTips
	if json.Unmarshal([]byte(detail), &tips) != nil || !ValidID(tips.MarkAsReadUserID) ||
		!ValidID(tips.ConversationID) || tips.HasReadSeq < 0 || len(tips.Seqs) > 1024 ||
		tips.MarkAsReadUserID != sender && tips.MarkAsReadUserID != recipient {
		return out
	}
	seen := make(map[int64]bool, len(tips.Seqs))
	for _, seq := range tips.Seqs {
		if seq <= 0 || seen[seq] {
			return out
		}
		seen[seq] = true
	}
	out.Kind, out.SystemNotice = "system_notice", &SystemNotice{Event: "read_receipt"}
	return out
}

func friendNoticeObject(raw []byte, names ...string) (map[string]json.RawMessage, bool) {
	fields, ok := messageObject(raw)
	return fields, ok && messageFields(fields, names...)
}
