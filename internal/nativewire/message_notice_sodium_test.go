//go:build sodium && cgo

package nativewire

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func unencryptedNotice(body []byte, header uint64) string {
	var envelope []byte
	for _, v := range []struct {
		field protowire.Number
		value uint64
	}{{1, header}, {2, 1700000000000}, {3, 19}} {
		envelope = protowire.AppendFixed64(protowire.AppendTag(envelope, v.field, protowire.Fixed64Type), v.value)
	}
	envelope = append(envelope, msgBytes(4, body)...)
	return base64.RawURLEncoding.EncodeToString(envelope)
}

func TestReadUnencryptedNoticeIsNeverAuthenticatedApplication(t *testing.T) {
	payload := msgBytes(106, msgBytes(5, []byte("<script>display only</script>")))
	body := mediaPlain(7, payload)
	header := uint64(123<<34 | 456<<4 | 2<<2 | 1)
	text := unencryptedNotice(body, header)
	group, notice, err := ReadUnencryptedGroupNotice(text)
	if err != nil || group != 456 || notice.Text != "<script>display only</script>" || notice.Verification != "transport" {
		t.Fatal("legacy display notice mismatch", err)
	}
	_, err = OpenMessage(bytes.Repeat([]byte{17}, 32), text)
	var stage *DecodeError
	if !errors.As(err, &stage) || stage.Stage != "envelope_authentication_missing" {
		t.Fatal("legacy notice became authenticated content", err)
	}
	for _, session := range []uint64{1, 3} {
		system := append(msgSource(1, 123, "sender"), msgSource(2, 456, "group")...)
		system = append(system, msgNum(5, session)...)
		system = append(system, msgNum(11, 7)...)
		system = append(system, payload...)
		group, notice, err := ReadUnencryptedGroupNotice(unencryptedNotice(system, 123<<34|456<<4|session<<2))
		if session == 3 && (err != nil || group != 456 || notice.Verification != "transport") {
			t.Fatal("fixed MSG_KIND_GROUP_SYSTEM was not recognized", err)
		}
		if session == 1 && err == nil {
			t.Fatal("private plaintext was accepted as a group notification")
		}
	}
	for _, bad := range []string{
		unencryptedNotice(body, header+1),
		unencryptedNotice(mediaPlain(0, msgBytes(100, msgBytes(1, []byte("unauthenticated text")))), header),
		unencryptedNotice(mediaPlain(7, msgBytes(106, msgBytes(5, []byte{0xff}))), header),
		unencryptedNotice(mediaPlain(7, msgBytes(106, msgBytes(5, []byte(strings.Repeat("x", 4097))))), header),
		unencryptedNotice(mediaPlain(7, append(payload, msgBytes(100, msgBytes(1, []byte("mixed")))...)), header),
		unencryptedNotice(mediaPlain(7, append(payload, payload...)), header),
		"%%",
	} {
		if _, _, err := ReadUnencryptedGroupNotice(bad); err == nil {
			t.Fatal("invalid or executable legacy content accepted")
		}
	}
}

func TestAuthenticatedNoticeTypesAndRouting(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	for _, fixture := range []struct {
		format    uint64
		field     protowire.Number
		textField protowire.Number
	}{
		{7, 106, 5}, {8, 107, 2}, {10, 201, 0}, {12, 100, 1}, {14, 201, 0}, {28, 100, 1}, {31, 100, 1}, {39, 112, 0},
	} {
		payload := msgNum(1, 1)
		if fixture.textField != 0 {
			payload = msgBytes(fixture.textField, []byte("synthetic notice"))
		}
		raw := mediaPlain(fixture.format, msgBytes(fixture.field, payload))
		out, err := OpenMessage(key, fixtureEnvelope(t, key, raw, false, uint64(123<<34|456<<4|2<<2|1)))
		if err != nil || out.Notice == nil || out.Notice.Verification != "authenticated" || !out.SupportedContent() || out.Text != "" {
			t.Fatalf("notice %d did not retain its trust boundary: %v", fixture.format, err)
		}
		if _, err := OpenMessage(bytes.Repeat([]byte{18}, 32), fixtureEnvelope(t, key, raw, false, uint64(123<<34|456<<4|2<<2|1))); err == nil {
			t.Fatal("notice skipped body authentication")
		}
	}
}

func TestLargeImageMetadataRetainedWithoutPreview(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	for _, dimensions := range [][3]uint64{{1560, 2080, 9737997}, {4284, 5712, 806052}, {32, 24, 120}} {
		image := msgBytes(1, msgBytes(1, []byte("123")))
		for i, n := range dimensions {
			image = append(image, msgNum(protowire.Number(i+2), n)...)
		}
		out, err := OpenMessage(key, fixtureEnvelope(t, key, mediaPlain(1, msgBytes(101, image)), false, uint64(123<<34|456<<4|2<<2|1)))
		if err != nil || out.Image == nil || !out.SupportedContent() {
			t.Fatal("bounded metadata rejected", err)
		}
		allowed := dimensions[2] <= MaxImageBytes && dimensions[0]*dimensions[1] <= MaxImagePixels
		if out.Image.PreviewAllowed() != allowed || out.Image.PreviewLimited == allowed {
			t.Fatal("resource policy lost")
		}
	}
}
