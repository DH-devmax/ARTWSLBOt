package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
)

type chiliHistoryTestReader struct {
	chiliObservationTransport
	heads         *sdkws.GetMaxSeqResp
	sequenceErr   error
	sequenceCalls int
	pullCalls     int
	windowCalls   int
	confirmCalls  int
	sendCalls     int
	pulled        int
	conversations []string
	pull          func(context.Context, string, int64, int64) ([]*sdkws.MsgData, error)
	confirm       func(context.Context, string, int64, int64) ([]*sdkws.MsgData, error)
}

func (r *chiliHistoryTestReader) Sequences(context.Context) (*sdkws.GetMaxSeqResp, error) {
	r.sequenceCalls++
	return r.heads, r.sequenceErr
}

func (r *chiliHistoryTestReader) History(ctx context.Context, conversation string, begin, end int64) ([]*sdkws.MsgData, error) {
	r.confirmCalls++
	return r.history(ctx, conversation, begin, end, r.confirm)
}

func (r *chiliHistoryTestReader) HistoryWindow(ctx context.Context, conversation string, begin, end int64) ([]*sdkws.MsgData, error) {
	r.windowCalls++
	return r.history(ctx, conversation, begin, end, nil)
}

func (r *chiliHistoryTestReader) history(ctx context.Context, conversation string, begin, end int64,
	read func(context.Context, string, int64, int64) ([]*sdkws.MsgData, error)) ([]*sdkws.MsgData, error) {
	r.pullCalls++
	r.pulled += int(end - begin + 1)
	r.conversations = append(r.conversations, conversation)
	if _, allowed := r.heads.MaxSeqs[conversation]; !allowed || end-begin >= 50 || begin < 1 {
		return nil, errors.New("synthetic out-of-scope read")
	}
	if read != nil {
		return read(ctx, conversation, begin, end)
	}
	return r.pull(ctx, conversation, begin, end)
}

func (r *chiliHistoryTestReader) SendText(context.Context, string, string, string,
	func(context.Context, func(context.Context) error) error) (lajiaoliao.Receipt, error) {
	r.sendCalls++
	return lajiaoliao.Receipt{}, errors.New("evidence must never send")
}

func chiliHistoryFixture() (lajiaoliao.MediaEvidenceQuery, *sdkws.MsgData) {
	at := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	q := lajiaoliao.MediaEvidenceQuery{Account: "account", Sender: "self", Group: "group", ClientID: "client",
		Epoch: 82, CurrentEpoch: 83, ContentType: 101, SubmittedAt: at,
		Digest: []byte{0xba, 0x78, 0x16, 0xbf, 0x8f, 0x01, 0xcf, 0xea, 0x41, 0x41, 0x40, 0xde, 0x5d, 0xae, 0x22, 0x23,
			0xb0, 0x03, 0x61, 0xa3, 0x96, 0x17, 0x7a, 0x9c, 0xb4, 0x10, 0xff, 0x61, 0xf2, 0x00, 0x15, 0xad}}
	message := &sdkws.MsgData{SendID: "self", RecvID: "group", GroupID: "group", ClientMsgID: "client",
		ServerMsgID: "server", SessionType: 3, MsgFrom: 100, ContentType: 101, Seq: 1,
		Content: []byte("abc"), SendTime: at.Add(time.Second).UnixMilli()}
	return q, message
}

func chiliHistoryReaderFor(message *sdkws.MsgData) *chiliHistoryTestReader {
	return &chiliHistoryTestReader{chiliObservationTransport: chiliObservationTransport{
		hints: make(chan struct{}, 1), done: make(chan struct{})},
		heads: &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"sg_group": 1}, MinSeqs: map[string]int64{"sg_group": 1}},
		pull: func(context.Context, string, int64, int64) ([]*sdkws.MsgData, error) {
			return []*sdkws.MsgData{proto.Clone(message).(*sdkws.MsgData)}, nil
		}}
}

func TestChiliHistoryExactEvidenceAndNoSend(t *testing.T) {
	q, message := chiliHistoryFixture()
	for _, mode := range []string{"valid", "sender", "peer", "group", "type", "content", "server", "before", "future", "missing"} {
		t.Run(mode, func(t *testing.T) {
			original := proto.Clone(message).(*sdkws.MsgData)
			switch mode {
			case "sender":
				original.SendID = "other"
			case "peer":
				original.RecvID = "other"
			case "group":
				original.GroupID = "other"
			case "type":
				original.ContentType = 102
			case "content":
				original.Content = []byte("abd")
			case "server":
				original.ServerMsgID = ""
			case "before":
				original.SendTime = q.SubmittedAt.Add(-time.Second).UnixMilli()
			case "future":
				original.SendTime = time.Now().Add(time.Minute).UnixMilli()
			case "missing":
				original.ClientMsgID = "different"
			}
			reader := chiliHistoryReaderFor(original)
			out, err := readChiliHistory(context.Background(), q, reader, chiliEvidenceBudget{}, func() error { return nil })
			if reader.sendCalls != 0 || reader.sequenceCalls != 1 || reader.pullCalls != 1 {
				t.Fatal("evidence used another operation or repeated an RPC")
			}
			if mode == "valid" {
				if err != nil || !out.Found || out.EventID != 0 || out.Source != "server_history" ||
					out.Receipt.ClientID != q.ClientID || out.Receipt.ServerID != "server" {
					t.Fatal("exact history evidence not returned", out, err)
				}
			} else if out.Found || out.EventID != 0 || out.Source != "" || mode != "missing" && err == nil {
				t.Fatal("conflicting history became positive evidence", out, err)
			}
		})
	}
}

