// SPDX-License-Identifier: AGPL-3.0-or-later
package nativeauth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

type addressLookup func(context.Context, string) ([]netip.Addr, error)

var benchmarkNetwork = netip.MustParsePrefix("198.18.0.0/15")

// A proxy's fake-IP DNS is not a public origin. An operator may opt in to a
// fixed encrypted resolver, but neither resolver may authorize private targets.
func resolveAddresses(ctx context.Context, host string, system, fallback addressLookup) ([]netip.Addr, error) {
	ips, err := system(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, rejection("native_dns_failed")
	}
	allFake := true
	for _, ip := range ips {
		if !benchmarkNetwork.Contains(ip.Unmap()) {
			allFake = false
		}
		if !publicAddress(ip) && !benchmarkNetwork.Contains(ip.Unmap()) {
			return nil, rejection("native_origin_rejected")
		}
	}
	if allFake {
		if fallback == nil {
			return nil, rejection("native_dns_fake_ip")
		}
		ips, err = fallback(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, rejection("native_dns_fallback_failed")
		}
	}
	for _, ip := range ips {
		if !publicAddress(ip) {
			return nil, rejection("native_origin_rejected")
		}
	}
	return ips, nil
}

var encryptedDNSClient = &http.Client{
	Transport: &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second,
		MaxConnsPerHost: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
	},
	Timeout:       5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func encryptedLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	// The endpoint is not caller-configurable and bypasses system DNS/proxies.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://1.1.1.1/dns-query?"+url.Values{"name": {host}, "type": {"A"}}.Encode(), nil)
	if err != nil {
		return nil, rejection("native_dns_fallback_failed")
	}
	req.Header.Set("Accept", "application/dns-json")
	response, err := encryptedDNSClient.Do(req)
	if err != nil {
		return nil, rejection("native_dns_fallback_failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, rejection("native_dns_fallback_failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(body) > 16384 {
		return nil, rejection("native_dns_fallback_failed")
	}
	return decodeDNSAnswer(body, host)
}

func decodeDNSAnswer(body []byte, host string) ([]netip.Addr, error) {
	var response struct {
		Status   *int
		TC       bool
		Question []struct {
			Name string
			Type int
		}
		Answer []struct {
			Type int
			Data string
		}
	}
	if json.Unmarshal(body, &response) != nil || response.Status == nil || *response.Status != 0 ||
		response.TC || len(response.Question) != 1 || response.Question[0].Name != host ||
		response.Question[0].Type != 1 {
		return nil, rejection("native_dns_fallback_failed")
	}
	var ips []netip.Addr
	for _, answer := range response.Answer {
		if answer.Type != 1 {
			continue
		}
		ip, err := netip.ParseAddr(answer.Data)
		if err != nil || !ip.Is4() || !publicAddress(ip) {
			return nil, rejection("native_dns_fallback_failed")
		}
		ips = append(ips, ip)
	}
	if len(ips) == 0 {
		return nil, rejection("native_dns_fallback_failed")
	}
	return ips, nil
}
