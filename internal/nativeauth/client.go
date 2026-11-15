// SPDX-License-Identifier: AGPL-3.0-or-later
package nativeauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"google.golang.org/protobuf/encoding/protowire"

	"wangshangliao/internal/nativewire"
)

type Error struct {
	Code       string
	Number     int
	RetryAfter time.Duration
	Submitted  bool
	Diagnostic string
}

func (e *Error) Error() string     { return e.Code }
func rejection(code string) *Error { return &Error{Code: code} }

type Session struct {
	Origin          string `json:"origin"`
	UID             uint64 `json:"uid"`
	Kind            uint64 `json:"kind"`
	Device          uint64 `json:"device"`
	JWT             string `json:"jwt"`
	MAC             []byte `json:"mac"`
	ProtocolVersion string `json:"protocol_version"`
	DesktopPlatform string `json:"desktop_platform,omitempty"`
	NIMID           string `json:"nim_id"`
	NIMToken        string `json:"nim_token"`
	NIMAuthType     int    `json:"nim_auth_type"`
	AccountID       string `json:"account_id,omitempty"`
}

type RequestIDSource func(context.Context, uint64) (uint64, error)

// RequestCountSource allocates one Android per-user metadata count before
// transport. The source owns advancement, including reservations not submitted.
type RequestCountSource func(context.Context) (uint64, error)

type requestObserverKey struct{}

// WithRequestObserver exposes only a request count, not a route or payload.
// It is optional and must not perform I/O; authentication is otherwise unchanged.
func WithRequestObserver(ctx context.Context, observe func()) context.Context {
	return context.WithValue(ctx, requestObserverKey{}, observe)
}

type Client struct {
	mu             sync.Mutex
	config         Deployment
	http           *http.Client
	fields         []uint64
	public         []byte
	private        []byte
	bodyKey        []byte
	jwt            string
	mac            []byte
	cooldown       time.Time
	safe           bool
	identity       map[string]json.RawMessage
	requestIDs     RequestIDSource
	requestCounts  RequestCountSource
	mobile         bool
	closed         bool
	loginUnknown   bool
	onboardingPeer *OnboardingOutboundTarget
}

type AuthClient interface {
	Request(context.Context, string, map[string]any) (map[string]json.RawMessage, error)
	Login(context.Context, map[string]any) error
	VerifySecurity(context.Context, string) error
	Finish(context.Context) (Session, string, error)
	SecurityRequired() bool
	Close()
}

var desktopIDs atomic.Uint64

// All accounts share the deployment's body key. Request nonces must not collide
// between clients using the same millisecond timestamp.
func nextRequestID(counter *atomic.Uint64, now uint64) (uint64, error) {
	for {
		last := counter.Load()
		id := now
		if id <= last {
			if last == ^uint64(0) {
				return 0, rejection("native_request_id_exhausted")
			}
			id = last + 1
		}
		if counter.CompareAndSwap(last, id) {
			return id, nil
		}
	}
}

func NewClient(d Deployment) (AuthClient, error) {
	return newClient(d, false)
}

// NewMobileClient creates an independent business-auth companion, never an IM client.
func NewMobileClient(d Deployment) (AuthClient, error) {
	return newClient(d, true)
}

func newClient(d Deployment, mobile bool) (AuthClient, error) {
	var err error
	d, err = validateDeployment(d, mobile)
	if err != nil {
		return nil, err
	}
	if !nativewire.Available() {
		return nil, rejection("native_crypto_unavailable")
	}
	seed, err := decodeKey(d.SigningSeed)
	if err != nil {
		return nil, err
	}
	defer clear(seed)
	public, private, err := nativewire.SigningKeypair(seed)
	if err != nil {
		return nil, err
	}
	body, err := decodeKey(d.BodyKey)
	if err != nil {
		clear(private)
		return nil, err
	}
	c := &Client{config: d, http: newHTTP(), fields: append([]uint64(nil), d.Metadata...),
		public: public, private: private, bodyKey: body, mobile: mobile}
	c.fields[3], c.fields[4], c.fields[8] = 0, 0, 0
	c.fields[0] = c.deviceIdentity(public)
	c.fields[1], c.fields[2] = 1, 1
	if mobile {
		c.fields[1] = 3
	}
	return c, nil
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	clear(c.private)
	clear(c.bodyKey)
	clear(c.mac)
	for _, value := range c.identity {
		clear(value)
	}
	c.jwt, c.identity, c.safe = "", nil, false
	c.requestCounts = nil
	c.config = Deployment{}
	c.http.CloseIdleConnections()
}

