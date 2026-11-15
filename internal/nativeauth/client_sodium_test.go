//go:build sodium && cgo

package nativeauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/zeebo/xxh3"
	"golang.org/x/crypto/curve25519"
	"google.golang.org/protobuf/encoding/protowire"

	"wangshangliao/internal/nativewire"
)

func decodeProto(t *testing.T, b []byte) (map[int]uint64, map[int][]byte) {
	t.Helper()
	scalars, blobs := map[int]uint64{}, map[int][]byte{}
	for len(b) > 0 {
		number, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatal("invalid protobuf")
		}
		b = b[n:]
		size := protowire.ConsumeFieldValue(number, typ, b)
		if size < 0 {
			t.Fatal("invalid protobuf value")
		}
		if typ == protowire.VarintType {
			scalars[int(number)], _ = protowire.ConsumeVarint(b)
		}
		if typ == protowire.BytesType {
			blobs[int(number)], _ = protowire.ConsumeBytes(b)
		}
		b = b[size:]
	}
	return scalars, blobs
}

func desktopRequest(t *testing.T, r *http.Request, key []byte) (map[int]uint64, []byte, map[string]json.RawMessage) {
	t.Helper()
	metadata, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
	signature, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-hash"))
	public, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-seed"))
	if !ed25519.Verify(public, metadata, signature) {
		t.Error("invalid request signature")
	}
	fields, _ := decodeProto(t, metadata)
	if r.Header.Get("x-device") != "1" || r.Header.Get("x-client") != "11" ||
		r.Header.Get("x-version") != "1.1770619133615.0" || fields[1] != xxh3.Hash(public) ||
		fields[2] != 1 || fields[3] != 1 {
		t.Error("request did not use the desktop contract")
	}
	body, _ := io.ReadAll(r.Body)
	aad := nativewire.BindingDigest(fields[6], fields[8], fields[1], nativewire.DefaultProtocolVersion)
	plain, err := nativewire.OpenBusiness(key, aad, body, nativewire.Limit)
	if err != nil || xxh3.Hash(plain) != fields[8] {
		t.Error("invalid desktop encrypted request")
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(plain, &params) != nil {
		t.Error("invalid parameters")
	}
	for _, name := range []string{"version", "appVersion", "appHash"} {
		if _, exists := params[name]; exists {
			t.Error("mobile APK identity leaked into desktop authentication")
		}
	}
	return fields, public, params
}

func TestSignedDesktopLogin(t *testing.T) {
	for _, wrongBinding := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "wrong-correlation"}[wrongBinding], func(t *testing.T) {
			d := syntheticDeployment()
			serverPrivate := make([]byte, 32)
			serverPrivate[0] = 17
			serverPublic, _ := curve25519.X25519(serverPrivate, curve25519.Basepoint)
			d.ServerKey = base64.StdEncoding.EncodeToString(serverPublic)
			// An imported profile cannot override the desktop identity headers.
			d.Headers = map[string]string{"x-device": "3", "x-client": "2", "x-version": "mobile"}
			key, _ := decodeKey(d.BodyKey)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, public, params := desktopRequest(t, r, key)
				var data any
				switch r.URL.Path {
				case "/v1/user/login":
					if len(params) != 4 || string(params["account"]) != `"synthetic"` ||
						string(params["passwd"]) != `"not-real"` ||
						string(params["validateStr"]) != `"synthetic-human"` ||
						string(params["type"]) != `"LOGIN_TYPE_ACCOUNT_PWD"` {
						t.Error("password login payload differs from the desktop contract")
					}
					exchange, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-context"))
					opened, err := nativewire.OpenSealedBox(serverPrivate, exchange)
					if err != nil {
						t.Error("invalid sealed exchange")
						w.WriteHeader(400)
						return
					}
					ef, ep := decodeProto(t, opened)
					if string(ep[3]) != string(public) || ef[2] != 1 {
						t.Error("exchange identity is not desktop")
					}
					correlation := ef[5]
					if wrongBinding {
						correlation++
					}
					var token []byte
					for _, value := range []struct {
						n protowire.Number
						v uint64
					}{{1, ef[4]}, {2, correlation}, {5, 7}, {6, 123}} {
						token = protowire.AppendTag(token, value.n, protowire.VarintType)
						token = protowire.AppendVarint(token, value.v)
					}
					token = protowire.AppendTag(token, 4, protowire.BytesType)
					token = protowire.AppendBytes(token, make([]byte, 16))
					_, signing, _ := nativewire.SigningKeypair(make([]byte, 32))
					curve, _ := nativewire.CurvePrivate(signing)
					clear(signing)
					curvePublic, _ := curve25519.X25519(curve, curve25519.Basepoint)
					clear(curve)
					sealed, _ := nativewire.SealBox(curvePublic, token)
					data = map[string]any{"uid": 123, "nimId": "synthetic-peer", "jwtToken": "synthetic-jwt",
						"token":    base64.RawURLEncoding.EncodeToString(sealed),
						"nickName": "synthetic", "safeCodeRequired": true, "safeCodeSet": true}
				case "/v1/user/verify_safe_code":
					if string(params["code"]) != `"123456"` {
						t.Error("security code missing")
					}
					w.Header().Set("x-set-jwt", "rotated-synthetic-jwt")
					data = map[string]any{}
				case "/v1/user/get-nim-token":
					if r.Header.Get("x-jwt") != "rotated-synthetic-jwt" {
						t.Error("JWT rotation was lost")
					}
					data = map[string]any{"nimToken": "synthetic-im", "kind": "NIM_LOGIN_KIND_DYNC"}
				default:
					t.Error("unexpected route")
					w.WriteHeader(404)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			value, err := NewClient(d)
			if err != nil {
				t.Fatal(err)
			}
			c := value.(*Client)
			c.config.Origin, c.http = server.URL, server.Client()
			defer c.Close()
			err = c.Login(context.Background(), map[string]any{"account": "synthetic", "passwd": "not-real",
				"validateStr": "synthetic-human", "type": "LOGIN_TYPE_ACCOUNT_PWD"})
			if wrongBinding {
				if err == nil || c.jwt != "" || calls.Load() != 1 {
					t.Fatal("accepted an unbound desktop login token")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = c.Finish(context.Background()); err == nil {
				t.Fatal("secondary security was bypassed")
			}
			if err = c.VerifySecurity(context.Background(), "123456"); err != nil {
				t.Fatal(err)
			}
			saved, nick, err := c.Finish(context.Background())
			if err != nil || saved.UID != 123 || saved.NIMID != "synthetic-peer" || nick != "synthetic" ||
				saved.ProtocolVersion != DesktopProtocolVersion || saved.NIMAuthType != 1 ||
				saved.JWT != "rotated-synthetic-jwt" || calls.Load() != 3 {
				t.Fatal("invalid desktop session", err)
			}
		})
	}
}

func TestSignedDesktopAnonymousRegistration(t *testing.T) {
	d := syntheticDeployment()
	key, _ := decodeKey(d.BodyKey)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fields, _, params := desktopRequest(t, r, key)
		if r.URL.Path != "/v1/user/register" || r.Header.Get("x-context") != "" ||
			r.Header.Get("x-jwt") != "" || fields[4] != 0 {
			t.Error("registration carried a user session or login exchange")
		}
		if string(params["password"]) != `"synthetic1"` || params["passwd"] != nil ||
			string(params["key"]) != `"synthetic-key"` || params["nickName"] != nil || len(params) != 5 {
			t.Error("registration payload mismatch")
		}
		var phone map[string]string
		if json.Unmarshal(params["phone"], &phone) != nil || len(phone) != 3 ||
			phone["countryCode"] != "86" || phone["nationalNumber"] != "13800000000" ||
			phone["maskedNationalNumber"] != "138****0000" {
			t.Error("desktop phone contract mismatch")
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"accountId": 456}})
	}))
	defer server.Close()
	value, err := NewClient(d)
	if err != nil {
		t.Fatal(err)
	}
	c := value.(*Client)
	c.config.Origin, c.http = server.URL, server.Client()
	defer c.Close()
	result, err := c.Request(context.Background(), "/v1/user/register", map[string]any{
		"password": "synthetic1", "key": "synthetic-key",
		"phone":       map[string]any{"countryCode": "86", "nationalNumber": "13800000000", "maskedNationalNumber": "138****0000"},
		"validateStr": "synthetic-human", "verificationCode": "123456",
	})
	if err != nil || string(result["accountId"]) != "456" || c.jwt != "" || calls.Load() != 1 {
		t.Fatal("registration did not remain separate from authentication", err)
	}
}