func assertChiliHistoryEvidenceCheck(t *testing.T, out lajiaoliao.MediaEvidence, want lajiaoliao.EvidenceCheck,
	started, finished time.Time) {
	t.Helper()
	if out.Check == nil {
		t.Fatal("successful history read omitted its diagnostic")
	}
	got := *out.Check
	if !got.Valid() || got.SampledAt.Location() != time.UTC ||
		got.SampledAt.Before(started) || got.SampledAt.After(finished) {
		t.Fatal("history diagnostic has invalid counts or sample time", got, started, finished)
	}
	got.SampledAt = time.Time{}
	if got != want {
		t.Fatalf("history diagnostic = %+v, want %+v", got, want)
	}
}

func TestChiliHistoryEmptyClientPlaceholders(t *testing.T) {
	for _, test := range []struct {
		name    string
		private bool
		echo    bool
	}{
		{name: "group_full_batch", echo: true},
		{name: "private_full_batch", private: true, echo: true},
		{name: "group_matching_placeholder_only"},
		{name: "private_matching_placeholder_only", private: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			q, message := chiliHistoryFixture()
			q.SubmittedAt = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
			message.SendTime = q.SubmittedAt.Add(time.Second).UnixMilli()
			conversation := "sg_group"
			if test.private {
				q.Peer = "peer"
				message.RecvID, message.GroupID, message.SessionType = q.Peer, "", 1
				conversation = "si_peer_self"
			}
			placeholder := proto.Clone(message).(*sdkws.MsgData)
			placeholder.ClientMsgID = ""
			batch := []*sdkws.MsgData{placeholder}
			if test.echo {
				placeholder.SendID, placeholder.RecvID, placeholder.GroupID = "", "", ""
				placeholder.SessionType = 0
				message.Seq = 2
				batch = append(batch, message)
			}
			reader := chiliHistoryReaderFor(message)
			reader.heads = &sdkws.GetMaxSeqResp{
				MaxSeqs: map[string]int64{conversation: int64(len(batch))},
				MinSeqs: map[string]int64{conversation: 1},
			}
			reader.pull = func(_ context.Context, got string, begin, end int64) ([]*sdkws.MsgData, error) {
				if got != conversation || begin != 1 || end != int64(len(batch)) {
					t.Fatal("placeholder batch read changed its scope", got, begin, end)
				}
				return batch, nil
			}
			started := time.Now().UTC()
			out, err := readChiliHistory(context.Background(), q, reader, chiliEvidenceBudget{count: 3},
				func() error { return nil })
			finished := time.Now().UTC()
			if err != nil || out.Found != test.echo || out.Truncated || out.EventID != 0 {
				t.Fatal("empty-client placeholder rejected or became evidence", out, err)
			}
			state := "message_not_found"
			if test.echo {
				state = "matched"
				want := lajiaoliao.Receipt{ClientID: q.ClientID, ServerID: message.ServerMsgID, Timestamp: message.SendTime}
				if out.Source != "server_history" || out.Receipt != want {
					t.Fatal("real echo in a full placeholder batch was not matched", out)
				}
			} else if out.Source != "" || out.Receipt != (lajiaoliao.Receipt{}) {
				t.Fatal("matching content without a client ID proved success", out)
			}
			assertChiliHistoryEvidenceCheck(t, out, lajiaoliao.EvidenceCheck{
				State: state, DurableRecords: 3, SequenceHeads: 1, Conversations: 1,
				Requested: len(batch), Returned: len(batch),
			}, started, finished)
			if reader.sequenceCalls != 1 || reader.windowCalls != 1 || reader.confirmCalls != 0 ||
				reader.pullCalls != 1 || reader.pulled != len(batch) || reader.sendCalls != 0 {
				t.Fatal("full placeholder batch sent, retried or required sparse confirmation", reader)
			}
			for _, item := range batch {
				if len(item.Content) != 3 || !bytes.Equal(item.Content, make([]byte, len(item.Content))) {
					t.Fatal("temporary placeholder or echo body retained", item.Seq)
				}
			}
		})
	}
}

