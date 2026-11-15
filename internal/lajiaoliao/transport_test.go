package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func mockTransport(t *testing.T, fn func(wireRequest) *wireResponse) *Transport {
	return mockTransportSession(t, Session{UserID: "synthetic"}, fn)
}

func mockTransportSession(t *testing.T, session Session, fn func(wireRequest) *wireResponse) *Transport {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				return
			}
			var input wireRequest
			if gob.NewDecoder(bytes.NewReader(raw)).Decode(&input) != nil {
				return
			}
			// A push arriving before the reply must not consume the RPC slot.
			var hint bytes.Buffer
			gob.NewEncoder(&hint).Encode(wireResponse{ReqIdentifier: 2001})
			if c.WriteMessage(websocket.BinaryMessage, hint.Bytes()) != nil {
				return
			}
			resp := fn(input)
			if resp == nil {
				continue
			}
			resp.MsgIncr = input.MsgIncr
			resp.ReqIdentifier = input.ReqIdentifier
			var out bytes.Buffer
			gob.NewEncoder(&out).Encode(resp)
			if c.WriteMessage(websocket.BinaryMessage, out.Bytes()) != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := newTransport(conn, session)
	t.Cleanup(client.Close)
	return client
}

func TestTwentySyntheticConnectionsRemainIndependent(t *testing.T) {
	clients := make([]*Transport, 0, 20)
	for i := range 20 {
		uid := fmt.Sprintf("synthetic-%d", i)
		clients = append(clients, mockTransportSession(t, Session{UserID: uid}, func(input wireRequest) *wireResponse {
			if input.SendID != uid || input.ReqIdentifier != 1001 {
				t.Error("connection identity crossed or native action was requested")
			}
			raw, _ := proto.Marshal(&sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{uid: 1}})
			return &wireResponse{Data: raw}
		}))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, client := range clients {
		wg.Add(1)
		go func(i int, client *Transport) {
			defer wg.Done()
			for range 3 {
				heads, err := client.Sequences(ctx)
				if err != nil || len(heads.MaxSeqs) != 1 || heads.MaxSeqs[fmt.Sprintf("synthetic-%d", i)] != 1 {
					t.Error("connection result crossed account scope", err)
					return
				}
			}
		}(i, client)
	}
	wg.Wait()
	clients[0].Close()
	for _, client := range clients[1:] {
		if _, err := client.Sequences(ctx); err != nil {
			t.Fatal("closing another connection affected this owner", err)
		}
	}
}

func TestPushReplyDemultiplexAndSequenceGap(t *testing.T) {
	client := mockTransport(t, func(input wireRequest) *wireResponse {
		var message proto.Message
		switch input.ReqIdentifier {
		case 1001:
			message = &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"sg_group": 2}}
		case 1002:
			message = &sdkws.PullMessageBySeqsResp{Msgs: map[string]*sdkws.PullMsgs{"sg_group": {Msgs: []*sdkws.MsgData{
				{Seq: 2, ClientMsgID: "m2", SendID: "other", GroupID: "group", SessionType: 3, ContentType: 101, Content: []byte(`{"content":"second"}`)},
			}}}}
		}
		raw, err := proto.Marshal(message)
		if err != nil {
			t.Error(err)
		}
		return &wireResponse{Data: raw}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	seq, err := client.Sequences(ctx)
	if err != nil || seq.MaxSeqs["sg_group"] != 2 {
		t.Fatal("push stole reply", err)
	}
	select {
	case <-client.Hints():
	default:
		t.Fatal("push hint lost")
	}
	if _, err = client.Pull(ctx, "sg_group", 1, 2); code(err) != "lajiaoliao_sequence_gap" {
		t.Fatal("silently skipped missing seq", err)
	}
}