func TestDesktopRejectionIsScopedAndNotRetried(t *testing.T) {
	for _, test := range []struct {
		number               int
		route, message, want string
	}{
		{1001, "/v1/user/register", "", "native_signup_rejected_unspecified"},
		{1001, "/v1/verify/sms-anon", "", "native_sms_rejected_unspecified"},
		{1001, "/v1/user/login", "", "native_login_rejected_unspecified"},
		{1001, "/v1/user/register", "短信验证码错误", "native_invalid_sms"},
		{1001, "/v1/user/register", "synthetic-secret-and-phone", "native_signup_rejected_unspecified"},
		{-1, "/v1/user/login", "", "native_login_rejected_unspecified"},
		{-1, "/v1/user/login", "账号或密码错误，请重新输入", "native_invalid_credentials"},
		{-1, "/v1/user/login", "账号或密码错误, 请重新输入", "native_invalid_credentials"},
		{-1, "/v1/user/login", "synthetic-secret-and-phone", "native_login_rejected_unspecified"},
		{-1, "/v1/verify/sms-anon", "", "native_sms_rejected_unspecified"},
	} {
		t.Run(test.want+test.message, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != test.route {
					t.Error("unexpected request route")
				}
				json.NewEncoder(w).Encode(map[string]any{"code": test.number, "msg": test.message})
			}))
			defer server.Close()
			value, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := value.(*Client)
			defer c.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			_, err = c.Request(context.Background(), test.route, map[string]any{})
			native, ok := err.(*Error)
			_, diagnostic := authBusinessRejection(test.route, nil, test.number, test.message)
			if !ok || native.Code != test.want || native.Number != test.number || !native.Submitted ||
				calls.Load() != 1 || native.Diagnostic != diagnostic || native.Error() == test.message {
				t.Fatal("generic rejection lost scope or retried", err)
			}
		})
	}
}

