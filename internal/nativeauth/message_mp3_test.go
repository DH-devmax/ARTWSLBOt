package nativeauth

import (
	"bytes"
	"testing"
)

func TestMP3UploadBoundsAndFrameDuration(t *testing.T) {
	frame := make([]byte, 72)
	copy(frame, []byte{0xff, 0xe3, 0x18, 0xc4}) // MPEG 2.5 Layer III, 8 kbps, 8 kHz.
	clip := bytes.Repeat(frame, 12)
	duration, err := mp3Duration(clip)
	if err != nil || duration != 1 {
		t.Fatal("valid frame duration", duration, err)
	}
	if meta, err := InspectUpload(MessageUpload{Kind: "voice", Name: "synthetic.mp3", Data: clip}); err != nil || meta.Duration != 1 {
		t.Fatal("MP3 voice not admitted", meta, err)
	}
	vbr := append([]byte{}, clip...)
	wider := make([]byte, 144)
	copy(wider, []byte{0xff, 0xe3, 0x28, 0xc4})
	vbr = append(vbr, wider...)
	if _, err := mp3Duration(vbr); err != nil {
		t.Fatal("variable bitrate rejected", err)
	}
	for _, invalid := range [][]byte{
		clip[:len(clip)-1],
		append(append([]byte{}, clip...), 0),
		{0xff, 0xeb, 0x18, 0xc4},
		{0xff, 0xe3, 0x08, 0xc4},
		[]byte("ID3\x04\x00\x00\x7f\x7f\x7f\x7f"),
		bytes.Repeat(frame, 4167),
		append(append([]byte{}, clip...), []byte{0xff, 0xfb, 0x90, 0x64}...),
	} {
		if _, err := mp3Duration(invalid); err == nil {
			t.Fatal("truncated, mixed or unbounded MP3 accepted")
		}
	}
}
