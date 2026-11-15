//go:build sodium && cgo

package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"wangshangliao/internal/nativewire"
)

type settingsRoundTrip func(*http.Request) (*http.Response, error)

func (f settingsRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSecurityDesktopContractUsesFencesAndOneShotSteps(t *testing.T) {
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	c := base.(*Client)
	defer c.Close()
	c.jwt, c.fields[3] = "synthetic-settings-jwt", 7
	c.config.Origin = "https://ifeng-api01.hfjtqy.com"
	routes := []string{}
	writes := 0
	c.http = &http.Client{Transport: settingsRoundTrip(func(r *http.Request) (*http.Response, error) {
		routes = append(routes, r.URL.Path)
		data := map[string]any{}
		if r.URL.Host == "authupapi01.hfjtqy.com" {
			if r.URL.Path != "/v1/assets/userupload" || r.Header.Get("token") != c.jwt {
				t.Fatal("unreviewed upload target")
			}
			if err := r.ParseMultipartForm(3 * 1024 * 1024); err != nil {
				t.Fatal(err)
			}
			defer r.MultipartForm.RemoveAll()
			for k, v := range map[string]string{"asset_type": "1", "use_for": "3", "height": "256", "width": "256"} {
				if r.FormValue(k) != v {
					t.Fatal("wrong upload metadata", k)
				}
			}
			for _, name := range []string{"file", "thumbnail"} {
				file, _, err := r.FormFile(name)
				if err != nil {
					t.Fatal(err)
				}
				cfg, format, err := image.DecodeConfig(file)
				file.Close()
				if err != nil || format != "jpeg" || cfg.Width != 256 || cfg.Height != 256 {
					t.Fatal("bad upload image")
				}
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"id":"98765"}`))}, nil
		}
		_, _, p := desktopRequest(t, r, c.bodyKey)
		switch r.URL.Path {
		case "/v1/verify/sms":
			data = map[string]any{"key": "synthetic-sms", "expiredSec": 300}
		case "/v1/verify/sms-anon":
			if string(p["ty"]) != `"VERIFY_FOR_RESET_PHONE"` {
				t.Fatal("wrong SMS purpose")
			}
			data = map[string]any{"key": "synthetic-new-sms", "expiredSec": 300}
		case "/v1/verify/verify":
			data = map[string]any{"key": "synthetic-certificate"}
		case "/v1/settings/password":
			if string(p["newPassword"]) != `"new-test12"` || string(p["verify"]) != `{"pwd":{"passwd":"old-test"}}` {
				t.Fatal("wrong password contract")
			}
			writes++
		case "/v1/settings/changePhone":
			if string(p["cert"]) != `"synthetic-certificate"` || string(p["newDeviceKey"]) != `"synthetic-new-sms"` {
				t.Fatal("lost verified flow")
			}
			writes++
		case "/v1/user/image-check":
			if string(p["assetId"]) != `"98765"` {
				t.Fatal("image-check skipped")
			}
		case "/v1/settings/avatar":
			if string(p["id"]) != `"98765"` {
				t.Fatal("wrong asset save")
			}
			writes++
		default:
			t.Fatal("unexpected native route", r.URL.Path)
		}
		body, _ := json.Marshal(map[string]any{"code": 0, "data": data})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	flow := "71000000-0000-4000-8000-000000000001"
	fenceCalls := 0
	fence := nativewire.SubmissionFence(func(ctx context.Context, submit func(context.Context) error) error { fenceCalls++; return submit(ctx) })
	state := &SecurityState{}
	for _, row := range []struct {
		kind string
		in   SecurityInput
	}{
		{"password_sms", SecurityInput{Captcha: "synthetic"}},
		{"password_verify", SecurityInput{Flow: flow, Code: "123456"}},
		{"password", SecurityInput{Flow: flow, OldPassword: "old-test", NewPassword: "new-test12"}},
		{"phone_old_sms", SecurityInput{Captcha: "synthetic"}},
		{"phone_old_verify", SecurityInput{Flow: flow, Code: "123456"}},
		{"phone_new_sms", SecurityInput{Flow: flow, Phone: "13800000000", Captcha: "synthetic"}},
		{"phone", SecurityInput{Flow: flow, Code: "123456"}},
	} {
		if err = c.ChangeSecurity(context.Background(), row.kind, row.in, state, fence); err != nil {
			t.Fatal(row.kind, err)
		}
	}
	var pngData bytes.Buffer
	png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 128, 64)))
	if err = c.ChangeSecurity(context.Background(), "avatar", SecurityInput{Image: pngData.Bytes()}, state, fence); err != nil {
		t.Fatal(err)
	}
	if writes != 3 || fenceCalls != 10 || len(routes) != 10 {
		t.Fatal("unexpected native retry/fence count", writes, fenceCalls, len(routes))
	}
	for _, path := range []string{"/v1/settings/password", "/v1/settings/changePhone", "/v1/verify/verify", "/v1/verify/sms"} {
		if _, err = c.Request(context.Background(), path, map[string]any{}); err == nil {
			t.Fatal("raw request bypassed fence")
		}
	}
	denied := errors.New("synthetic revoked")
	if err = c.ChangeSecurity(context.Background(), "password_sms", SecurityInput{Captcha: "synthetic"}, state,
		func(context.Context, func(context.Context) error) error { return denied }); !errors.Is(err, denied) || len(routes) != 10 {
		t.Fatal("denied fence reached HTTP", err)
	}
}
