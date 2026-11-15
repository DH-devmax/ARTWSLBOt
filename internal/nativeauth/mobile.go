package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Mobile companions keep business credentials separate from desktop/IM sessions.
// When supplied, counts owns the per-user counter; an allocation error never
// falls back to the saved session's local count.
func ResumeMobile(d Deployment, saved Session, ids RequestIDSource, counts ...RequestCountSource) (*Client, error) {
	if err := validateDeploymentMode(d, true); err != nil {
		return nil, err
	}
	if len(counts) > 1 {
		return nil, rejection("native_request_count_invalid")
	}
	if saved.Origin != d.Origin || saved.ProtocolVersion != MobileProtocolVersion ||
		saved.DesktopPlatform != "" || saved.UID == 0 || saved.UID >= 1<<31 ||
		!validToken(saved.JWT) || len(saved.MAC) != 16 ||
		!ValidOnboardingOutboundAccount(saved.AccountID) || saved.Kind >= 1<<32 ||
		saved.NIMID != "" || saved.NIMToken != "" || saved.NIMAuthType != 0 {
		return nil, rejection("native_reauth_required")
	}
	if ids == nil {
		return nil, rejection("native_request_id_unavailable")
	}
	base, err := NewMobileClient(d)
	if err != nil {
		return nil, err
	}
	c := base.(*Client)
	if c.fields[0] != saved.Device || !bytes.Equal(saved.MAC, c.public[16:32]) {
		c.Close()
		return nil, rejection("native_reauth_required")
	}
	c.fields[3], c.fields[4] = saved.UID, saved.Kind
	c.jwt, c.mac, c.requestIDs = saved.JWT, append([]byte(nil), saved.MAC...), ids
	if len(counts) == 1 {
		c.requestCounts = counts[0]
	}
	c.identity = map[string]json.RawMessage{
		"uid":       json.RawMessage(strconv.FormatUint(saved.UID, 10)),
		"accountId": json.RawMessage(saved.AccountID),
	}
	return c, nil
}

func (c *Client) finishMobile(ctx context.Context) (Session, string, error) {
	if ctx.Err() != nil {
		return Session{}, "", rejection("native_cancelled")
	}
	if c.safe {
		return Session{}, "", rejection("native_security_required")
	}
	var uid uint64
	account := directoryID(c.identity["accountId"])
	if c.loginUnknown || json.Unmarshal(c.identity["uid"], &uid) != nil ||
		uid != c.fields[3] || uid == 0 || uid >= 1<<31 || !validToken(c.jwt) ||
		!ValidOnboardingOutboundAccount(account) || c.fields[0] != c.deviceIdentity(c.public) ||
		len(c.mac) != 16 || !bytes.Equal(c.mac, c.public[16:32]) {
		return Session{}, "", rejection("native_login_identity")
	}
	var nickname string
	if raw := c.identity["nickName"]; len(raw) != 0 &&
		(json.Unmarshal(raw, &nickname) != nil || !mobileText(nickname, 1024, true)) {
		return Session{}, "", rejection("native_login_identity")
	}
	if runes := []rune(nickname); len(runes) > 128 {
		nickname = string(runes[:128])
	}
	return Session{Origin: c.config.Origin, UID: uid, Kind: c.fields[4],
		Device: c.fields[0], JWT: c.jwt, MAC: append([]byte(nil), c.mac...),
		ProtocolVersion: MobileProtocolVersion, AccountID: account}, nickname, nil
}

