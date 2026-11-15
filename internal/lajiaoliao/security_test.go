package lajiaoliao

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSecurityIndependentDesktopContractsAndNoReplay(t *testing.T) {
	s := Session{UserID: "self-native", ChatToken: "chat-fixture", IMToken: "im-fixture", DeviceID: "device-fixture"}
	flow := "73000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		kind, route string
		input       SecurityChange
		token       string
	}{
		{"password_sms", "/account/code/send", SecurityChange{}, ""},
		{"phone_old_sms", "/account/code/send", SecurityChange{}, ""},
		{"phone_old_verify", "/account/code/verify", SecurityChange{Flow: flow, Code: "123456"}, ""},
		{"password", "/account/password/reset", SecurityChange{Flow: flow, Code: "123456", Password: "synthetic-password"}, ""},
		{"phone_new_sms", "/account/contact/check", SecurityChange{Flow: flow, Phone: "13900000000"}, s.ChatToken},
		{"phone", "/account/contact/change", SecurityChange{Flow: flow, Code: "123456"}, s.ChatToken},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			writes, fences := 0, 0
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				switch r.URL.Path {
				case "/user/find/full":
					return map[string]any{"users": []any{map[string]any{"userID": s.UserID, "phoneNumber": "13800000000", "areaCode": "+86"}}}, nil
				case "/client_config/get":
					return map[string]any{"config": map[string]any{"captcha": map[string]any{"enabled": false}}}, nil
				}
				if r.URL.Path != tc.route || r.Header.Get("token") != tc.token || r.Header.Get("platform") != "3" {
					t.Fatal("wrong security route or token", r.URL.Path)
				}
				writes++
				if tc.kind == "password" {
					sum := md5.Sum([]byte(tc.input.Password))
					if len(body) != 4 || body["passwordMd5"] != hex.EncodeToString(sum[:]) || body["verifyCode"] != tc.input.Code {
						t.Fatal("reset wire not verified desktop shape")
					}
				}
				if tc.kind == "password_sms" && body["usedFor"] != float64(2) || tc.kind == "phone_old_sms" && body["usedFor"] != float64(6) {
					t.Fatal("SMS purpose mixed")
				}
				if tc.kind == "phone_old_verify" && body["usedFor"] != float64(6) {
					t.Fatal("old-phone verification purpose mixed")
				}
				if tc.kind == "phone_new_sms" && (body["contactType"] != "phone" || body["newPhoneNumber"] != tc.input.Phone) {
					t.Fatal("new-phone target mixed")
				}
				return nil, errors.New("synthetic-timeout")
			})
			state := SecurityState{Phone: "13800000000", Area: "+86"}
			err := client.ChangeSecurity(context.Background(), s, tc.kind, tc.input, &state, func(ctx context.Context, send func(context.Context) error) error { fences++; return send(ctx) })
			var failure *Failure
			if !errors.As(err, &failure) || failure.Code != "lajiaoliao_result_unknown" || writes != 1 || fences != 1 || state.Phone != "13800000000" {
				t.Fatal("unknown result replay or state advanced", err, writes, fences)
			}
		})
	}
}

func TestSecurityInputAndPurposeValidation(t *testing.T) {
	flow := "73000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		kind  string
		input SecurityChange
	}{
		{"password_verify", SecurityChange{Flow: flow, Code: "123456"}},
		{"password", SecurityChange{Flow: flow, Code: "123456", Password: " padded "}},
		{"password", SecurityChange{Flow: flow, Code: "no", Password: "synthetic"}},
		{"phone_new_sms", SecurityChange{Flow: flow, Phone: "13800000000", Password: "extra"}},
		{"password_sms", SecurityChange{Phone: "13800000000"}},
		{"phone", SecurityChange{Flow: flow, Code: "123456", Captcha: "extra"}},
	} {
		if tc.input.Validate(tc.kind) == nil {
			t.Fatal("unrecognized security input accepted", tc.kind)
		}
	}
	for _, purpose := range []int{2, 6, 7} {
		client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
			return map[string]any{"config": map[string]any{"captcha": map[string]any{"enabled": true, "provider": "aliyun", "region": "cn", "prefix": "fixture", "scenes": []any{
				map[string]any{"usedFor": 3, "enabled": true, "sceneId": "login-scene"},
				map[string]any{"usedFor": purpose, "enabled": true, "sceneId": "correct-scene"},
			}}}}, nil
		})
		challenge, err := client.SecurityChallenge(context.Background(), "device-fixture", purpose)
		if err != nil || challenge.SceneID != "correct-scene" {
			t.Fatal("wrong security challenge", purpose, err)
		}
	}
	client := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(`{"errCode":20006,"errMsg":"private-phone-code-password","data":null}`), nil
	})}}
	_, err := client.request(context.Background(), "/account/password/reset", struct{}{}, "", "3", "device-fixture")
	if err == nil || err.Error() != "lajiaoliao_code_invalid" || strings.Contains(err.Error(), "private") {
		t.Fatal("raw error disclosed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = client.ChangeSecurity(ctx, Session{}, "password", SecurityChange{Flow: flow, Code: "123456", Password: "synthetic"}, &SecurityState{Phone: "13800000000", Area: "+86"}, func(ctx context.Context, send func(context.Context) error) error { return ctx.Err() }); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
