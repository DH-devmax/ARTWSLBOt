package nativeauth

import (
	"context"
	"encoding/json"
	"strings"

	"wangshangliao/internal/nativewire"
)

// SecurityInput lives only for the HTTP request. Server-side flow state supplies
// keys and certificates; the browser cannot provide them or an upstream route.
type SecurityInput struct {
	Flow        string `json:"flow,omitempty"`
	Captcha     string `json:"captcha,omitempty"`
	Code        string `json:"code,omitempty"`
	Phone       string `json:"phone,omitempty"`
	OldPassword string `json:"old_password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
	Image       []byte `json:"image,omitempty"`
}

type SecurityState struct {
	Key            string `json:"key,omitempty"`
	Cert           string `json:"cert,omitempty"`
	Phone          string `json:"phone,omitempty"`
	AreaCode       string `json:"area_code,omitempty"`
	Asset          string `json:"asset,omitempty"`
	ExpiresSeconds int    `json:"-"`
}

func SecurityKind(kind string) bool {
	switch kind {
	case "avatar", "password_sms", "password_verify", "password", "phone_old_sms", "phone_old_verify", "phone_new_sms", "phone":
		return true
	}
	return false
}

func (in SecurityInput) Validate(kind string) error {
	canonical := SecurityInput{}
	switch kind {
	case "avatar":
		if len(in.Image) == 0 || len(in.Image) > 1024*1024 {
			return rejection("native_avatar_invalid")
		}
		canonical.Image = in.Image
	case "password_sms", "phone_old_sms":
		if len(in.Captcha) < 1 || len(in.Captcha) > 8192 {
			return rejection("native_captcha_required")
		}
		canonical.Captcha = in.Captcha
	case "password_verify", "phone_old_verify", "phone":
		if !sixDigits(in.Code) {
			return rejection("native_security_code_invalid")
		}
		canonical.Code, canonical.Flow = in.Code, in.Flow
	case "phone_new_sms":
		if !phoneDigits(in.Phone) || in.Phone[0] != '1' || len(in.Captcha) < 1 || len(in.Captcha) > 8192 {
			return rejection("native_security_phone_invalid")
		}
		canonical.Phone, canonical.Captcha, canonical.Flow = in.Phone, in.Captcha, in.Flow
	case "password":
		if len(in.OldPassword) < 1 || len(in.OldPassword) > 64 || !signupPassword(in.NewPassword) || in.OldPassword == in.NewPassword {
			return rejection("native_security_password_invalid")
		}
		canonical.OldPassword, canonical.NewPassword, canonical.Flow = in.OldPassword, in.NewPassword, in.Flow
	default:
		return rejection("native_setting_unsupported")
	}
	// Compare separately because image buffers are not comparable.
	if in.Flow != canonical.Flow || in.Captcha != canonical.Captcha || in.Code != canonical.Code ||
		in.Phone != canonical.Phone || in.OldPassword != canonical.OldPassword || in.NewPassword != canonical.NewPassword ||
		len(in.Image) != len(canonical.Image) || kind != "avatar" && in.Flow != "" && len(in.Flow) != 36 {
		return rejection("native_setting_fields_invalid")
	}
	if kind != "avatar" && kind != "password_sms" && kind != "phone_old_sms" && canonical.Flow == "" {
		return rejection("native_security_flow_required")
	}
	return nil
}

func (c *Client) ChangeSecurity(ctx context.Context, kind string, in SecurityInput, state *SecurityState, fence nativewire.SubmissionFence) error {
	if err := in.Validate(kind); err != nil {
		return err
	}
	if state == nil || fence == nil {
		return rejection("native_setting_fence_required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx = context.WithValue(ctx, settingFenceKey{}, fence)
	if kind == "avatar" {
		return c.changeAvatar(ctx, in.Image, state, fence)
	}
	route := "/v1/verify/verify"
	params := map[string]any{}
	switch kind {
	case "password_sms", "phone_old_sms":
		route = "/v1/verify/sms"
		ty := "VERIFY_FOR_RESET_PASSWORD"
		if kind == "phone_old_sms" {
			ty = "VERIFY_FOR_CHANGE_PHONE"
		}
		params = map[string]any{"ty": ty, "validateStr": in.Captcha}
	case "password_verify", "phone_old_verify":
		if state.Key == "" {
			return rejection("native_security_flow_required")
		}
		ty := "VERIFY_FOR_RESET_PASSWORD"
		if kind == "phone_old_verify" {
			ty = "VERIFY_FOR_CHANGE_PHONE"
		}
		params = map[string]any{"ty": ty, "key": state.Key, "answer": in.Code}
	case "phone_new_sms":
		if state.Cert == "" {
			return rejection("native_security_flow_required")
		}
		route = "/v1/verify/sms-anon"
		params = map[string]any{"ty": "VERIFY_FOR_RESET_PHONE", "phone": securityPhone(in.Phone), "validateStr": in.Captcha}
	case "password":
		route = "/v1/settings/password"
		params = map[string]any{"verify": map[string]any{"pwd": map[string]any{"passwd": in.OldPassword}}, "newPassword": in.NewPassword}
	case "phone":
		if state.Cert == "" || state.Key == "" || !phoneDigits(state.Phone) {
			return rejection("native_security_flow_required")
		}
		route = "/v1/settings/changePhone"
		params = map[string]any{"phone": securityPhone(state.Phone), "newDeviceKey": state.Key, "newDeviceCode": in.Code, "cert": state.Cert}
	}
	data, err := c.request(ctx, route, params, nil)
	if err != nil {
		return err
	}
	if strings.HasSuffix(kind, "_sms") || kind == "phone_old_verify" || kind == "password_verify" {
		var key string
		if json.Unmarshal(data["key"], &key) != nil || key == "" || len(key) > 4096 {
			return &Error{Code: "native_response_invalid", Submitted: true}
		}
		if kind == "phone_old_verify" {
			state.Cert = key
		} else {
			state.Key = key
		}
		if kind == "phone_new_sms" {
			state.Phone = in.Phone
		}
		if strings.HasSuffix(kind, "_sms") {
			_ = json.Unmarshal(data["expiredSec"], &state.ExpiresSeconds)
		}
	}
	return nil
}

func securityPhone(phone string) map[string]string {
	return map[string]string{"countryCode": "86", "nationalNumber": phone, "maskedNationalNumber": phone[:3] + "****" + phone[7:]}
}
