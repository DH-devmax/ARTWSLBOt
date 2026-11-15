package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/hex"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

func TestHistoryUsesPinnedReadOnlySeqListContract(t *testing.T) {
	var calls atomic.Int64
	client := mockTransport(t, func(request wireRequest) *wireResponse {
		calls.Add(1)
		// msg.GetSeqMessageReq: userID=1, conversations=2, order=3;
		// ConversationSeqs: conversationID=2, packed seqs=3.
		want, _ := hex.DecodeString("0a0973796e7468657469631214120c73695f706565725f73656c661a04010203041801")
		if request.ReqIdentifier != 1005 || request.SendID != "synthetic" || !bytes.Equal(request.Data, want) {
			t.Error("history did not use the pinned 1005 request contract")
		}
		messages := []*sdkws.MsgData{}
		for seq := int64(4); seq >= 1; seq-- {
			messages = append(messages, &sdkws.MsgData{Seq: seq, ClientMsgID: "client", Content: []byte("synthetic")})
		}
		raw, _ := proto.Marshal(&sdkws.PullMessageBySeqsResp{Msgs: map[string]*sdkws.PullMsgs{
			"si_peer_self": {Msgs: messages, IsEnd: true, EndSeq: 1},
		}})
		return &wireResponse{Data: raw}
	})
	messages, err := client.History(context.Background(), "si_peer_self", 1, 4)
	if err != nil || len(messages) != 4 || messages[0].Seq != 1 || messages[3].Seq != 4 || calls.Load() != 1 {
		t.Fatal("read-only history failed or repeated a request", err)
	}
}

func TestHistoryRejectsIncompleteOrForeignEvidence(t *testing.T) {
	for _, mode := range []string{"missing", "end_does_not_prove_missing", "nil", "duplicate", "foreign",
		"notification", "both_maps", "oversized", "bad_id", "empty", "missing_map", "wrong_map", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			client := mockTransport(t, func(request wireRequest) *wireResponse {
				calls.Add(1)
				if request.ReqIdentifier != 1005 {
					t.Error("evidence lookup used sync or send")
				}
				batch := &sdkws.PullMsgs{Msgs: []*sdkws.MsgData{
					{Seq: 1, ClientMsgID: "one", Content: []byte("first")},
					{Seq: 2, ClientMsgID: "two", Content: []byte("second")},
				}}
				response := &sdkws.PullMessageBySeqsResp{Msgs: map[string]*sdkws.PullMsgs{"sg_group": batch}}
				switch mode {
				case "missing", "end_does_not_prove_missing":
					batch.Msgs = batch.Msgs[1:]
					batch.IsEnd = mode == "end_does_not_prove_missing"
					batch.EndSeq = 2
				case "nil":
					batch.Msgs[0] = nil
				case "duplicate":
					batch.Msgs[1].Seq = 1
				case "foreign":
					response.Msgs["sg_other"] = batch
				case "notification":
					response.Msgs = nil
					response.NotificationMsgs = map[string]*sdkws.PullMsgs{"sg_group": batch}
				case "both_maps":
					response.NotificationMsgs = map[string]*sdkws.PullMsgs{"sg_group": batch}
				case "oversized":
					batch.Msgs[0].Content = make([]byte, 65537)
				case "bad_id":
					batch.Msgs[0].ClientMsgID = "bad\x00id"
				case "empty":
					batch.Msgs = nil
				case "missing_map":
					response.Msgs = nil
				case "wrong_map":
					response.Msgs = map[string]*sdkws.PullMsgs{"sg_other": batch}
				case "malformed":
					return &wireResponse{Data: []byte{0xff}}
				}
				raw, _ := proto.Marshal(response)
				return &wireResponse{Data: raw}
			})
			out, err := client.History(context.Background(), "sg_group", 1, 2)
			if err == nil || out != nil || calls.Load() != 1 {
				t.Fatal("incomplete or conflicting history became evidence", mode, err)
			}
			want := map[string]string{
				"missing": "partial", "end_does_not_prove_missing": "partial", "nil": "record",
				"duplicate": "sequence", "foreign": "scope", "notification": "scope", "both_maps": "scope",
				"oversized": "record", "bad_id": "record", "empty": "empty", "missing_map": "empty", "wrong_map": "scope",
			}[mode]
			if want != "" && code(err) != "lajiaoliao_history_"+want ||
				mode == "malformed" && code(err) != "lajiaoliao_protocol_boundary" {
				t.Fatal("history failure lost its fixed category", mode, err)
			}
		})
	}
}

