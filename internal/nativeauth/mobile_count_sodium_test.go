//go:build sodium && cgo

package nativeauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMobileRequestCountSourceSurvivesResumedClientsAndUnknownResponse(t *testing.T) {
	base, _ := newSyntheticMobileClient(t)
	saved := syntheticMobileSaved(base)
	var allocated, transported atomic.Uint64
	allocated.Store(41)
	source := RequestCountSource(func(context.Context) (uint64, error) {
		return allocated.Add(1) - 1, nil
	})
	for range 3 {
		c, err := ResumeMobile(base.config, saved, base.requestIDs, source)
		if err != nil {
			t.Fatal("mobile resume rejected its request-count source", err)
		}
		c.http = &http.Client{Transport: mediaTransport(func(r *http.Request) (*http.Response, error) {
			fields, _, _ := mobileRequest(t, r, c.bodyKey)
			request := transported.Add(1)
			if fields[5] != 40+request || fields[4] != saved.UID {
				t.Error("signed mobile metadata reused the saved count or changed the UID")
			}
			status := http.StatusOK
			if request == 2 {
				status = http.StatusBadGateway
			}
			return &http.Response{StatusCode: status, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{}}`)), Request: r}, nil
		})}
		for range 2 {
			_, err := c.Request(context.Background(), onboardingOutboundInfoRoute,
				map[string]any{"uid": 8, "showTempChat": false})
			if transported.Load() == 2 {
				var native *Error
				if !errors.As(err, &native) || !native.Submitted || native.Code != "native_transport_unknown" {
					t.Error("unknown response lost its submitted state")
				}
			} else if err != nil {
				t.Error("allocated mobile request count failed", err)
			}
		}
		c.Close()
	}
	if allocated.Load() != 47 || transported.Load() != 6 {
		t.Fatal("resumption or unknown response reset, retried or skipped the durable allocator")
	}
}

func TestMobileRequestCountSourceFailuresStayBeforeTransport(t *testing.T) {
	for _, mode := range []string{"zero", "highbit", "max32", "count_error", "overflow", "overflow64",
		"nonce_error", "nonce_zero", "cancelled", "cancel_in_source", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			base, _ := newSyntheticMobileClient(t)
			var nonceCalls, countCalls, transportCalls atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			nonceErr := errors.New("synthetic-durable-nonce-failure")
			countErr := errors.New("synthetic-durable-count-failure")
			ids := RequestIDSource(func(work context.Context, now uint64) (uint64, error) {
				nonceCalls.Add(1)
				if mode == "nonce_error" {
					return 0, nonceErr
				}
				if mode == "nonce_zero" {
					return 0, nil
				}
				return base.requestIDs(work, now)
			})
			counts := RequestCountSource(func(work context.Context) (uint64, error) {
				countCalls.Add(1)
				if work != ctx {
					t.Error("counter allocation lost the caller's cancellation context")
				}
				switch mode {
				case "count_error":
					return 17, countErr
				case "overflow":
					return 1 << 32, nil
				case "overflow64":
					return ^uint64(0), nil
				case "max32":
					return 1<<32 - 1, nil
				case "highbit":
					return 1 << 31, nil
				case "cancel_in_source":
					cancel()
					return 17, nil
				}
				return 0, nil
			})
			c, err := ResumeMobile(base.config, syntheticMobileSaved(base), ids, counts)
			if err != nil {
				t.Fatal("count-aware mobile resume failed", err)
			}
			defer c.Close()
			c.http = &http.Client{Transport: mediaTransport(func(r *http.Request) (*http.Response, error) {
				transportCalls.Add(1)
				fields, _, _ := mobileRequest(t, r, c.bodyKey)
				expected := uint64(0)
				if mode == "max32" {
					expected = 1<<32 - 1
				} else if mode == "highbit" {
					expected = 1 << 31
				}
				// The existing codec preserves Java/protobuf int32 sign extension.
				wireExpected := expected
				if expected >= 1<<31 {
					wireExpected = uint64(int64(int32(expected)))
				}
				if fields[5] != wireExpected {
					t.Error("counter value was masked, replaced or incremented before signing")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{}}`)), Request: r}, nil
			})}
			route := onboardingOutboundInfoRoute
			if mode == "cancelled" {
				cancel()
			}
			if mode == "unsupported" {
				route = "/v1/user/get-nim-token"
			}
			_, err = c.Request(ctx, route, map[string]any{"uid": 8, "showTempChat": false})
			success := mode == "zero" || mode == "highbit" || mode == "max32"
			if success {
				if err != nil || transportCalls.Load() != 1 || countCalls.Load() != 1 || nonceCalls.Load() != 1 {
					t.Fatal("valid allocated count was not submitted exactly once", err)
				}
				return
			}
			if err == nil || transportCalls.Load() != 0 {
				t.Fatal("counter or nonce failure fell back or reached transport")
			}
			expectedCountCalls, expectedNonceCalls := int32(1), int32(1)
			switch mode {
			case "nonce_error", "nonce_zero":
				expectedCountCalls = 0
			case "cancelled", "unsupported":
				expectedCountCalls, expectedNonceCalls = 0, 0
			}
			if countCalls.Load() != expectedCountCalls || nonceCalls.Load() != expectedNonceCalls {
				t.Fatal("failed allocation was retried or ran outside its request guards")
			}
			if mode == "count_error" && !errors.Is(err, countErr) ||
				mode == "nonce_error" && !errors.Is(err, nonceErr) {
				t.Fatal("allocator failure was replaced by a local fallback")
			}
			var native *Error
			if errors.As(err, &native) && native.Submitted {
				t.Fatal("pre-transport allocator failure was reported as submitted")
			}
			if mode == "overflow" || mode == "overflow64" {
				if !errors.As(err, &native) || native.Code != "native_request_count_exhausted" {
					t.Fatal("out-of-range mobile count was not rejected explicitly")
				}
			}
		})
	}
}

func TestMobileRequestCountSourceOptionalCompatibility(t *testing.T) {
	base, _ := newSyntheticMobileClient(t)
	saved := syntheticMobileSaved(base)
	if c, err := ResumeMobile(base.config, saved, base.requestIDs, nil, nil); err == nil {
		c.Close()
		t.Fatal("mobile resume silently accepted ambiguous count sources")
	}
	for _, sources := range [][]RequestCountSource{nil, {nil}} {
		c, err := ResumeMobile(base.config, saved, base.requestIDs, sources...)
		if err != nil {
			t.Fatal("legacy mobile resume without a counter source stopped working", err)
		}
		var transported uint64
		c.http = &http.Client{Transport: mediaTransport(func(r *http.Request) (*http.Response, error) {
			fields, _, _ := mobileRequest(t, r, c.bodyKey)
			if fields[5] != saved.Kind+transported {
				t.Error("source-free mobile authentication lost its local per-response advancement")
			}
			transported++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{}}`)), Request: r}, nil
		})}
		for range 2 {
			if _, err := c.Request(context.Background(), onboardingOutboundInfoRoute,
				map[string]any{"uid": 8, "showTempChat": false}); err != nil {
				t.Fatal("source-free compatibility request failed", err)
			}
		}
		session, _, err := c.Finish(context.Background())
		if err != nil || session.Kind != saved.Kind+2 || transported != 2 {
			t.Fatal("source-free Finish lost the current authentication counter", err)
		}
		c.Close()
	}
}