func TestChiliHistoryPlaceholderBudgetAndBounds(t *testing.T) {
	for _, mode := range []string{"counted", "message_limit", "byte_limit", "sequence", "content_limit"} {
		t.Run(mode, func(t *testing.T) {
			q, message := chiliHistoryFixture()
			message.ClientMsgID = ""
			message.RecvID, message.GroupID, message.SessionType = "", "", 0
			budget := chiliEvidenceBudget{count: 7, bytes: 11}
			wantComplete := mode == "counted"
			var wantErr error
			switch mode {
			case "message_limit":
				budget.count = chiliEvidenceLimit
			case "byte_limit":
				budget.bytes = chiliEvidenceBytes - proto.Size(message) + 1
			case "sequence":
				message.Seq = 2
				wantErr = errChiliEvidenceGap
			case "content_limit":
				message.Content = bytes.Repeat([]byte{'x'}, 65537)
				wantErr = errChiliEvidenceGap
			}
			wantBudget := budget
			if wantComplete {
				wantBudget.count++
				wantBudget.bytes += proto.Size(message)
			}
			size := len(message.Content)
			var out lajiaoliao.MediaEvidence
			complete, seq, err := inspectChiliHistoryWindow(q, 1, 1, []*sdkws.MsgData{message}, &budget, &out)
			if complete != wantComplete || !errors.Is(err, wantErr) || seq != 0 ||
				out != (lajiaoliao.MediaEvidence{}) || budget != wantBudget {
				t.Fatal("placeholder bypassed validation, budgeting or evidence isolation", complete, seq, budget, out, err)
			}
			if len(message.Content) != size || !bytes.Equal(message.Content, make([]byte, size)) {
				t.Fatal("temporary placeholder content retained after validation", mode)
			}
		})
	}
}

func TestChiliHistoryMalformedNonemptyClientIDs(t *testing.T) {
	for _, test := range []struct {
		name, id string
	}{
		{name: "leading_space", id: " client"},
		{name: "trailing_space", id: "client "},
		{name: "newline", id: "client\nid"},
		{name: "nul", id: "client\x00id"},
		{name: "overlong", id: string(bytes.Repeat([]byte{'x'}, 129))},
		{name: "invalid_utf8", id: "client\xff"},
	} {
		t.Run(test.name, func(t *testing.T) {
			q, message := chiliHistoryFixture()
			message.ClientMsgID = test.id
			reader := chiliHistoryReaderFor(message)
			reader.pull = func(context.Context, string, int64, int64) ([]*sdkws.MsgData, error) {
				return []*sdkws.MsgData{message}, nil
			}
			out, err := readChiliHistory(context.Background(), q, reader, chiliEvidenceBudget{}, func() error { return nil })
			var failure *lajiaoliao.Failure
			if !errors.As(err, &failure) || failure.Code != "lajiaoliao_evidence_history_validate_gap" ||
				out != (lajiaoliao.MediaEvidence{}) {
				t.Fatal("malformed nonempty client ID was accepted or exposed a diagnostic", out, err)
			}
			if reader.sequenceCalls != 1 || reader.windowCalls != 1 || reader.confirmCalls != 0 ||
				reader.pullCalls != 1 || reader.pulled != 1 || reader.sendCalls != 0 {
				t.Fatal("malformed client ID triggered another operation", reader)
			}
			if len(message.Content) != 3 || !bytes.Equal(message.Content, make([]byte, len(message.Content))) {
				t.Fatal("malformed record content retained")
			}
		})
	}
}

