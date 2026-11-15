//go:build sodium && cgo

package nativeauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDesktopAnonymousCheck(t *testing.T) {
	for _, platform := range []string{"windows", "macos"} {
		for _, response := range []string{`true`, `false`, `null`, `"true"`, `{}`} {
			t.Run(platform+"/"+response, func(t *testing.T) {
				d := syntheticDeployment()
				d.DesktopPlatform = platform
				calls, ids := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != "/v1/settings/get-sys-cfg" || r.Method != "POST" ||
						r.Header.Get("x-client") != desktopClient(platform) ||
						r.Header.Get("x-device") != "1" || r.Header.Get("x-jwt") != "" {
						t.Error("unexpected anonymous desktop request")
					}
					meta, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
					fields, _ := decodeProto(t, meta)
					if fields[6] != 42 {
						t.Error("check did not use supplied durable nonce")
					}
					w.Write([]byte(`{"code":0,"data":{"enableYidunVerify":` + response + `,"private":"not-returned"}}`))
				}))
				defer server.Close()
				value, err := CheckDeployment(context.Background(), d,
					func(context.Context, uint64) (uint64, error) { ids++; return 42, nil },
					func(d Deployment) (AuthClient, error) {
						raw, err := NewClient(d)
						if err != nil {
							return nil, err
						}
						c := raw.(*Client)
						c.config.Origin, c.http = server.URL, server.Client()
						return c, nil
					})
				valid := response == "true" || response == "false"
				if valid && (err != nil || !value.Verified || value.CaptchaRequired != (response == "true")) {
					t.Fatal("valid anonymous response rejected", err)
				}
				if !valid && err == nil {
					t.Fatal("malformed anonymous response accepted")
				}
				encoded, _ := json.Marshal(value)
				if calls != 1 || ids != 1 || string(encoded) == "" {
					t.Fatal("check retried or bypassed nonce source")
				}
			})
		}
	}
}
