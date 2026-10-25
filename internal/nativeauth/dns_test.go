// SPDX-License-Identifier: AGPL-3.0-or-later
package nativeauth

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestAddressGateAndOptInEncryptedDNS(t *testing.T) {
	for _, test := range []struct {
		name, code string
		system     []string
		fallback   []string
		enabled    bool
		calls      int
	}{
		{"public", "", []string{"8.8.8.8"}, nil, true, 0},
		{"fake-disabled", "native_dns_fake_ip", []string{"198.18.3.150"}, nil, false, 0},
		{"fake-enabled", "", []string{"198.18.3.150", "::ffff:198.19.0.1"}, []string{"8.8.8.8"}, true, 1},
		{"loopback", "native_origin_rejected", []string{"127.0.0.1"}, nil, true, 0},
		{"private-mixed", "native_origin_rejected", []string{"198.18.3.150", "10.0.0.1"}, nil, true, 0},
		{"public-mixed", "native_origin_rejected", []string{"198.18.3.150", "8.8.8.8"}, nil, true, 0},
		{"fallback-private", "native_origin_rejected", []string{"198.18.3.150"}, []string{"169.254.169.254"}, true, 1},
		{"fallback-fake", "native_origin_rejected", []string{"198.18.3.150"}, []string{"198.18.0.1"}, true, 1},
		{"fallback-empty", "native_dns_fallback_failed", []string{"198.18.3.150"}, nil, true, 1},
		{"system-empty", "native_dns_failed", nil, []string{"8.8.8.8"}, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			parse := func(values []string) []netip.Addr {
				var ips []netip.Addr
				for _, value := range values {
					ips = append(ips, netip.MustParseAddr(value))
				}
				return ips
			}
			calls := 0
			var fallback addressLookup
			if test.enabled {
				fallback = func(_ context.Context, host string) ([]netip.Addr, error) {
					calls++
					if host != "example.invalid" {
						t.Fatal("wrong hostname")
					}
					return parse(test.fallback), nil
				}
			}
			ips, err := resolveAddresses(context.Background(), "example.invalid",
				func(context.Context, string) ([]netip.Addr, error) { return parse(test.system), nil }, fallback)
			if test.code == "" {
				if err != nil || len(ips) == 0 {
					t.Fatal("expected public resolution", err)
				}
			} else if err == nil || err.Error() != test.code {
				t.Fatal("wrong safe resolution error", err)
			}
			if calls != test.calls {
				t.Fatal("unexpected fallback call count")
			}
		})
	}
}

func TestEncryptedDNSAnswerValidation(t *testing.T) {
	for _, test := range []struct {
		body string
		ok   bool
	}{
		{`{"Status":0,"Question":[{"Name":"example.invalid","Type":1}],"Answer":[{"Type":5,"Data":"alias.invalid."},{"Type":1,"Data":"8.8.8.8"}]}`, true},
		{`{"Status":0,"Question":[{"Name":"other.invalid","Type":1}],"Answer":[{"Type":1,"Data":"8.8.8.8"}]}`, false},
		{`{"Status":0,"Question":[{"Name":"example.invalid","Type":28}],"Answer":[{"Type":1,"Data":"8.8.8.8"}]}`, false},
		{`{"Status":0,"TC":true,"Question":[{"Name":"example.invalid","Type":1}],"Answer":[{"Type":1,"Data":"8.8.8.8"}]}`, false},
		{`{"Status":0,"Question":[{"Name":"example.invalid","Type":1}],"Answer":[{"Type":1,"Data":"8.8.8.8"},{"Type":1,"Data":"127.0.0.1"}]}`, false},
		{`{"Status":0,"Question":[{"Name":"example.invalid","Type":1}],"Answer":[{"Type":1,"Data":"198.18.0.1"}]}`, false},
		{`{"Status":0,"Question":[{"Name":"example.invalid","Type":1}],"Answer":[{"Type":1,"Data":"not-an-ip"}]}`, false},
		{`{"Status":3}`, false},
		{`{"Status":0}`, false},
		{`{}`, false},
		{`not-json`, false},
	} {
		ips, err := decodeDNSAnswer([]byte(test.body), "example.invalid")
		if (err == nil) != test.ok || test.ok && len(ips) != 1 {
			t.Fatal("DNS answer validation mismatch")
		}
	}
}

func TestPreconnectFailuresRemainSafeTypedErrors(t *testing.T) {
	for _, code := range []string{"native_origin_rejected", "native_dns_failed", "native_dns_fake_ip", "native_dns_fallback_failed"} {
		client := newHTTPWithLookup(func(context.Context, string) ([]netip.Addr, error) {
			return nil, rejection(code)
		})
		_, err := client.Get("https://example.invalid/")
		var native *Error
		if !errors.As(err, &native) || native.Code != code || native.Submitted {
			t.Fatal("pre-connect error lost classification")
		}
		client.CloseIdleConnections()
	}
}
