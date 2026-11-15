package lajiaoliao

import (
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

// PushMessage has no durable sequence. Its native identity is deduplicated
// separately and must never advance a conversation's recovery cursor.
type PushMessage struct {
	Conversation string
	Message      *sdkws.MsgData
}

func readPushMessages(raw []byte) ([]PushMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var push sdkws.PushMessages
	if len(raw) > 2<<20 || proto.Unmarshal(raw, &push) != nil ||
		len(push.Msgs)+len(push.NotificationMsgs) > 10000 {
		return nil, &Failure{Code: "lajiaoliao_protocol_boundary"}
	}
	var out []PushMessage
	valid := false
	count := 0
	defer func() {
		for _, stream := range []map[string]*sdkws.PullMsgs{push.Msgs, push.NotificationMsgs} {
			for _, batch := range stream {
				for _, message := range batch.GetMsgs() {
					if message != nil && (!valid || message.Seq != 0) {
						clear(message.Content)
					}
				}
			}
		}
	}()
	for _, stream := range []map[string]*sdkws.PullMsgs{push.Msgs, push.NotificationMsgs} {
		for conversation, batch := range stream {
			if !ValidID(conversation) || batch == nil {
				return nil, &Failure{Code: "lajiaoliao_protocol_boundary"}
			}
			count += len(batch.Msgs)
			if count > 256 {
				return nil, &Failure{Code: "lajiaoliao_protocol_boundary"}
			}
			for _, message := range batch.Msgs {
				if message == nil || message.Seq < 0 || len(message.Content) > 65536 ||
					proto.Size(message) > 128<<10 {
					return nil, &Failure{Code: "lajiaoliao_protocol_boundary"}
				}
				if message.Seq != 0 {
					continue
				}
				if !ValidID(message.ClientMsgID) || len(out) >= 50 {
					return nil, &Failure{Code: "lajiaoliao_protocol_boundary"}
				}
				out = append(out, PushMessage{Conversation: conversation, Message: message})
			}
		}
	}
	valid = true
	return out, nil
}

func clearPushMessages(messages []PushMessage) {
	for _, item := range messages {
		if item.Message != nil {
			clear(item.Message.Content)
		}
	}
}
