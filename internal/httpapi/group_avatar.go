package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func groupAvatarReference(group nativeauth.DirectoryGroup) string {
	asset := strings.TrimSuffix(group.Avatar, ".jpg")
	if !nativewire.ValidAssetID(group.ID) || !nativewire.ValidAssetID(group.Team) || !nativewire.ValidAssetID(asset) {
		return ""
	}
	sum := sha256.Sum256([]byte(group.Team + ":" + asset))
	return hex.EncodeToString(sum[:])
}

func nativeGroupAvatarURL(account string, group nativeauth.DirectoryGroup) string {
	ref := groupAvatarReference(group)
	if ref == "" {
		return ""
	}
	return "/api/v1/accounts/" + account + "/native-groups/" + group.ID + "/avatar/" + ref
}

// A directory avatar also works for a group without retained chat messages.
// The resource always comes from this account's sealed directory, never the URL.
func (s *Server) nativeGroupAvatar(w http.ResponseWriter, r *http.Request, p Principal) error {
	if r.URL.RawQuery != "" || !nativewire.ValidAssetID(r.PathValue("group")) ||
		!conversationPattern.MatchString(r.PathValue("reference")) {
		return fail(400, "invalid_media_reference")
	}
	select {
	case s.mediaSlots <- struct{}{}:
		defer func() { <-s.mediaSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		return fail(429, "native_media_busy")
	}
	var sealed []byte
	var epoch int64
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT n.payload_sealed,a.epoch
        FROM native_directory_snapshots n JOIN accounts a ON a.id=n.account_id
        WHERE n.account_id=$1 AND a.platform='wsl' AND n.sampled_at>clock_timestamp()-interval '7 days'`,
		r.PathValue("id")).Scan(&sealed, &epoch)
	if err == pgx.ErrNoRows {
		return fail(404, "native_media_not_found")
	}
	if err != nil {
		return err
	}
	plain, err := s.Vault.Open("native-directory:"+r.PathValue("id"), sealed)
	if err != nil {
		return fail(503, "native_directory_unreadable")
	}
	var groups []nativeauth.DirectoryGroup
	err = json.Unmarshal(plain, &groups)
	clear(plain)
	if err != nil {
		return fail(503, "native_directory_unreadable")
	}
	asset := ""
	for _, group := range groups {
		if group.ID == r.PathValue("group") && groupAvatarReference(group) == r.PathValue("reference") {
			asset = group.Avatar
			break
		}
	}
	if asset == "" {
		return fail(404, "native_media_not_found")
	}
	deployment, version, err := s.loadNativeDeployment(r.Context())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	file, err := nativeauth.FetchImage(ctx, s.mediaClient, deployment, asset, true, nil)
	if err != nil {
		code := "native_media_unavailable"
		if e, ok := err.(*nativeauth.Error); ok {
			code = e.Code
		}
		return fail(502, code)
	}
	defer clear(file.Data)
	if err = s.accountEventAccess(r, p, "messages"); err != nil {
		return err
	}
	var current []byte
	err = s.DB.Platform.QueryRow(ctx, `SELECT n.payload_sealed FROM native_directory_snapshots n
        JOIN accounts a ON a.id=n.account_id CROSS JOIN native_deployment d
        WHERE n.account_id=$1 AND a.epoch=$2 AND d.version=$3
        AND n.sampled_at>clock_timestamp()-interval '7 days'`,
		r.PathValue("id"), epoch, version).Scan(&current)
	if err == pgx.ErrNoRows {
		return fail(409, "native_media_scope_changed")
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(current, sealed) {
		return fail(409, "native_media_scope_changed")
	}
	return writeNativeMedia(w, file)
}
