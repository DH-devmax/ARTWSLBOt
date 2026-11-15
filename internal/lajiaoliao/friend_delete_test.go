package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"
)

func TestFriendDeleteChangeContract(t *testing.T) {
	good := Change{Kind: "friend_delete", Group: "synthetic-group", Target: "9007199254740993"}
	if err := good.Validate(); err != nil {
		t.Fatal("string-ID deletion rejected", err)
	}
	for _, id := range []string{"1", "0008", "friend-native", "\u597d\u53cb", strings.Repeat("x", 128)} {
		change := good
		change.Group, change.Target = id, id
		if err := change.Validate(); err != nil {
			t.Fatal("native string identity was narrowed", id, err)
		}
	}
	for _, id := range []string{"", " target", "target ", "target\x00", "target\n", "target\r",
		strings.Repeat("x", 129), string([]byte{0xff})} {
		for _, field := range []string{"group", "target"} {
			change := good
			if field == "group" {
				change.Group = id
			} else {
				change.Target = id
			}
			if err := change.Validate(); err == nil {
				t.Fatalf("invalid %s accepted: %q", field, id)
			}
		}
	}
	for name, mutate := range map[string]func(*Change){
		"text":           func(c *Change) { c.Text = "unrelated" },
		"enabled":        func(c *Change) { c.Enabled = true },
		"search_account": func(c *Change) { c.SearchAccount = true },
		"search_mobile":  func(c *Change) { c.SearchMobile = true },
		"minutes":        func(c *Change) { c.Minutes = 1 },
		"kind":           func(c *Change) { c.Kind = "friend_delete_batch" },
	} {
		t.Run(name, func(t *testing.T) {
			change := good
			mutate(&change)
			if err := change.Validate(); err == nil {
				t.Fatal("friend deletion accepted unrelated fields")
			}
		})
	}
}

func friendDeleteChiliFixture(t *testing.T, reply func(*http.Request) (*http.Response, error)) (*Client, *atomic.Int32) {
	t.Helper()
	c := NewClient()
	t.Cleanup(c.Close)
	calls := new(atomic.Int32)
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid deletion JSON")
		}
		if r.Method != http.MethodPost || r.URL.Scheme != "https" || r.URL.Host != "im-api.lajiaoliao.com" ||
			r.URL.Path != "/friend/delete_friend" || r.Header.Get("token") != "synthetic-im" ||
			r.Header.Get("platform") != "5" || r.Header.Get("deviceID") != "device-fixture" ||
			r.Header.Get("version") != "1.0.3" || r.Header.Get("operationID") == "" ||
			!reflect.DeepEqual(body, map[string]any{"ownerUserID": "self-native", "friendUserID": "9007199254740993"}) {
			t.Error("delete departed from the fixed SDK single string-ID contract", r.URL, body)
		}
		return reply(r)
	})
	return c, calls
}

