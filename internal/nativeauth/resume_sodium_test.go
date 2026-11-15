//go:build sodium && cgo

package nativeauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"wangshangliao/internal/nativewire"
)

func TestResumeBindsSavedSessionWithoutLogin(t *testing.T) {
	d := syntheticDeployment()
	base, err := NewClient(d)
	if err != nil {
		t.Fatal(err)
	}
	client := base.(*Client)
	saved := Session{Origin: d.Origin, ProtocolVersion: d.ProtocolVersion, UID: 123, Device: client.fields[0],
		JWT: "synthetic-jwt", MAC: make([]byte, 16), NIMID: "synthetic-peer", NIMToken: "synthetic-im"}
	client.Close()
	resumed, err := Resume(d, saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.fields[3] != 123 || resumed.jwt != saved.JWT {
		t.Fatal("saved identity not restored")
	}
	resumed.Close()
	for _, mutate := range []func(*Session){
		func(s *Session) { s.Origin = "https://other.example" },
		func(s *Session) { s.Device++ },
		func(s *Session) { s.ProtocolVersion = "unsupported" },
		func(s *Session) { s.ProtocolVersion = "2.8.4" },
		func(s *Session) { s.ProtocolVersion = "3.7.4" },
		func(s *Session) { s.MAC = nil },
		func(s *Session) { s.NIMAuthType = 3 },
		func(s *Session) { s.DesktopPlatform = "macos" },
		func(s *Session) { s.DesktopPlatform = "android" },
	} {
		changed := saved
		mutate(&changed)
		if bad, err := Resume(d, changed, nil); err == nil {
			bad.Close()
			t.Fatal("unbound session restored")
		}
	}
}

type renewalRoundTrip func(*http.Request) (*http.Response, error)

func (f renewalRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func callbackSession(t *testing.T, d Deployment) Session {
	t.Helper()
	base, err := NewClient(d)
	if err != nil {
		t.Fatal(err)
	}
	original := base.(*Client)
	defer original.Close()
	return Session{Origin: d.Origin, ProtocolVersion: d.ProtocolVersion, DesktopPlatform: d.DesktopPlatform,
		UID: 123, Device: original.fields[0], JWT: "synthetic-saved-jwt", MAC: make([]byte, 16),
		NIMID: "synthetic-peer", NIMToken: "synthetic-old-im", NIMAuthType: 2}
}

func TestRenewIMUsesAuthenticatedDesktopDefaultWithoutRetry(t *testing.T) {
	for _, test := range []struct {
		name, response, failure string
		number                  int
	}{
		{"callback", `{"code":0,"data":{"nimToken":"synthetic-new-im","kind":"NIM_LOGIN_KIND_CALLBACK"}}`, "", 0},
		{"missing-kind", `{"code":0,"data":{"nimToken":"synthetic-new-im"}}`, "", 0},
		{"static-conflict", `{"code":0,"data":{"nimToken":"synthetic-new-im","kind":"NIM_LOGIN_KIND_STATIC"}}`, "native_nim_token_invalid", 0},
		{"dynamic-conflict", `{"code":0,"data":{"nimToken":"synthetic-new-im","kind":"NIM_LOGIN_KIND_DYNC"}}`, "native_nim_token_invalid", 0},
		{"invalid-kind", `{"code":0,"data":{"nimToken":"synthetic-new-im","kind":null}}`, "native_nim_token_invalid", 0},
		{"client-rejected", `{"code":1200,"msg":"synthetic-private-reason"}`, "native_business_rejected", 1200},
		{"unauthenticated", `{"code":401,"msg":"synthetic-private-reason"}`, "native_reauth_required", 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := syntheticDeployment()
			d.DesktopPlatform = "macos"
			saved := callbackSession(t, d)
			before := saved
			before.MAC = append([]byte(nil), saved.MAC...)
			c, err := Resume(d, saved, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			key, _ := decodeKey(d.BodyKey)
			defer clear(key)
			var calls atomic.Int32
			c.http = &http.Client{Transport: renewalRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.String() != d.Origin+"/v1/user/get-nim-token" ||
					r.Header.Get("x-jwt") != saved.JWT || r.Header.Get("x-id") != "123" ||
					r.Header.Get("x-token") != "123" || r.Header.Get("x-device") != "1" ||
					r.Header.Get("x-client") != "10" || r.Header.Get("x-version") != "1.1770619133615.0" ||
					r.Header.Get("x-context") != "" {
					t.Error("renewal changed its saved authentication or native wire contract")
				}
				metadata, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
				fields, _ := decodeProto(t, metadata)
				body, _ := io.ReadAll(r.Body)
				plain, openErr := nativewire.OpenBusiness(key,
					nativewire.BindingDigest(fields[6], fields[8], fields[1], nativewire.DefaultProtocolVersion),
					body, nativewire.Limit)
				if openErr != nil {
					t.Error("invalid renewal body")
				}
				var params map[string]json.RawMessage
				decodeErr := json.Unmarshal(plain, &params)
				clear(plain)
				var identity desktopRenewalClient
				identityErr := json.Unmarshal(params["client"], &identity)
				if decodeErr != nil || len(params) != 2 || string(params["nimDeviceId"]) != `"synthetic-device"` ||
					identityErr != nil || identity.Kind != "CLIENT_KIND_DESKTOP_MAC_ARM64" ||
					identity.Version != "2.8.4" ||
					identity.V1 != "2d3366f53767465ac3833c82c5c095e9c1edbadf47c0f6b776ac6201ef829aeb" ||
					identity.V2 != "34251676bf6ecfacb4a14365b14e5e5690d4b818f06c71a088b2847225ec6cef" ||
					fields[4] != saved.UID || params["uid"] != nil || params["loginToken"] != nil {
					t.Error("final encrypted body lost the reviewed profile or acquired a login branch")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(bytes.NewBufferString(test.response))}, nil
			})}
			renewed, err := c.RenewIM(context.Background(), saved)
			if calls.Load() != 1 || !reflect.DeepEqual(saved, before) ||
				c.config.ProtocolVersion != "2.8.3" || c.fields[0] != saved.Device {
				t.Fatal("renewal retried, relabeled the saved codec or changed the original credentials")
			}
			if test.number == 401 {
				if c.jwt != "" || c.mac != nil {
					t.Fatal("provider reauthentication requirement retained usable in-memory credentials")
				}
			} else if c.jwt != saved.JWT || !bytes.Equal(c.mac, saved.MAC) {
				t.Fatal("renewal changed restored credentials without a reauthentication rejection")
			}
			if test.failure != "" {
				var failure *Error
				if !errors.As(err, &failure) || failure.Code != test.failure ||
					failure.Number != test.number || failure.Diagnostic != "" ||
					test.number != 0 && !failure.Submitted || renewed.NIMToken != "" ||
					err.Error() != test.failure {
					t.Fatal("rejection lost its bounded result or exposed upstream text")
				}
				return
			}
			if err != nil || renewed.NIMToken != "synthetic-new-im" || renewed.NIMAuthType != 2 {
				t.Fatal("renewal did not preserve the fixed callback contract")
			}
			want := saved
			want.NIMToken = "synthetic-new-im"
			if !reflect.DeepEqual(renewed, want) {
				t.Fatal("renewal changed saved identity, platform, codec or credentials")
			}
		})
	}
}

