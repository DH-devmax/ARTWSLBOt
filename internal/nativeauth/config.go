// SPDX-License-Identifier: AGPL-3.0-or-later
package nativeauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"wangshangliao/internal/nativewire"
)

type Deployment struct {
	Origin          string            `json:"origin"`
	Headers         map[string]string `json:"headers"`
	Metadata        []uint64          `json:"metadata"`
	SigningSeed     string            `json:"signing_seed"`
	BodyKey         string            `json:"body_key"`
	MessageKey      string            `json:"message_key"`
	ServerKey       string            `json:"server_key"`
	CaptchaID       string            `json:"captcha_id"`
	AppKey          string            `json:"app_key"`
	DeviceID        string            `json:"device_id"`
	ProtocolVersion string            `json:"protocol_version"`
}

func ParseDeployment(data []byte) (Deployment, error) {
	var d Deployment
	if len(data) == 0 || len(data) > 60000 {
		return d, errors.New("native_deployment_invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&d)
	var extra any
	if err != nil || decoder.Decode(&extra) != io.EOF {
		return Deployment{}, errors.New("native_deployment_invalid")
	}
	if d.ProtocolVersion == "" {
		d.ProtocolVersion = "3.7.1"
	}
	u, err := url.Parse(d.Origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") ||
		strings.ContainsAny(d.Origin, "\r\n\\") {
		return Deployment{}, errors.New("native_deployment_invalid")
	}
	d.Origin = strings.TrimRight(d.Origin, "/")
	if !supportedProtocol(d.ProtocolVersion) {
		return Deployment{}, errors.New("native_protocol_unsupported")
	}
	for _, key := range []string{d.SigningSeed, d.BodyKey, d.ServerKey} {
		if _, err := decodeKey(key); err != nil {
			return Deployment{}, errors.New("native_deployment_invalid")
		}
	}
	if d.MessageKey != "" {
		if _, err := decodeKey(d.MessageKey); err != nil {
			return Deployment{}, errors.New("native_deployment_invalid")
		}
	}
	if _, err = nativewire.Metadata(d.Metadata); err != nil || len(d.CaptchaID) == 0 ||
		len(d.CaptchaID) > 128 || len(d.DeviceID) == 0 || len(d.DeviceID) > 256 ||
		len(d.AppKey) > 256 || len(d.Headers) > 16 {
		return Deployment{}, errors.New("native_deployment_invalid")
	}
	for name, value := range d.Headers {
		if len(name) > 128 || len(value) > 1024 || strings.ContainsAny(name+value, "\r\n") {
			return Deployment{}, errors.New("native_deployment_invalid")
		}
	}
	return d, nil
}

func decodeKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(key) != 32 {
		return nil, errors.New("native_deployment_invalid")
	}
	return key, nil
}

// The origin is superadmin-provided, but it still cannot reach local/cloud metadata services.
func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96"} {
		if netip.MustParsePrefix(prefix).Contains(address) {
			return false
		}
	}
	return true
}

func newHTTP() *http.Client {
	transport := &http.Transport{
		Proxy: nil, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConnsPerHost: 1, MaxConnsPerHost: 1, DisableCompression: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("native_origin_rejected")
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("native_dns_failed")
			}
			for _, ip := range ips {
				if !publicAddress(ip) {
					return nil, errors.New("native_origin_rejected")
				}
			}
			var dialer net.Dialer
			dialer.Timeout = 5 * time.Second
			// Dial only checked addresses, with IPv4 first on hosts without IPv6 routing.
			var last error
			for _, ipv4 := range []bool{true, false} {
				for _, ip := range ips {
					if ip.Unmap().Is4() != ipv4 {
						continue
					}
					conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
					if err == nil {
						return conn, nil
					}
					last = err
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
				}
			}
			return nil, last
		},
	}
	return &http.Client{
		Transport: transport, Timeout: 12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