func TestHistoryWindowIsOnlyBoundedCandidateDiscovery(t *testing.T) {
	for _, mode := range []string{"sparse", "empty", "missing_map", "outside", "duplicate", "too_many", "bad_id", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			client := mockTransport(t, func(request wireRequest) *wireResponse {
				calls.Add(1)
				if request.ReqIdentifier != 1005 {
					t.Error("history candidate discovery used a mutation or live sync")
				}
				batch := &sdkws.PullMsgs{Msgs: []*sdkws.MsgData{
					{Seq: 3, ClientMsgID: "three", Content: []byte("third")},
					{Seq: 1, ClientMsgID: "one", Content: []byte("first")},
				}, IsEnd: true, EndSeq: 3}
				result := &sdkws.PullMessageBySeqsResp{Msgs: map[string]*sdkws.PullMsgs{"sg_group": batch}}
				switch mode {
				case "empty":
					batch.Msgs = nil
				case "missing_map":
					result.Msgs = nil
				case "outside":
					batch.Msgs[0].Seq = 4
				case "duplicate":
					batch.Msgs[0].Seq = 1
				case "too_many":
					batch.Msgs = append(batch.Msgs, batch.Msgs...)
				case "bad_id":
					batch.Msgs[0].ClientMsgID = "bad\x00id"
				case "foreign":
					result.Msgs = map[string]*sdkws.PullMsgs{"sg_other": batch}
				}
				raw, _ := proto.Marshal(result)
				return &wireResponse{Data: raw}
			})
			messages, err := client.HistoryWindow(context.Background(), "sg_group", 1, 3)
			switch mode {
			case "sparse":
				if err != nil || len(messages) != 2 || messages[0].Seq != 1 || messages[1].Seq != 3 {
					t.Fatal("valid sparse candidate window rejected", err)
				}
			case "empty", "missing_map":
				if err != nil || len(messages) != 0 {
					t.Fatal("empty candidate window inferred completeness", err)
				}
			default:
				if err == nil || messages != nil {
					t.Fatal("foreign or malformed candidate window accepted", err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("candidate discovery retried")
			}
		})
	}
}

func TestHistoryPreservesEmptyClientSequencePlaceholders(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(map[bool]string{false: "window", true: "complete"}[strict], func(t *testing.T) {
			var calls atomic.Int64
			client := mockTransport(t, func(request wireRequest) *wireResponse {
				calls.Add(1)
				if request.ReqIdentifier != 1005 {
					t.Error("history placeholder handling used a mutation or sync request")
				}
				raw, err := proto.Marshal(&sdkws.PullMessageBySeqsResp{Msgs: map[string]*sdkws.PullMsgs{
					"si_peer_self": {Msgs: []*sdkws.MsgData{
						{Seq: 3, ClientMsgID: "third", Content: []byte("synthetic third")},
						{Seq: 2},
						{Seq: 1, ClientMsgID: "first", Content: []byte("synthetic first")},
					}},
				}})
				if err != nil {
					t.Fatal(err)
				}
				return &wireResponse{Data: raw}
			})
			read := client.HistoryWindow
			if strict {
				read = client.History
			}
			messages, err := read(context.Background(), "si_peer_self", 1, 3)
			if err != nil || len(messages) != 3 || calls.Load() != 1 ||
				messages[0].ClientMsgID != "first" || messages[1].ClientMsgID != "" ||
				messages[1].Seq != 2 || messages[2].ClientMsgID != "third" {
				t.Fatal("legal sequence placeholder discarded its neighboring messages", err)
			}
		})
	}
}

func TestHistoryBoundsCancellationAndNoRetry(t *testing.T) {
	var calls atomic.Int64
	client := mockTransport(t, func(request wireRequest) *wireResponse {
		calls.Add(1)
		if request.ReqIdentifier != 1005 {
			t.Error("read used a native mutation")
		}
		return nil
	})
	for _, input := range []struct {
		conversation string
		first, last  int64
	}{{"", 1, 1}, {"sg_group", 0, 1}, {"sg_group", 2, 1}, {"sg_group", 1, 51}} {
		if _, err := client.History(context.Background(), input.conversation, input.first, input.last); code(err) != "lajiaoliao_invalid_range" {
			t.Fatal("invalid range reached the connection", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid reads used the connection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.History(ctx, "sg_group", 1, 1); err != context.Canceled || calls.Load() != 0 {
		t.Fatal("cancelled read used the connection", err)
	}
	// The maximum sequence must not overflow the request-building loop.
	ctx, stop := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer stop()
	out, err := client.History(ctx, "sg_group", math.MaxInt64, math.MaxInt64)
	if err == nil || out != nil || calls.Load() != 1 {
		t.Fatal("unknown read retried or exceeded its bound", err)
	}
}