func TestChiliHistoryEvidenceCheckStates(t *testing.T) {
	for _, test := range []struct {
		name      string
		last      int64
		candidate bool
		truncated bool
		calls     int
		want      lajiaoliao.EvidenceCheck
	}{
		{
			name: "conversation_not_returned",
			want: lajiaoliao.EvidenceCheck{State: "conversation_not_returned", DurableRecords: 3, SequenceHeads: 1},
		},
		{
			name: "history_empty",
			want: lajiaoliao.EvidenceCheck{State: "history_empty", DurableRecords: 3, SequenceHeads: 2, Conversations: 1},
		},
		{
			name: "message_not_found", last: 3, calls: 1,
			want: lajiaoliao.EvidenceCheck{State: "message_not_found", DurableRecords: 3, SequenceHeads: 2,
				Conversations: 1, Requested: 3, Returned: 3},
		},
		{
			name: "scan_truncated", last: 201, truncated: true, calls: 4,
			want: lajiaoliao.EvidenceCheck{State: "scan_truncated", SequenceHeads: 2,
				Conversations: 1, Requested: 200, Returned: 200},
		},
		{
			name: "candidate_scan_truncated", last: 201, candidate: true, truncated: true, calls: 4,
			want: lajiaoliao.EvidenceCheck{State: "scan_truncated", SequenceHeads: 2,
				Conversations: 1, Requested: 200, Returned: 200},
		},
		{
			name: "matched", last: 3, candidate: true, calls: 1,
			want: lajiaoliao.EvidenceCheck{State: "matched", DurableRecords: 3, SequenceHeads: 2,
				Conversations: 1, Requested: 3, Returned: 3},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			q, message := chiliHistoryFixture()
			q.SubmittedAt = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
			message.SendTime = q.SubmittedAt.Add(time.Second).UnixMilli()
			reader := chiliHistoryReaderFor(message)
			reader.heads = &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"sg_other": 17}}
			if test.name != "conversation_not_returned" {
				reader.heads.MaxSeqs["sg_group"] = test.last
			}
			reader.pull = func(_ context.Context, conversation string, begin, end int64) ([]*sdkws.MsgData, error) {
				if conversation != "sg_group" {
					t.Fatal("diagnostic read a foreign conversation", conversation)
				}
				batch := make([]*sdkws.MsgData, 0, end-begin+1)
				for seq := begin; seq <= end; seq++ {
					copy := proto.Clone(message).(*sdkws.MsgData)
					copy.Seq = seq
					if !test.candidate || seq != test.last {
						copy.ClientMsgID = fmt.Sprintf("different-%d", seq)
					}
					batch = append(batch, copy)
				}
				return batch, nil
			}
			started := time.Now().UTC()
			out, err := readChiliHistory(context.Background(), q, reader,
				chiliEvidenceBudget{count: test.want.DurableRecords}, func() error { return nil })
			finished := time.Now().UTC()
			wantFound := test.candidate && !test.truncated
			if err != nil || out.Found != wantFound || out.Truncated != test.truncated || out.EventID != 0 {
				t.Fatal("history diagnostic changed the evidence result", out, err)
			}
			if wantFound {
				want := lajiaoliao.Receipt{ClientID: q.ClientID, ServerID: message.ServerMsgID, Timestamp: message.SendTime}
				if out.Source != "server_history" || out.Receipt != want {
					t.Fatal("matched diagnostic omitted its exact receipt", out)
				}
			} else if out.Source != "" || out.Receipt != (lajiaoliao.Receipt{}) {
				t.Fatal("incomplete or negative scan retained positive evidence", out)
			}
			assertChiliHistoryEvidenceCheck(t, out, test.want, started, finished)
			if reader.sequenceCalls != 1 || reader.windowCalls != test.calls || reader.confirmCalls != 0 ||
				reader.pullCalls != test.calls || reader.pulled != test.want.Requested || reader.sendCalls != 0 {
				t.Fatal("diagnostic sent, retried or exceeded its fixed read scope", reader)
			}
		})
	}
}

func TestChiliHistoryPrivateConversationContract(t *testing.T) {
	for _, mode := range []string{"sdk", "app", "both", "missing", "foreign", "inbound", "conflict", "shared_limit", "string_sort"} {
		t.Run(mode, func(t *testing.T) {
			q, message := chiliHistoryFixture()
			q.Sender, q.Peer = "10", "9"
			message.SendID, message.RecvID, message.GroupID, message.SessionType = q.Sender, q.Peer, "", 1
			reader := chiliHistoryReaderFor(message)
			reader.heads = &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{}, MinSeqs: map[string]int64{}}
			const sorted, ordered = "si_10_9", "si_9_10"
			switch mode {
			case "sdk", "string_sort", "foreign", "inbound":
				reader.heads.MaxSeqs[sorted] = 1
			case "app":
				reader.heads.MaxSeqs[ordered] = 1
			case "both", "conflict":
				reader.heads.MaxSeqs[sorted], reader.heads.MaxSeqs[ordered] = 1, 1
			case "shared_limit":
				reader.heads.MaxSeqs[sorted], reader.heads.MaxSeqs[ordered] = 101, 101
			}
			reader.heads.MaxSeqs["si_other_10"] = 1
			reader.heads.MaxSeqs["sg_group"] = 1
			reader.pull = func(_ context.Context, conversation string, begin, end int64) ([]*sdkws.MsgData, error) {
				if conversation != sorted && conversation != ordered {
					t.Fatal("read a conversation without pinned route evidence", conversation)
				}
				batch := []*sdkws.MsgData{}
				for seq := begin; seq <= end; seq++ {
					copy := proto.Clone(message).(*sdkws.MsgData)
					copy.Seq = seq
					if mode == "shared_limit" {
						copy.ClientMsgID = fmt.Sprintf("different-%d", seq)
					}
					if mode == "foreign" {
						copy.RecvID = "other"
					}
					if mode == "inbound" {
						copy.SendID, copy.RecvID, copy.ClientMsgID = q.Peer, q.Sender, "inbound-client"
					}
					if mode == "conflict" && conversation == ordered {
						copy.ServerMsgID = "different-server"
					}
					batch = append(batch, copy)
				}
				return batch, nil
			}
			out, err := readChiliHistory(context.Background(), q, reader, chiliEvidenceBudget{}, func() error { return nil })
			switch mode {
			case "sdk", "app", "both", "string_sort":
				if err != nil || !out.Found || out.EventID != 0 || out.Source != "server_history" ||
					out.Receipt.ClientID != q.ClientID || out.Receipt.ServerID != message.ServerMsgID {
					t.Fatal("pinned private history failed to resolve evidence", out, err)
				}
				if mode == "both" && reader.pullCalls != 2 || mode == "string_sort" && reader.conversations[0] != sorted {
					t.Fatal("private candidates did not follow the pinned string contract", reader.conversations)
				}
			case "missing", "inbound":
				if err != nil || out.Found || out.Source != "" || mode == "missing" && reader.pullCalls != 0 {
					t.Fatal("missing outbound evidence was treated as sent", out, err)
				}
			case "shared_limit":
				if err != nil || out.Found || !out.Truncated || reader.pulled != chiliEvidenceLimit {
					t.Fatal("private candidates did not share one message budget", out, reader.pulled, err)
				}
			default:
				if err == nil || out.Found || out.Source != "" {
					t.Fatal("conflicting private route became evidence", out, err)
				}
			}
			if reader.sequenceCalls != 1 || reader.sendCalls != 0 || reader.pulled > chiliEvidenceLimit {
				t.Fatal("private history sent, retried or exceeded its shared limit")
			}
		})
	}
	q, _ := chiliHistoryFixture()
	q.Sender, q.Peer = "9", "10"
	ids, err := chiliHistoryConversations(q, &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"si_10_9": 1}})
	if err != nil || len(ids) != 1 || ids[0] != "si_10_9" {
		t.Fatal("identical SDK and App candidates were not deduplicated", ids, err)
	}
}

