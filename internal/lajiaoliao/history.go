// SPDX-License-Identifier: AGPL-3.0-or-later
package lajiaoliao

import (
	"context"
	"sort"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// OpenIM 3.8.3 conversation_msg/message_check.go uses RPC 1005 for history.
// The msg.GetSeqMessageReq wire fields are encoded without linking its gRPC
// server package. Its response maps are wire-identical to the existing sdkws
// PullMessageBySeqsResp. Live receive/ACK continues to use strict RPC 1002.
func (t *Transport) History(ctx context.Context, conversation string, begin, end int64) ([]*sdkws.MsgData, error) {
	return t.history(ctx, conversation, begin, end, true)
}

// HistoryWindow permits missing sequences for candidate discovery only. A
// candidate from a sparse window needs a separate complete History read.
func (t *Transport) HistoryWindow(ctx context.Context, conversation string, begin, end int64) ([]*sdkws.MsgData, error) {
	return t.history(ctx, conversation, begin, end, false)
}

func (t *Transport) history(ctx context.Context, conversation string, begin, end int64, complete bool) ([]*sdkws.MsgData, error) {
	if !validID(t.session.UserID) || !validID(conversation) || begin < 1 || end < begin || end-begin >= 50 {
		return nil, &Failure{Code: "lajiaoliao_invalid_range"}
	}
	seqs := make([]byte, 0, 50*protowire.SizeVarint(uint64(end)))
	for seq := begin; seq <= end; seq++ {
		seqs = protowire.AppendVarint(seqs, uint64(seq))
		if seq == end {
			break
		}
	}
	part := protowire.AppendTag(nil, 2, protowire.BytesType)
	part = protowire.AppendString(part, conversation)
	part = protowire.AppendTag(part, 3, protowire.BytesType)
	part = protowire.AppendBytes(part, seqs)
	request := protowire.AppendTag(nil, 1, protowire.BytesType)
	request = protowire.AppendString(request, t.session.UserID)
	request = protowire.AppendTag(request, 2, protowire.BytesType)
	request = protowire.AppendBytes(request, part)
	request = protowire.AppendTag(request, 3, protowire.VarintType)
	request = protowire.AppendVarint(request, uint64(sdkws.PullOrder_PullOrderDesc))
	result := &sdkws.PullMessageBySeqsResp{}
	if err := t.rpcData(ctx, 1005, request, func(b []byte) error { return proto.Unmarshal(b, result) }, nil); err != nil {
		clearHistoryContents(result)
		return nil, err
	}
	messages := result.Msgs[conversation]
	if len(result.NotificationMsgs) != 0 || len(result.Msgs) > 1 ||
		len(result.Msgs) == 1 && messages == nil {
		clearHistoryContents(result)
		return nil, &Failure{Code: "lajiaoliao_history_scope"}
	}
	if messages == nil || len(messages.Msgs) == 0 {
		if !complete {
			return nil, nil
		}
		return nil, &Failure{Code: "lajiaoliao_history_empty"}
	}
	if len(messages.Msgs) > int(end-begin+1) || complete && len(messages.Msgs) != int(end-begin+1) {
		clearHistoryContents(result)
		return nil, &Failure{Code: "lajiaoliao_history_partial"}
	}
	for _, message := range messages.Msgs {
		if message == nil {
			clearHistoryContents(result)
			return nil, &Failure{Code: "lajiaoliao_history_record"}
		}
	}
	sort.Slice(messages.Msgs, func(i, j int) bool { return messages.Msgs[i].Seq < messages.Msgs[j].Seq })
	for i, message := range messages.Msgs {
		if len(message.Content) > 65536 || proto.Size(message) > 128<<10 ||
			message.ClientMsgID == "" && message.Seq <= 0 ||
			message.ClientMsgID != "" && !validID(message.ClientMsgID) {
			clearHistoryContents(result)
			return nil, &Failure{Code: "lajiaoliao_history_record"}
		}
		if message.Seq < begin || message.Seq > end || i > 0 && message.Seq <= messages.Msgs[i-1].Seq ||
			complete && message.Seq != begin+int64(i) {
			clearHistoryContents(result)
			return nil, &Failure{Code: "lajiaoliao_history_sequence"}
		}
	}
	return messages.Msgs, nil
}

func clearHistoryContents(result *sdkws.PullMessageBySeqsResp) {
	for _, messages := range []map[string]*sdkws.PullMsgs{result.Msgs, result.NotificationMsgs} {
		for _, batch := range messages {
			if batch == nil {
				continue
			}
			for _, message := range batch.Msgs {
				if message != nil {
					clear(message.Content)
				}
			}
		}
	}
}
