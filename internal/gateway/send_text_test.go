package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"wangshangliao/internal/nativewire"
)

const outboundClientID = "e391ad75-cbbc-4ce1-a333-b4b38be7e415"

func syntheticSender(t *testing.T, reply func(*websocket.Conn, nativewire.Frame), count *atomic.Int64,
	readReply ...func(*websocket.Conn, nativewire.Frame)) *IMTransport {
	t.Helper()
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
			f, err := nativewire.ReadPacket(raw)
			if err != nil {
				return
			}
			switch {
			case f.Service == 2 && f.Command == 3:
				body, _ := nativewire.Properties([]nativewire.Property{{Key: 102, Value: []byte("synthetic")}})
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, body))
			case f.Service == 1:
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, nil))
			case f.Service == 8 && f.Command == 27:
				if len(readReply) > 0 {
					readReply[0](conn, f)
					continue
				}
				body := append([]byte(nil), f.Body...)
				body = append(body, 0) // synthetic complete empty individual mute list
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, body))
			case f.Service == 8 && f.Command == 2 || f.Service == 7 && f.Command == 1:
				count.Add(1)
				reply(conn, f)
			}
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	transport, err := openIM(ctx, websocket.DefaultDialer, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { transport.close(context.Canceled) })
	return transport
}

func TestPrivateTextKeepsSeparateRouteAndSingleSubmission(t *testing.T) {
	for _, mode := range []string{"accepted", "revoked", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			var count atomic.Int64
			transport := syntheticSender(t, func(conn *websocket.Conn, f nativewire.Frame) {
				fields, end, err := nativewire.ReadProperties(f.Body, 0)
				if err != nil || end != len(f.Body) || f.Service != 7 || f.Command != 1 ||
					string(fields[0]) != "0" || string(fields[1]) != "synthetic-peer" {
					t.Error("private submission became a group request")
					return
				}
				if mode == "ambiguous" {
					_ = conn.Close()
					return
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, syntheticReceipt(outboundClientID)))
			}, &count)
			fence := allowSynthetic
			if mode == "revoked" {
				fence = func(context.Context, func(context.Context) error) error {
					return errors.New("synthetic_revoked")
				}
			}
			out, err := transport.sendPrivateText(context.Background(), "synthetic-peer", outboundClientID, "synthetic", fence)
			if mode == "accepted" {
				if err != nil || !out.Attempted || out.Receipt.ServerID != "9001" || count.Load() != 1 {
					t.Fatal("private receipt failed", out, err)
				}
			} else if mode == "revoked" {
				if err == nil || out.Attempted || count.Load() != 0 {
					t.Fatal("revoked private send wrote a packet", out, err)
				}
			} else if err == nil || !out.Attempted || count.Load() != 1 || out.Receipt.ServerID != "" {
				t.Fatal("unknown private send retried or reported accepted", out, err)
			}
		})
	}
}

func syntheticReceipt(client string) []byte {
	body, _ := nativewire.Properties([]nativewire.Property{{Key: 12, Value: []byte("9001")},
		{Key: 7, Value: []byte("1700000000123")}, {Key: 11, Value: []byte(client)}})
	return body
}

func allowSynthetic(ctx context.Context, write func(context.Context) error) error { return write(ctx) }

func TestGroupReplyKeepsMetadataInSingleWrite(t *testing.T) {
	var count atomic.Int64
	transport := syntheticSender(t, func(conn *websocket.Conn, frame nativewire.Frame) {
		fields, end, err := nativewire.ReadProperties(frame.Body, 0)
		if err != nil || end != len(frame.Body) || frame.Service != 8 || frame.Command != 2 ||
			string(fields[29]) != "8" || string(fields[30]) != "789" ||
			string(fields[32]) != "9000" || string(fields[36]) != "1700000000123" {
			t.Error("quote metadata lost or crossed native route")
		}
		_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(frame, syntheticReceipt(outboundClientID)))
	}, &count)
	reference := &nativewire.ReplyRoute{Sender: "8", Target: "789", TimeMS: "1700000000123",
		ServerID: "9000", ClientID: "a391ad75-cbbc-4ce1-a333-b4b38be7e415"}
	out, err := transport.sendGroupTextReply(context.Background(), "789", outboundClientID, "synthetic", reference, allowSynthetic)
	if err != nil || !out.Attempted || count.Load() != 1 {
		t.Fatal("reply bypassed single-write path", out, err)
	}
	reference.Target = "790"
	out, err = transport.sendGroupTextReply(context.Background(), "789", outboundClientID, "synthetic", reference, allowSynthetic)
	if err == nil || out.Attempted || count.Load() != 1 {
		t.Fatal("cross-team quote silently became plain text", out, err)
	}
}

