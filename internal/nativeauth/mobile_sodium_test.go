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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spaolacci/murmur3"
	"golang.org/x/crypto/curve25519"

	"wangshangliao/internal/nativewire"
)

func newSyntheticMobileClient(t *testing.T) (*Client, []byte) {
	t.Helper()
	d := syntheticMobileDeployment()
	serverPrivate := make([]byte, 32)
	serverPrivate[0] = 17
	serverPublic, err := curve25519.X25519(serverPrivate, curve25519.Basepoint)
	if err != nil {
		t.Fatal("synthetic server key failed")
	}
	d.ServerKey = base64.StdEncoding.EncodeToString(serverPublic)
	// A deployment cannot override the fixed Android transport identity.
	d.Headers = map[string]string{"x-device": "1", "x-client": "11", "x-version": "desktop"}
	value, err := NewMobileClient(d)
	if err != nil {
		t.Fatal("synthetic mobile client rejected", err)
	}
	c := value.(*Client)
	t.Cleanup(c.Close)
	// This test source stands in for the caller's shared durable allocator.
	var counter atomic.Uint64
	c.requestIDs = func(_ context.Context, now uint64) (uint64, error) {
		return nextRequestID(&counter, now)
	}
	return c, serverPrivate
}

func mobileRequest(t *testing.T, r *http.Request, key []byte) (map[int]uint64, []byte, map[string]json.RawMessage) {
	t.Helper()
	metadata, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
	signature, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-hash"))
	public, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-seed"))
	if len(public) != ed25519.PublicKeySize || !ed25519.Verify(public, metadata, signature) {
		t.Error("invalid mobile request signature")
		return nil, nil, nil
	}
	fields, _ := decodeProto(t, metadata)
	low, _ := murmur3.Sum128(public)
	if r.Header.Get("x-device") != "3" || r.Header.Get("x-client") != "2" ||
		r.Header.Get("x-version") != "1.1703240058617.0" || fields[1] != javaAbs64(low) ||
		fields[2] != 3 || fields[3] != 1 {
		t.Error("request differs from the pinned Android transport identity")
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 65537))
	aad := nativewire.BindingDigest(fields[6], fields[8], fields[1], mobileExchangeVersion)
	plain, err := nativewire.OpenBusiness(key, aad, body, nativewire.Limit)
	digest, _ := murmur3.Sum128(plain)
	if err != nil || digest != fields[8] || uint64(len(plain)) != fields[7] {
		t.Error("mobile body digest, AAD or length is not bound")
	}
	if r.Header.Get("x-jwt") != "" {
		var input [24]byte
		binary.LittleEndian.PutUint64(input[:8], fields[1])
		binary.LittleEndian.PutUint64(input[8:16], fields[6])
		binary.LittleEndian.PutUint64(input[16:], fields[10])
		mac, err := nativewire.SipHash24(public[16:], input[:])
		if err != nil || mac != fields[9] || r.Header.Get("x-id") != strconv.FormatUint(fields[4], 10) {
			t.Error("mobile authenticated metadata lost its derived MAC or UID")
		}
	} else if fields[4] != 0 || fields[9] != 0 {
		t.Error("anonymous mobile request carried user credentials")
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(plain, &params) != nil {
		t.Error("invalid mobile JSON payload")
	}
	return fields, public, params
}

