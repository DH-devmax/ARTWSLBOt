package lajiaoliao

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/url"
	"strings"
)

// Only bounded origin metadata is diagnostic. Signed paths, queries, headers
// and response bodies must never reach logs or public operation results.
func uploadOriginFailure(stage, raw string) *Failure {
	code := "lajiaoliao_avatar_origin_unverified"
	if stage != "avatar-signed" && stage != "avatar-resource" {
		code = "lajiaoliao_media_origin_unverified"
	}
	scheme, port, host := "invalid", "", "unparsed"
	if u, err := url.Parse(raw); err == nil {
		if u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "" {
			scheme = u.Scheme
		}
		if u.Port() == "" || u.Port() == "443" || u.Port() == "80" {
			port = u.Port()
		} else {
			port = "other"
		}
		domain := u.Hostname()
		if domain != "" && len(domain) <= 128 &&
			strings.Trim(domain, "abcdefghijklmnopqrstuvwxyz0123456789.-") == "" {
			host = domain
		} else {
			sum := sha256.Sum256([]byte(domain))
			host = "unreviewed-" + hex.EncodeToString(sum[:8])
		}
	}
	slog.Warn("native upload origin rejected", "platform", "lajiaoliao", "stage", stage,
		"scheme", scheme, "port", port, "origin", host)
	return &Failure{Code: code, Submitted: true}
}
