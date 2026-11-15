// SPDX-License-Identifier: AGPL-3.0-or-later
package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

func validPhone(phone string) bool {
	if len(phone) != 11 || phone[0] != '1' || phone[1] < '3' || phone[1] > '9' {
		return false
	}
	for _, ch := range phone {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// The official desktop uses purpose 3 for SMS and login_or_signup. It does
// not submit a guessed password/user schema to the unused register endpoint.
func (c *Client) SignupSMS(ctx context.Context, phone, proof, device string) error {
	if !validPhone(phone) || len(proof) > 32768 || len(device) < 20 || len(device) > 100 {
		return &Failure{Code: "lajiaoliao_invalid_input"}
	}
	challenge, err := c.Challenge(ctx, device)
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) {
			failure.Submitted = false
		}
		return err
	}
	if challenge.Required && strings.TrimSpace(proof) == "" {
		return &Failure{Code: "lajiaoliao_captcha_required"}
	}
	_, err = c.request(ctx, "/account/code/send", map[string]any{
		"areaCode": "+86", "phoneNumber": phone, "usedFor": 3, "captchaVerifyParam": proof,
	}, "", "3", device)
	return err
}

func (c *Client) Signup(ctx context.Context, phone, code, device string) (Session, error) {
	if !validPhone(phone) || len(code) != 6 || len(device) < 20 || len(device) > 100 {
		return Session{}, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return Session{}, &Failure{Code: "lajiaoliao_invalid_input"}
		}
	}
	raw, err := c.request(ctx, "/pc/account/login_or_signup", map[string]any{
		"areaCode": "+86", "phoneNumber": phone, "verifyCode": code,
		"platform": 3, "deviceID": device, "deviceName": "Windows",
	}, "", "3", device)
	if err != nil {
		return Session{}, err
	}
	defer clear(raw)
	var result struct {
		UserID    string `json:"userID"`
		ChatToken string `json:"chatToken"`
		IMToken   string `json:"imToken"`
	}
	if json.Unmarshal(raw, &result) != nil || !validID(result.UserID) ||
		result.ChatToken == "" || len(result.ChatToken) > 8192 || result.IMToken == "" || len(result.IMToken) > 8192 {
		return Session{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	session, err := c.Renew(ctx, Session{UserID: result.UserID, ChatToken: result.ChatToken,
		IMToken: result.IMToken, DeviceID: device})
	if err != nil {
		return Session{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	return session, nil
}
