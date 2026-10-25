//go:build sodium && cgo

package nativeauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spaolacci/murmur3"
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

func TestSignedNativeLoginProtocols(t *testing.T) {
	for _, protocol := range []string{"3.7.4", "3.7.3", "3.7.1", "2.8.3"} {
		modern := protocol == "3.7.3" || protocol == "3.7.4"
		for _, wrongBinding := range []bool{false, true} {
			if modern && wrongBinding {
				continue
			}
			t.Run(protocol+map[bool]string{false: "/valid", true: "/wrong-correlation"}[wrongBinding], func(t *testing.T) {
				d := syntheticDeployment()
				d.ProtocolVersion = protocol
				serverPrivate := make([]byte, 32)
				serverPrivate[0] = 17
				serverPublic, _ := curve25519.X25519(serverPrivate, curve25519.Basepoint)
				d.ServerKey = base64.StdEncoding.EncodeToString(serverPublic)
				d.Headers = map[string]string{"content-type": "application/octet-stream", "x-version": "legacy"}
				key, _ := decodeKey(d.BodyKey)
				var mu sync.Mutex
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					requests++
					metadata, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
					signature, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-hash"))
					public, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-seed"))
					if !ed25519.Verify(public, metadata, signature) {
						t.Error("invalid signature")
						w.WriteHeader(400)
						return
					}
					fields, _ := decodeProto(t, metadata)
					version := nativewire.DefaultProtocolVersion
					if protocol == "3.7.3" {
						version = 1703240058615
					} else if protocol == "3.7.4" {
						version = 1703240058617
						device, _ := murmur3.Sum128(public)
						if int64(device) < 0 {
							device = -device
						}
						if fields[1] != device || r.Header.Get("x-version") != "1.1703240058617.0" {
							t.Error("current device hash or protocol version mismatch")
						}
					}
					material := make([]byte, 32)
					binary.LittleEndian.PutUint64(material, fields[6])
					binary.LittleEndian.PutUint64(material[8:], fields[8])
					binary.LittleEndian.PutUint64(material[16:], fields[1])
					binary.LittleEndian.PutUint64(material[24:], version)
					hash := xxh3.Hash128(material)
					aad := make([]byte, 16)
					binary.LittleEndian.PutUint64(aad, hash.Lo)
					binary.LittleEndian.PutUint64(aad[8:], hash.Hi)
					body, _ := io.ReadAll(r.Body)
					plain, err := nativewire.OpenBusiness(key, aad, body, nativewire.Limit)
					if err != nil {
						t.Error("invalid encrypted request")
						w.WriteHeader(400)
						return
					}
					var params map[string]json.RawMessage
					if json.Unmarshal(plain, &params) != nil {
						t.Error("invalid params")
					}
					if protocol == "3.7.4" {
						digest, _ := murmur3.Sum128(plain)
						if digest != fields[8] || fields[2] != 3 || fields[3] != 1 || fields[13] != 1 {
							t.Error("current metadata does not match official SDK")
						}
					}
					var data any
					switch r.URL.Path {
					case "/v1/user/login":
						if protocol == "3.7.4" && (string(params["version"]) != `"3.7.4"` ||
							string(params["appVersion"]) != "337" || string(params["appHash"]) != `"`+android374AppHash+`"`) {
							t.Error("current APK identity is missing")
						}
						exchange, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-context"))
						opened, err := nativewire.OpenSealedBox(serverPrivate, exchange)
						if err != nil {
							t.Error("invalid sealed exchange")
							w.WriteHeader(400)
							return
						}
						ef, ep := decodeProto(t, opened)
						if string(ep[3]) != string(public) {
							t.Error("exchange signer mismatch")
						}
						reply := map[string]any{"uid": 123, "nimId": "synthetic-peer", "jwtToken": "synthetic-jwt", "nimToken": "legacy-im",
							"nickName": "synthetic", "safeCodeRequired": protocol != "3.7.1", "safeCodeSet": true}
						if !modern {
							var token []byte
							correlation := ef[5]
							if wrongBinding {
								correlation++
							}
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
							curvePublic, _ := curve25519.X25519(curve, curve25519.Basepoint)
							sealed, _ := nativewire.SealBox(curvePublic, token)
							reply["token"] = base64.RawURLEncoding.EncodeToString(sealed)
						}
						data = reply
					case "/v1/user/verify_safe_code":
						if string(params["code"]) != `"123456"` {
							t.Error("security code missing")
						}
						w.Header().Set("x-set-jwt", "rotated-synthetic-jwt")
						data = map[string]any{}
					case "/v1/user/get-nim-token":
						if r.Header.Get("x-jwt") != "rotated-synthetic-jwt" {
							t.Error("JWT rotation not installed")
						}
						data = map[string]any{"nimToken": "synthetic-im", "kind": "NIM_LOGIN_KIND_DYNAMIC"}
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
				c.config.Origin = server.URL
				c.http = server.Client()
				defer c.Close()
				err = c.Login(context.Background(), map[string]any{"account": "synthetic", "passwd": "not-real", "validateStr": "synthetic-human", "type": "LOGIN_TYPE_ACCOUNT_PWD"})
				if wrongBinding {
					if err == nil || c.jwt != "" {
						t.Fatal("accepted unbound login token")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if c.SecurityRequired() {
					if _, _, err = c.Finish(context.Background()); err == nil {
						t.Fatal("security gate bypassed")
					}
					if err = c.VerifySecurity(context.Background(), "123456"); err != nil {
						t.Fatal(err)
					}
				}
				saved, nick, err := c.Finish(context.Background())
				if err != nil || saved.UID != 123 || saved.NIMID != "synthetic-peer" || nick != "synthetic" {
					t.Fatal("invalid session", err)
				}
				if protocol != "3.7.1" && (saved.NIMAuthType != 1 || saved.JWT != "rotated-synthetic-jwt") {
					t.Fatal("invalid current session")
				}
				mu.Lock()
				count := requests
				mu.Unlock()
				if protocol != "3.7.1" && count != 3 {
					t.Fatal("unexpected request count", count)
				}
			})
		}
	}
}

func TestSignedAnonymousRegistrationUsesOfficialRoute(t *testing.T) {
	d := syntheticDeployment()
	key, _ := decodeKey(d.BodyKey)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/user/register" || r.Header.Get("x-context") != "" || r.Header.Get("x-jwt") != "" {
			t.Error("registration was routed through the login exchange")
		}
		metadata, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
		signature, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-hash"))
		public, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-seed"))
		if !ed25519.Verify(public, metadata, signature) {
			t.Error("registration signature invalid")
		}
		fields, _ := decodeProto(t, metadata)
		material := make([]byte, 32)
		binary.LittleEndian.PutUint64(material, fields[6])
		binary.LittleEndian.PutUint64(material[8:], fields[8])
		binary.LittleEndian.PutUint64(material[16:], fields[1])
		binary.LittleEndian.PutUint64(material[24:], 1703240058615)
		hash := xxh3.Hash128(material)
		aad := make([]byte, 16)
		binary.LittleEndian.PutUint64(aad, hash.Lo)
		binary.LittleEndian.PutUint64(aad[8:], hash.Hi)
		body, _ := io.ReadAll(r.Body)
		plain, err := nativewire.OpenBusiness(key, aad, body, nativewire.Limit)
		if err != nil {
			t.Error("registration encryption invalid", err)
		}
		var params map[string]any
		if json.Unmarshal(plain, &params) != nil || params["password"] != "synthetic1" || params["passwd"] != nil ||
			params["key"] != "synthetic-key" || params["nickName"] != "synthetic" {
			t.Error("registration payload mismatch")
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
		"password": "synthetic1", "key": "synthetic-key", "nickName": "synthetic",
		"phone":       map[string]any{"countryCode": 86, "nationalNumber": 13800000000, "maskedNationalNumber": ""},
		"validateStr": "synthetic-human", "verificationCode": "123456",
	})
	if err != nil || string(result["accountId"]) != "456" || c.jwt != "" || calls.Load() != 1 {
		t.Fatal("registration did not return a separate account identity", err)
	}
}

func TestAndroid374NativeContextVector(t *testing.T) {
	d := syntheticDeployment()
	d.ProtocolVersion = "3.7.4"
	private := make([]byte, 32)
	private[0] = 17
	public, _ := curve25519.X25519(private, curve25519.Basepoint)
	d.ServerKey = base64.StdEncoding.EncodeToString(public)
	value, err := NewClient(d)
	if err != nil {
		t.Fatal(err)
	}
	c := value.(*Client)
	defer c.Close()
	for i := range c.public {
		c.public[i] = byte(i)
	}
	c.fields[0] = 99
	contextBytes, err := c.exchangeContext(3, 1700000000123456789)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := nativewire.OpenSealedBox(private, contextBytes)
	// Produced by emulating the pinned APK's serializer at libcore.so+0x2dae4.
	expected := "086310031a20000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f2080e2cfaa0628959a97ece39fe7cb173001"
	if err != nil || hex.EncodeToString(plain) != expected {
		t.Fatal("official context serializer mismatch", err)
	}
}

func TestAndroid374SignupHasDeviceContextButNoUserSession(t *testing.T) {
	d := syntheticDeployment()
	d.ProtocolVersion = "3.7.4"
	private := make([]byte, 32)
	private[0] = 17
	public, _ := curve25519.X25519(private, curve25519.Basepoint)
	d.ServerKey = base64.StdEncoding.EncodeToString(public)
	key, _ := decodeKey(d.BodyKey)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		metadata, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
		signature, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-hash"))
		signer, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-seed"))
		if !ed25519.Verify(signer, metadata, signature) {
			t.Error("invalid signature")
		}
		fields, _ := decodeProto(t, metadata)
		if fields[4] != 0 || r.Header.Get("x-jwt") != "" || r.Header.Get("x-id") != "" {
			t.Error("registration carried a user session")
		}
		contextBytes, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-context"))
		data := map[string]any{"key": "synthetic-challenge"}
		switch r.URL.Path {
		case "/v1/verify/sms-anon":
			if len(contextBytes) != 0 {
				t.Error("SMS unexpectedly carried an account context")
			}
		case "/v1/user/register":
			opened, err := nativewire.OpenSealedBox(private, contextBytes)
			if err != nil {
				t.Error("registration is missing the official sealed context")
			} else {
				contextFields, blobs := decodeProto(t, opened)
				if contextFields[1] != fields[1] || contextFields[2] != 3 ||
					contextFields[6] != 1 || !bytes.Equal(blobs[3], signer) {
					t.Error("registration context is not bound to the signing device")
				}
			}
			data = map[string]any{"accountId": 456}
		default:
			t.Error("unexpected route")
		}
		aad := nativewire.BindingDigest(fields[6], fields[8], fields[1], 1703240058617)
		body, _ := io.ReadAll(r.Body)
		plain, err := nativewire.OpenBusiness(key, aad, body, nativewire.Limit)
		digest, _ := murmur3.Sum128(plain)
		if err != nil || digest != fields[8] {
			t.Error("request hash is not Murmur3", err)
		}
		reply, _ := json.Marshal(map[string]any{"code": 0, "data": data})
		sealed, _ := nativewire.SealBusinessDigest(key, fields[6], reply, fields[8], fields[1], 1703240058617)
		w.Write(sealed)
	}))
	defer server.Close()
	value, err := NewClient(d)
	if err != nil {
		t.Fatal(err)
	}
	c := value.(*Client)
	c.config.Origin, c.http = server.URL, server.Client()
	defer c.Close()
	if _, err = c.Request(context.Background(), "/v1/verify/sms-anon", map[string]any{"ty": "VERIFY_FOR_REGISTER"}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Request(context.Background(), "/v1/user/register", map[string]any{"password": "synthetic1"}); err != nil ||
		c.jwt != "" || c.fields[3] != 0 || c.identity != nil || calls.Load() != 2 {
		t.Fatal("registration was incorrectly treated as an authenticated login", err)
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
	c.config.Origin = server.URL
	c.http = server.Client()
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
			if !ok || !native.Submitted || (native.Code != "native_transport_unknown" && native.Code != "native_response_invalid") {
				t.Fatal("unverifiable request not classified as submitted/unknown")
			}
			if calls.Load() != 1 {
				t.Fatal("request automatically retried")
			}
		})
	}
}
