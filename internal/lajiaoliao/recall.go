package lajiaoliao

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/nativewire"
)

func ReadRecallNotice(raw []byte) (*nativewire.RecallNotice, error) {
	if len(raw) > 128<<10 {
		return nil, &Failure{Code: "lajiaoliao_recall_shape"}
	}
	var message sdkws.MsgData
	if proto.Unmarshal(raw, &message) != nil {
		return nil, &Failure{Code: "lajiaoliao_recall_shape"}
	}
	if message.ContentType != 2101 || message.MsgFrom != 200 {
		return nil, nil
	}
	defer clear(message.Content)
	var wrapper struct {
		Detail string `json:"detail"`
	}
	var detail struct {
		RevokerUserID  string `json:"revokerUserID"`
		ClientMsgID    string `json:"clientMsgID"`
		RevokeTime     int64  `json:"revokeTime"`
		SessionType    int32  `json:"sesstionType"`
		Seq            int64  `json:"seq"`
		ConversationID string `json:"conversationID"`
		IsAdminRevoke  bool   `json:"isAdminRevoke"`
	}
	if !recallJSON(message.Content, &wrapper) || len(wrapper.Detail) > 8192 ||
		!recallJSON([]byte(wrapper.Detail), &detail) || !recallID(detail.RevokerUserID) ||
		!recallID(detail.ClientMsgID) || !recallID(detail.ConversationID) ||
		detail.RevokeTime <= 0 || detail.Seq <= 0 || detail.SessionType != message.SessionType {
		return nil, &Failure{Code: "lajiaoliao_recall_shape"}
	}
	out := &nativewire.RecallNotice{ClientID: detail.ClientMsgID, Actor: detail.RevokerUserID,
		Sender: message.SendID, RecalledAt: uint64(detail.RevokeTime)}
	switch message.SessionType {
	case 2, 3:
		if !recallID(message.GroupID) {
			return nil, &Failure{Code: "lajiaoliao_recall_shape"}
		}
		out.Scene, out.Target = 1, message.GroupID
	case 1:
		if !recallID(message.SendID) || !recallID(message.RecvID) {
			return nil, &Failure{Code: "lajiaoliao_recall_shape"}
		}
		out.Target = message.RecvID
	default:
		return nil, &Failure{Code: "lajiaoliao_recall_shape"}
	}
	return out, nil
}

func recallID(value string) bool {
	return len(value) > 0 && len(value) <= 128 && utf8.ValidString(value) &&
		strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func recallJSON(raw []byte, out any) bool {
	if !utf8.Valid(raw) || len(raw) == 0 || len(raw) > 16384 {
		return false
	}
	// Reject duplicate keys before decoding critical routing/identity fields.
	keys := map[string]bool{}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return false
	}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || keys[name] {
			return false
		}
		keys[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return false
		}
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out) == nil && decoder.Decode(new(any)) == io.EOF
}
