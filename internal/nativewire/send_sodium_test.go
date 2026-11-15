//go:build sodium && cgo

package nativewire

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestSealGroupTextDesktopRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	for _, text := range []string{"hello", "synthetic \U0001f642 <script>not-html</script>", strings.Repeat("abc ", 950)} {
		m := syntheticText()
		m.Text = text
		sealed, err := SealGroupText(key, m, 777, 3)
		if err != nil {
			t.Fatal("synthetic sealing failed", err)
		}
		out, err := OpenMessage(key, sealed)
		if err != nil || out.Sender.ID != m.SenderID || out.Target.ID != m.GroupID ||
			out.ClientID != m.ClientID || out.CreatedAt != 1700000000123 || out.Text != text ||
			len(out.Mentions) != 0 || out.Quote != nil || out.Session != 2 {
			t.Fatal("desktop outbound format failed to decode", err)
		}
		if _, err = OpenMessage(bytes.Repeat([]byte{18}, 32), sealed); err == nil {
			t.Fatal("wrong message key authenticated")
		}
	}
	for _, args := range [][2]uint64{{0, 3}, {777, 0}} {
		if _, err := SealGroupText(key, syntheticText(), args[0], args[1]); err == nil {
			t.Fatal("unreserved nonce/AAD accepted")
		}
	}
	// Independent protobuf encoding: desktop Timestamp plus the exact reviewed
	// session/device/version/client/content fields. No transport credentials.
	plain, _ := EncodeGroupText(syntheticText())
	const golden = "0a02087b120308c8031a0b0880e2cfaa0610c0a9d33a20012802300292032465333931616437352d636262632d346365312d613333332d623462333862653765343135a206160a1473796e7468657469632067726f75702074657874"
	if hex.EncodeToString(plain) != golden {
		t.Fatalf("desktop protobuf vector changed: %x", plain)
	}
}

func TestSealRichGroupAndPrivateRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	m := syntheticText()
	m.Text = "\U0001f642 @阿明 hello"
	m.Mentions = []MessageMention{{UID: 7, Name: "阿明", Start: 3, End: 7}}
	m.Quote = &MessageQuote{ClientID: syntheticClientID, ServerID: "9001", Text: "quoted"}
	sealed, err := SealGroupText(key, m, 778, 3)
	if err != nil {
		t.Fatal(err)
	}
	out, err := OpenMessage(key, sealed)
	if err != nil || out.Text != m.Text || !reflect.DeepEqual(out.Mentions, m.Mentions) ||
		!reflect.DeepEqual(out.Quote, m.Quote) || out.Session != 2 {
		t.Fatal("rich group roundtrip changed components", out, err)
	}
	private := PrivateText{SenderID: m.SenderID, PeerID: m.GroupID, ClientID: m.ClientID, Text: m.Text, CreatedAt: m.CreatedAt}
	sealed, err = SealPrivateText(key, private, 779, 3)
	if err != nil {
		t.Fatal(err)
	}
	out, err = OpenMessage(key, sealed)
	if err != nil || out.Session != 1 || out.Target.ID != m.GroupID || out.Text != m.Text {
		t.Fatal("private session was encoded as a group", out, err)
	}
}
