//go:build sodium && cgo

package nativewire

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func mediaImage(asset string, sticker uint64) []byte {
	b := msgBytes(1, msgBytes(1, []byte(asset)))
	b = append(b, msgNum(2, 32)...)
	b = append(b, msgNum(3, 24)...)
	b = append(b, msgNum(4, 120)...)
	return append(b, msgNum(5, sticker)...)
}

func mediaPlain(format uint64, payload []byte) []byte {
	sender := append(msgNum(1, 123), msgBytes(2, []byte("synthetic-sender"))...)
	sender = append(sender, msgBytes(3, []byte("9007199254740993"))...)
	b := append(msgBytes(1, sender), msgSource(2, 456, "group")...)
	b = append(b, msgNum(4, 1)...)
	b = append(b, msgNum(5, 2)...)
	b = append(b, msgNum(11, format)...)
	return append(b, payload...)
}

func mediaVideo() []byte {
	b := msgBytes(1, msgBytes(1, []byte("18446744073709551615")))
	b = append(b, msgBytes(2, msgBytes(1, []byte("9007199254740993")))...)
	b = append(b, msgNum(4, 61)...)
	b = append(b, msgNum(5, 320)...)
	b = append(b, msgNum(6, 240)...)
	return append(b, msgNum(7, 120)...)
}

func TestAuthenticatedVideo(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	open := func(format uint64, payload []byte) (ApplicationMessage, error) {
		return OpenMessage(key, fixtureEnvelope(t, key, mediaPlain(format, payload), true, uint64(123<<34|456<<4|2<<2|1)))
	}
	out, err := open(3, msgBytes(103, mediaVideo()))
	if err != nil || !out.SupportedContent() || out.Video == nil ||
		out.Video.Asset != "18446744073709551615" || out.Video.Screenshot != "9007199254740993" ||
		out.Video.Duration != 61 || out.Video.Width != 320 || out.Video.Height != 240 || out.Video.Size != 120 {
		t.Fatal("video metadata lost", err)
	}
	encoded := fixtureEnvelope(t, key, mediaPlain(3, msgBytes(103, mediaVideo())), false, uint64(123<<34|456<<4|2<<2|1))
	if _, err = OpenMessage(bytes.Repeat([]byte{18}, 32), encoded); err == nil {
		t.Fatal("video bypassed authentication")
	}
	external := "https://127.0.0.1/private?secret=must-not-expose"
	out, err = open(3, msgBytes(103, msgBytes(3, []byte(external))))
	if err != nil || !out.Video.External || out.Video.Asset != "" {
		t.Fatal("external video was not represented safely", err)
	}
	withAsset := func(extra []byte) []byte {
		return append(msgBytes(1, msgBytes(1, []byte("9"))), extra...)
	}
	cases := [][]byte{
		nil,
		msgBytes(103, nil),
		msgBytes(103, msgBytes(1, msgBytes(1, []byte("https://127.0.0.1/a")))),
		msgBytes(103, withAsset(msgBytes(2, msgBytes(1, []byte("../9"))))),
		msgBytes(103, withAsset(msgNum(4, 1<<31))),
		msgBytes(103, withAsset(msgNum(5, 16385))),
		msgBytes(103, withAsset(msgNum(7, ^uint64(0)))),
		msgBytes(103, withAsset(msgBytes(3, []byte{0xff}))),
		msgBytes(103, append(withAsset(msgNum(4, 1)), msgNum(4, 2)...)),
		append(msgBytes(103, mediaVideo()), msgBytes(103, mediaVideo())...),
		append(msgBytes(103, mediaVideo()), msgBytes(100, msgBytes(1, []byte("mixed")))...),
	}
	for index, payload := range cases {
		_, err = open(3, payload)
		var stage *DecodeError
		if !errors.As(err, &stage) || stage.Stage != "schema_video" {
			t.Fatalf("invalid video %d admitted: %v", index, err)
		}
	}
	entry := append(msgNum(23, 3), msgBytes(103, mediaVideo())...)
	out, err = open(9, msgBytes(317, msgBytes(3, entry)))
	if err != nil || out.Forward.Entries[0].Video == nil || out.Forward.Entries[0].Reason != "" {
		t.Fatal("forward video lost", err)
	}
	if (ApplicationMessage{Format: 3}).SupportedContent() {
		t.Fatal("missing video counted as supported")
	}
}

func TestAuthenticatedImageStickerAndAvatar(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	for _, sticker := range []uint64{0, 1} {
		plain := mediaPlain(1, msgBytes(101, mediaImage("18446744073709551615", sticker)))
		encoded := fixtureEnvelope(t, key, plain, true, uint64(123<<34|456<<4|2<<2|1))
		out, err := OpenMessage(key, encoded)
		if err != nil || out.Image == nil || !out.SupportedContent() ||
			out.Image.Asset != "18446744073709551615" || out.Image.Sticker != (sticker == 1) ||
			out.Sender.Avatar != "9007199254740993" {
			t.Fatal("authenticated media metadata lost", err)
		}
		if _, err = OpenMessage(bytes.Repeat([]byte{18}, 32), encoded); err == nil {
			t.Fatal("media bypassed authentication")
		}
	}
	for _, asset := range []string{"0", "01", "-1", "18446744073709551616", "https://example.com/a", "../9", strings.Repeat("9", 100)} {
		plain := mediaPlain(1, msgBytes(101, mediaImage(asset, 0)))
		_, err := OpenMessage(key, fixtureEnvelope(t, key, plain, false, uint64(123<<34|456<<4|2<<2|1)))
		var stage *DecodeError
		if !errors.As(err, &stage) || stage.Stage != "schema_image" {
			t.Fatal("invalid asset was admitted", err)
		}
	}
	invalid := mediaImage("123", 0)
	invalid = append(invalid, msgNum(2, 65535)...)
	plain := mediaPlain(1, msgBytes(101, invalid))
	if _, err := OpenMessage(key, fixtureEnvelope(t, key, plain, false, uint64(123<<34|456<<4|2<<2|1))); err == nil {
		t.Fatal("duplicate image dimension admitted")
	}
}

