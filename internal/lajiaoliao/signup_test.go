package lajiaoliao

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestDesktopSignupSMSAndLoginOrSignupContract(t *testing.T) {
	device := "device_windows_artwslbot_synthetic"
	paths := []string{}
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.Header.Get("deviceID") != device {
			t.Fatal("unstable device")
		}
		switch r.URL.Path {
		case "/client_config/get":
			return response(`{"errCode":0,"data":{"config":{"captcha":{"enabled":true,"provider":"aliyun","region":"cn","prefix":"fixture","scenes":[{"usedFor":3,"enabled":true,"sceneId":"sms-login"}]}}}}`), nil
		case "/account/code/send":
			if body["usedFor"] != float64(3) || body["captchaVerifyParam"] != "human-proof" || body["phoneNumber"] != "13800000000" {
				t.Fatal("SMS purpose or proof changed")
			}
			return response(`{"errCode":0}`), nil
		case "/pc/account/login_or_signup":
			if body["verifyCode"] != "123456" || body["platform"] != float64(3) ||
				body["password"] != nil || body["passwordMD5"] != nil || body["confirmSameClassDeviceSwitch"] != nil {
				t.Fatal("invented register fields or takeover")
			}
			return response(`{"errCode":0,"data":{"userID":"signup-user","chatToken":"fixture","imToken":"fixture"}}`), nil
		case "/pc/account/get_im_token":
			return response(`{"errCode":0,"data":{"imToken":"renewed"}}`), nil
		default:
			t.Fatal("unreviewed route", r.URL.Path)
			return nil, io.EOF
		}
	})}}
	if err := c.SignupSMS(context.Background(), "13800000000", "human-proof", device); err != nil || len(paths) != 2 {
		t.Fatal("SMS sent more than once or logged in", err, paths)
	}
	session, err := c.Signup(context.Background(), "13800000000", "123456", device)
	if err != nil || session.UserID != "signup-user" || session.IMToken != "renewed" || len(paths) != 4 {
		t.Fatal("registration/login contract", err, paths)
	}
}

func TestSignupNeverRetriesUnknownAndRejectsMalformedInput(t *testing.T) {
	calls := 0
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, io.ErrUnexpectedEOF
	})}}
	device := "device_windows_artwslbot_synthetic"
	if _, err := c.Signup(context.Background(), "13800000000", "123456", device); code(err) != "lajiaoliao_result_unknown" || calls != 1 {
		t.Fatal("unknown call retried", err, calls)
	}
	for _, otp := range []string{"", "12345", "abcdef", "1234567"} {
		if _, err := c.Signup(context.Background(), "13800000000", otp, device); code(err) != "lajiaoliao_invalid_input" || calls != 1 {
			t.Fatal("invalid OTP sent", err)
		}
	}
	if err := c.SignupSMS(context.Background(), "12800000000", "", device); code(err) != "lajiaoliao_invalid_input" || calls != 1 {
		t.Fatal("invalid phone sent", err)
	}
}
