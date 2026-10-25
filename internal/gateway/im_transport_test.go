package gateway

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"wangshangliao/internal/nativewire"
)

type discoveryRoundTrip func(*http.Request) (*http.Response, error)

func (f discoveryRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscoveryIgnoresUnrelatedCommonFields(t *testing.T) {
	client := &http.Client{Transport: discoveryRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "lbs.netease.im" || r.URL.Query().Get("k") != "synthetic-app" {
			t.Fatal("invalid discovery contract")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(
			`{"common":{"link":["secure.example:443"],"link.default":["secure.example:443"],"unrelated":{"version":1}}}`))}, nil
	})}
	endpoints, err := discoverIM(context.Background(), client, "synthetic-app", "synthetic-peer")
	if err != nil || len(endpoints) != 1 || endpoints[0] != "wss://secure.example:443/websocket" {
		t.Fatal("discovery rejected unrelated metadata or duplicated endpoints")
	}
}

func replyPacket(f nativewire.Frame, body []byte) []byte {
	packet, _ := nativewire.Packet(f.Service, f.Command, f.Serial, body)
	return packet
}

func TestIMSingleReaderAndOrderedSync(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			f, _ := nativewire.ReadPacket(raw)
			switch f.Service {
			case 2:
				body, _ := nativewire.Properties([]nativewire.Property{{Key: 102, Value: []byte("connection")}})
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, body))
			case 1:
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, nil))
			case 5:
				push, _ := nativewire.Packet(7, 14, 777, []byte("synthetic-notice"))
				_ = conn.WriteMessage(websocket.BinaryMessage, push)
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, binary.LittleEndian.AppendUint64(nil, 88)))
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	login, _ := nativewire.Properties([]nativewire.Property{{Key: 19, Value: []byte("synthetic")}})
	transport, err := openIM(ctx, websocket.DefaultDialer, "ws"+strings.TrimPrefix(server.URL, "http"), login)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close(context.Canceled)
	if _, err = transport.request(ctx, 5, 1, nil); err != nil {
		t.Fatal(err)
	}
	first, second := <-transport.pushes, <-transport.pushes
	if first.Frame.Service != 7 || second.Frame.Service != 5 {
		t.Fatal("sync reply overtook push")
	}
	if _, err = transport.heartbeat(ctx); err != nil {
		t.Fatal("pushes consumed heartbeat reply")
	}
}

func TestIMKickDoesNotContestOtherOwner(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		f, _ := nativewire.ReadPacket(raw)
		kick, _ := nativewire.Packet(2, 5, f.Serial, nil)
		_ = conn.WriteMessage(websocket.BinaryMessage, kick)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := openIM(ctx, websocket.DefaultDialer, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err == nil || err.Error() != "im_kicked" {
		t.Fatal("kick did not stop login")
	}
	if receiverError(err) != "im_kicked" {
		t.Fatal("lost bounded failure reason")
	}
}
