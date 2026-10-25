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
	NIMID           string `json:"nim_id"`
	NIMToken        string `json:"nim_token"`
	NIMAuthType     int    `json:"nim_auth_type"`
	AccountID       string `json:"account_id,omitempty"`
}

type RequestIDSource func(context.Context, bool, uint64) (uint64, error)

type Client struct {
	mu         sync.Mutex
	config     Deployment
	http       *http.Client
	fields     []uint64
	public     []byte
	private    []byte
	bodyKey    []byte
	jwt        string
	mac        []byte
	cooldown   time.Time
	safe       bool
	identity   map[string]json.RawMessage
	requestIDs RequestIDSource
}

type AuthClient interface {
	Request(context.Context, string, map[string]any) (map[string]json.RawMessage, error)
	Login(context.Context, map[string]any) error
	VerifySecurity(context.Context, string) error
	Finish(context.Context) (Session, string, error)
	SecurityRequired() bool
	Close()
}

var currentIDs, legacyIDs atomic.Uint64

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
	if !supportedProtocol(d.ProtocolVersion) {
		return nil, rejection("native_protocol_unsupported")
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
		return nil, err
	}
	c := &Client{config: d, http: newHTTP(), fields: append([]uint64(nil), d.Metadata...),
		public: public, private: private, bodyKey: body}
	c.fields[3], c.fields[4], c.fields[8] = 0, 0, 0
	c.fields[0] = c.deviceIdentity(public)
	if c.android() {
		c.fields[1], c.fields[2] = 3, 1
	} else if d.ProtocolVersion == "2.8.3" {
		c.fields[1], c.fields[2] = 1, 1
	}
	return c, nil
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.private)
	clear(c.bodyKey)
	clear(c.mac)
	for _, value := range c.identity {
		clear(value)
	}
	c.jwt, c.identity = "", nil
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
	anonymous := route == "/v1/user/login" || route == "/v1/user/register" ||
		route == "/v1/user/get-change-device-verify" || route == "/v1/verify/sms-anon"
	if !anonymous && route != "/v1/user/RefreshToken" && route != "/v1/user/get-nim-token" && route != "/v1/user/verify_safe_code" {
		return nil, rejection("native_unsupported_route")
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
	current := c.android()
	id, version := uint64(now.UnixNano()), c.wireVersion()
	counter := &legacyIDs
	if current {
		id = uint64(now.UnixMilli())
		counter = &currentIDs
	}
	var err error
	if c.requestIDs != nil {
		id, err = c.requestIDs(ctx, current, id)
	} else {
		id, err = nextRequestID(counter, id)
	}
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, rejection("native_request_id_exhausted")
	}
	plain, err := json.Marshal(params)
	if err != nil {
		return nil, rejection("native_invalid_input")
	}
	digest := c.bodyDigest(plain)
	body, err := nativewire.SealBusinessDigest(c.bodyKey, id, plain, digest, c.fields[0], version)
	if err != nil {
		return nil, rejection("native_encoding_failed")
	}
	fields := append([]uint64(nil), c.fields...)
	if current {
		fields[1], fields[2], fields[10], fields[11], fields[13] = 3, 1, 0, 0, 0
	}
	aad := nativewire.BindingDigest(id, digest, fields[0], version)
	fields[5], fields[6], fields[7], fields[9], fields[12] = id, uint64(len(plain)), digest, uint64(now.Unix()), 1
	fields[8] = 0
	if len(c.mac) == 16 && (!current || fields[3] > 0) {
		input := make([]byte, 24)
		binary.LittleEndian.PutUint64(input, fields[0])
		binary.LittleEndian.PutUint64(input[8:], id)
		binary.LittleEndian.PutUint64(input[16:], fields[9])
		fields[8], err = nativewire.SipHash24(c.mac, input)
		if err != nil {
			return nil, rejection("native_encoding_failed")
		}
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
	for _, name := range []string{"x-version", "x-device", "accept", "content-type"} {
		if value := c.config.Headers[name]; value != "" {
			req.Header.Set(name, value)
		}
	}
	if current {
		req.Header.Set("x-version", "1."+strconv.FormatUint(version, 10)+".0")
		req.Header.Set("x-device", "3")
		req.Header.Set("x-client", "2")
		if c.config.ProtocolVersion == "3.7.4" {
			req.Header.Set("content-type", "application/json; charset=utf-8")
		}
	} else if c.config.ProtocolVersion == "2.8.3" {
		req.Header.Set("x-version", "1.1770619133615.0")
		req.Header.Set("x-device", "1")
		req.Header.Set("x-client", "11")
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
	// The current SDK supplies a sealed device context to registration too.
	// It authenticates the device, not the newly created user.
	if route == "/v1/user/register" && c.config.ProtocolVersion == "3.7.4" {
		exchange, err = c.exchangeContext(3, uint64(now.UnixNano()))
		if err != nil {
			return nil, err
		}
	}
	if len(exchange) > 0 {
		req.Header.Set("x-context", b64(exchange))
	}
	if err := ctx.Err(); err != nil {
		return nil, rejection("native_cancelled")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{Code: "native_transport_unknown", Submitted: true}
	}
	defer response.Body.Close()
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
	encoded, err := io.ReadAll(io.LimitReader(response.Body, nativewire.Limit+4097))
	if err != nil || len(encoded) > nativewire.Limit+4096 {
		return nil, &Error{Code: "native_response_invalid", Submitted: true}
	}
	decoded := encoded
	if !bytes.HasPrefix(encoded, []byte(`{"`)) {
		decoded, err = nativewire.OpenBusiness(c.bodyKey, aad, encoded, nativewire.Limit)
	}
	if err != nil || len(decoded) > nativewire.Limit {
		return nil, &Error{Code: "native_response_invalid", Submitted: true}
	}
	var envelope struct {
		Code *int                       `json:"code"`
		Msg  string                     `json:"msg"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(decoded, &envelope) != nil || envelope.Code == nil {
		return nil, &Error{Code: "native_response_invalid", Submitted: true}
	}
	if *envelope.Code != 0 {
		number, code := *envelope.Code, "native_business_rejected"
		switch number {
		case 401:
			code, c.jwt, c.mac = "native_reauth_required", "", nil
		case 429:
			code = "native_rate_limited"
			c.cooldown = time.Now().Add(30 * time.Second)
		case 1001:
			code = "native_login_rejected_unspecified"
		case 1039:
			code = "native_phone_not_found"
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
		default:
			switch envelope.Msg {
			case "账号或密码错误", "密码错误":
				code = "native_invalid_credentials"
			case "验证码错误", "短信验证码错误":
				code = "native_invalid_sms"
			case "验证码已过期":
				code = "native_sms_expired"
			}
		}
		return nil, &Error{Code: code, Number: number, RetryAfter: max(0, time.Until(c.cooldown)), Submitted: true}
	}
	if route == "/v1/user/verify_safe_code" {
		token := response.Header.Get("x-set-jwt")
		if token == "" {
			_ = json.Unmarshal(envelope.Data["jwtToken"], &token)
		}
		if !c.safe || c.jwt == "" || (token != "" && !validToken(token)) {
			return nil, rejection("native_security_rotation_invalid")
		}
		if token != "" {
			c.jwt = token
		}
		c.safe = false
	}
	if current && c.jwt != "" {
		c.fields[4] = (c.fields[4] + 1) & 0x7fffffff
	}
	return envelope.Data, nil
}

func (c *Client) Login(ctx context.Context, params map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.jwt, c.mac, c.safe, c.identity = "", nil, false, nil
	c.fields[3], c.fields[4] = 0, 0
	current := c.android()
	kind := uint64(1)
	if current {
		kind, c.mac = 3, append([]byte(nil), c.public[16:32]...)
		params["version"], params["appVersion"] = "3.7.3", 335
		params["appHash"] = "3C:F2:35:2F:6D:B2:85:67:98:25:B1:2F:06:5E:FC:5C:7A:AC:0F:6D:74:67:DA:4E:F9:50:05:58:C4:42:F1:96=02ED673B263F7EA80B307ADE1967C49A7264F955CDF33EB36F7673591029E195"
		if c.config.ProtocolVersion == "3.7.4" {
			params["version"], params["appVersion"], params["appHash"] = "3.7.4", 337, android374AppHash
		}
	}
	now := uint64(time.Now().UnixNano())
	sealed, err := c.exchangeContext(kind, now)
	if err != nil {
		return rejection("native_exchange_failed")
	}
	result, err := c.request(ctx, "/v1/user/login", params, sealed)
	if err != nil {
		return err
	}
	var uid uint64
	var jwt string
	if json.Unmarshal(result["uid"], &uid) != nil || uid == 0 || uid >= 1<<31 ||
		json.Unmarshal(result["jwtToken"], &jwt) != nil || !validToken(jwt) {
		return rejection("native_login_binding")
	}
	mac, tokenKind := c.mac, uint64(0)
	if !current {
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
		var seconds, correlation, tokenUID uint64
		seconds, correlation, tokenKind, tokenUID, mac, err = readLoginToken(opened)
		if err != nil || seconds != now/1e9 || correlation != now || tokenUID != uid {
			return rejection("native_login_binding")
		}
	}
	var required, set bool
	for name, dest := range map[string]*bool{"safeCodeRequired": &required, "safeCodeSet": &set} {
		if raw, ok := result[name]; ok && (string(raw) != "true" && string(raw) != "false" || json.Unmarshal(raw, dest) != nil) {
			return rejection("native_login_binding")
		}
	}
	c.fields[3], c.fields[4], c.jwt, c.mac, c.safe, c.identity = uid, tokenKind, jwt, mac, required, result
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
	if !c.safe || !sixDigits(code) {
		return rejection("native_security_input")
	}
	_, err := c.request(ctx, "/v1/user/verify_safe_code", map[string]any{"code": code}, nil)
	return err
}

func (c *Client) Finish(ctx context.Context) (Session, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
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
	authType := 0
	if c.android() || c.config.ProtocolVersion == "2.8.3" {
		result, err := c.request(ctx, "/v1/user/get-nim-token", map[string]any{
			"nimDeviceId": c.config.DeviceID, "uid": c.fields[3], "loginToken": c.jwt}, nil)
		if err != nil {
			return session, "", err
		}
		_ = json.Unmarshal(result["nimToken"], &token)
		kind := "NIM_LOGIN_KIND_STATIC"
		if raw, ok := result["kind"]; ok && json.Unmarshal(raw, &kind) != nil {
			return session, "", rejection("native_nim_token_invalid")
		}
		types := map[string]int{"NIM_LOGIN_KIND_STATIC": 0, "NIM_LOGIN_KIND_DYNAMIC": 1, "NIM_LOGIN_KIND_CALLBACK": 2}
		var ok bool
		if authType, ok = types[kind]; !ok {
			return session, "", rejection("native_nim_token_invalid")
		}
	} else {
		_ = json.Unmarshal(c.identity["nimToken"], &token)
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
		NIMID: nimID, NIMToken: token, NIMAuthType: authType}
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
