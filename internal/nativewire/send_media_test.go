//go:build sodium && cgo

package nativewire

import (
	"bytes"
	"testing"
)

func TestMediaUsesReviewedFormatsAndOriginalEnvelope(t *testing.T) {
	for _, fixture := range []struct {
		media  OutboundMedia
		format uint64
		field  int
	}{
		{OutboundMedia{Kind: "image", Asset: "18446744073709551000", Size: 1024, Width: 80, Height: 60}, 1, 101},
		{OutboundMedia{Kind: "voice", Asset: "456", Size: 16044, Duration: 1}, 2, 102},
		{OutboundMedia{Kind: "video", Asset: "457", Size: 8096, Width: 320, Height: 180, Duration: 2}, 3, 103},
		{OutboundMedia{Kind: "file", Asset: "458", Size: 11, Filename: "测试.txt"}, 6, 104},
	} {
		t.Run(fixture.media.Kind, func(t *testing.T) {
			message := syntheticText()
			message.Text, message.Media = "", &fixture.media
			plain, err := EncodeGroupText(message)
			if err != nil {
				t.Fatal(err)
			}
			fields, err := messageFields(plain)
			if err != nil || len(fields[100]) != 0 || len(fields[11]) != 1 || fields[11][0].n != fixture.format {
				t.Fatal("media was downgraded or used asset type as message format")
			}
			sealed, err := SealGroupText(bytes.Repeat([]byte{17}, 32), message, 800, 2)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := OpenMessage(bytes.Repeat([]byte{17}, 32), sealed)
			if err != nil || decoded.Format != fixture.format || !decoded.SupportedContent() {
				t.Fatal("synthetic media roundtrip", err)
			}
			message.Text = "silently dropped text"
			if _, err = EncodeGroupText(message); err == nil {
				t.Fatal("mixed text/media accepted")
			}
		})
	}
	for _, media := range []OutboundMedia{
		{Kind: "image", Asset: "https://example.com/image", Size: 1, Width: 1, Height: 1},
		{Kind: "image", Asset: "1", Size: 1, Width: 99999999, Height: 1},
		{Kind: "voice", Asset: "1", Size: 1, Duration: 301},
		{Kind: "file", Asset: "1", Size: 1, Filename: "../file"},
		{Kind: "file", Asset: "1", Size: MaxUploadBytes + 1, Filename: "file"},
	} {
		if media.Validate() == nil {
			t.Fatal("invalid media accepted")
		}
	}
}