func syntheticMobileLoginResponse(t *testing.T, c *Client, r *http.Request, serverPrivate []byte, mode string) map[string]any {
	t.Helper()
	fields, public, params := mobileRequest(t, r, c.bodyKey)
	if len(params) != 16 || string(params["version"]) != `"3.7.4"` ||
		string(params["appVersion"]) != "337" || string(params["appHash"]) != strconv.Quote(MobileAppHash) ||
		string(params["qrcodeId"]) != "0" || string(params["qrcodeKey"]) != "0" {
		t.Error("mobile login omitted its fixed APK identity or Gson defaults")
	}
	exchange, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-context"))
	opened, err := nativewire.OpenSealedBox(serverPrivate, exchange)
	if err != nil {
		t.Error("invalid mobile exchange")
		return nil
	}
	defer clear(opened)
	ef, ep := decodeProto(t, opened)
	if !bytes.Equal(ep[3], public) || ef[1] != fields[1] || ef[2] != 3 ||
		ef[4] != ef[5]/1e9 || ef[6] != 1 {
		t.Error("mobile login exchange is not correlated to its device and time")
	}
	data := map[string]any{"uid": 123, "accountId": 456, "nickName": "synthetic-mobile",
		"jwtToken":         "synthetic-mobile-jwt",
		"phone":            map[string]any{"countryCode": 86, "nationalNumber": 13800000000, "maskedNationalNumber": ""},
		"safeCodeRequired": mode == "security", "safeCodeSet": true}
	switch mode {
	case "wrong_account":
		data["accountId"] = 457
	case "missing_uid":
		delete(data, "uid")
	case "missing_account":
		delete(data, "accountId")
	case "invalid_uid":
		data["uid"] = uint64(1 << 31)
	case "wrong_phone":
		data["phone"] = map[string]any{"countryCode": 86, "nationalNumber": 13800000001}
	case "invalid_phone":
		data["phone"] = map[string]any{"countryCode": 86, "nationalNumber": "not-a-number"}
	case "invalid_jwt":
		data["jwtToken"] = "synthetic\njwt"
	case "invalid_security_flag":
		data["safeCodeRequired"] = nil
	}
	return data
}

func syntheticMobilePassword() map[string]any {
	return map[string]any{"account": "456", "passwd": "synthetic-password",
		"type": "LOGIN_TYPE_ACCOUNT_PWD", "validateStr": "synthetic-human"}
}

