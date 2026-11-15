package nativeauth

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestSecurityInputsDoNotAdmitOtherFields(t *testing.T) {
	flow := "71000000-0000-4000-8000-000000000001"
	for _, row := range []struct {
		kind string
		in   SecurityInput
	}{
		{"password_sms", SecurityInput{Captcha: "synthetic"}},
		{"password_verify", SecurityInput{Flow: flow, Code: "123456"}},
		{"password", SecurityInput{Flow: flow, OldPassword: "old-test", NewPassword: "new-test12"}},
		{"phone_old_sms", SecurityInput{Captcha: "synthetic"}},
		{"phone_new_sms", SecurityInput{Flow: flow, Phone: "13800000000", Captcha: "synthetic"}},
		{"phone", SecurityInput{Flow: flow, Code: "123456"}},
	} {
		if err := row.in.Validate(row.kind); err != nil {
			t.Fatal(row.kind, err)
		}
		row.in.Image = []byte("not allowed")
		if row.in.Validate(row.kind) == nil {
			t.Fatal("cross-kind secret accepted", row.kind)
		}
	}
	for _, in := range []SecurityInput{
		{Flow: flow, OldPassword: "same12", NewPassword: "same12"},
		{Flow: flow, OldPassword: "old-test", NewPassword: "abcdef"},
		{Flow: flow, OldPassword: "old-test", NewPassword: "new test12"},
		{Flow: flow, OldPassword: "old-test", NewPassword: "新密码test12"},
	} {
		if in.Validate("password") == nil {
			t.Fatal("invalid password allowed")
		}
	}
}

func TestAvatarNormalizationBoundsAndMetadata(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 800, 300))
	source.Set(400, 150, color.RGBA{R: 255, A: 255})
	var input bytes.Buffer
	png.Encode(&input, source)
	out, err := NormalizeAvatar(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || format != "jpeg" || cfg.Width != 256 || cfg.Height != 256 {
		t.Fatal("not bounded square JPEG", cfg, format, err)
	}
	for _, bad := range [][]byte{nil, []byte("<svg>not an image</svg>"), []byte(strings.Repeat("a", 1024*1024+1))} {
		if _, err = NormalizeAvatar(bad); err == nil {
			t.Fatal("invalid avatar accepted")
		}
	}
}