func TestChiliHistoryTimeBoundaryDoesNotChangeDurableMatcher(t *testing.T) {
	q, message := chiliHistoryFixture()
	message.SendTime = q.SubmittedAt.Add(-time.Second).UnixMilli()
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	if _, matched := matchChiliMediaEcho(q, raw); !matched {
		t.Fatal("history-only time check changed the existing durable matcher")
	}
	reader := chiliHistoryReaderFor(message)
	out, err := readChiliHistory(context.Background(), q, reader, chiliEvidenceBudget{}, func() error { return nil })
	if err == nil || out.Found {
		t.Fatal("history ignored the original submission time", out, err)
	}
}

func TestChiliSparseHistoryRequiresIndependentCompleteRecord(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "changed_receipt", "changed_content", "foreign",
		"wrong_seq", "duplicate", "placeholder", "confirm_error", "owner_changed", "confirm_cancelled", "no_match"} {
		t.Run(mode, func(t *testing.T) {
			q, message := chiliHistoryFixture()
			q.Peer = "peer"
			message.RecvID, message.GroupID, message.SessionType = q.Peer, "", 1
			message.Seq = 2
			reader := chiliHistoryReaderFor(message)
			reader.heads = &sdkws.GetMaxSeqResp{
				MaxSeqs: map[string]int64{"si_peer_self": 3}, MinSeqs: map[string]int64{"si_peer_self": 1},
			}
			var bodies [][]byte
			reader.pull = func(context.Context, string, int64, int64) ([]*sdkws.MsgData, error) {
				copy := proto.Clone(message).(*sdkws.MsgData)
				if mode == "no_match" {
					copy.ClientMsgID = "different"
				}
				bodies = append(bodies, copy.Content)
				return []*sdkws.MsgData{copy}, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader.confirm = func(_ context.Context, conversation string, first, last int64) ([]*sdkws.MsgData, error) {
				if conversation != "si_peer_self" || first != 2 || last != 2 {
					t.Fatal("verification did not target exactly the discovered sequence")
				}
				copy := proto.Clone(message).(*sdkws.MsgData)
				bodies = append(bodies, copy.Content)
				switch mode {
				case "missing":
					return nil, nil
				case "changed_receipt":
					copy.ServerMsgID = "changed"
				case "changed_content":
					copy.Content[0] = 'z'
				case "foreign":
					copy.RecvID = "other"
				case "wrong_seq":
					copy.Seq = 3
				case "duplicate":
					return []*sdkws.MsgData{copy, proto.Clone(copy).(*sdkws.MsgData)}, nil
				case "placeholder":
					copy.ClientMsgID = ""
				case "confirm_error":
					return nil, &lajiaoliao.Failure{Code: "lajiaoliao_history_empty"}
				case "confirm_cancelled":
					cancel()
				}
				return []*sdkws.MsgData{copy}, nil
			}
			guard := func() error {
				if mode == "owner_changed" && reader.confirmCalls != 0 {
					return ErrTextOwner
				}
				return nil
			}
			started := time.Now().UTC()
			out, err := readChiliHistory(ctx, q, reader, chiliEvidenceBudget{}, guard)
			finished := time.Now().UTC()
			if mode == "valid" {
				if err != nil || !out.Found || out.EventID != 0 || out.Source != "server_history" ||
					out.Receipt.ClientID != q.ClientID || out.Receipt.ServerID != message.ServerMsgID {
					t.Fatal("independently verified exact record not returned", out, err)
				}
				assertChiliHistoryEvidenceCheck(t, out, lajiaoliao.EvidenceCheck{
					State: "matched", SequenceHeads: 1, Conversations: 1, Requested: 4, Returned: 2,
				}, started, finished)
			} else if err == nil || out.Found || out.EventID != 0 || out.Source != "" || out.Check != nil {
				t.Fatal("unverified sparse candidate became evidence", out, err)
			}
			wantConfirm := 1
			if mode == "no_match" {
				wantConfirm = 0
			}
			if reader.windowCalls != 1 || reader.confirmCalls != wantConfirm || reader.sendCalls != 0 ||
				reader.pulled != 3+wantConfirm {
				t.Fatal("evidence retried, sent or exceeded its exact read scope", reader)
			}
			for i, body := range bodies {
				// Error-only mock results never crossed the reader port.
				if mode == "missing" || mode == "confirm_error" {
					if i == 1 {
						continue
					}
				}
				if !bytes.Equal(body, make([]byte, len(body))) {
					t.Fatal("temporary candidate content retained", i)
				}
			}
		})
	}
}