func TestRegistrationSMSDiagnosticsAndCooldownNeverRetry(t *testing.T) {
	for _, number := range []int{1001, 1000, 1002, 1036, 1037, 1038, 1040, 1043, 1044, 1200, 1201} {
		t.Run(strconv.Itoa(number), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/verify/sms-anon" {
					t.Error("unexpected diagnostic route")
				}
				json.NewEncoder(w).Encode(map[string]any{"code": number,
					"msg": "手机号13800000000 验证码123456 token=synthetic-ticket"})
			}))
			defer server.Close()
			value, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := value.(*Client)
			defer c.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			params := map[string]any{"ty": "VERIFY_FOR_REGISTER"}
			_, err = c.Request(context.Background(), "/v1/verify/sms-anon", params)
			native, ok := err.(*Error)
			want, diagnostic := authBusinessRejection("/v1/verify/sms-anon", params, number, "unrecognized")
			if !ok || native.Code != want || native.Diagnostic != diagnostic ||
				native.Number != number || !native.Submitted || calls.Load() != 1 {
				t.Fatal("SMS error classification or request count changed", err)
			}
			raw, _ := json.Marshal(native)
			if bytes.Contains(raw, []byte("13800000000")) || bytes.Contains(raw, []byte("123456")) ||
				bytes.Contains(raw, []byte("synthetic-ticket")) {
				t.Fatal("raw provider content leaked")
			}
			if number == 1038 || number == 1043 {
				if native.RetryAfter <= 0 {
					t.Fatal("missing local cooldown")
				}
				_, err = c.Request(context.Background(), "/v1/verify/sms-anon", params)
				if err == nil || calls.Load() != 1 {
					t.Fatal("SMS request escaped cooldown")
				}
			}
		})
	}
}