func TestTextReceiptFenceAndUnknownNoRepeat(t *testing.T) {
	var sends atomic.Int64
	client := mockTransport(t, func(input wireRequest) *wireResponse {
		if input.ReqIdentifier == 1001 {
			raw, _ := proto.Marshal(&sdkws.GetMaxSeqResp{})
			return &wireResponse{Data: raw}
		}
		sends.Add(1)
		var m sdkws.MsgData
		if proto.Unmarshal(input.Data, &m) != nil || m.GroupID != "group" || m.SessionType != 3 {
			t.Error("routing changed")
		}
		return nil // Lost receipt: only the caller may reconcile; never resend.
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := client.SendText(ctx, "group", "before-verification", "hello", func(ctx context.Context, write func(context.Context) error) error { return write(ctx) }); code(err) != "lajiaoliao_connection_unverified" || sends.Load() != 0 {
		t.Fatal("unverified connection sent text", err)
	}
	if _, err := client.Sequences(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := client.SendText(ctx, "group", "synthetic-client", "hello", func(context.Context, func(context.Context) error) error { return context.Canceled })
	if err == nil || sends.Load() != 0 {
		t.Fatal("cancelled fence wrote")
	}
	_, err = client.SendText(ctx, "group", "synthetic-client", "hello", func(ctx context.Context, write func(context.Context) error) error { return write(ctx) })
	if code(err) != "lajiaoliao_result_unknown" || sends.Load() != 1 {
		t.Fatal("unknown send repeated", err, sends.Load())
	}
}

func TestTextFenceFailureTracksTheActualSocketWrite(t *testing.T) {
	for _, mode := range []string{"owner-refused", "cancelled-before-write", "failed-after-write"} {
		t.Run(mode, func(t *testing.T) {
			writes := make(chan struct{}, 1)
			client := mockTransport(t, func(wireRequest) *wireResponse {
				writes <- struct{}{}
				return nil
			})
			client.verified.Store(true)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := client.SendText(ctx, "group", "synthetic-client", "synthetic text",
				func(work context.Context, write func(context.Context) error) error {
					switch mode {
					case "owner-refused":
						return errors.New("synthetic owner changed")
					case "cancelled-before-write":
						cancelled, stop := context.WithCancel(work)
						stop()
						return write(cancelled)
					default:
						if err := write(work); err != nil {
							return err
						}
						return errors.New("synthetic final commit failed")
					}
				})
			var failure *Failure
			if !errors.As(err, &failure) {
				t.Fatal("submission boundary was not preserved:", err)
			}
			if mode == "failed-after-write" {
				if failure.Code != "lajiaoliao_result_unknown" || !failure.Submitted {
					t.Fatal("a socket write was incorrectly declared unsubmitted:", err)
				}
				select {
				case <-writes:
				case <-ctx.Done():
					t.Fatal("synthetic socket never received its one message")
				}
			} else {
				if failure.Code != "lajiaoliao_not_submitted" || failure.Submitted {
					t.Fatal("a refused write became an unknown submission:", err)
				}
				if mode == "cancelled-before-write" && !errors.Is(err, context.Canceled) {
					t.Fatal("submission classification hid caller cancellation:", err)
				}
				select {
				case <-writes:
					t.Fatal("refused final fence sent a message")
				default:
				}
			}
		})
	}
}

func TestRPCReplySurvivesImmediatePeerDisconnect(t *testing.T) {
	for _, mode := range []string{"success", "rejected", "wrong-request", "invalid-receipt", "missing",
		"read-success", "read-missing", "failed-fence"} {
		t.Run(mode, func(t *testing.T) {
			var writes atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_, raw, err := conn.ReadMessage()
				if err != nil {
					return
				}
				writes.Add(1)
				var request wireRequest
				if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				if mode == "missing" || mode == "read-missing" {
					return
				}
				receipt := protowire.AppendTag(nil, 1, protowire.BytesType)
				receipt = protowire.AppendString(receipt, "server-string")
				receipt = protowire.AppendTag(receipt, 2, protowire.BytesType)
				receipt = protowire.AppendString(receipt, "client-string")
				receipt = protowire.AppendTag(receipt, 3, protowire.VarintType)
				receipt = protowire.AppendVarint(receipt, 1700000000123)
				response := wireResponse{ReqIdentifier: request.ReqIdentifier, MsgIncr: request.MsgIncr, Data: receipt}
				switch mode {
				case "rejected":
					response.ErrCode = 1501
				case "wrong-request":
					response.ReqIdentifier++
				case "invalid-receipt":
					response.Data = []byte{0xff}
				}
				var encoded bytes.Buffer
				if err := gob.NewEncoder(&encoded).Encode(response); err != nil {
					t.Error(err)
					return
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, encoded.Bytes()); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			client := newTransport(conn, Session{UserID: "self-string"})
			defer client.Close()
			client.verified.Store(true)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			fences := 0
			fenceErr := errors.New("synthetic final fence failed")
			fence := func(work context.Context, send func(context.Context) error) error {
				fences++
				if err := send(work); err != nil {
					return err
				}
				// Wait until both the reply (if any) and disconnect are observable.
				select {
				case <-client.readerDone:
					if mode == "failed-fence" {
						return fenceErr
					}
					return nil
				case <-work.Done():
					return work.Err()
				}
			}
			var receipt Receipt
			if strings.HasPrefix(mode, "read-") {
				err = client.rpc(ctx, 1001, &sdkws.GetMaxSeqReq{}, func(raw []byte) error {
					var decodeErr error
					receipt, decodeErr = decodeReceipt(raw)
					return decodeErr
				}, fence)
			} else {
				receipt, err = client.SendText(ctx, "group-string", "client-string", "synthetic text", fence)
			}
			if fences != 1 || writes.Load() != 1 {
				t.Fatal("disconnect caused another submission", fences, writes.Load())
			}
			if mode == "success" || mode == "read-success" {
				if err != nil || receipt.ClientID != "client-string" || receipt.ServerID != "server-string" {
					t.Fatal("accepted reply was lost on disconnect", receipt, err)
				}
			} else {
				want := "lajiaoliao_protocol_boundary"
				if mode == "rejected" {
					want = "lajiaoliao_token_expired"
				} else if mode == "missing" || mode == "failed-fence" {
					want = "lajiaoliao_result_unknown"
				} else if mode == "read-missing" {
					want = "lajiaoliao_disconnected"
				}
				var failure *Failure
				if !errors.As(err, &failure) || failure.Code != want ||
					failure.Submitted != (mode != "read-missing") {
					t.Fatal("disconnect changed the original submission evidence", err, want)
				}
				if mode == "failed-fence" && (!errors.Is(err, fenceErr) || receipt != (Receipt{})) {
					t.Fatal("accepted reply bypassed the failed final fence", receipt, err)
				}
			}
			client.mu.Lock()
			pending := len(client.pending)
			client.mu.Unlock()
			if pending != 0 {
				t.Fatal("completed RPC leaked its pending slot", pending)
			}
		})
	}
}

func TestReceiptPinnedWireFields(t *testing.T) {
	raw := protowire.AppendTag(nil, 1, protowire.BytesType)
	raw = protowire.AppendString(raw, "server")
	raw = protowire.AppendTag(raw, 2, protowire.BytesType)
	raw = protowire.AppendString(raw, "client")
	raw = protowire.AppendTag(raw, 3, protowire.VarintType)
	raw = protowire.AppendVarint(raw, 1700000000000)
	r, err := decodeReceipt(raw)
	if err != nil || r.ClientID != "client" || r.ServerID != "server" || r.Timestamp != 1700000000000 {
		t.Fatal(r, err)
	}
	raw = protowire.AppendTag(raw, 2, protowire.BytesType)
	raw = protowire.AppendString(raw, "other")
	if _, err = decodeReceipt(raw); err == nil {
		t.Fatal("duplicate receipt identity accepted")
	}
}

func TestBoundaryFailuresStopAutomaticReconnect(t *testing.T) {
	for _, code := range []string{"lajiaoliao_kicked", "lajiaoliao_reauth_required", "lajiaoliao_sequence_gap",
		"lajiaoliao_protocol_boundary", "lajiaoliao_sequence_limit", "lajiaoliao_session_invalid", "lajiaoliao_receive_queue_full",
		"lajiaoliao_receive_persistence_failed"} {
		if !IsTerminal(&Failure{Code: code}) {
			t.Fatal("unsafe stream was eligible for automatic retry", code)
		}
	}
	if IsTerminal(&Failure{Code: "lajiaoliao_disconnected"}) || IsTerminal(context.Canceled) {
		t.Fatal("temporary disconnect or explicit cancellation became a terminal protocol failure")
	}
}

func TestPullRetainsSequenceFillersWithoutAcceptingMissingRecords(t *testing.T) {
	for _, mode := range []string{"filler", "deleted", "missing", "invalid-id", "oversized", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			client := mockTransport(t, func(input wireRequest) *wireResponse {
				messages := []*sdkws.MsgData{{Seq: 41, ClientMsgID: "first"},
					{Seq: 42}, {Seq: 43, ClientMsgID: "last"}}
				switch mode {
				case "deleted":
					messages[1].Status = 4
				case "missing":
					messages = append(messages[:1], messages[2:]...)
				case "invalid-id":
					messages[1].ClientMsgID = "invalid\x00id"
				case "oversized":
					messages[1].Content = bytes.Repeat([]byte("x"), 65537)
				case "duplicate":
					messages[1].Seq = 41
				}
				raw, _ := proto.Marshal(&sdkws.PullMessageBySeqsResp{NotificationMsgs: map[string]*sdkws.PullMsgs{
					"n_self": {Msgs: messages},
				}})
				return &wireResponse{Data: raw}
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			batch, err := client.Pull(ctx, "n_self", 41, 43)
			if mode == "filler" || mode == "deleted" {
				if err != nil || len(batch) != 3 || batch[1].Seq != 42 || batch[1].ClientMsgID != "" {
					t.Fatal("server placeholder was lost or relabeled", err)
				}
			} else if code(err) != "lajiaoliao_sequence_gap" {
				t.Fatal("invalid sequence range advanced", err)
			}
		})
	}
}
