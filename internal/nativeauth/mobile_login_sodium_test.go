//go:build sodium && cgo

package nativeauth

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"wangshangliao/internal/nativewire"
)

func TestMobileLoginMatchesAndroidResponseContract(t *testing.T) {
	for _, method := range []string{"account", "phone", "sms"} {
		for _, representation := range []string{"omitted", "empty", "legacy", "masked", "number_text", "no_phone"} {
			t.Run(method+"/"+representation, func(t *testing.T) {
				c, private := newSyntheticMobileClient(t)
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					data := syntheticMobileLoginResponse(t, c, r, private, "password")
					delete(data, "token")
					switch representation {
					case "empty":
						data["token"] = ""
					case "legacy":
						data["token"] = "synthetic-legacy-token-not-a-sealed-box"
					case "masked":
						data["phone"] = map[string]any{"maskedNationalNumber": "138****0000"}
					case "number_text":
						data["phone"] = map[string]any{"countryCode": 86, "nationalNumber": "13800000000",
							"maskedNationalNumber": "138****0000"}
					case "no_phone":
						delete(data, "phone")
					}
					plain, _ := json.Marshal(map[string]any{"code": 0, "data": data})
					defer clear(plain)
					metadata, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
					fields, _ := decodeProto(t, metadata)
					body, err := nativewire.SealBusinessDigest(c.bodyKey, fields[6], plain,
						fields[8], fields[1], mobileExchangeVersion)
					if err != nil {
						t.Error("synthetic mobile response failed to seal")
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.Write(body)
				}))
				defer server.Close()
				c.config.Origin, c.http = server.URL, server.Client()
				params := syntheticMobilePassword()
				switch method {
				case "phone":
					params["type"], params["account"] = "LOGIN_TYPE_PHONE_PWD", "13800000000"
				case "sms":
					params = map[string]any{"type": "LOGIN_TYPE_SMS", "phone": map[string]any{
						"countryCode": 86, "nationalNumber": 13800000000}, "key": "synthetic-sms-key",
						"verificationCode": "123456", "validateStr": "synthetic-human"}
				}
				if err := c.Login(context.Background(), params); err != nil {
					t.Fatal("pinned mobile response was rejected", err)
				}
				saved, _, err := c.Finish(context.Background())
				if err != nil || saved.UID != 123 || saved.AccountID != "456" ||
					saved.Kind != 0 || !bytes.Equal(saved.MAC, c.public[16:32]) ||
					len(c.identity) != 3 || calls.Load() != 1 {
					t.Fatal("mobile authentication lost its independent identity or requested IM", err)
				}
				encoded, _ := json.Marshal(saved)
				for _, hidden := range []string{"13800000000", "138****0000", "synthetic-sms-key",
					"synthetic-legacy-token", "synthetic-password", "123456"} {
					if bytes.Contains(encoded, []byte(hidden)) {
						t.Fatal("request secrets or legacy token persisted in the mobile session")
					}
				}
			})
		}
	}
}

func TestMobileLoginResponseFailuresHaveSafeDiagnostics(t *testing.T) {
	for _, test := range []struct{ mode, diagnostic string }{
		{"missing_uid", "login_identity_invalid"},
		{"invalid_jwt", "login_identity_invalid"},
		{"missing_account", "login_identity_invalid"},
		{"wrong_account", "login_account_mismatch"},
		{"wrong_phone", "login_phone_mismatch"},
		{"invalid_phone", "login_phone_invalid"},
		{"invalid_security_flag", "login_security_flags_invalid"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			c, private := newSyntheticMobileClient(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				data := syntheticMobileLoginResponse(t, c, r, private, test.mode)
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			params := syntheticMobilePassword()
			if strings.Contains(test.mode, "phone") {
				params["type"], params["account"] = "LOGIN_TYPE_PHONE_PWD", "13800000000"
			}
			err := c.Login(context.Background(), params)
			var native *Error
			if !errors.As(err, &native) || native.Code != "native_mobile_login_response_invalid" ||
				native.Diagnostic != test.diagnostic || !native.Submitted ||
				safeAuthDiagnostic(native.Diagnostic) == "" || !c.loginUnknown ||
				c.jwt != "" || c.identity != nil || len(c.mac) != 0 {
				t.Fatal("failed response lost its safe diagnostic or retained authentication")
			}
			if err := c.Login(context.Background(), params); err == nil || calls.Load() != 1 {
				t.Fatal("invalid successful response was automatically retried")
			}
		})
	}
}

func TestMobilePhoneResponseProfileIsNotAnIdentityProof(t *testing.T) {
	params := map[string]any{"type": "LOGIN_TYPE_SMS", "phone": map[string]any{
		"countryCode": 86, "nationalNumber": 13800000000}}
	for _, raw := range []string{`null`, `{}`, `{"countryCode":86,"nationalNumber":0,"maskedNationalNumber":"138****0000"}`,
		`{"countryCode":"86","nationalNumber":"13800000000"}`} {
		data := map[string]json.RawMessage{"accountId": json.RawMessage("456"), "phone": json.RawMessage(raw)}
		if mobileLoginAccountDiagnostic(data, params) != "" {
			t.Fatal("default or privacy-preserving phone profile rejected")
		}
	}
	for _, raw := range []string{`[]`, `"13800000000"`, `{"countryCode":1,"nationalNumber":13800000000}`,
		`{"countryCode":86,"nationalNumber":"13800000000","unknown":true}`,
		`{"countryCode":86,"nationalNumber":13800000001}`, `{"maskedNationalNumber":"bad\nprofile"}`} {
		data := map[string]json.RawMessage{"accountId": json.RawMessage("456"), "phone": json.RawMessage(raw)}
		if mobileLoginAccountDiagnostic(data, params) == "" {
			t.Fatal("invalid or conflicting full phone profile was accepted")
		}
	}
}

func TestMobilePlainJSONResponseRequiresTrustedTLS(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		name := "untrusted"
		if trusted {
			name = "trusted"
		}
		t.Run(name, func(t *testing.T) {
			c, private := newSyntheticMobileClient(t)
			var requests, connections atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				data := syntheticMobileLoginResponse(t, c, r, private, "password")
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.StartTLS()
			defer server.Close()
			c.config.Origin = server.URL
			transport := c.http.Transport.(*http.Transport)
			// The fixture replaces only public-address dialing. TLS verification,
			// redirect policy and all production request handling remain intact.
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				connections.Add(1)
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
			if trusted {
				roots := x509.NewCertPool()
				roots.AddCert(server.Certificate())
				transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			}
			err := c.Login(context.Background(), syntheticMobilePassword())
			if trusted {
				if err != nil {
					t.Fatal("official plain JSON contract rejected over trusted TLS", err)
				}
				saved, _, err := c.Finish(context.Background())
				if err != nil || saved.UID != 123 || requests.Load() != 1 || connections.Load() != 1 {
					t.Fatal("trusted TLS login did not preserve the pinned identity", err)
				}
				return
			}
			var native *Error
			if !errors.As(err, &native) || native.Code != "native_transport_unknown" ||
				!native.Submitted || !c.loginUnknown || requests.Load() != 0 ||
				c.jwt != "" || c.identity != nil || len(c.mac) != 0 {
				t.Fatal("untrusted TLS supplied authentication or left a retryable login")
			}
			before := connections.Load()
			if c.Login(context.Background(), syntheticMobilePassword()) == nil || connections.Load() != before {
				t.Fatal("untrusted TLS response caused another login connection")
			}
		})
	}
}