func TestSMSStructuredRejectionsPreserveBusinessCodeAndNeverRetry(t *testing.T) {
	for _, test := range []struct {
		body, code, diagnostic string
		number                 int
	}{
		{`{"code":1001,"msg":{"phone":"13800000000","ticket":"synthetic-secret"},"data":[]}`,
			"native_sms_rejected_unspecified", "provider_reason_nontext", 1001},
		{`{"code":1038,"msg":["123456"],"data":"synthetic-secret"}`,
			"native_sms_rate_limited", "provider_code_classified", 1038},
		{`{"code":1001,"msg":null}`, "native_sms_rejected_unspecified", "provider_reason_empty", 1001},
		{`{"code":1001}`, "native_sms_rejected_unspecified", "provider_reason_empty", 1001},
		{`{"code":0,"msg":[],"data":{}}`, "native_response_invalid", "", 0},
		{`{"code":0,"msg":"","data":[]}`, "native_response_invalid", "", 0},
		{`{"code":"1001","msg":""}`, "native_response_invalid", "", 0},
	} {
		t.Run(test.code+test.diagnostic+test.body, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/verify/sms-anon" {
					t.Error("unexpected route")
				}
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			value, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := value.(*Client)
			defer c.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			_, err = c.Request(context.Background(), "/v1/verify/sms-anon", map[string]any{"ty": "VERIFY_FOR_REGISTER"})
			native, ok := err.(*Error)
			if !ok || native.Code != test.code || native.Number != test.number ||
				native.Diagnostic != test.diagnostic || !native.Submitted || calls.Load() != 1 {
				t.Fatal("structured rejection lost diagnostics or retried", err)
			}
			raw, _ := json.Marshal(native)
			for _, secret := range []string{"13800000000", "123456", "synthetic-secret"} {
				if bytes.Contains(raw, []byte(secret)) || bytes.Contains([]byte(native.Error()), []byte(secret)) {
					t.Fatal("structured provider reason escaped")
				}
			}
		})
	}
}

func TestNonAuthRejectionsRetainStrictEnvelope(t *testing.T) {
	for _, body := range []string{
		`{"code":1001,"msg":{"token":"synthetic-secret"},"data":{}}`,
		`{"code":1001,"msg":"","data":[]}`,
	} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			_, _ = io.WriteString(w, body)
		}))
		value, err := NewClient(syntheticDeployment())
		if err != nil {
			t.Fatal(err)
		}
		c := value.(*Client)
		c.config.Origin, c.http = server.URL, server.Client()
		_, err = c.Request(context.Background(), "/v1/settings/get-sys-cfg", nil)
		c.Close()
		server.Close()
		native, ok := err.(*Error)
		if !ok || native.Code != "native_response_invalid" || !native.Submitted || calls.Load() != 1 {
			t.Fatal("authentication diagnostics changed non-auth contract", err)
		}
	}
}

func TestNative429IsNotRetried(t *testing.T) {
	value, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	c := value.(*Client)
	defer c.Close()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	}))
	defer server.Close()
	c.config.Origin, c.http = server.URL, server.Client()
	for range 2 {
		if _, err = c.Request(context.Background(), "/v1/verify/sms-anon", map[string]any{}); err == nil {
			t.Fatal("accepted 429")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("retried during cooldown")
	}
}

func TestUnverifiableNativeReplyIsNotSuccess(t *testing.T) {
	for _, mode := range []string{"server-error", "malformed", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			value, err := NewClient(syntheticDeployment())
			if err != nil {
				t.Fatal(err)
			}
			c := value.(*Client)
			defer c.Close()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "server-error":
					w.WriteHeader(503)
				case "malformed":
					io.WriteString(w, `{"missing_code":true}`)
				case "truncated":
					io.WriteString(w, `{"code":`)
				}
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			_, err = c.Request(context.Background(), "/v1/verify/sms-anon", map[string]any{})
			native, ok := err.(*Error)
			if !ok || !native.Submitted ||
				(native.Code != "native_transport_unknown" && native.Code != "native_response_invalid") ||
				calls.Load() != 1 {
				t.Fatal("unknown native result became success or was retried")
			}
		})
	}
}