func TestChiliSparseHistoryEmptyRangesAndConfirmationStayBounded(t *testing.T) {
	for _, mode := range []string{"empty_large", "candidate_at_limit", "duplicate_candidate", "cancel_before_confirm"} {
		t.Run(mode, func(t *testing.T) {
			q, message := chiliHistoryFixture()
			reader := chiliHistoryReaderFor(message)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader.heads.MaxSeqs["sg_group"] = 250
			reader.pull = func(_ context.Context, _ string, first, last int64) ([]*sdkws.MsgData, error) {
				if mode == "empty_large" {
					return nil, nil
				}
				copy := proto.Clone(message).(*sdkws.MsgData)
				copy.Seq = last
				if mode == "cancel_before_confirm" {
					cancel()
				}
				if mode == "candidate_at_limit" && reader.windowCalls < 4 {
					return nil, nil
				}
				return []*sdkws.MsgData{copy}, nil
			}
			if mode == "candidate_at_limit" {
				reader.heads.MaxSeqs["sg_group"] = 200
			}
			out, err := readChiliHistory(ctx, q, reader, chiliEvidenceBudget{}, func() error { return nil })
			if out.Found || out.Source != "" || reader.sendCalls != 0 || reader.pulled > chiliEvidenceLimit ||
				reader.confirmCalls != 0 || reader.windowCalls > 4 {
				t.Fatal("sparse ranges bypassed read budgets or became evidence", out, err, reader.pulled)
			}
			if mode == "empty_large" || mode == "candidate_at_limit" {
				if !out.Truncated || err != nil || reader.pulled != chiliEvidenceLimit {
					t.Fatal("empty ranges did not consume the shared sequence budget", out, err)
				}
			} else if err == nil {
				t.Fatal("conflict or cancellation accepted", mode)
			}
		})
	}
}

func TestChiliHistoryBoundsGapsAndTruncation(t *testing.T) {
	q, message := chiliHistoryFixture()
	for _, mode := range []string{"gap", "empty", "nil", "byte_limit", "message_limit", "over_limit", "large_message", "foreign_route", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			reader := chiliHistoryReaderFor(message)
			budget := chiliEvidenceBudget{}
			switch mode {
			case "byte_limit":
				budget.bytes = chiliEvidenceBytes
			case "message_limit":
				budget.count = chiliEvidenceLimit
			case "over_limit":
				reader.heads.MaxSeqs["sg_group"] = 201
			case "conflict":
				reader.heads.MaxSeqs["sg_group"] = 2
			}
			reader.pull = func(_ context.Context, _ string, begin, end int64) ([]*sdkws.MsgData, error) {
				batch := []*sdkws.MsgData{}
				for seq := begin; seq <= end; seq++ {
					copy := proto.Clone(message).(*sdkws.MsgData)
					copy.Seq = seq
					if mode == "over_limit" {
						copy.ClientMsgID = fmt.Sprintf("different-%d", seq)
					}
					batch = append(batch, copy)
				}
				switch mode {
				case "gap":
					batch[0].Seq++
				case "empty":
					batch = nil
				case "nil":
					batch[0] = nil
				case "large_message":
					batch[0].Content = make([]byte, 65537)
				case "foreign_route":
					batch[0].GroupID = "other"
				case "conflict":
					batch[1].ServerMsgID = "conflicting-server"
				}
				return batch, nil
			}
			out, err := readChiliHistory(context.Background(), q, reader, budget, func() error { return nil })
			if out.Found || out.EventID != 0 || out.Source != "" || reader.pulled > chiliEvidenceLimit || reader.sendCalls != 0 {
				t.Fatal("bounded evidence failure became success or exceeded limits", out, err)
			}
			if mode == "byte_limit" || mode == "message_limit" || mode == "over_limit" {
				if !out.Truncated || err != nil {
					t.Fatal("truncation not preserved", out, err)
				}
			} else if err == nil {
				t.Fatal("malformed history was accepted", mode)
			}
		})
	}
}

