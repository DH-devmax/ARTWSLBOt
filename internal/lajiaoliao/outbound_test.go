package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestOutboundUsesPinnedQuoteMentionAndPrivateContracts(t *testing.T) {
	for _, mode := range []string{"text", "quote", "mention", "private"} {
		t.Run(mode, func(t *testing.T) {
			writes := make(chan *sdkws.MsgData, 1)
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
				var request wireRequest
				if gob.NewDecoder(bytes.NewReader(raw)).Decode(&request) != nil || request.ReqIdentifier != 1003 {
					return
				}
				var m sdkws.MsgData
				if proto.Unmarshal(request.Data, &m) != nil {
					return
				}
				writes <- &m
				receipt := protowire.AppendTag(nil, 1, protowire.BytesType)
				receipt = protowire.AppendString(receipt, "server-string")
				receipt = protowire.AppendTag(receipt, 2, protowire.BytesType)
				receipt = protowire.AppendString(receipt, m.ClientMsgID)
				receipt = protowire.AppendTag(receipt, 3, protowire.VarintType)
				receipt = protowire.AppendVarint(receipt, 1700000000123)
				var response bytes.Buffer
				gob.NewEncoder(&response).Encode(wireResponse{ReqIdentifier: 1003, MsgIncr: request.MsgIncr, Data: receipt})
				conn.WriteMessage(websocket.BinaryMessage, response.Bytes())
			}))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			transport := newTransport(conn, Session{UserID: "self-string"})
			defer transport.Close()
			transport.verified.Store(true)
			message := Outbound{Text: "bounded message"}
			peer := ""
			if mode == "quote" {
				message.Quote = &TextQuote{ClientID: "source-client", ServerID: "source-server",
					Sender: "peer-string", Group: "group-string", Time: 1700000000000, Seq: 10, Text: "source text"}
			}
			if mode == "mention" {
				message.Mention = &MessagePerson{ID: "peer-string", Name: "synthetic"}
			}
			if mode == "private" {
				peer = "peer-string"
			}
			fences := 0
			receipt, err := transport.SendOutbound(context.Background(), "group-string", peer, "client-string", message,
				func(ctx context.Context, send func(context.Context) error) error { fences++; return send(ctx) })
			if err != nil || fences != 1 || receipt.ClientID != "client-string" || receipt.ServerID != "server-string" {
				t.Fatal(receipt, fences, err)
			}
			wire := <-writes
			if wire.SessionType != 3 || wire.GroupID != "group-string" || wire.RecvID != "group-string" {
				if mode != "private" || wire.SessionType != 1 || wire.GroupID != "" || wire.RecvID != peer {
					t.Fatal("cross scene or numeric route", wire)
				}
			}
			var object map[string]json.RawMessage
			if json.Unmarshal(wire.Content, &object) != nil {
				t.Fatal("invalid JSON content")
			}
			switch mode {
			case "mention":
				if wire.ContentType != 106 || string(object["atUserList"]) != `["peer-string"]` {
					t.Fatal("mention collapsed into text", string(wire.Content))
				}
			case "quote":
				if wire.ContentType != 114 || !bytes.Contains(object["quoteMessage"], []byte(`"seq":10`)) {
					t.Fatal("quote lost source identity", string(wire.Content))
				}
			default:
				if wire.ContentType != 101 {
					t.Fatal("wrong text content type")
				}
			}
		})
	}
}
