package gateway

import (
	"context"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func TestDecoderScopeAndUnsupported(t *testing.T) {
	if conversationKey("private", 7, "peer-a") == conversationKey("private", 7, "peer-b") ||
		conversationKey("group", 7, "peer-a") == conversationKey("private", 7, "peer-a") {
		t.Fatal("conversation scope collapsed")
	}
	outer := nativewire.IMMessage{Scene: 0, Target: "me", Sender: "peer", Fields: map[uint32]string{8: "5"}}
	out, scene, key, state, _ := decodeApplication("", nativeauth.Session{}, outer)
	if state != "unsupported" || scene != "private" || key != "" || out.Message.Text != "" {
		t.Fatal("unsupported native event invented authenticated content")
	}
	outer.Fields[8] = "100"
	_, _, key, state, reason := decodeApplication("", nativeauth.Session{}, outer)
	if state != "failed" || reason != "message_key_missing" || key != "" {
		t.Fatal("missing key admitted a conversation")
	}
}

func TestMediaProjectionKeepsRoutingAndUnsupportedStatus(t *testing.T) {
	session := nativeauth.Session{UID: 123, NIMID: "self"}
	outer := nativewire.IMMessage{Scene: 1, Target: "team", Sender: "peer"}
	message := nativewire.ApplicationMessage{Session: 2, Format: 1,
		Sender: nativewire.MessageSource{ID: 7}, Target: nativewire.MessageSource{ID: 456},
		Image: &nativewire.MessageImage{Asset: "9007199254740993", Sticker: true}}
	out, scene, key, state, _ := bindDecodedApplication(session, outer, message, DecodedMessage{}, "group")
	if scene != "group" || key == "" || state != "decoded" || out.Message.Image.Asset != message.Image.Asset {
		t.Fatal("media did not enter scoped projection")
	}
	message.Image = nil
	_, _, _, state, _ = bindDecodedApplication(session, outer, message, DecodedMessage{}, "group")
	if state != "unsupported" {
		t.Fatal("missing image was presented as decoded")
	}
	message.Format = 42
	_, _, _, state, _ = bindDecodedApplication(session, outer, message, DecodedMessage{}, "group")
	if state != "unsupported" {
		t.Fatal("unknown format was presented as text")
	}
}

func TestBusinessGateCancellation(t *testing.T) {
	s := NewReadScheduler(context.Background())
	defer s.Close()
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.Do(context.Background(), "synthetic", "first", ReadBackground, func(ctx context.Context) error {
			close(started)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		})
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.Do(ctx, "synthetic", "second", ReadBackground, func(ctx context.Context) error { return nil }); err == nil {
		t.Fatal("concurrent account HTTP or ignored cancellation")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPrivateIncomingOutgoingBinding(t *testing.T) {
	session := nativeauth.Session{UID: 123, NIMID: "self"}
	incoming := nativewire.IMMessage{Scene: 0, Target: "self", Sender: "peer"}
	inner := nativewire.ApplicationMessage{Session: 1, Sender: nativewire.MessageSource{ID: 456}, Target: nativewire.MessageSource{ID: 123}}
	_, _, incomingKey, state, _ := bindDecodedApplication(session, incoming, inner, DecodedMessage{}, "private")
	if state != "decoded" || incomingKey == "" {
		t.Fatal("valid incoming private routing rejected")
	}
	outgoing := nativewire.IMMessage{Scene: 0, Target: "peer", Sender: "self"}
	inner.Sender, inner.Target = inner.Target, inner.Sender
	result, _, outgoingKey, state, _ := bindDecodedApplication(session, outgoing, inner, DecodedMessage{}, "private")
	if state != "decoded" || outgoingKey != incomingKey || !result.Outgoing {
		t.Fatal("sent and received private messages split or lost routing")
	}
	outgoing.Sender = "unrelated-peer"
	_, _, key, state, _ := bindDecodedApplication(session, outgoing, inner, DecodedMessage{}, "private")
	if state != "failed" || key != "" {
		t.Fatal("application-only identity admitted an unrelated transport")
	}
}