func TestMobileProtocolMatchesAndroidOracle(t *testing.T) {
	c, _ := newSyntheticMobileClient(t)
	plain := []byte(`{"synthetic":true}`)
	digest := c.bodyDigest(plain)
	if digest != 15588532282309243290 || c.wireVersion() != 1703240058617 {
		t.Fatal("mobile digest or version differs from the fixed Android oracle")
	}
	aad := nativewire.BindingDigest(42, digest, 99, c.wireVersion())
	if hex.EncodeToString(aad) != "bddbe6eadf23c22e3e4b7a8aee7cf691" {
		t.Fatal("mobile AAD no longer uses the existing XXH3 binding")
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	body, err := nativewire.SealBusinessDigest(key, 42, plain, digest, 99, c.wireVersion())
	if err != nil || hex.EncodeToString(body) != "7e23af94d76de2e4b314af2c5f833ef4f2ba2e78d82ef29427045744787f2e37fd6eeffb6ce72b58fc558543a50186064a6106160e119efd10a5eb9face8" {
		t.Fatal("mobile request body differs from the fixed Android oracle", err)
	}
	for _, change := range []struct{ id, digest, device, version uint64 }{
		{43, digest, 99, c.wireVersion()}, {42, digest + 1, 99, c.wireVersion()},
		{42, digest, 100, c.wireVersion()}, {42, digest, 99, nativewire.DefaultProtocolVersion},
	} {
		aad := nativewire.BindingDigest(change.id, change.digest, change.device, change.version)
		if _, err := nativewire.OpenBusiness(key, aad, body, nativewire.Limit); err == nil {
			t.Fatal("tampered mobile binding opened the oracle body")
		}
	}
}

func TestMobileSignedLoginAndFinishNeverRequestIM(t *testing.T) {
	for _, mode := range []string{"password", "phone", "sms", "security"} {
		t.Run(mode, func(t *testing.T) {
			c, private := newSyntheticMobileClient(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var data map[string]any
				switch r.URL.Path {
				case "/v1/user/login":
					data = syntheticMobileLoginResponse(t, c, r, private, mode)
				case "/v1/user/verify_safe_code":
					fields, _, params := mobileRequest(t, r, c.bodyKey)
					if mode != "security" || fields[4] != 123 || fields[5] != 0 ||
						len(params) != 1 || string(params["code"]) != `"123456"` {
						t.Error("secondary security lost the mobile UID or count")
					}
					w.Header().Set("x-set-jwt", "rotated-synthetic-mobile-jwt")
					data = map[string]any{}
				default:
					t.Error("mobile authentication reached IM or another unsupported route")
					w.WriteHeader(http.StatusNotFound)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			params := syntheticMobilePassword()
			switch mode {
			case "phone":
				params["type"], params["account"] = "LOGIN_TYPE_PHONE_PWD", "13800000000"
			case "sms":
				params = map[string]any{"type": "LOGIN_TYPE_SMS", "phone": map[string]any{
					"countryCode": 86, "nationalNumber": 13800000000}, "key": "synthetic-key",
					"verificationCode": "123456", "validateStr": "synthetic-human"}
			}
			if err := c.Login(context.Background(), params); err != nil {
				t.Fatal("synthetic manual mobile login failed", err)
			}
			if mode == "security" {
				if _, _, err := c.Finish(context.Background()); err == nil {
					t.Fatal("mobile Finish bypassed secondary security")
				}
				if err := c.VerifySecurity(context.Background(), "123456"); err != nil {
					t.Fatal("synthetic mobile security verification failed", err)
				}
			}
			before := calls.Load()
			saved, nickname, err := c.Finish(context.Background())
			if err != nil || saved.UID != 123 || saved.AccountID != "456" || nickname != "synthetic-mobile" ||
				saved.ProtocolVersion != MobileProtocolVersion || saved.DesktopPlatform != "" ||
				saved.NIMToken != "" || saved.NIMID != "" || saved.NIMAuthType != 0 ||
				saved.Device != c.fields[0] || !bytes.Equal(saved.MAC, c.public[16:]) ||
				calls.Load() != before || len(c.identity) != 3 {
				t.Fatal("mobile Finish did not preserve only its bound business session", err)
			}
			if mode == "security" && (saved.JWT != "rotated-synthetic-mobile-jwt" || saved.Kind != 1) {
				t.Fatal("mobile security rotation or per-user count was lost")
			}
			if err := c.Login(context.Background(), params); err == nil || calls.Load() != before {
				t.Fatal("authenticated mobile transaction replayed login")
			}
			saved.MAC[0] ^= 1
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if _, _, err := c.Finish(cancelled); err == nil || calls.Load() != before {
				t.Fatal("cancelled Finish returned a session or requested credentials")
			}
		})
	}
}

func TestMobileLoginRejectsUnboundOrUnknownResponsesWithoutRetry(t *testing.T) {
	for _, mode := range []string{"wrong_account", "wrong_phone", "invalid_jwt", "missing_uid",
		"missing_account", "invalid_uid", "invalid_phone", "invalid_security_flag", "unknown", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			c, private := newSyntheticMobileClient(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "unknown" {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				if mode == "malformed" {
					w.Write([]byte(`{"code":0,"data":`))
					return
				}
				data := syntheticMobileLoginResponse(t, c, r, private, mode)
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			params := syntheticMobilePassword()
			if mode == "wrong_phone" || mode == "invalid_phone" {
				params["type"], params["account"] = "LOGIN_TYPE_PHONE_PWD", "13800000000"
			}
			if err := c.Login(context.Background(), params); err == nil || c.jwt != "" ||
				c.fields[3] != 0 || c.mac != nil || !c.loginUnknown {
				t.Fatal("mobile accepted or retained unbound credentials")
			}
			if _, _, err := c.Finish(context.Background()); err == nil {
				t.Fatal("failed login produced a business session")
			}
			if err := c.Login(context.Background(), params); err == nil || calls.Load() != 1 {
				t.Fatal("unknown or invalid successful response was automatically replayed")
			}
		})
	}
}

func TestMobileLoginRejectsReplayFromPreviousRequest(t *testing.T) {
	first, private := newSyntheticMobileClient(t)
	second, _ := newSyntheticMobileClient(t)
	second.requestIDs = first.requestIDs
	var response []byte
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			data := syntheticMobileLoginResponse(t, first, r, private, "password")
			delete(data, "token")
			plain, _ := json.Marshal(map[string]any{"code": 0, "data": data})
			metadata, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("x-request"))
			fields, _ := decodeProto(t, metadata)
			var err error
			response, err = nativewire.SealBusinessDigest(first.bodyKey, fields[6], plain, fields[8], fields[1], mobileExchangeVersion)
			clear(plain)
			if err != nil {
				t.Error("could not authenticate the synthetic response")
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		} else {
			mobileRequest(t, r, second.bodyKey)
		}
		w.Write(response)
	}))
	defer server.Close()
	for _, c := range []*Client{first, second} {
		c.config.Origin, c.http = server.URL, server.Client()
	}
	if err := first.Login(context.Background(), syntheticMobilePassword()); err != nil {
		t.Fatal("initial synthetic login failed", err)
	}
	if err := second.Login(context.Background(), syntheticMobilePassword()); err == nil ||
		!second.loginUnknown || second.jwt != "" {
		t.Fatal("a previous response authenticated a different mobile request")
	}
	if err := second.Login(context.Background(), syntheticMobilePassword()); err == nil || calls.Load() != 2 {
		t.Fatal("replayed mobile login response triggered another request")
	}
}