func TestForwardRecordBoundsAndUnsupportedEntries(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	forward := msgBytes(1, []byte("synthetic-chat-record"))
	text := append(msgNum(1, 7), msgBytes(2, []byte("<script>sender</script>"))...)
	text = append(text, msgBytes(8, []byte("12345"))...)
	text = append(text, msgNum(25, 1700000000000)...)
	text = append(text, msgBytes(100, msgBytes(1, []byte("<img src=x> plain text")))...)
	forward = append(forward, msgBytes(3, text)...)
	image := append(msgNum(23, 1), msgBytes(101, mediaImage("987", 1))...)
	forward = append(forward, msgBytes(3, image)...)
	for _, format := range []uint64{9, 6} {
		forward = append(forward, msgBytes(3, msgNum(23, format))...)
	}
	for range MaxForwardEntries {
		forward = append(forward, msgBytes(3, text)...)
	}
	plain := mediaPlain(9, msgBytes(317, forward))
	out, err := OpenMessage(key, fixtureEnvelope(t, key, plain, true, uint64(123<<34|456<<4|2<<2|1)))
	if err != nil || out.Forward == nil || !out.SupportedContent() {
		t.Fatal("forward decode failed", err)
	}
	f := out.Forward
	if !f.Truncated || f.Total != 104 || len(f.Entries) != 100 || f.Entries[0].Text != "<img src=x> plain text" ||
		f.Entries[1].Image == nil || !f.Entries[1].Image.Sticker ||
		f.Entries[2].Reason != "nested_forward_unresolved" || f.Entries[3].Reason != "forward_format_unsupported" {
		t.Fatal("forward content, limits or unsupported types hidden")
	}
}

func TestAuthenticatedFriendCard(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	card := msgNum(1, 789)
	card = append(card, msgBytes(2, []byte("9007199254740993.jpg"))...)
	card = append(card, msgBytes(3, []byte("<img src=x> 联系人"))...)
	card = append(card, msgNum(4, 12345678)...)
	card = append(card, msgNum(5, 2)...)
	card = append(card, msgNum(6, 1)...)
	card = append(card, msgBytes(7, append(msgNum(1, 1700000000), msgNum(2, 123000000)...))...)
	encoded := fixtureEnvelope(t, key, mediaPlain(13, msgBytes(108, card)), true, uint64(123<<34|456<<4|2<<2|1))
	out, err := OpenMessage(key, encoded)
	if err != nil || !out.SupportedContent() || out.FriendCard == nil {
		t.Fatal("friend card decode failed", err)
	}
	c := out.FriendCard
	if c.UserID != 789 || c.AccountID != 12345678 || c.Nickname != "<img src=x> 联系人" ||
		c.Avatar != "9007199254740993.jpg" || c.SharedAt != 1700000000123 || !c.IsPretty || c.VIPLevel != 2 ||
		out.Sender.ID != 123 || out.Target.ID != 456 {
		t.Fatal("card metadata or authenticated routing was changed")
	}
	if _, err := OpenMessage(bytes.Repeat([]byte{18}, 32), encoded); err == nil {
		t.Fatal("card bypassed authentication")
	}
	withID := func(extra []byte) []byte { return append(msgNum(1, 789), extra...) }
	cases := [][]byte{
		nil,
		msgBytes(108, nil),
		msgBytes(108, msgNum(1, 1<<30)),
		msgBytes(108, withID(msgNum(1, 790))),
		msgBytes(108, withID(msgBytes(3, []byte{0xff}))),
		msgBytes(108, withID(msgNum(6, 2))),
		msgBytes(108, withID(msgNum(4, 1<<31))),
		msgBytes(108, withID(msgBytes(7, msgNum(2, 1000000000)))),
		msgBytes(108, withID(msgBytes(2, []byte(strings.Repeat("x", 1025))))),
		append(msgBytes(108, withID(nil)), msgBytes(108, withID(nil))...),
		append(msgBytes(108, withID(nil)), msgBytes(100, msgBytes(1, []byte("mixed")))...),
		append(msgBytes(108, withID(nil)), msgBytes(104, nil)...),
	}
	for index, payload := range cases {
		_, err := OpenMessage(key, fixtureEnvelope(t, key, mediaPlain(13, payload), false, uint64(123<<34|456<<4|2<<2|1)))
		var stage *DecodeError
		if !errors.As(err, &stage) || stage.Stage != "schema_friend_card" {
			t.Fatalf("invalid card %d admitted: %v", index, err)
		}
	}
	if (ApplicationMessage{Format: 13}).SupportedContent() {
		t.Fatal("missing card counted as supported")
	}
}
