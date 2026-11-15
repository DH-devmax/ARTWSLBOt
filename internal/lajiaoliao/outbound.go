package lajiaoliao

import (
	"context"
	"encoding/json"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

// Outbound contains only reviewed OpenIM message fields. The caller supplies
// references and member names from its own authorized, retained source.
type Outbound struct {
	Text    string         `json:"text,omitempty"`
	Mention *MessagePerson `json:"mention,omitempty"`
	Quote   *TextQuote     `json:"quote,omitempty"`
	Media   *MediaAsset    `json:"media,omitempty"`
}

type TextQuote struct {
	ClientID string `json:"clientMsgID"`
	ServerID string `json:"serverMsgID"`
	Sender   string `json:"sendID"`
	Group    string `json:"groupID"`
	Time     int64  `json:"sendTime"`
	Seq      int64  `json:"seq"`
	Text     string `json:"-"`
}

type OutboundSource struct {
	ClientID, ServerID, Sender, Recipient, Group, Text string
	Session                                            int32
	Time, Seq                                          int64
	CanQuote                                           bool
}

func ReadOutboundSource(raw []byte) (OutboundSource, error) {
	var m sdkws.MsgData
	if len(raw) > 128<<10 || proto.Unmarshal(raw, &m) != nil || m.MsgFrom != 100 ||
		!ValidID(m.ClientMsgID) || !ValidID(m.SendID) || !ValidID(m.RecvID) ||
		m.SessionType != 1 && m.SessionType != 3 || m.SessionType == 3 && !ValidID(m.GroupID) {
		return OutboundSource{}, &Failure{Code: "lajiaoliao_message_shape"}
	}
	defer clear(m.Content)
	if m.SessionType == 1 && m.ContentType != 101 {
		view, err := ReadMessageView(raw)
		if err != nil || view == nil || view.View == nil || view.View.Kind == "unsupported" {
			return OutboundSource{}, &Failure{Code: "lajiaoliao_message_shape"}
		}
	}
	out := OutboundSource{ClientID: m.ClientMsgID, ServerID: m.ServerMsgID, Sender: m.SendID,
		Recipient: m.RecvID, Group: m.GroupID, Session: m.SessionType, Time: m.SendTime, Seq: m.Seq}
	if m.ContentType == 101 || m.ContentType == 106 || m.ContentType == 114 {
		remaining := 32
		view, err := readMessageContent(m.ContentType, m.Content, 0, &remaining)
		if err != nil {
			return OutboundSource{}, err
		}
		out.Text = view.Text
		out.CanQuote = !view.Truncated && validText(out.Text, 8192, false) &&
			ValidID(out.ServerID) && out.Time > 0 && out.Seq > 0
	}
	return out, nil
}

func (o Outbound) Content() ([]byte, int32, error) {
	if o.Media != nil {
		if o.Text != "" || o.Mention != nil || o.Quote != nil {
			return nil, 0, &Failure{Code: "lajiaoliao_invalid_input"}
		}
		return o.Media.content()
	}
	if !validText(o.Text, 3000, false) || o.Mention != nil && o.Quote != nil {
		return nil, 0, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	var value any = struct {
		Content string `json:"content"`
	}{o.Text}
	kind := int32(101)
	if m := o.Mention; m != nil {
		if !ValidID(m.ID) || !validText(m.Name, 1000, true) {
			return nil, 0, &Failure{Code: "lajiaoliao_invalid_input"}
		}
		kind = 106
		value = map[string]any{"text": o.Text, "atUserList": []string{m.ID}, "isAtSelf": false,
			"atUsersInfo": []map[string]string{{"atUserID": m.ID, "groupNickname": m.Name}}}
	}
	if q := o.Quote; q != nil {
		if !ValidID(q.ClientID) || !ValidID(q.ServerID) || !ValidID(q.Sender) || !ValidID(q.Group) ||
			q.Time <= 0 || q.Seq < 1 || !validText(q.Text, 8192, false) {
			return nil, 0, &Failure{Code: "lajiaoliao_invalid_input"}
		}
		kind = 114
		value = map[string]any{"text": o.Text, "quoteMessage": map[string]any{
			"clientMsgID": q.ClientID, "serverMsgID": q.ServerID, "sendID": q.Sender,
			"recvID": q.Group, "groupID": q.Group, "sendTime": q.Time, "seq": q.Seq,
			"sessionType": 3, "msgFrom": 100, "contentType": 101,
			"textElem": map[string]string{"content": q.Text}}}
	}
	raw, err := json.Marshal(value)
	return raw, kind, err
}

// SendOutbound uses the account's existing single-reader connection. The
// immutable client ID identifies one attempt, including uncertain outcomes.
func (t *Transport) SendOutbound(ctx context.Context, group, peer, clientID string, message Outbound,
	fence func(context.Context, func(context.Context) error) error) (Receipt, error) {
	if !t.verified.Load() || fence == nil || !ValidID(group) || !ValidID(clientID) ||
		peer != "" && (!ValidID(peer) || message.Quote != nil || message.Mention != nil ||
			message.Media != nil && message.Media.Kind != "image") {
		return Receipt{}, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	content, kind, err := message.Content()
	if err != nil {
		return Receipt{}, err
	}
	defer clear(content)
	target, nativeGroup, sessionType := group, group, int32(3)
	if peer != "" {
		target, nativeGroup, sessionType = peer, "", 1
	}
	var result Receipt
	err = t.rpc(ctx, 1003, &sdkws.MsgData{SendID: t.session.UserID, RecvID: target, GroupID: nativeGroup,
		ClientMsgID: clientID, SenderPlatformID: 5, SessionType: sessionType, MsgFrom: 100, ContentType: kind,
		Content: content, CreateTime: time.Now().UnixMilli(),
		Options: map[string]bool{"history": true, "persistent": true, "senderSync": true}},
		func(raw []byte) error { var e error; result, e = decodeReceipt(raw); return e }, fence)
	if err != nil {
		return Receipt{}, err
	}
	if result.ClientID != clientID || !ValidID(result.ServerID) || result.Timestamp <= 0 {
		return Receipt{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	return result, nil
}