func TestMobileManagerManualDeviceSMSAndSecurityFlow(t *testing.T) {
	c, private := newSyntheticMobileClient(t)
	c.requestIDs = nil
	m := NewMobileManager()
	defer m.Close()
	m.Factory = func(Deployment) (AuthClient, error) { return c, nil }
	var requests, logins, allocated, commits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var data map[string]any
		switch r.URL.Path {
		case "/v1/user/login":
			if logins.Add(1) == 1 {
				mobileRequest(t, r, c.bodyKey)
				json.NewEncoder(w).Encode(map[string]any{"code": 1069, "data": map[string]any{}})
				return
			}
			data = syntheticMobileLoginResponse(t, c, r, private, "security")
		case "/v1/user/get-change-device-verify":
			_, _, params := mobileRequest(t, r, c.bodyKey)
			if len(params) != 3 || string(params["account"]) != `"456"` {
				t.Error("manual device verification changed its account")
			}
			data = map[string]any{"phone": map[string]any{"countryCode": 86, "nationalNumber": 13800000000},
				"sms": map[string]any{"key": "synthetic-device-key"}}
		case "/v1/user/verify_safe_code":
			mobileRequest(t, r, c.bodyKey)
			w.Header().Set("x-set-jwt", "rotated-synthetic-jwt")
			data = map[string]any{}
		default:
			t.Error("mobile manager reached an IM, registration or unrelated route")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer server.Close()
	c.config.Origin, c.http = server.URL, server.Client()
	var counter atomic.Uint64
	source := func(_ context.Context, now uint64) (uint64, error) {
		allocated.Add(1)
		return nextRequestID(&counter, now)
	}
	v, err := m.Start("owner", "account", syntheticMobileDeployment(),
		func(_ context.Context, s Session, nickname string) error {
			if s.UID != 123 || s.AccountID != "456" || s.JWT != "rotated-synthetic-jwt" ||
				s.ProtocolVersion != MobileProtocolVersion || s.NIMID != "" || s.NIMToken != "" ||
				nickname != "synthetic-mobile" {
				t.Error("mobile manager committed IM or unbound business credentials")
			}
			commits.Add(1)
			return nil
		}, source)
	if err != nil {
		t.Fatal("mobile manager rejected its injected durable allocator", err)
	}
	if _, err := m.Action("owner", "account", v.ID, Input{Action: "password", Account: "456",
		Password: "synthetic-password", Captcha: "synthetic-human"}); err != nil {
		t.Fatal("manual mobile password action failed", err)
	}
	waitState(t, m, v.ID, "sms_required")
	if requests.Load() != 2 || commits.Load() != 0 {
		t.Fatal("device verification retried login or committed early")
	}
	if _, err := m.Action("owner", "account", v.ID, Input{Action: "sms_verify",
		Code: "123456", Captcha: "synthetic-human"}); err != nil {
		t.Fatal("manual mobile SMS action failed", err)
	}
	waitState(t, m, v.ID, "security_required")
	if commits.Load() != 0 {
		t.Fatal("mobile manager committed before secondary security")
	}
	if _, err := m.Action("owner", "account", v.ID, Input{Action: "security_verify", Code: "123456"}); err != nil {
		t.Fatal("manual mobile security action failed", err)
	}
	waitState(t, m, v.ID, "authenticated")
	if commits.Load() != 1 || requests.Load() != 4 || allocated.Load() != 4 {
		t.Fatal("mobile manager added IM requests, retries or independent nonces")
	}
	if _, err := m.Action("owner", "account", v.ID, Input{Action: "security_verify", Code: "123456"}); err == nil {
		t.Fatal("completed mobile security action was replayed")
	}
}

func syntheticMobileSaved(c *Client) Session {
	return Session{Origin: c.config.Origin, UID: 7, Kind: 9, Device: c.fields[0],
		JWT: "synthetic-mobile-jwt", MAC: bytes.Clone(c.public[16:]),
		ProtocolVersion: MobileProtocolVersion, AccountID: "456"}
}

func TestMobileResumeBindsIndependentBusinessCredentials(t *testing.T) {
	c, _ := newSyntheticMobileClient(t)
	d, saved := c.config, syntheticMobileSaved(c)
	resumed, err := ResumeMobile(d, saved, c.requestIDs)
	if err != nil {
		t.Fatal("bound mobile session could not resume", err)
	}
	defer resumed.Close()
	session, _, err := resumed.Finish(context.Background())
	if err != nil || session.UID != saved.UID || session.Kind != saved.Kind ||
		session.AccountID != saved.AccountID || session.JWT != saved.JWT {
		t.Fatal("mobile resume changed business identity", err)
	}
	for _, mutate := range []func(*Session){
		func(s *Session) { s.Origin = "https://other.invalid" },
		func(s *Session) { s.ProtocolVersion = DesktopProtocolVersion },
		func(s *Session) { s.DesktopPlatform = "windows" },
		func(s *Session) { s.UID = 0 },
		func(s *Session) { s.UID = 1 << 31 },
		func(s *Session) { s.Kind = 1 << 32 },
		func(s *Session) { s.Device++ },
		func(s *Session) { s.JWT = "bad\njwt" },
		func(s *Session) { s.MAC = make([]byte, 16) },
		func(s *Session) { s.MAC = s.MAC[:15] },
		func(s *Session) { s.AccountID = "0456" },
		func(s *Session) { s.NIMID = "synthetic-im" },
		func(s *Session) { s.NIMToken = "synthetic-im-token" },
		func(s *Session) { s.NIMAuthType = 1 },
	} {
		s := saved
		s.MAC = bytes.Clone(saved.MAC)
		mutate(&s)
		if value, err := ResumeMobile(d, s, c.requestIDs); err == nil {
			value.Close()
			t.Fatal("foreign or IM credentials resumed as a mobile companion")
		}
	}
	if _, err := ResumeMobile(d, saved, nil); err == nil {
		t.Fatal("mobile resume created an independent nonce source")
	}
	if value, err := Resume(d, saved, c.requestIDs); err == nil {
		value.Close()
		t.Fatal("desktop resume accepted mobile configuration")
	}
	if value, err := Resume(syntheticDeployment(), saved, c.requestIDs); err == nil {
		value.Close()
		t.Fatal("desktop resume accepted a mobile session")
	}
	if value, err := NewClient(d); err == nil {
		value.Close()
		t.Fatal("desktop constructor accepted mobile configuration")
	}
	if value, err := NewMobileClient(syntheticDeployment()); err == nil {
		value.Close()
		t.Fatal("mobile constructor accepted desktop configuration")
	}
	saved.MAC[0] ^= 1
	if _, _, err := resumed.Finish(context.Background()); err != nil {
		t.Fatal("resumed client aliased the caller's credential bytes")
	}
}

func TestMobileWhitelistAndNonceFailuresNeverReachTransport(t *testing.T) {
	c, _ := newSyntheticMobileClient(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	c.config.Origin, c.http = server.URL, server.Client()
	c.fields[3], c.jwt, c.mac = 7, "synthetic-jwt", bytes.Clone(c.public[16:])
	c.identity = map[string]json.RawMessage{"uid": json.RawMessage("7"), "accountId": json.RawMessage("456")}
	ctx := context.WithValue(context.Background(), settingFenceKey{}, nativewire.SubmissionFence(
		func(work context.Context, submit func(context.Context) error) error { return submit(work) }))
	for _, route := range []string{"/v1/user/register", "/v1/user/get-nim-token", "/v1/user/RefreshToken",
		"/v1/settings/get-sys-cfg", "/v1/user/get-user-info", "/v1/user/update-user-info",
		"/v1/group/get-group-list", "/v1/group/get-group-members", "/v1/group/mute-group-member",
		"/v1/friend/friend-apply-handle", "/v1/friend/get-friend-list", "/v1/friend/search-conversation?extra=1"} {
		if _, err := c.Request(ctx, route, map[string]any{}); err == nil {
			t.Fatal("mobile whitelist admitted a non-onboarding route")
		}
	}
	if _, err := c.RenewIM(ctx, Session{}); err == nil {
		t.Fatal("mobile companion renewed an IM credential")
	}
	if err := c.changeAvatar(ctx, nil, nil, nil); err == nil {
		t.Fatal("mobile companion reached direct avatar upload")
	}
	if _, err := c.UploadMessage(ctx, MessageUpload{}, nil); err == nil {
		t.Fatal("mobile companion reached direct message media upload")
	}
	if _, err := c.Request(ctx, "/v1/verify/sms-anon",
		map[string]any{"ty": "VERIFY_FOR_REGISTER"}); err == nil {
		t.Fatal("mobile companion admitted registration SMS")
	}
	c.requestIDs = nil
	if _, err := c.LookupOnboardingOutboundFriend(ctx, "340738649"); err == nil {
		t.Fatal("mobile request silently allocated a process-local nonce")
	}
	c.requestIDs = func(context.Context, uint64) (uint64, error) {
		return 0, errors.New("synthetic-shared-nonce-failure")
	}
	if _, err := c.LookupOnboardingOutboundFriend(ctx, "340738649"); err == nil {
		t.Fatal("failed shared nonce allocation reached transport")
	}
	if calls.Load() != 0 {
		t.Fatal("rejected mobile route or nonce failure reached transport")
	}
	c.Close()
	if _, _, err := c.Finish(ctx); err == nil || c.Login(ctx, syntheticMobilePassword()) == nil ||
		c.VerifySecurity(ctx, "123456") == nil {
		t.Fatal("closed mobile client remained usable")
	}
}

func TestMobileOnboardingUsesNumericGsonFieldsAndSingleFence(t *testing.T) {
	for _, mode := range []string{"pending", "default_account", "revoked", "duplicate", "unknown", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := newSyntheticMobileClient(t)
			c.fields[3], c.jwt, c.mac = 7, "synthetic-mobile-jwt", bytes.Clone(c.public[16:])
			c.identity = map[string]json.RawMessage{"uid": json.RawMessage("7"), "accountId": json.RawMessage("456")}
			var reads, writes, fences atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fields, _, params := mobileRequest(t, r, c.bodyKey)
				if fields[4] != 7 {
					t.Error("mobile onboarding changed authenticated identity")
				}
				var data map[string]any
				switch r.URL.Path {
				case onboardingOutboundSearchRoute:
					reads.Add(1)
					if len(params) != 3 || string(params["qrcodeId"]) != "0" ||
						string(params["key"]) != `"340738649"` {
						t.Error("mobile search did not use numeric qrcodeId")
					}
					data = map[string]any{"items": []any{onboardingOutboundSearchRow()}}
				case onboardingOutboundInfoRoute:
					reads.Add(1)
					data = onboardingOutboundInfoRow()
					if mode == "default_account" {
						data["accountId"] = 0
					}
				case onboardingOutboundApplyRoute:
					writes.Add(1)
					note, _ := onboardingOutboundNote(onboardingOutboundTestOperation)
					if fences.Load() != 1 || reads.Load() != 2 || len(params) != 10 ||
						string(params["uid"]) != "0" || string(params["questions"]) != "[]" ||
						string(params["fromGroupId"]) != "0" || string(params["applyMsg"]) != strconv.Quote(note) ||
						string(params["friendId"]) != `{"id":340738649,"idType":"ID_TP_ACCOUNT"}` {
						t.Error("mobile application lost its fixed payload or fresh final fence")
					}
					if mode == "unknown" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					data = map[string]any{"flId": "9007199254740993",
						"state": "FRIEND_STATE_APPLIED", "state1": "FRIEND_STATE_PENDING"}
				case "/v1/friend/friend-apply-list":
					reads.Add(1)
					if len(params) != 1 || string(params["lastId"]) != "0" {
						t.Error("mobile reconciliation changed its numeric server window")
					}
					note, _ := onboardingOutboundNote(onboardingOutboundTestOperation)
					row := onboardingFriendRow("9007199254740993", "8", note)
					row["direction"] = "FROM_ME"
					data = map[string]any{"list": []any{row}}
				default:
					t.Error("mobile onboarding escaped its fixed route set")
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer server.Close()
			c.config.Origin, c.http = server.URL, server.Client()
			request := OnboardingOutboundRequest{OperationID: onboardingOutboundTestOperation,
				SelfID: "7", Target: OnboardingOutboundTarget{ID: "8", Account: "340738649"},
				ExpiresAt: time.Now().Add(time.Minute)}
			fence := func(ctx context.Context, target OnboardingOutboundTarget, submit func(context.Context) error) error {
				fences.Add(1)
				if target.ID != "8" || target.Account != request.Target.Account || !target.CanRequest {
					t.Error("mobile final fence lost the fresh exact target")
				}
				switch mode {
				case "revoked":
					return errors.New("synthetic-revoked")
				case "cancelled":
					stopped, stop := context.WithCancel(ctx)
					stop()
					return submit(stopped)
				}
				err := submit(ctx)
				if mode == "duplicate" {
					return submit(ctx)
				}
				return err
			}
			result, err := c.SendOnboardingOutboundFriend(context.Background(), request, fence)
			if mode == "pending" || mode == "default_account" {
				if err != nil || result.State != "pending" || result.IsFriend ||
					result.ApplicationID != "9007199254740993" {
					t.Fatal("mobile application lost its exact receipt", err)
				}
			} else if err == nil {
				t.Fatal("blocked or unknown mobile application reported success")
			}
			expectedWrites := int32(1)
			if mode == "revoked" || mode == "cancelled" {
				expectedWrites = 0
			}
			if writes.Load() != expectedWrites || fences.Load() != 1 {
				t.Fatal("mobile application bypassed its fence or retried")
			}
			if mode == "unknown" {
				var native *Error
				if !errors.As(err, &native) || !native.Submitted {
					t.Fatal("unknown mobile submission lost its durable review signal")
				}
				result, err := c.ReconcileOnboardingOutboundFriend(context.Background(), request)
				if err != nil || result.State != "pending" || result.ApplicationID != "9007199254740993" ||
					writes.Load() != 1 || reads.Load() != 5 {
					t.Fatal("mobile unknown-result reconciliation retried or lost operation identity", err)
				}
			}
		})
	}
}
