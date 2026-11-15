package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/gob"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

func TestPushRetainsZeroSequenceFromBothMaps(t *testing.T) {
	push := &sdkws.PushMessages{
		Msgs: map[string]*sdkws.PullMsgs{"si_self_peer": {Msgs: []*sdkws.MsgData{
			{Seq: 0, ClientMsgID: "private", Content: []byte(`{"content":"synthetic"}`)},
			{Seq: 7, ClientMsgID: "recover-through-cursor"},
		}}},
		NotificationMsgs: map[string]*sdkws.PullMsgs{"n_self": {Msgs: []*sdkws.MsgData{
			{Seq: 0, ClientMsgID: "notice", Content: []byte(`{"detail":"{}"}`)},
		}}},
	}
	raw, _ := proto.Marshal(push)
	items, err := readPushMessages(raw)
	if err != nil || len(items) != 2 {
		t.Fatal("zero-sequence push lost", err)
	}
	clear(raw)
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Message.ClientMsgID] = true
		if item.Message.Seq != 0 || len(item.Message.Content) == 0 {
			t.Fatal("push content did not survive source cleanup")
		}
	}
	if !seen["private"] || !seen["notice"] || seen["recover-through-cursor"] {
		t.Fatal("push and recovery paths overlapped", seen)
	}
	clearPushMessages(items)
}

func TestPushRejectsMalformedAndOverCapacity(t *testing.T) {
	for _, mode := range []string{"missing-id", "negative", "nil-message", "oversized", "too-many", "invalid-conversation"} {
		t.Run(mode, func(t *testing.T) {
			conversation := "si_self_peer"
			messages := []*sdkws.MsgData{{ClientMsgID: "synthetic"}}
			switch mode {
			case "missing-id":
				messages[0].ClientMsgID = ""
			case "negative":
				messages[0].Seq = -1
			case "nil-message":
				messages[0] = nil
			case "oversized":
				messages[0].Content = bytes.Repeat([]byte("x"), 65537)
			case "too-many":
				for range 50 {
					messages = append(messages, &sdkws.MsgData{ClientMsgID: "synthetic"})
				}
			case "invalid-conversation":
				conversation = "invalid\x00"
			}
			raw, _ := proto.Marshal(&sdkws.PushMessages{Msgs: map[string]*sdkws.PullMsgs{
				conversation: {Msgs: messages},
			}})
			// Protobuf serializes a nil message as an empty message, which must
			// still fail the native message identity check.
			if _, err := readPushMessages(raw); err == nil {
				t.Fatal("malformed push accepted")
			}
		})
	}
}

func TestPushQueueDoesNotBlockRPCAndFailsExplicitlyWhenFull(t *testing.T) {
	for _, mode := range []string{"normal", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer c.Close()
				_, raw, err := c.ReadMessage()
				if err != nil {
					return
				}
				var input wireRequest
				if gob.NewDecoder(bytes.NewReader(raw)).Decode(&input) != nil {
					return
				}
				count := 1
				if mode == "overflow" {
					count = 9
				}
				for range count {
					data, _ := proto.Marshal(&sdkws.PushMessages{Msgs: map[string]*sdkws.PullMsgs{
						"si_self_peer": {Msgs: []*sdkws.MsgData{{ClientMsgID: "synthetic"}}},
					}})
					var frame bytes.Buffer
					gob.NewEncoder(&frame).Encode(wireResponse{ReqIdentifier: 2001, Data: data})
					if c.WriteMessage(websocket.BinaryMessage, frame.Bytes()) != nil {
						return
					}
				}
				data, _ := proto.Marshal(&sdkws.GetMaxSeqResp{})
				var frame bytes.Buffer
				gob.NewEncoder(&frame).Encode(wireResponse{ReqIdentifier: input.ReqIdentifier, MsgIncr: input.MsgIncr, Data: data})
				c.WriteMessage(websocket.BinaryMessage, frame.Bytes())
				c.ReadMessage()
			}))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			client := newTransport(conn, Session{UserID: "self"})
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err = client.Sequences(ctx)
			if mode == "normal" {
				if err != nil {
					t.Fatal("push consumed or blocked the RPC slot", err)
				}
				select {
				case batch := <-client.Pushes():
					if len(batch) != 1 {
						t.Fatal("push queue lost its contents")
					}
					clearPushMessages(batch)
				default:
					t.Fatal("nonpersistent push was discarded")
				}
			} else {
				select {
				case <-client.Done():
					if code(client.Err()) != "lajiaoliao_receive_queue_full" {
						t.Fatal("queue overflow was hidden", client.Err())
					}
				case <-ctx.Done():
					t.Fatal("bounded push queue blocked the reader")
				}
				if code(err) != "lajiaoliao_receive_queue_full" {
					t.Fatal("read RPC hid the terminal receive failure", err)
				}
			}
		})
	}
}

func TestPushSurvivesRemoteDisconnectUntilOwnerDrains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		data, _ := proto.Marshal(&sdkws.PushMessages{Msgs: map[string]*sdkws.PullMsgs{
			"si_self_peer": {Msgs: []*sdkws.MsgData{{ClientMsgID: "disconnect-push", Content: []byte("synthetic")}}},
		}})
		var frame bytes.Buffer
		gob.NewEncoder(&frame).Encode(wireResponse{ReqIdentifier: 2001, Data: data})
		c.WriteMessage(websocket.BinaryMessage, frame.Bytes())
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := newTransport(conn, Session{UserID: "self"})
	defer client.Close()
	select {
	case <-client.readerDone:
	case <-time.After(time.Second):
		t.Fatal("remote disconnect did not stop the reader")
	}
	select {
	case batch := <-client.Pushes():
		if len(batch) != 1 || string(batch[0].Message.Content) != "synthetic" {
			t.Fatal("disconnect erased an already accepted push")
		}
		clearPushMessages(batch)
	default:
		t.Fatal("nonpersistent push was discarded on disconnect")
	}
	client.Close()
	client.Close()
}