func TestNativeTextMatchedReceiptAndNoRetry(t *testing.T) {
	for _, mode := range []string{"accepted", "rejected", "mismatch", "wrong_serial", "disconnected", "truncated", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			var count atomic.Int64
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			transport := syntheticSender(t, func(conn *websocket.Conn, f nativewire.Frame) {
				switch mode {
				case "disconnected":
					_ = conn.Close()
				case "cancelled":
					cancel()
				default:
					body := syntheticReceipt(outboundClientID)
					if mode == "mismatch" {
						body = syntheticReceipt("d391ad75-cbbc-4ce1-a333-b4b38be7e415")
					}
					if mode == "truncated" {
						body = body[:3]
					}
					if mode == "wrong_serial" {
						f.Serial++
					}
					packet := replyPacket(f, body)
					if mode == "rejected" {
						packet = []byte{0, 8, 2, byte(f.Serial), byte(f.Serial >> 8), 2}
						packet = binary.LittleEndian.AppendUint16(packet, 403)
					}
					_ = conn.WriteMessage(websocket.BinaryMessage, packet)
				}
			}, &count)
			result, err := transport.sendGroupText(ctx, "789", outboundClientID, "synthetic", allowSynthetic)
			if !result.Attempted || count.Load() != 1 {
				t.Fatal("submission repeated or not attempted", count.Load())
			}
			switch mode {
			case "accepted":
				if err != nil || result.Code != 200 || result.Receipt.ServerID != "9001" {
					t.Fatal("receipt correlation failed", err)
				}
			case "rejected":
				if err != nil || result.Code != 403 || result.Receipt.ServerID != "" {
					t.Fatal("explicit rejection not distinguished")
				}
			default:
				if err == nil || result.Receipt.ServerID != "" {
					t.Fatal("uncertain submission falsely succeeded")
				}
			}
		})
	}
}

func TestNativeTextFenceDoesNotBlockHeartbeat(t *testing.T) {
	var count atomic.Int64
	transport := syntheticSender(t, func(*websocket.Conn, nativewire.Frame) {}, &count)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		result, err := transport.sendGroupText(ctx, "789", outboundClientID, "synthetic",
			func(ctx context.Context, write func(context.Context) error) error {
				close(entered)
				<-release
				return errors.New("synthetic_revoked")
			})
		if result.Attempted {
			done <- errors.New("revoked fence wrote a packet")
			return
		}
		done <- err
	}()
	<-entered
	if _, err := transport.heartbeat(ctx); err != nil {
		close(release)
		t.Fatal("control check blocked independent heartbeat", err)
	}
	close(release)
	if err := <-done; err == nil || count.Load() != 0 {
		t.Fatal("rejected fence submitted native text")
	}
	// The single native connection remains usable after a denied business send.
	if _, err := transport.heartbeat(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledMuteReadLateReplyDoesNotBreakReceive(t *testing.T) {
	var writes atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{}, 1)
	defer close(release)
	transport := syntheticSender(t, func(*websocket.Conn, nativewire.Frame) { t.Error("unexpected native mutation") }, &writes,
		func(conn *websocket.Conn, frame nativewire.Frame) {
			close(entered)
			<-release
			body := append(append([]byte(nil), frame.Body...), 0)
			_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(frame, body))
		})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body, _ := nativewire.MutedMembersRequest("789")
	done := make(chan error, 1)
	go func() { _, err := transport.request(ctx, 8, 27, body); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("SDK read did not reach synthetic peer")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("slow SDK read did not honour cancellation")
	}
	release <- struct{}{}
	select {
	case late := <-transport.pushes:
		out, err := nativewire.DecodePush(late.Frame)
		if err != nil || out.Kind != "notice" || out.Boundary != nil || len(out.Messages) != 0 {
			t.Fatal("late SDK read corrupted receive boundary", out, err)
		}
	case <-time.After(time.Second):
		t.Fatal("late SDK read did not enter the normal bounded receive path")
	}
	if _, err := transport.heartbeat(context.Background()); err != nil || writes.Load() != 0 {
		t.Fatal("cancelled read damaged the existing connection", err)
	}
}