func TestFriendDeleteFixedContractAndSingleSubmission(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "repeat", true: "concurrent"}[concurrent], func(t *testing.T) {
			c, calls := friendDeleteChiliFixture(t, func(*http.Request) (*http.Response, error) {
				return response(`{"errCode":0,"data":{}}`), nil
			})
			s := Session{UserID: "self-native", IMToken: "synthetic-im", DeviceID: "device-fixture"}
			fences := 0
			err := c.Execute(context.Background(), s, Change{Kind: "friend_delete", Group: "synthetic-group", Target: "9007199254740993"},
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

func TestFriendDeleteRequiresScopeIdentityAndFence(t *testing.T) {
	c, calls := friendDeleteChiliFixture(t, func(*http.Request) (*http.Response, error) {
		return response(`{"errCode":0,"data":{}}`), nil
	})
	s := Session{UserID: "self-native", IMToken: "synthetic-im", DeviceID: "device-fixture"}
	change := Change{Kind: "friend_delete", Group: "synthetic-group", Target: "9007199254740993"}
	fence := func(ctx context.Context, submit func(context.Context) error) error { return submit(ctx) }
	if err := c.Execute(context.Background(), s, change, nil); err == nil {
		t.Fatal("missing fence accepted")
	}
	for _, bad := range []Change{
		{Kind: "friend_delete", Target: change.Target},
		{Kind: "friend_delete", Group: change.Group},
		{Kind: "friend_delete", Group: change.Group, Target: change.Target, Text: "extra"},
		{Kind: "friend_delete", Group: change.Group, Target: s.UserID},
	} {
		if err := c.Execute(context.Background(), s, bad, fence); err == nil {
			t.Fatal("invalid or self deletion accepted", bad)
		}
	}
	for _, self := range []string{"", " self", string([]byte{0xff})} {
		badSession := s
		badSession.UserID = self
		if err := c.Execute(context.Background(), badSession, change, fence); err == nil {
			t.Fatal("unverified session identity accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid deletion reached transport", calls.Load())
	}
}

func TestFriendDeleteCancellationAndRevocation(t *testing.T) {
	for _, mode := range []string{"cancelled", "deadline", "fence_cancelled", "denied", "no_submission"} {
		t.Run(mode, func(t *testing.T) {
			c, calls := friendDeleteChiliFixture(t, func(*http.Request) (*http.Response, error) {
				return response(`{"errCode":0,"data":{}}`), nil
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
			err := c.Execute(ctx, Session{UserID: "self-native", IMToken: "synthetic-im", DeviceID: "device-fixture"},
				Change{Kind: "friend_delete", Group: "synthetic-group", Target: "9007199254740993"},
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
			var failure *Failure
			if err == nil || calls.Load() != 0 || errors.As(err, &failure) && failure.Submitted {
				t.Fatal("pre-submit cancellation/revocation reached native transport", err, calls.Load())
			}
			if mode == "denied" && !errors.Is(err, denied) {
				t.Fatal("lost final fence rejection", err)
			}
		})
	}
}

func TestFriendDeleteUnknownResponseNeverRetries(t *testing.T) {
	for _, mode := range []string{"empty", "omitted", "null", "transport", "read", "malformed", "missing_code",
		"array", "scalar", "unexpected_fields", "server", "redirect", "rate_limit", "rejected",
		"post_submit_cancel", "post_submit_fence_error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, calls := friendDeleteChiliFixture(t, func(*http.Request) (*http.Response, error) {
				body := `{"errCode":0,"data":{}}`
				switch mode {
				case "transport":
					return nil, io.ErrUnexpectedEOF
				case "post_submit_cancel":
					cancel()
					return nil, context.Canceled
				case "omitted":
					body = `{"errCode":0}`
				case "null":
					body = `{"errCode":0,"data":null}`
				case "malformed":
					body = `{"errCode":`
				case "missing_code":
					body = `{"data":{}}`
				case "array":
					body = `{"errCode":0,"data":[]}`
				case "scalar":
					body = `{"errCode":0,"data":false}`
				case "unexpected_fields":
					body = `{"errCode":0,"data":{"deleted":false}}`
				case "rejected":
					body = `{"errCode":1001,"data":{}}`
				}
				resp := response(body)
				switch mode {
				case "read":
					resp.Body = io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))
				case "server":
					resp.StatusCode = http.StatusServiceUnavailable
				case "redirect":
					resp.StatusCode = http.StatusTemporaryRedirect
					resp.Header.Set("Location", "https://unreviewed.invalid/delete")
				case "rate_limit":
					resp.StatusCode = http.StatusTooManyRequests
				}
				return resp, nil
			})
			err := c.Execute(ctx, Session{UserID: "self-native", IMToken: "synthetic-im", DeviceID: "device-fixture"},
				Change{Kind: "friend_delete", Group: "synthetic-group", Target: "9007199254740993"},
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
			var failure *Failure
			if success && err != nil || !success && (!errors.As(err, &failure) || !failure.Submitted) || calls.Load() != 1 {
				t.Fatal("deletion lost its single submitted attempt", err, calls.Load())
			}
		})
	}
}