func mobileText(value string, limit int, empty bool) bool {
	return len(value) <= limit && utf8.ValidString(value) &&
		(empty || strings.TrimSpace(value) != "") &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func mobileString(params map[string]any, name string, limit int, empty bool) bool {
	value, ok := params[name].(string)
	return ok && mobileText(value, limit, empty)
}

func mobilePhone(value any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 512 {
		return nil, rejection("native_invalid_input")
	}
	defer clear(raw)
	var phone struct {
		CountryCode          json.Number `json:"countryCode"`
		NationalNumber       json.Number `json:"nationalNumber"`
		MaskedNationalNumber string      `json:"maskedNationalNumber"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&phone) != nil || phone.CountryCode != "86" ||
		!phoneDigits(string(phone.NationalNumber)) ||
		!mobileText(phone.MaskedNationalNumber, 32, true) {
		return nil, rejection("native_invalid_input")
	}
	number, _ := strconv.ParseUint(string(phone.NationalNumber), 10, 64)
	return map[string]any{"countryCode": 86, "nationalNumber": number,
		"maskedNationalNumber": phone.MaskedNationalNumber}, nil
}

func mobilePasswordParams(params map[string]any) bool {
	kind, _ := params["type"].(string)
	account, _ := params["account"].(string)
	return mobileString(params, "passwd", 1024, false) &&
		(kind == "LOGIN_TYPE_ACCOUNT_PWD" && ValidOnboardingOutboundAccount(account) ||
			kind == "LOGIN_TYPE_PHONE_PWD" && phoneDigits(account))
}

func mobileLoginParams(params map[string]any) (map[string]any, error) {
	if !mobileString(params, "validateStr", 8192, false) {
		return nil, rejection("native_captcha_required")
	}
	out := map[string]any{
		"key": "", "verificationCode": "", "account": "", "passwd": "",
		"phone":    map[string]any{"countryCode": 0, "nationalNumber": 0, "maskedNationalNumber": ""},
		"qrcodeId": 0, "qrcodeKey": 0, "groupAccount": 0, "user": "", "pType": "", "token": "",
		"version": MobileProtocolVersion, "appVersion": 337, "appHash": android374AppHash,
		"validateStr": params["validateStr"], "type": params["type"],
	}
	if params["type"] == "LOGIN_TYPE_SMS" {
		code, _ := params["verificationCode"].(string)
		if len(params) != 5 || !sixDigits(code) || !mobileString(params, "key", 8192, false) {
			return nil, rejection("native_invalid_input")
		}
		phone, err := mobilePhone(params["phone"])
		if err != nil {
			return nil, err
		}
		out["phone"], out["key"], out["verificationCode"] = phone, params["key"], code
	} else {
		if len(params) != 4 || !mobilePasswordParams(params) {
			return nil, rejection("native_invalid_input")
		}
		out["account"], out["passwd"] = params["account"], params["passwd"]
	}
	return out, nil
}

func mobileLoginFailure(diagnostic string) *Error {
	return &Error{Code: "native_mobile_login_response_invalid", Diagnostic: diagnostic, Submitted: true}
}

func (c *Client) acceptMobileLogin(result map[string]json.RawMessage, params map[string]any, uid uint64, jwt string) error {
	if diagnostic := mobileLoginAccountDiagnostic(result, params); diagnostic != "" {
		return mobileLoginFailure(diagnostic)
	}
	var required bool
	for _, name := range []string{"safeCodeRequired", "safeCodeSet"} {
		var value bool
		if raw, exists := result[name]; exists {
			if (string(raw) != "true" && string(raw) != "false") || json.Unmarshal(raw, &value) != nil {
				return mobileLoginFailure("login_security_flags_invalid")
			}
		}
		if name == "safeCodeRequired" {
			required = value
		}
	}
	// Android 3.7.4 does not authenticate through LoginResponse.token.
	// Its SDK derives the request MAC from signPk[16:32], not that desktop
	// ticket. Control still binds the returned UID to the existing account.
	c.fields[3], c.fields[4] = uid, 0
	c.jwt, c.mac, c.safe, c.identity = jwt, bytes.Clone(c.public[16:32]), required, result
	for key, raw := range result {
		if key != "uid" && key != "accountId" && key != "nickName" {
			clear(raw)
			delete(result, key)
		}
	}
	return nil
}

func mobileLoginAccountDiagnostic(data map[string]json.RawMessage, params map[string]any) string {
	account := directoryID(data["accountId"])
	if !ValidOnboardingOutboundAccount(account) {
		return "login_identity_invalid"
	}
	if params["type"] == "LOGIN_TYPE_ACCOUNT_PWD" {
		if account != params["account"] {
			return "login_account_mismatch"
		}
		return ""
	}
	// The response can omit or mask a phone. It is profile data, not the
	// identity proof. A full returned number must still match the request;
	// persistence independently checks the authenticated UID in every case.
	expected, _ := params["account"].(string)
	if params["type"] == "LOGIN_TYPE_SMS" {
		phone, err := mobilePhone(params["phone"])
		if err != nil {
			return "login_phone_invalid"
		}
		expected = strconv.FormatUint(phone["nationalNumber"].(uint64), 10)
	}
	raw := bytes.TrimSpace(data["phone"])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var phone struct {
		CountryCode          json.Number `json:"countryCode"`
		NationalNumber       json.Number `json:"nationalNumber"`
		MaskedNationalNumber string      `json:"maskedNationalNumber"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 512 || decoder.Decode(&phone) != nil ||
		!mobileText(phone.MaskedNationalNumber, 32, true) ||
		phone.CountryCode != "" && phone.CountryCode != "0" && phone.CountryCode != "86" {
		return "login_phone_invalid"
	}
	if phone.NationalNumber == "" || phone.NationalNumber == "0" {
		return ""
	}
	if !phoneDigits(string(phone.NationalNumber)) || phone.CountryCode != "86" {
		return "login_phone_invalid"
	}
	if string(phone.NationalNumber) != expected {
		return "login_phone_mismatch"
	}
	return ""
}

// Only the manual authentication flow and exact onboarding lookups/applications
// are available. Profile, group, registration and IM methods fail at dispatch.
func (c *Client) mobileRequestParams(route string, params map[string]any, exchange []byte) (map[string]any, error) {
	switch route {
	case "/v1/user/login":
		if len(exchange) == 0 || c.loginUnknown || c.jwt != "" {
			return nil, rejection("native_auth_state_conflict")
		}
		return params, nil
	case "/v1/verify/sms-anon":
		if c.loginUnknown || c.jwt != "" || params["ty"] != "VERIFY_FOR_LOGIN" ||
			!mobileString(params, "validateStr", 8192, false) || len(params) < 3 || len(params) > 4 {
			return nil, rejection("native_invalid_input")
		}
		for name := range params {
			if name != "ty" && name != "phone" && name != "validateStr" && name != "Key" {
				return nil, rejection("native_invalid_input")
			}
		}
		if key, ok := params["Key"]; ok {
			text, ok := key.(string)
			if !ok || !mobileText(text, 8192, true) {
				return nil, rejection("native_invalid_input")
			}
		}
		phone, err := mobilePhone(params["phone"])
		if err != nil {
			return nil, err
		}
		key, _ := params["Key"].(string)
		return map[string]any{"ty": "VERIFY_FOR_LOGIN", "phone": phone,
			"validateStr": params["validateStr"], "Key": key}, nil
	case "/v1/user/get-change-device-verify":
		if c.loginUnknown || c.jwt != "" || len(params) != 3 || !mobilePasswordParams(params) {
			return nil, rejection("native_invalid_input")
		}
		return params, nil
	case "/v1/user/verify_safe_code":
		code, _ := params["code"].(string)
		if c.loginUnknown || !c.safe || len(params) != 1 || !sixDigits(code) {
			return nil, rejection("native_security_input")
		}
		return params, nil
	case onboardingOutboundSearchRoute, onboardingOutboundInfoRoute, onboardingOutboundApplyRoute,
		"/v1/friend/friend-apply-list":
		if c.safe {
			return nil, rejection("native_security_required")
		}
		if c.loginUnknown || !validToken(c.jwt) || len(c.mac) != 16 ||
			!bytes.Equal(c.mac, c.public[16:32]) || c.fields[3] == 0 ||
			!ValidOnboardingOutboundAccount(directoryID(c.identity["accountId"])) {
			return nil, rejection("native_reauth_required")
		}
		switch route {
		case onboardingOutboundSearchRoute:
			account, _ := params["key"].(string)
			if len(params) != 3 || !ValidOnboardingOutboundAccount(account) ||
				params["type"] != "CONVERSATION_TYPE_SEARCH" || params["qrcodeId"] != 0 {
				return nil, rejection("native_invalid_input")
			}
		case "/v1/friend/friend-apply-list":
			if len(params) != 1 || params["lastId"] != "0" && params["lastId"] != 0 {
				return nil, rejection("native_invalid_input")
			}
			return map[string]any{"lastId": 0}, nil
		}
	default:
		return nil, rejection("native_unsupported_route")
	}
	return params, nil
}
