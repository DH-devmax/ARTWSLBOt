package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func TestIMLoginModesAndCallbackExtension(t *testing.T) {
	for _, protocol := range []string{nativeauth.DesktopProtocolVersion} {
		for _, mode := range []int{0, 1, 2} {
			d := nativeauth.Deployment{ProtocolVersion: protocol, DeviceID: "synthetic-device"}
			s := nativeauth.Session{ProtocolVersion: protocol, NIMID: "synthetic-peer", NIMToken: "synthetic-token", NIMAuthType: mode}
			body, err := imLoginBody(d, s, "synthetic-app", "synthetic-connection")
			if err != nil {
				t.Fatal(err)
			}
			fields, offset, err := nativewire.ReadProperties(body, 0)
			if err != nil || offset != len(body) || string(fields[3]) != "16" ||
				string(fields[13]) != d.DeviceID || string(fields[1000]) != s.NIMToken {
				t.Fatal("login envelope no longer matches the pinned Web transport")
			}
			if mode == 0 {
				if _, ok := fields[115]; ok {
					t.Fatal("static login acquired an authentication override")
				}
			} else if string(fields[115]) != string(rune('0'+mode)) {
				t.Fatal("authentication mode lost")
			}
			if mode == 2 {
				if string(fields[116]) != s.NIMToken {
					t.Fatal("desktop callback login omitted the official token extension")
				}
				s.NIMToken = "synthetic-renewed-token"
				later, _ := imLoginBody(d, s, "synthetic-app", "next-connection")
				next, _, _ := nativewire.ReadProperties(later, 0)
				if string(next[116]) != s.NIMToken || string(next[1000]) != s.NIMToken ||
					string(fields[116]) != "synthetic-token" {
					t.Fatal("desktop callback extension was stale or mutated across connections")
				}
			} else if _, ok := fields[116]; ok {
				t.Fatal("callback extension leaked into another authentication mode")
			}
		}
	}
	if _, err := imLoginBody(nativeauth.Deployment{ProtocolVersion: nativeauth.DesktopProtocolVersion},
		nativeauth.Session{ProtocolVersion: nativeauth.DesktopProtocolVersion, NIMAuthType: 3}, "", ""); err == nil {
		t.Fatal("unknown authentication mode was sent")
	}
	for _, version := range []string{"", "3.7.1", "3.7.3", "3.7.4"} {
		if _, err := imLoginBody(nativeauth.Deployment{ProtocolVersion: version},
			nativeauth.Session{ProtocolVersion: version}, "", ""); err == nil {
			t.Fatal("removed mobile protocol can still construct an IM login")
		}
	}
}

func TestIMLoginRejectionPreservesSafeCode(t *testing.T) {
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
		reply := []byte{0, f.Service, f.Command, byte(f.Serial), byte(f.Serial >> 8), 2}
		reply = binary.LittleEndian.AppendUint16(reply, 302)
		reply = append(reply, []byte("do-not-expose-upstream-body")...)
		_ = conn.WriteMessage(websocket.BinaryMessage, reply)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := openIM(ctx, websocket.DefaultDialer, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	var rejected *IMLoginError
	stage, code := receiverDiagnostic(err)
	if !errors.As(err, &rejected) || rejected.Code != 302 || stage != "im_login" ||
		code != 302 || receiverError(err) != "im_auth_rejected" ||
		err.Error() != "im_auth_rejected" || !receiverBlocked(receiverError(err)) {
		t.Fatal("login rejection lost its numeric result or disclosed upstream content")
	}
}

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

func TestIMRateLimitsPreserveRetryAfter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := &http.Client{Transport: discoveryRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"120"}},
			Body: io.NopCloser(strings.NewReader("synthetic rate limit"))}, nil
	})}
	_, err := discoverIM(ctx, client, "synthetic-key", "synthetic-peer")
	var limited *RateLimited
	if !errors.As(err, &limited) || receiverRetry(err, time.Second) != 120*time.Second ||
		receiverError(err) != "native_rate_limited" || receiverBlocked(receiverError(err)) {
		t.Fatal("discovery limit lost cooldown or became a permanent block")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.WriteHeader(429)
	}))
	defer server.Close()
	_, err = openIM(ctx, websocket.DefaultDialer, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if !errors.As(err, &limited) || receiverRetry(err, time.Second) != 45*time.Second {
		t.Fatal("WebSocket handshake limit lost cooldown")
	}
}

func TestReceiverRetryClassification(t *testing.T) {
	for _, code := range []string{"im_disconnected", "im_reply_timeout", "im_sync_timeout",
		"im_connection_failed", "bounded_ingress_full", "native_connection_rotation_required"} {
		got := receiverError(errors.New(code))
		if got != code || receiverBlocked(got) {
			t.Fatalf("recoverable connection error became a permanent block: %s", code)
		}
	}
	for _, err := range []error{nativewire.ErrPushBoundary, errors.New("im_kicked"),
		&nativeauth.Error{Code: "native_reauth_required"},
		&nativeauth.Error{Code: "native_transport_unknown", Submitted: true},
		&nativeauth.Error{Code: "native_response_invalid", Submitted: true},
		&nativeauth.Error{Code: "native_nim_token_invalid"}} {
		if !receiverBlocked(receiverError(err)) {
			t.Fatalf("unsafe renewal or packet boundary was automatically retryable: %T", err)
		}
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