func TestChiliHistoryCancellationAndOwner(t *testing.T) {
	q, message := chiliHistoryFixture()
	t.Run("owner_lost_after_read", func(t *testing.T) {
		reader := chiliHistoryReaderFor(message)
		out, err := readChiliHistory(context.Background(), q, reader, chiliEvidenceBudget{}, func() error {
			if reader.pullCalls > 0 {
				return ErrTextOwner
			}
			return nil
		})
		var failure *lajiaoliao.Failure
		if !errors.As(err, &failure) || failure.Code != "lajiaoliao_evidence_history_guard_owner_changed" ||
			out.Found || out.Check != nil {
			t.Fatal("late evidence survived ownership change", out, err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		reader := chiliHistoryReaderFor(message)
		reader.pull = func(ctx context.Context, _ string, _, _ int64) ([]*sdkws.MsgData, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		out, err := readChiliHistory(ctx, q, reader, chiliEvidenceBudget{}, func() error { return nil })
		var failure *lajiaoliao.Failure
		if !errors.As(err, &failure) || failure.Code != "lajiaoliao_evidence_pull_timeout" ||
			out.Found || out.Check != nil || reader.pullCalls != 1 {
			t.Fatal("timeout became evidence or retry", out, err)
		}
	})
	t.Run("content_cleared", func(t *testing.T) {
		batch := []*sdkws.MsgData{proto.Clone(message).(*sdkws.MsgData)}
		var out lajiaoliao.MediaEvidence
		ok, err := inspectChiliHistoryBatch(q, 1, 1, batch, &chiliEvidenceBudget{}, &out)
		if !ok || err != nil || !bytes.Equal(batch[0].Content, make([]byte, len(batch[0].Content))) {
			t.Fatal("temporary body not cleared", err)
		}
	})
}

func TestChiliHistoryDiagnosticStagesStayFixed(t *testing.T) {
	q, message := chiliHistoryFixture()
	for _, mode := range []string{"sequences", "pull", "gap", "route", "conflict", "guard", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			reader := chiliHistoryReaderFor(message)
			want := ""
			guard := func() error { return nil }
			switch mode {
			case "sequences":
				reader.sequenceErr = &lajiaoliao.Failure{Code: "lajiaoliao_request_rejected"}
				want = "lajiaoliao_evidence_sequences_rejected"
			case "pull":
				reader.pull = func(context.Context, string, int64, int64) ([]*sdkws.MsgData, error) {
					return nil, &lajiaoliao.Failure{Code: "lajiaoliao_rpc_busy"}
				}
				want = "lajiaoliao_evidence_pull_busy"
			case "gap":
				reader.pull = func(context.Context, string, int64, int64) ([]*sdkws.MsgData, error) { return nil, nil }
				want = "lajiaoliao_evidence_history_partial_gap"
			case "route":
				message := proto.Clone(message).(*sdkws.MsgData)
				message.RecvID = "foreign"
				reader = chiliHistoryReaderFor(message)
				want = "lajiaoliao_evidence_history_validate_route_changed"
			case "conflict":
				message := proto.Clone(message).(*sdkws.MsgData)
				message.Content = []byte("different")
				reader = chiliHistoryReaderFor(message)
				want = "lajiaoliao_evidence_history_validate_conflict"
			case "guard":
				guard = func() error { return ErrTextOwner }
				want = "lajiaoliao_evidence_history_guard_owner_changed"
			case "unavailable":
				reader.sequenceErr = errors.New("synthetic secret SQL payload")
				want = "lajiaoliao_evidence_sequences_unavailable"
			}
			out, err := readChiliHistory(context.Background(), q, reader, chiliEvidenceBudget{}, guard)
			var failure *lajiaoliao.Failure
			if !errors.As(err, &failure) || failure.Code != want || failure.Submitted ||
				out.Found || out.Check != nil || reader.sendCalls != 0 {
				t.Fatal("history failure did not keep its safe stage", out, err)
			}
			if failure.Error() != want || failure.Unwrap() != nil {
				t.Fatal("diagnostic carried raw failure data")
			}
			outer := chiliEvidenceFailure(context.Background(), "schedule", err)
			if outer.Error() != want {
				t.Fatal("scheduler erased inner failure stage", outer)
			}
		})
	}
}

func TestChiliHistoryDiagnosticWhitelistAndDeadline(t *testing.T) {
	for _, test := range []struct {
		stage, code, want string
	}{
		{"pull", "lajiaoliao_sequence_gap", "lajiaoliao_evidence_pull_gap"},
		{"pull", "lajiaoliao_history_empty", "lajiaoliao_evidence_history_empty_gap"},
		{"pull", "lajiaoliao_history_partial", "lajiaoliao_evidence_history_partial_gap"},
		{"pull", "lajiaoliao_history_scope", "lajiaoliao_evidence_history_scope_gap"},
		{"pull", "lajiaoliao_history_record", "lajiaoliao_evidence_history_record_gap"},
		{"pull", "lajiaoliao_history_sequence", "lajiaoliao_evidence_history_sequence_gap"},
		{"sequences", "lajiaoliao_protocol_boundary", "lajiaoliao_evidence_sequences_protocol"},
		{"sequences", "lajiaoliao_token_expired", "lajiaoliao_evidence_sequences_reauth_required"},
		{"pull", "lajiaoliao_evidence_pull_secret", "lajiaoliao_evidence_pull_unavailable"},
		{"secret", "lajiaoliao_request_rejected", "lajiaoliao_evidence_input_unavailable"},
	} {
		err := chiliEvidenceFailure(context.Background(), test.stage, &lajiaoliao.Failure{Code: test.code, Submitted: true})
		var failure *lajiaoliao.Failure
		if !errors.As(err, &failure) || failure.Code != test.want || failure.Submitted || !chiliEvidenceDiagnosticCode(failure.Code) {
			t.Fatal("non-whitelisted failure crossed the evidence port", err)
		}
	}
	limited := chiliEvidenceFailure(context.Background(), "sequences",
		&lajiaoliao.Failure{Code: "lajiaoliao_rate_limited", RetryAfter: time.Minute})
	var cooldown *lajiaoliao.Failure
	if !errors.As(limited, &cooldown) || cooldown.Code != "lajiaoliao_rate_limited" ||
		cooldown.RetryAfter != time.Minute {
		t.Fatal("stage diagnostic bypassed the scheduler cooldown contract", limited)
	}
	if err := chiliEvidenceFailure(context.Background(), "schedule", limited); err.Error() != "lajiaoliao_evidence_schedule_rate_limited" {
		t.Fatal("port boundary did not tag rate limiting", err)
	}
	for _, cancelled := range []bool{false, true} {
		var ctx context.Context
		var cancel context.CancelFunc
		want := "lajiaoliao_evidence_pull_timeout"
		if cancelled {
			ctx, cancel = context.WithCancel(context.Background())
			want = "lajiaoliao_evidence_pull_cancelled"
			cancel()
		} else {
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}
		err := chiliEvidenceFailure(ctx, "pull", &lajiaoliao.Failure{Code: "lajiaoliao_result_unknown"})
		cancel()
		if err.Error() != want {
			t.Fatal("RPC result_unknown hid context termination", err)
		}
	}
	q, _ := chiliHistoryFixture()
	_, err := new(ReceiverManager).ChiliMediaEvidence(context.Background(), q)
	if err == nil || err.Error() != "lajiaoliao_evidence_account_unavailable" {
		t.Fatal("storage failure was misreported as identity change", err)
	}
}

func TestChiliHistoryCurrentConnectionEpoch(t *testing.T) {
	q, _ := chiliHistoryFixture()
	for _, mode := range []string{"valid", "fallback", "old_epoch", "identity", "platform", "expired", "closed"} {
		t.Run(mode, func(t *testing.T) {
			query := q
			now := time.Now()
			reader := chiliHistoryReaderFor(&sdkws.MsgData{})
			connection := &ownedChiliConnection{transport: reader, ctx: context.Background(), uid: query.Sender}
			worker := &receiverWorker{spec: receiverSpec{ID: query.Account, UID: query.Sender,
				Platform: "lajiaoliao", Epoch: 83, Revision: 4}, chiliConnection: connection,
				leaseUntil: now.Add(time.Minute), snapshot: ReceiverSnapshot{State: "connected", HeartbeatAt: &now}}
			manager := &ReceiverManager{owner: "synthetic-owner", jobs: map[string]*receiverWorker{query.Account: worker}}
			switch mode {
			case "fallback":
				query.Epoch, query.CurrentEpoch = 83, 0
			case "old_epoch":
				query.CurrentEpoch = 0
			case "identity":
				worker.spec.UID = "other"
			case "platform":
				worker.spec.Platform = "wsl"
			case "expired":
				worker.leaseUntil = now
			case "closed":
				reader.Close()
			}
			_, got, err := manager.chiliEvidenceConnection(query)
			if mode == "valid" || mode == "fallback" {
				if err != nil || got != connection {
					t.Fatal("same authenticated identity could not use current connection", err)
				}
			} else if !errors.Is(err, ErrTextOwner) {
				t.Fatal("stale connection was accepted", mode, err)
			}
			if query.Epoch != q.Epoch && mode != "fallback" {
				t.Fatal("original epoch mutated")
			}
		})
	}
}
