package lajiaoliao

import (
	"context"
	"crypto/md5" // Desktop password/reset wire encoding only, never persisted.
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

type SecurityChange struct {
	Flow, Captcha, Code, Phone, Password string
}

type SecurityState struct {
	Phone, Area string
}

func phoneNumber(value string) bool {
	return len(value) == 11 && value[0] == '1' && value[1] >= '3' && value[1] <= '9' &&
		strings.Trim(value, "0123456789") == ""
}

func (in SecurityChange) Validate(kind string) error {
	canonical := SecurityChange{}
	switch kind {
	case "password_sms", "phone_old_sms":
		canonical.Captcha = in.Captcha
	case "phone_new_sms":
		if !phoneNumber(in.Phone) {
			return &Failure{Code: "lajiaoliao_invalid_input"}
		}
		canonical.Flow, canonical.Phone, canonical.Captcha = in.Flow, in.Phone, in.Captcha
	case "phone_old_verify", "phone":
		canonical.Flow, canonical.Code = in.Flow, in.Code
	case "password":
		// The client-config may impose stricter rules; the server remains
		// authoritative. Never apply WSL's unrelated 6-16 character policy.
		if !utf8.ValidString(in.Password) || len(in.Password) < 1 || len(in.Password) > 256 ||
			in.Password != strings.TrimSpace(in.Password) {
			return &Failure{Code: "lajiaoliao_password_invalid"}
		}
		canonical.Flow, canonical.Code, canonical.Password = in.Flow, in.Code, in.Password
	default:
		return &Failure{Code: "lajiaoliao_action_unavailable"}
	}
	if in != canonical || len(in.Captcha) > 32768 || !utf8.ValidString(in.Captcha) ||
		in.Flow != "" && (len(in.Flow) != 36 || strings.Trim(in.Flow, "0123456789abcdef-") != "") ||
		kind != "password_sms" && kind != "phone_old_sms" && in.Flow == "" ||
		(kind == "password" || kind == "phone_old_verify" || kind == "phone") &&
			(len(in.Code) != 6 || strings.Trim(in.Code, "0123456789") != "") {
		return &Failure{Code: "lajiaoliao_invalid_input"}
	}
	return nil
}

func (c *Client) ChangeSecurity(ctx context.Context, session Session, kind string, in SecurityChange,
	state *SecurityState, fence func(context.Context, func(context.Context) error) error) error {
	if err := in.Validate(kind); err != nil {
		return err
	}
	if state == nil || fence == nil {
		return &Failure{Code: "lajiaoliao_fence_required"}
	}
	route, token := "", ""
	payload := map[string]any{}
	if kind == "password_sms" || kind == "phone_old_sms" {
		_, phone, area, err := c.selfDetails(ctx, session)
		if err != nil {
			return err
		}
		// Only the currently bound self number, obtained server-side, is used.
		if !phoneNumber(phone) || area != "+86" && area != "86" && area != "" {
			return &Failure{Code: "lajiaoliao_bound_phone_unavailable"}
		}
		state.Phone, state.Area = phone, "+86"
	}
	purpose := 2
	if kind == "phone_old_sms" || kind == "phone_old_verify" {
		purpose = 6
	}
	if strings.HasSuffix(kind, "_sms") {
		challengePurpose := purpose
		if kind == "phone_new_sms" {
			challengePurpose = 7
		}
		challenge, err := c.SecurityChallenge(ctx, session.DeviceID, challengePurpose)
		if err != nil {
			return err
		}
		if challenge.Required && in.Captcha == "" {
			return &Failure{Code: "lajiaoliao_captcha_required"}
		}
	}
	switch kind {
	case "password_sms", "phone_old_sms", "phone_old_verify", "password":
		if !phoneNumber(state.Phone) || state.Area != "+86" {
			return &Failure{Code: "lajiaoliao_bound_phone_unavailable"}
		}
		payload = map[string]any{"areaCode": state.Area, "phoneNumber": state.Phone}
		switch kind {
		case "password_sms", "phone_old_sms":
			route = "/account/code/send"
			payload["usedFor"], payload["captchaVerifyParam"] = purpose, in.Captcha
		case "phone_old_verify":
			route = "/account/code/verify"
			payload["usedFor"], payload["verifyCode"] = 6, in.Code
		case "password":
			route = "/account/password/reset"
			sum := md5.Sum([]byte(in.Password))
			payload["verifyCode"], payload["passwordMd5"] = in.Code, hex.EncodeToString(sum[:])
		}
	case "phone_new_sms":
		if state.Area != "+86" || !phoneNumber(state.Phone) || state.Phone == in.Phone {
			return &Failure{Code: "lajiaoliao_invalid_input"}
		}
		route, token = "/account/contact/check", session.ChatToken
		payload = map[string]any{"contactType": "phone", "newAreaCode": "+86", "newPhoneNumber": in.Phone}
		if in.Captcha != "" {
			payload["captchaVerifyParam"] = in.Captcha
		}
	case "phone":
		if !phoneNumber(state.Phone) || state.Area != "+86" {
			return &Failure{Code: "lajiaoliao_invalid_input"}
		}
		route, token = "/account/contact/change", session.ChatToken
		payload = map[string]any{"contactType": "phone", "newAreaCode": state.Area, "newPhoneNumber": state.Phone, "newCode": in.Code}
	}
	err := fence(ctx, func(work context.Context) error {
		raw, err := c.request(work, route, payload, token, "3", session.DeviceID)
		clear(raw)
		return err
	})
	if err == nil && kind == "phone_new_sms" {
		state.Phone = in.Phone
	}
	return err
}