func TestRenewIMRejectsUnverifiedProfilesBeforeSubmission(t *testing.T) {
	for _, test := range []struct {
		name, platform, code string
		mode                 int
	}{
		{"windows", "windows", "native_client_profile_unavailable", 2},
		{"implicit-windows", "", "native_client_profile_unavailable", 2},
		{"static", "macos", "native_nim_token_invalid", 0},
		{"dynamic", "macos", "native_nim_token_invalid", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := syntheticDeployment()
			d.DesktopPlatform = test.platform
			saved := callbackSession(t, d)
			saved.NIMAuthType = test.mode
			before := saved
			before.MAC = append([]byte(nil), saved.MAC...)
			c, err := Resume(d, saved, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var calls atomic.Int32
			c.http = &http.Client{Transport: renewalRoundTrip(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, errors.New("synthetic-unexpected-submission")
			})}
			renewed, err := c.RenewIM(context.Background(), saved)
			var rejected *Error
			if !errors.As(err, &rejected) || rejected.Code != test.code ||
				rejected.Number != 0 || rejected.Diagnostic != "" || rejected.Submitted ||
				calls.Load() != 0 || !reflect.DeepEqual(renewed, Session{}) {
				t.Fatal("unverified profile or callback reinterpretation reached transport")
			}
			if !reflect.DeepEqual(saved, before) || c.jwt != saved.JWT ||
				!bytes.Equal(c.mac, saved.MAC) || c.fields[3] != saved.UID ||
				c.config.DesktopPlatform != test.platform {
				t.Fatal("fail-closed renewal changed the restored session or platform")
			}
		})
	}
}

func TestRenewIMCancellationBeforeSubmission(t *testing.T) {
	d := syntheticDeployment()
	d.DesktopPlatform = "macos"
	saved := callbackSession(t, d)
	c, err := Resume(d, saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.http = &http.Client{Transport: renewalRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("cancelled renewal reached transport")
		return nil, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.RenewIM(ctx, saved); err == nil {
		t.Fatal("cancelled renewal succeeded")
	}
}
