//go:build sodium && cgo

package nativeauth

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

type failingTransport struct {
	err   error
	calls int
}

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return nil, f.err
}

func TestNativeNetworkFailureDoesNotRetryOrExposeRawError(t *testing.T) {
	for _, test := range []struct {
		err        error
		diagnostic string
	}{
		{io.EOF, "connection_closed"},
		{&net.DNSError{Err: "synthetic-sensitive-detail", IsTimeout: true}, "network_timeout"},
		{errors.New("synthetic-sensitive-detail"), "network_unavailable"},
	} {
		value, err := NewClient(syntheticDeployment())
		if err != nil {
			t.Fatal(err)
		}
		client := value.(*Client)
		failure := &failingTransport{err: test.err}
		client.http = &http.Client{Transport: failure}
		_, err = client.Request(context.Background(), "/v1/verify/sms-anon", map[string]any{})
		var native *Error
		if !errors.As(err, &native) || native.Code != "native_transport_unknown" ||
			!native.Submitted || native.Diagnostic != test.diagnostic || failure.calls != 1 {
			t.Fatal("unsafe transport failure handling")
		}
		client.Close()
	}
}

func TestNativePreconnectFailureIsNotUnknownSubmission(t *testing.T) {
	for _, code := range []string{"native_origin_rejected", "native_dns_failed", "native_dns_fake_ip", "native_dns_fallback_failed"} {
		value, err := NewClient(syntheticDeployment())
		if err != nil {
			t.Fatal(err)
		}
		client := value.(*Client)
		client.http = newHTTPWithLookup(func(context.Context, string) ([]netip.Addr, error) {
			return nil, rejection(code)
		})
		_, err = client.Request(context.Background(), "/v1/verify/sms-anon", map[string]any{})
		var native *Error
		if !errors.As(err, &native) || native.Code != code || native.Submitted {
			t.Fatal("pre-connect failure became unknown submission")
		}
		client.Close()
	}
}