func (c *Client) SecurityRequired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.safe
}

func (c *Client) Request(ctx context.Context, route string, params map[string]any) (map[string]json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.request(ctx, route, params, nil)
}

func validToken(token string) bool {
	if token == "" || len(token) > 8192 {
		return false
	}
	for _, r := range token {
		if r > 127 || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (c *Client) request(ctx context.Context, route string, params map[string]any, exchange []byte) (map[string]json.RawMessage, error) {
	if c.closed {
		return nil, rejection("native_auth_closed")
	}
	if c.mobile {
		var err error
		params, err = c.mobileRequestParams(route, params, exchange)
		if err != nil {
			return nil, err
		}
		if c.requestIDs == nil {
			return nil, rejection("native_request_id_unavailable")
		}
	}
	anonymous := route == "/v1/user/login" || route == "/v1/user/register" ||
		route == "/v1/user/get-change-device-verify" || route == "/v1/verify/sms-anon" ||
		route == "/v1/settings/get-sys-cfg"
	if !anonymous && route != "/v1/user/RefreshToken" && route != "/v1/user/get-nim-token" &&
		route != "/v1/user/verify_safe_code" && route != "/v1/group/get-group-list" &&
		route != "/v1/group/get-group-members" && !nativeSettingsRoute(route) && !nativeSettingMutationRoute(route) &&
		!onboardingOutboundRoute(route) {
		return nil, rejection("native_unsupported_route")
	}
	fence, fenced := ctx.Value(settingFenceKey{}).(nativewire.SubmissionFence)
	settingMutation := nativeSettingMutationRoute(route) || route == onboardingOutboundApplyRoute ||
		route == "/v1/verify/sms-anon" && fenced
	if settingMutation && (!fenced || fence == nil) {
		return nil, rejection("native_setting_fence_required")
	}
	if !anonymous && c.jwt == "" {
		return nil, rejection("native_reauth_required")
	}
	if time.Now().Before(c.cooldown) {
		return nil, &Error{Code: "native_rate_limited", RetryAfter: time.Until(c.cooldown)}
	}
	if ctx.Err() != nil {
		return nil, rejection("native_cancelled")
	}
	now := time.Now()
	id, version := uint64(now.UnixNano()), c.wireVersion()
	var err error
	if c.requestIDs != nil {
		id, err = c.requestIDs(ctx, id)
	} else {
		id, err = nextRequestID(&desktopIDs, id)
	}
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, rejection("native_request_id_exhausted")
	}
	plain, err := json.Marshal(params)
	if err != nil || c.mobile && len(plain) > 32768 {
		clear(plain)
		return nil, rejection("native_invalid_input")
	}
	defer clear(plain)
	digest := c.bodyDigest(plain)
	body, err := nativewire.SealBusinessDigest(c.bodyKey, id, plain, digest, c.fields[0], version)
	if err != nil {
		return nil, rejection("native_encoding_failed")
	}
	fields := append([]uint64(nil), c.fields...)
	aad := nativewire.BindingDigest(id, digest, fields[0], version)
	fields[5], fields[6], fields[7], fields[9], fields[12] = id, uint64(len(plain)), digest, uint64(now.Unix()), 1
	fields[8] = 0
	if len(c.mac) == 16 {
		input := make([]byte, 24)
		binary.LittleEndian.PutUint64(input, fields[0])
		binary.LittleEndian.PutUint64(input[8:], id)
		binary.LittleEndian.PutUint64(input[16:], fields[9])
		fields[8], err = nativewire.SipHash24(c.mac, input)
		if err != nil {
			return nil, rejection("native_encoding_failed")
		}
	}
	if c.mobile && c.jwt != "" && c.requestCounts != nil {
		count, err := c.requestCounts(ctx)
		if err != nil {
			return nil, err
		}
		if count >= 1<<32 {
			return nil, rejection("native_request_count_exhausted")
		}
		if ctx.Err() != nil {
			return nil, rejection("native_cancelled")
		}
		fields[4] = count
	}
	metadata, err := nativewire.Metadata(fields)
	if err != nil {
		return nil, rejection("native_encoding_failed")
	}
	signature, err := nativewire.Sign(c.private, metadata)
	if err != nil {
		return nil, rejection("native_encoding_failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.Origin+route, bytes.NewReader(body))
	if err != nil {
		return nil, rejection("native_invalid_input")
	}
	for _, name := range []string{"accept", "content-type"} {
		if value := c.config.Headers[name]; value != "" {
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("x-version", "1."+strconv.FormatUint(version, 10)+".0")
	req.Header.Set("x-device", "1")
	req.Header.Set("x-client", desktopClient(c.config.DesktopPlatform))
	if c.mobile {
		req.Header.Set("x-device", "3")
		req.Header.Set("x-client", "2")
	}
	b64 := base64.RawURLEncoding.EncodeToString
	req.Header.Set("x-request", b64(metadata))
	req.Header.Set("x-seed", b64(c.public))
	req.Header.Set("x-hash", b64(signature))
	req.Header.Set("x-trace-id", strconv.FormatUint(fields[0], 10)+"."+strconv.FormatUint(id, 10))
	if c.jwt != "" {
		req.Header.Set("x-id", strconv.FormatUint(fields[3], 10))
		req.Header.Set("x-token", strconv.FormatUint(fields[3], 10))
		req.Header.Set("x-jwt", c.jwt)
	}
	if len(exchange) > 0 {
		req.Header.Set("x-context", b64(exchange))
	}
	if err := ctx.Err(); err != nil {
		return nil, rejection("native_cancelled")
	}
	if observe, ok := ctx.Value(requestObserverKey{}).(func()); ok {
		observe()
	}
	var response *http.Response
	var settingBody []byte
	attempted := false
	submit := func(work context.Context) error {
		attempted = true
		response, err = c.http.Do(req.WithContext(work))
		if err == nil && settingMutation {
			settingBody, err = io.ReadAll(io.LimitReader(response.Body, nativewire.Limit+4097))
		}
		return err
	}
	if settingMutation {
		err = fence(ctx, submit)
	} else {
		err = submit(ctx)
	}
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		if !attempted {
			return nil, err
		}
		var native *Error
		if errors.As(err, &native) {
			// These errors originate only in the pre-connect address gate.
			switch native.Code {
			case "native_origin_rejected", "native_dns_failed", "native_dns_fake_ip", "native_dns_fallback_failed":
				return nil, native
			}
		}
		diagnostic := "network_unavailable"
		var network net.Error
		if errors.As(err, &network) && network.Timeout() {
			diagnostic = "network_timeout"
		} else if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			diagnostic = "connection_closed"
		}
		return nil, &Error{Code: "native_transport_unknown", Submitted: true, Diagnostic: diagnostic}
	}
	defer response.Body.Close()
	if c.mobile && c.jwt != "" && c.requestCounts == nil {
		// The Android SDK advances its existing per-user count after a response.
		c.fields[4] = uint64(uint32(c.fields[4] + 1))
	}
	if response.StatusCode == 401 {
		c.jwt, c.mac = "", nil
		return nil, &Error{Code: "native_reauth_required", Number: 401, Submitted: true}
	}
	if response.StatusCode == 429 {
		delay := 30 * time.Second
		if n, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 32); err == nil {
			delay = time.Duration(max(1, min(n, 86400))) * time.Second
		} else if date, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil {
			delay = min(24*time.Hour, max(time.Second, time.Until(date)))
		}
		c.cooldown = time.Now().Add(delay)
		return nil, &Error{Code: "native_rate_limited", Number: 429, RetryAfter: delay, Submitted: true}
	}
	if response.StatusCode >= 500 {
		return nil, &Error{Code: "native_transport_unknown", Number: response.StatusCode, Submitted: true}
	}
	if response.StatusCode != 200 {
		return nil, &Error{Code: "native_http_rejected", Number: response.StatusCode, Submitted: true}
	}
	responseLimit := nativewire.Limit
	// This fixed account-wide history route returns a bounded server window
	// whose authenticated JSON can exceed the ordinary business response cap.
	if route == "/v1/group/get-apply-logs" {
		responseLimit = 3 * nativewire.Limit
	}
	encoded := settingBody
	if !settingMutation {
		encoded, err = io.ReadAll(io.LimitReader(response.Body, int64(responseLimit)+4097))
	}
	if err != nil || len(encoded) > responseLimit+4096 {
		return nil, &Error{Code: "native_response_invalid", Submitted: true}
	}
	decoded := encoded
	if !bytes.HasPrefix(encoded, []byte(`{"`)) {
		decoded, err = nativewire.OpenBusiness(c.bodyKey, aad, encoded, responseLimit)
	}
	if err != nil || len(decoded) > responseLimit {
		return nil, &Error{Code: "native_response_invalid", Submitted: true}
	}
	var envelope struct {
		Code *int            `json:"code"`
		Msg  json.RawMessage `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(decoded, &envelope) != nil || envelope.Code == nil {
		return nil, &Error{Code: "native_response_invalid", Submitted: true}
	}
	if *envelope.Code != 0 {
		number := *envelope.Code
		code, diagnostic := authBusinessResponseRejection(route, params, number, envelope.Msg)
		if onboardingLookupRoute(route) {
			// Error payloads still require the fixed business envelope. Only a
			// bounded reason classification survives this request.
			var data map[string]json.RawMessage
			if len(envelope.Data) > 0 && json.Unmarshal(envelope.Data, &data) != nil {
				return nil, &Error{Code: "native_response_invalid", Submitted: true}
			}
			code, diagnostic = onboardingLookupRejection(number, envelope.Msg)
		}
		if diagnostic == "" {
			// Non-authentication routes retain their original strict envelope.
			var message string
			var data map[string]json.RawMessage
			if len(envelope.Msg) > 0 && json.Unmarshal(envelope.Msg, &message) != nil ||
				len(envelope.Data) > 0 && json.Unmarshal(envelope.Data, &data) != nil {
				return nil, &Error{Code: "native_response_invalid", Submitted: true}
			}
		}
		switch number {
		case 401:
			code, c.jwt, c.mac = "native_reauth_required", "", nil
		case 429:
			code = "native_rate_limited"
			c.cooldown = time.Now().Add(30 * time.Second)
		case 1039:
			if diagnostic == "" {
				code = "native_phone_not_found"
			}
		case 1069:
			code = "native_device_verification_required"
		case 1400:
			code = "native_security_setup_required"
		case 1401:
			code = "native_account_clearing"
		case 1402:
			code, c.safe = "native_security_required", true
		case 1403:
			code = "native_security_invalid"
		case 1404:
			code = "native_security_locked"
		case 1405:
			code = "native_security_phone_required"
		}
		if onboardingLookupRoute(route) && code == "native_reauth_required" {
			c.jwt, c.mac = "", nil
		}
		if code == "native_rate_limited" || code == "native_sms_rate_limited" {
			// This is a local guard, not a claim about the provider's window.
			delay := 30 * time.Second
			if code == "native_sms_rate_limited" {
				delay = time.Minute
			}
			if until := time.Now().Add(delay); c.cooldown.Before(until) {
				c.cooldown = until
			}
		}
		return nil, &Error{Code: code, Number: number, Diagnostic: diagnostic,
			RetryAfter: max(0, time.Until(c.cooldown)), Submitted: true}
	}
	var data map[string]json.RawMessage
	var message string
	if len(envelope.Msg) > 0 && json.Unmarshal(envelope.Msg, &message) != nil ||
		len(envelope.Data) > 0 && json.Unmarshal(envelope.Data, &data) != nil {
		return nil, &Error{Code: "native_response_invalid", Submitted: true}
	}
	if route == "/v1/user/verify_safe_code" {
		token := response.Header.Get("x-set-jwt")
		if token == "" {
			_ = json.Unmarshal(data["jwtToken"], &token)
		}
		if !c.safe || c.jwt == "" || (token != "" && !validToken(token)) {
			return nil, rejection("native_security_rotation_invalid")
		}
		if token != "" {
			c.jwt = token
		}
		c.safe = false
	}
	return data, nil
}

func (c *Client) Login(ctx context.Context, params map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return rejection("native_auth_closed")
	}
	if c.mobile && (c.loginUnknown || c.jwt != "") {
		return rejection("native_auth_state_conflict")
	}
	kind := uint64(1)
	if c.mobile {
		var err error
		params, err = mobileLoginParams(params)
		if err != nil {
			return err
		}
		kind = 3
	}
	clear(c.mac)
	clearOnboardingOutboundData(c.identity)
	c.jwt, c.mac, c.safe, c.identity = "", nil, false, nil
	c.fields[3], c.fields[4] = 0, 0
	for _, name := range []string{"version", "appVersion", "appHash"} {
		if _, exists := params[name]; exists && !c.mobile {
			return rejection("native_desktop_required")
		}
	}
	now := uint64(time.Now().UnixNano())
	sealed, err := c.exchangeContext(kind, now)
	if err != nil {
		return rejection("native_exchange_failed")
	}
	result, err := c.request(ctx, "/v1/user/login", params, sealed)
	if err != nil {
		var native *Error
		if c.mobile && errors.As(err, &native) && native.Submitted &&
			(native.Code == "native_transport_unknown" || native.Code == "native_response_invalid") {
			c.loginUnknown = true
		}
		return err
	}
	accepted := false
	defer func() {
		if !accepted {
			clearOnboardingOutboundData(result)
			if c.mobile {
				c.loginUnknown = true
			}
		}
	}()
	var uid uint64
	var jwt string
	if json.Unmarshal(result["uid"], &uid) != nil || uid == 0 || uid >= 1<<31 ||
		json.Unmarshal(result["jwtToken"], &jwt) != nil || !validToken(jwt) {
		if c.mobile {
			return mobileLoginFailure("login_identity_invalid")
		}
		return rejection("native_login_binding")
	}
	if c.mobile {
		err = c.acceptMobileLogin(result, params, uid, jwt)
		accepted = err == nil
		return err
	}
	var token string
	if json.Unmarshal(result["token"], &token) != nil {
		return rejection("native_login_binding")
	}
	encoded, err := nativewire.Base64URL(token)
	if err != nil || len(encoded) > 65584 {
		return rejection("native_login_binding")
	}
	private, err := nativewire.CurvePrivate(c.private)
	if err != nil {
		return rejection("native_login_binding")
	}
	opened, err := nativewire.OpenSealedBox(private, encoded)
	clear(private)
	if err != nil {
		return rejection("native_login_binding")
	}
	defer clear(opened)
	seconds, correlation, tokenKind, tokenUID, mac, err := readLoginToken(opened)
	defer func() {
		if !accepted {
			clear(mac)
		}
	}()
	if err != nil || seconds != now/1e9 || correlation != now || tokenUID != uid {
		clear(mac)
		return rejection("native_login_binding")
	}
	var required, set bool
	for name, dest := range map[string]*bool{"safeCodeRequired": &required, "safeCodeSet": &set} {
		if raw, ok := result[name]; ok && (string(raw) != "true" && string(raw) != "false" || json.Unmarshal(raw, dest) != nil) {
			return rejection("native_login_binding")
		}
	}
	c.fields[3], c.fields[4], c.jwt, c.mac, c.safe, c.identity = uid, tokenKind, jwt, mac, required, result
	accepted = true
	return nil
}

func (c *Client) exchangeContext(kind, correlation uint64) ([]byte, error) {
	values := []uint64{c.fields[0], kind, 0, correlation / 1e9, correlation, c.fields[2]}
	var exchange []byte
	for i, value := range values {
		if i == 2 {
			exchange = protowire.AppendTag(exchange, 3, protowire.BytesType)
			exchange = protowire.AppendBytes(exchange, c.public)
		} else if value != 0 {
			exchange = protowire.AppendTag(exchange, protowire.Number(i+1), protowire.VarintType)
			exchange = protowire.AppendVarint(exchange, value)
		}
	}
	server, err := decodeKey(c.config.ServerKey)
	if err != nil {
		return nil, rejection("native_exchange_failed")
	}
	return nativewire.SealBox(server, exchange)
}

func readLoginToken(data []byte) (seconds, correlation, kind, uid uint64, mac []byte, err error) {
	seen := map[protowire.Number]bool{}
	for len(data) > 0 {
		number, typ, n := protowire.ConsumeTag(data)
		if n < 0 || (number <= 6 && seen[number]) {
			err = errors.New("native_login_binding")
			return
		}
		seen[number] = true
		data = data[n:]
		n = protowire.ConsumeFieldValue(number, typ, data)
		if n < 0 {
			err = errors.New("native_login_binding")
			return
		}
		if number == 1 || number == 2 || number == 5 || number == 6 {
			value, used := protowire.ConsumeVarint(data)
			if typ != protowire.VarintType || used < 0 {
				err = errors.New("native_login_binding")
				return
			}
			switch number {
			case 1:
				seconds = value
			case 2:
				correlation = value
			case 5:
				kind = uint64(uint32(value))
			case 6:
				uid = value
			}
		} else if number == 4 {
			value, used := protowire.ConsumeBytes(data)
			if typ != protowire.BytesType || used < 0 || len(value) < 16 || len(value) > 1024 {
				err = errors.New("native_login_binding")
				return
			}
			mac = append([]byte(nil), value[:16]...)
		}
		data = data[n:]
	}
	if len(mac) != 16 || seconds == 0 || correlation == 0 || uid == 0 {
		err = errors.New("native_login_binding")
	}
	return
}

func (c *Client) VerifySecurity(ctx context.Context, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return rejection("native_auth_closed")
	}
	if !c.safe || !sixDigits(code) {
		return rejection("native_security_input")
	}
	_, err := c.request(ctx, "/v1/user/verify_safe_code", map[string]any{"code": code}, nil)
	return err
}

func (c *Client) Finish(ctx context.Context) (Session, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Session{}, "", rejection("native_auth_closed")
	}
	if c.mobile {
		return c.finishMobile(ctx)
	}
	var session Session
	if c.safe {
		return session, "", rejection("native_security_required")
	}
	var nimID, token, nickname string
	var rawID any
	if json.Unmarshal(c.identity["nimId"], &rawID) != nil {
		return session, "", rejection("native_login_identity")
	}
	switch value := rawID.(type) {
	case string:
		nimID = value
	default:
		var number json.Number
		if json.Unmarshal(c.identity["nimId"], &number) == nil {
			nimID = number.String()
		}
	}
	if nimID == "" || len(nimID) > 128 || strings.IndexFunc(nimID, unicode.IsSpace) >= 0 {
		return session, "", rejection("native_login_identity")
	}
	result, err := c.request(ctx, "/v1/user/get-nim-token", map[string]any{
		"nimDeviceId": c.config.DeviceID, "uid": c.fields[3], "loginToken": c.jwt}, nil)
	if err != nil {
		return session, "", err
	}
	token, authType, err := decodeIMToken(result)
	if err != nil {
		return session, "", err
	}
	if !validToken(token) {
		return session, "", rejection("native_nim_token_invalid")
	}
	for _, key := range []string{"nickName", "userNick", "nickname"} {
		if json.Unmarshal(c.identity[key], &nickname) == nil && nickname != "" {
			break
		}
	}
	nickname = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, nickname)
	if runes := []rune(nickname); len(runes) > 128 {
		nickname = string(runes[:128])
	}
	session = Session{Origin: c.config.Origin, UID: c.fields[3], Kind: c.fields[4], Device: c.fields[0],
		JWT: c.jwt, MAC: append([]byte(nil), c.mac...), ProtocolVersion: c.config.ProtocolVersion,
		DesktopPlatform: c.config.DesktopPlatform, NIMID: nimID, NIMToken: token, NIMAuthType: authType}
	if raw, ok := c.identity["accountId"]; ok {
		var account uint64
		if json.Unmarshal(raw, &account) != nil || account == 0 || account >= 1<<31 {
			return Session{}, "", rejection("native_login_identity")
		}
		session.AccountID = strconv.FormatUint(account, 10)
	}
	return session, nickname, nil
}

func sixDigits(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, b := range []byte(value) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
