//go:build sodium && cgo

package nativeauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"wangshangliao/internal/nativewire"
)

func friendDeleteDesktopFixture(t *testing.T, reply func(*http.Request) (*http.Response, error)) (*Client, *atomic.Int32) {
	t.Helper()
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	c := base.(*Client)
	t.Cleanup(c.Close)
	c.jwt, c.fields[3] = "synthetic-delete-jwt", 7
	calls := new(atomic.Int32)
	c.http.Transport = settingsRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		_, _, params := desktopRequest(t, r, c.bodyKey)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/friend/del-friend" ||
			len(params) != 1 || string(params["uid"]) != "8" || r.Header.Get("x-id") != "7" {
			t.Error("delete departed from the fixed desktop uid-only contract", r.URL.Path, params)
		}
		return reply(r)
	})
	return c, calls
}

func friendDeleteDesktopResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body))}
}

func TestDesktopFriendDeleteFixedContractAndSingleSubmission(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "repeat", true: "concurrent"}[concurrent], func(t *testing.T) {
			c, calls := friendDeleteDesktopFixture(t, func(*http.Request) (*http.Response, error) {
				return friendDeleteDesktopResponse(`{"code":0,"data":{}}`), nil
			})
			change := SettingChange{Kind: "friend_delete", Group: "123", Target: "8"}
			fences := 0
			err := c.ChangeSocial(context.Background(), change, nil,
				func(ctx context.Context, submit func(context.Context) error) error {
					fences++
					results := make(chan error, 8)
					var wg sync.WaitGroup
					for i := 0; i < cap(results); i++ {
						wg.Add(1)
						run := func() { defer wg.Done(); results <- submit(ctx) }
						if concurrent {
							go run()
						} else {
							run()
						}
					}
					wg.Wait()
					close(results)
					for err := range results {
						if err != nil {
							return err
						}
					}
					return nil
				})
			if err != nil || calls.Load() != 1 || fences != 1 {
				t.Fatal("delete submitted more than once", err, calls.Load(), fences)
			}
		})
	}
}

func TestDesktopFriendDeleteRequiresScopeIdentityAndFence(t *testing.T) {
	c, calls := friendDeleteDesktopFixture(t, func(*http.Request) (*http.Response, error) {
		return friendDeleteDesktopResponse(`{"code":0,"data":{}}`), nil
	})
	change := SettingChange{Kind: "friend_delete", Group: "123", Target: "8"}
	fence := nativewire.SubmissionFence(func(ctx context.Context, submit func(context.Context) error) error { return submit(ctx) })
	if _, err := c.Request(context.Background(), "/v1/friend/del-friend", map[string]any{"uid": 8}); err == nil {
		t.Fatal("raw request bypassed the submission fence")
	}
	if err := c.ChangeSetting(context.Background(), change, fence); err == nil {
		t.Fatal("ordinary setting entry bypassed social flow")
	}
	if err := c.ChangeSocial(context.Background(), change, nil, nil); err == nil {
		t.Fatal("missing deletion fence accepted")
	}
	if err := c.ChangeSocial(context.Background(), change, &FriendApplication{}, fence); err == nil {
		t.Fatal("application supplied to friend deletion")
	}
	for _, bad := range []SettingChange{
		{Kind: "friend_delete", Target: "8"},
		{Kind: "friend_delete", Group: "123"},
		{Kind: "friend_delete", Group: "123", Target: "8", Text: "extra"},
		{Kind: "friend_delete", Group: "123", Target: "7"},
	} {
		if err := c.ChangeSocial(context.Background(), bad, nil, fence); err == nil {
			t.Fatal("invalid or self deletion accepted", bad)
		}
	}
	c.fields[3] = 0
	if err := c.ChangeSocial(context.Background(), change, nil, fence); err == nil {
		t.Fatal("unverified session identity accepted")
	}
	c.fields[3], c.mobile = 7, true
	if err := c.ChangeSocial(context.Background(), change, nil, fence); err == nil {
		t.Fatal("delete widened the mobile companion whitelist")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid deletion reached transport", calls.Load())
	}
}

func TestDesktopFriendDeleteCancellationAndRevocation(t *testing.T) {
	for _, mode := range []string{"cancelled", "deadline", "fence_cancelled", "denied", "no_submission"} {
		t.Run(mode, func(t *testing.T) {
			c, calls := friendDeleteDesktopFixture(t, func(*http.Request) (*http.Response, error) {
				return friendDeleteDesktopResponse(`{"code":0,"data":{}}`), nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer deadlineCancel()
			}
			denied := errors.New("synthetic revoked")
			err := c.ChangeSocial(ctx, SettingChange{Kind: "friend_delete", Group: "123", Target: "8"}, nil,
				func(work context.Context, submit func(context.Context) error) error {
					switch mode {
					case "denied":
						return denied
					case "no_submission":
						return nil
					case "fence_cancelled":
						cancel()
					}
					return submit(work)
				})
			var failure *Error
			if err == nil || calls.Load() != 0 || errors.As(err, &failure) && failure.Submitted {
				t.Fatal("pre-submit cancellation/revocation reached native transport", err, calls.Load())
			}
			if mode == "denied" && !errors.Is(err, denied) {
				t.Fatal("lost final fence rejection", err)
			}
		})
	}
}

func TestDesktopFriendDeleteUnknownResponseNeverRetries(t *testing.T) {
	for _, mode := range []string{"empty", "omitted", "null", "transport", "read", "malformed", "missing_code",
		"array", "unexpected_fields", "server", "redirect", "rejected", "post_submit_cancel", "post_submit_fence_error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, calls := friendDeleteDesktopFixture(t, func(*http.Request) (*http.Response, error) {
				body := `{"code":0,"data":{}}`
				switch mode {
				case "transport":
					return nil, io.ErrUnexpectedEOF
				case "post_submit_cancel":
					cancel()
					return nil, context.Canceled
				case "omitted":
					body = `{"code":0}`
				case "null":
					body = `{"code":0,"data":null}`
				case "malformed":
					body = `{"code":`
				case "missing_code":
					body = `{"data":{}}`
				case "array":
					body = `{"code":0,"data":[]}`
				case "unexpected_fields":
					body = `{"code":0,"data":{"deleted":false}}`
				case "rejected":
					body = `{"code":400,"msg":"synthetic","data":{}}`
				}
				resp := friendDeleteDesktopResponse(body)
				switch mode {
				case "read":
					resp.Body = io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))
				case "server":
					resp.StatusCode = http.StatusServiceUnavailable
				case "redirect":
					resp.StatusCode = http.StatusTemporaryRedirect
					resp.Header.Set("Location", "https://unreviewed.invalid/delete")
				}
				return resp, nil
			})
			err := c.ChangeSocial(ctx, SettingChange{Kind: "friend_delete", Group: "123", Target: "8"}, nil,
				func(work context.Context, submit func(context.Context) error) error {
					first := submit(work)
					second := submit(work)
					if mode == "post_submit_fence_error" {
						return errors.New("synthetic post-submit commit failure")
					}
					if first != nil {
						return first
					}
					return second
				})
			success := mode == "empty" || mode == "omitted" || mode == "null"
			var failure *Error
			if success && err != nil || !success && (!errors.As(err, &failure) || !failure.Submitted) || calls.Load() != 1 {
				t.Fatal("deletion lost its single submitted attempt", err, calls.Load())
			}
		})
	}
}
