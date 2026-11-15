// SPDX-License-Identifier: AGPL-3.0-or-later
// Package lajiaoliao adapts the pinned AstrBot Go bridge protocol without its
// worker process, local web server, token files or Python business runtime.
package lajiaoliao

import (
	"bytes"
	"context"
	"crypto/md5" // Required by the pinned desktop login wire contract, not password storage.
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const SourceRevision = "e62e2ce12e7d8591afe995c56478205224294afd"

const objectStorageHost = "lj-chat-im-01-hz-acc.lajiaoliao.com"
const objectReadHost = "lj-chat-im-01.lajiaoliao.com"

type Failure struct {
	Code       string
	Submitted  bool
	RetryAfter time.Duration
	cause      error
}

func (e *Failure) Error() string { return e.Code }
func (e *Failure) Unwrap() error { return e.cause }

type Session struct {
	UserID    string `json:"user_id"`
	ChatToken string `json:"chat_token"`
	IMToken   string `json:"im_token"`
	DeviceID  string `json:"device_id"`
}

type Challenge struct {
	Required bool   `json:"required"`
	Region   string `json:"region,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	SceneID  string `json:"scene_id,omitempty"`
}

type Client struct{ http *http.Client }
type observerKey struct{}

func WithRequestObserver(ctx context.Context, fn func()) context.Context {
	return context.WithValue(ctx, observerKey{}, fn)
}

func public(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !netip.MustParsePrefix("100.64.0.0/10").Contains(ip) &&
		!netip.MustParsePrefix("198.18.0.0/15").Contains(ip) &&
		!netip.MustParsePrefix("192.0.0.0/24").Contains(ip)
}

func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" || (host != "api.lajiaoliao.com" && host != "im-api.lajiaoliao.com" &&
		host != "ws.lajiaoliao.com" && host != "lj-im-chat-01.lajiaoliao.com" &&
		host != objectStorageHost && host != objectReadHost) {
		return nil, errors.New("lajiaoliao_origin_rejected")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("lajiaoliao_dns_failed")
	}
	for _, ip := range addresses {
		if !public(ip) {
			return nil, errors.New("lajiaoliao_origin_rejected")
		}
	}
	for _, ip := range addresses {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("lajiaoliao_connect_failed")
}

func NewClient() *Client {
	return &Client{http: &http.Client{
		Timeout:       12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{Proxy: nil, DialContext: dialPublic, TLSHandshakeTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 8 * time.Second, MaxConnsPerHost: 2, MaxIdleConnsPerHost: 2,
			MaxIdleConns: 4, IdleConnTimeout: time.Minute},
	}}
}

func NewDeviceID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "device_windows_artwslbot_" + hex.EncodeToString(value[:]), nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

var chatRoutes = map[string]bool{"/client_config/get": true, "/pc/account/login": true, "/pc/account/get_im_token": true,
	"/account/code/send": true, "/pc/account/login_or_signup": true,
	"/friend/privacy/get": true, "/friend/privacy/set": true, "/user/update": true,
	"/user/find/full": true, "/push_config/get": true, "/push_config/set": true,
	"/user/search/full": true,
	"/account/code/verify": true, "/account/password/reset": true,
	"/account/contact/check": true, "/account/contact/change": true}
var imRoutes = map[string]bool{"/msg/revoke_msg": true, "/group/get_joined_group_list": true, "/group/get_group_member_list": true,
	"/group/get_group_members_info": true, "/group/get_groups_info": true, "/group/mute_group_member": true, "/group/cancel_mute_group_member": true,
	"/group/mute_group": true, "/group/cancel_mute_group": true,
	"/group/kick_group": true, "/group/set_group_info_ex": true, "/group/set_group_member_info": true,
	"/group/get_group_permanent_ban_list": true, "/group/remove_group_permanent_ban": true,
	"/group/invite_user_to_group": true, "/group/quit_group": true, "/group/get_recv_group_applicationList": true,
	"/group/get_user_req_group_applicationList": true, "/group/group_application_response": true,
	"/friend/get_friend_list": true, "/friend/get_friend_apply_list": true, "/friend/add_friend_response": true,
	"/friend/get_designated_friends": true, "/friend/get_self_friend_apply_list": true, "/friend/add_friend": true,
	"/user/get_users_info": true, "/user/update_user_info_ex": true,
	"/object/part_limit": true, "/object/initiate_multipart_upload": true, "/object/complete_multipart_upload": true}

func (c *Client) request(ctx context.Context, route string, payload any, token, clientPlatform, device string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	origin := "https://api.lajiaoliao.com"
	if !chatRoutes[route] {
		if !imRoutes[route] {
			return nil, &Failure{Code: "lajiaoliao_route_forbidden"}
		}
		origin = "https://im-api.lajiaoliao.com"
	}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 65536 {
		return nil, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	defer clear(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+route, bytes.NewReader(body))
	if err != nil {
		return nil, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	if route == "/object/initiate_multipart_upload" || route == "/object/complete_multipart_upload" {
		req.GetBody = nil
	}
	id, err := NewDeviceID()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("operationID", id)
	req.Header.Set("version", "1.0.3")
	req.Header.Set("platform", clientPlatform)
	req.Header.Set("deviceID", device)
	if token != "" {
		req.Header.Set("token", token)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if observe, ok := ctx.Value(observerKey{}).(func()); ok {
		observe()
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		retry := time.Minute
		if at, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
			retry = max(time.Second, time.Until(at))
		}
		if seconds, err := time.ParseDuration(resp.Header.Get("Retry-After") + "s"); err == nil {
			retry = max(time.Second, seconds)
		}
		return nil, &Failure{Code: "lajiaoliao_rate_limited", RetryAfter: min(retry, time.Hour)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	defer clear(raw)
	if err != nil || len(raw) > 2<<20 {
		return nil, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	var envelope struct {
		Code *int            `json:"errCode"`
		Data json.RawMessage `json:"data"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(raw, &envelope) != nil || envelope.Code == nil {
		return nil, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	if *envelope.Code != 0 {
		code := "lajiaoliao_request_rejected"
		if route == "/account/code/send" || route == "/account/code/verify" ||
			route == "/account/password/reset" || route == "/account/contact/check" || route == "/account/contact/change" {
			if known := map[int]string{20001: "lajiaoliao_password_invalid", 20002: "lajiaoliao_account_not_found",
				20003: "lajiaoliao_phone_registered", 20005: "lajiaoliao_rate_limited",
				20006: "lajiaoliao_code_invalid", 20007: "lajiaoliao_code_expired",
				20008: "lajiaoliao_code_attempts_exceeded", 20009: "lajiaoliao_code_used",
				20012: "lajiaoliao_operation_restricted"}[*envelope.Code]; known != "" {
				code = known
			}
		}
		if *envelope.Code == 1501 {
			code = "lajiaoliao_token_expired"
		}
		if *envelope.Code >= 1502 && *envelope.Code <= 1507 {
			code = "lajiaoliao_reauth_required"
		}
		if *envelope.Code == 1506 {
			code = "lajiaoliao_kicked"
		}
		return nil, &Failure{Code: code, Submitted: true}
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return append(json.RawMessage(nil), raw...), nil
	}
	return append(json.RawMessage(nil), envelope.Data...), nil
}

func (c *Client) Challenge(ctx context.Context, device string) (Challenge, error) {
	return c.SecurityChallenge(ctx, device, 3)
}

func (c *Client) SecurityChallenge(ctx context.Context, device string, purpose int) (Challenge, error) {
	if purpose != 2 && purpose != 3 && purpose != 6 && purpose != 7 {
		return Challenge{}, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	raw, err := c.request(ctx, "/client_config/get", map[string]any{}, "", "3", device)
	if err != nil {
		return Challenge{}, err
	}
	var result struct {
		Config *struct {
			Captcha *struct {
				Enabled  bool   `json:"enabled"`
				Provider string `json:"provider"`
				Region   string `json:"region"`
				Prefix   string `json:"prefix"`
				Scenes   []struct {
					UsedFor int    `json:"usedFor"`
					Enabled bool   `json:"enabled"`
					SceneID string `json:"sceneId"`
				} `json:"scenes"`
			} `json:"captcha"`
		} `json:"config"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Config == nil || result.Config.Captcha == nil {
		return Challenge{}, &Failure{Code: "lajiaoliao_verification_unavailable"}
	}
	cfg := result.Config.Captcha
	if !cfg.Enabled {
		return Challenge{}, nil
	}
	for _, scene := range cfg.Scenes {
		if scene.UsedFor != purpose {
			continue
		}
		if !scene.Enabled {
			return Challenge{}, nil
		}
		if cfg.Provider != "aliyun" || cfg.Region == "" || cfg.Prefix == "" || scene.SceneID == "" {
			return Challenge{}, &Failure{Code: "lajiaoliao_verification_unavailable"}
		}
		return Challenge{true, cfg.Region, cfg.Prefix, scene.SceneID}, nil
	}
	return Challenge{}, &Failure{Code: "lajiaoliao_verification_unavailable"}
}

func (c *Client) Login(ctx context.Context, phone, password, proof, device, expected string) (Session, error) {
	phone = strings.TrimPrefix(strings.TrimSpace(phone), "+86")
	if len(phone) != 11 || phone[0] != '1' || len(password) < 1 || len(password) > 256 ||
		len(proof) > 32768 || len(device) < 20 || len(device) > 100 {
		return Session{}, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	for _, r := range phone {
		if r < '0' || r > '9' {
			return Session{}, &Failure{Code: "lajiaoliao_invalid_input"}
		}
	}
	challenge, err := c.Challenge(ctx, device)
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) {
			failure.Submitted = false
		}
		return Session{}, err
	}
	if challenge.Required && strings.TrimSpace(proof) == "" {
		return Session{}, &Failure{Code: "lajiaoliao_captcha_required"}
	}
	hash := md5.Sum([]byte(password))
	raw, err := c.request(ctx, "/pc/account/login", map[string]any{
		"areaCode": "+86", "phoneNumber": phone, "passwordMD5": hex.EncodeToString(hash[:]),
		"verifyCode": "", "confirmSameClassDeviceSwitch": false, "platform": 3,
		"deviceID": device, "deviceName": "Windows", "captchaVerifyParam": proof,
	}, "", "3", device)
	if err != nil {
		return Session{}, err
	}
	defer clear(raw)
	var result struct {
		User    string `json:"userID"`
		Chat    string `json:"chatToken"`
		IM      string `json:"imToken"`
		Confirm bool   `json:"needConfirmSameClassDeviceSwitch"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return Session{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	if result.Confirm {
		return Session{}, &Failure{Code: "lajiaoliao_device_confirmation_required", Submitted: true}
	}
	if !validID(result.User) || result.Chat == "" || result.IM == "" ||
		len(result.Chat) > 8192 || len(result.IM) > 8192 {
		return Session{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	if expected != "" && expected != result.User {
		return Session{}, &Failure{Code: "lajiaoliao_identity_mismatch", Submitted: true}
	}
	session, err := c.Renew(ctx, Session{result.User, result.Chat, result.IM, device})
	if err != nil {
		// Native login already succeeded. A later ticket failure is not a
		// rejection of that login and cannot make the password call retryable.
		return Session{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	return session, nil
}

func (c *Client) Renew(ctx context.Context, session Session) (Session, error) {
	raw, err := c.request(ctx, "/pc/account/get_im_token", map[string]any{"platform": 5}, session.ChatToken, "5", session.DeviceID)
	if err != nil {
		return Session{}, err
	}
	defer clear(raw)
	var result struct {
		IM string `json:"imToken"`
	}
	if json.Unmarshal(raw, &result) != nil || result.IM == "" || len(result.IM) > 8192 {
		return Session{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	session.IMToken = result.IM
	return session, nil
}
