package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func code(err error) string {
	var e *Failure
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestDesktopLoginNativeContract(t *testing.T) {
	var paths []string
	device := "device_windows_artwslbot_synthetic"
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Scheme != "https" || r.Header.Get("deviceID") != device || r.Header.Get("version") != "1.0.3" {
			t.Fatal("wrong native headers")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/client_config/get":
			return response(`{"errCode":0,"data":{"config":{"captcha":{"enabled":false}}}}`), nil
		case "/pc/account/login":
			if r.Header.Get("platform") != "3" || body["passwordMD5"] != "5f4dcc3b5aa765d61d8327deb882cf99" ||
				body["confirmSameClassDeviceSwitch"] != false || body["platform"] != float64(3) {
				t.Fatal("desktop wire changed")
			}
			return response(`{"errCode":0,"data":{"userID":"synthetic-uid","chatToken":"synthetic-chat","imToken":"synthetic-old"}}`), nil
		case "/pc/account/get_im_token":
			if r.Header.Get("platform") != "5" || r.Header.Get("token") != "synthetic-chat" {
				t.Fatal("IM renewal context changed")
			}
			return response(`{"errCode":0,"data":{"imToken":"synthetic-im"}}`), nil
		default:
			t.Fatal("unexpected route")
			return nil, nil
		}
	})}}
	s, err := c.Login(context.Background(), "13800000000", "password", "", device, "")
	if err != nil || s.UserID != "synthetic-uid" || s.IMToken != "synthetic-im" || len(paths) != 3 {
		t.Fatal("login chain failed", err)
	}
}

func TestMissingCaptchaNeverSubmitsPassword(t *testing.T) {
	calls := 0
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/client_config/get" {
			t.Fatal("password submitted without verification")
		}
		return response(`{"errCode":0,"data":{"config":{"captcha":{"enabled":true,"provider":"aliyun","region":"cn","prefix":"fixture","scenes":[{"usedFor":3,"enabled":true,"sceneId":"scene"}]}}}}`), nil
	})}}
	if _, err := c.Login(context.Background(), "13800000000", "synthetic", "", strings.Repeat("x", 32), ""); code(err) != "lajiaoliao_captcha_required" || calls != 1 {
		t.Fatal(err)
	}
}

func TestDeviceConfirmationDoesNotTakeOver(t *testing.T) {
	calls := 0
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(`{"errCode":0,"data":{"config":{"captcha":{"enabled":false}}}}`), nil
		}
		return response(`{"errCode":0,"data":{"needConfirmSameClassDeviceSwitch":true}}`), nil
	})}}
	_, err := c.Login(context.Background(), "13800000000", "synthetic", "", strings.Repeat("x", 32), "")
	if code(err) != "lajiaoliao_device_confirmation_required" || calls != 2 {
		t.Fatal(err)
	}
}

func TestTicketFailureAfterLoginAlwaysRequiresReview(t *testing.T) {
	for _, mode := range []string{"rejected", "limited", "disconnected"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				switch r.URL.Path {
				case "/client_config/get":
					return response(`{"errCode":0,"data":{"config":{"captcha":{"enabled":false}}}}`), nil
				case "/pc/account/login":
					return response(`{"errCode":0,"data":{"userID":"synthetic","chatToken":"synthetic","imToken":"synthetic"}}`), nil
				case "/pc/account/get_im_token":
					if mode == "disconnected" {
						return nil, io.ErrUnexpectedEOF
					}
					resp := response(`{"errCode":500}`)
					if mode == "limited" {
						resp.StatusCode = 429
					}
					return resp, nil
				default:
					t.Fatal("unexpected native retry")
					return nil, io.ErrUnexpectedEOF
				}
			})}}
			_, err := c.Login(context.Background(), "13800000000", "synthetic", "", strings.Repeat("x", 32), "")
			var failure *Failure
			if !errors.As(err, &failure) || failure.Code != "lajiaoliao_result_unknown" || !failure.Submitted || calls != 3 {
				t.Fatal("partial success became retryable or was repeated", calls, err)
			}
		})
	}
}

func TestFixedRoutesAndRetryAfter(t *testing.T) {
	calls := 0
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		resp := response(`{}`)
		resp.StatusCode = 429
		resp.Header.Set("Retry-After", "35")
		return resp, nil
	})}}
	if _, err := c.request(context.Background(), "/arbitrary", nil, "", "", ""); code(err) != "lajiaoliao_route_forbidden" || calls != 0 {
		t.Fatal("arbitrary route")
	}
	_, err := c.Challenge(context.Background(), "synthetic")
	var failure *Failure
	if !errors.As(err, &failure) || failure.RetryAfter != 35*time.Second {
		t.Fatal("cooldown lost", err)
	}
}

func TestPublicNativeOriginsRejectPrivateAddresses(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "192.168.0.1", "169.254.169.254", "::1", "::ffff:127.0.0.1", "fc00::1", "198.18.0.1"} {
		if public(netip.MustParseAddr(value)) {
			t.Fatal("private origin accepted", value)
		}
	}
}

func TestMuteRechecksRolesAndUsesFence(t *testing.T) {
	step := 0
	fenced := false
	c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		step++
		if step <= 2 {
			role, user := 60, "self"
			if step == 2 {
				role, user = 20, "member"
			}
			raw, _ := json.Marshal(map[string]any{"errCode": 0, "data": map[string]any{"members": []map[string]any{{"groupID": "group", "userID": user, "roleLevel": role}}}})
			return response(string(raw)), nil
		}
		if !fenced || r.URL.Path != "/group/mute_group_member" {
			t.Fatal("mutation bypassed final fence")
		}
		return response(`{"errCode":0}`), nil
	})}}
	s := Session{UserID: "self", IMToken: "synthetic"}
	err := c.Mute(context.Background(), s, "group", "member", 60, func(ctx context.Context, write func(context.Context) error) error { fenced = true; return write(ctx) })
	if err != nil || step != 3 {
		t.Fatal(err)
	}
	step = 0
	fenced = false
	err = c.Mute(context.Background(), s, "group", "self", 60, func(context.Context, func(context.Context) error) error { t.Fatal("self target"); return nil })
	if err == nil || step != 0 {
		t.Fatal("protected member reached network")
	}
}
