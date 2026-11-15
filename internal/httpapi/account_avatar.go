package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

const accountAvatarLifetime = 10 * time.Minute

type accountAvatarReference struct {
	Asset      string `json:"asset"`
	UID        string `json:"uid"`
	Epoch      int64  `json:"epoch"`
	Version    int64  `json:"version"`
	Deployment int64  `json:"deployment"`
	Credential string `json:"credential"`
	Expires    int64  `json:"expires"`
}

func accountAvatarScope(account, actor, workspace string) string {
	return "native-account-avatar:" + account + ":" + actor + ":" + workspace
}

// Use the already-read settings snapshot. Showing an avatar must not enqueue
// another native settings request or persist an image/cache record.
func (s *Server) nativeAccountAvatarURL(account, actor, workspace string, view gateway.SettingSnapshot) (string, error) {
	if view.Settings == nil || view.Settings.UID != view.SelfID {
		return "", nil
	}
	asset := view.Settings.Avatar
	if workspace == "lajiaoliao" {
		asset = view.AvatarResource
		if !lajiaoliao.ValidID(view.SelfID) || len(asset) > 2048 ||
			!(lajiaoliao.Resource{URL: asset, Kind: "avatar"}).Valid() {
			return "", nil
		}
	} else if !nativewire.ValidAssetID(view.SelfID) || !nativewire.ValidAssetID(strings.TrimSuffix(asset, ".jpg")) {
		return "", nil
	}
	ref := accountAvatarReference{
		Asset: asset, UID: view.SelfID, Epoch: view.AccountEpoch,
		Version: view.AccountVersion, Deployment: view.DeploymentVersion,
		Credential: hex.EncodeToString(view.CredentialDigest[:]),
		Expires:    time.Now().Add(accountAvatarLifetime).Unix(),
	}
	plain, err := json.Marshal(ref)
	if err != nil {
		return "", err
	}
	defer clear(plain)
	sealed, err := s.Vault.Seal(accountAvatarScope(account, actor, workspace), plain)
	if err != nil {
		return "", err
	}
	return "/api/v1/accounts/" + account + "/native-avatar/" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Server) openAccountAvatar(account, actor, workspace, encoded string) (accountAvatarReference, error) {
	var ref accountAvatarReference
	limit := 1024
	if workspace == "lajiaoliao" {
		limit = 4096
	}
	if len(encoded) < 64 || len(encoded) > limit {
		return ref, fail(400, "invalid_media_reference")
	}
	sealed, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return ref, fail(400, "invalid_media_reference")
	}
	plain, err := s.Vault.Open(accountAvatarScope(account, actor, workspace), sealed)
	if err != nil {
		return ref, fail(404, "native_media_not_found")
	}
	defer clear(plain)
	now := time.Now().Unix()
	if json.Unmarshal(plain, &ref) != nil || ref.Expires <= now ||
		ref.Expires > now+int64(accountAvatarLifetime.Seconds()) ||
		ref.Epoch < 1 || ref.Version < 1 ||
		!conversationPattern.MatchString(ref.Credential) {
		return ref, fail(404, "native_media_not_found")
	}
	if workspace == "lajiaoliao" {
		if ref.Deployment != 0 || !lajiaoliao.ValidID(ref.UID) || len(ref.Asset) > 2048 ||
			!(lajiaoliao.Resource{URL: ref.Asset, Kind: "avatar"}).Valid() {
			return ref, fail(404, "native_media_not_found")
		}
	} else if ref.Deployment < 1 || !nativewire.ValidAssetID(ref.UID) ||
		!nativewire.ValidAssetID(strings.TrimSuffix(ref.Asset, ".jpg")) {
		return ref, fail(404, "native_media_not_found")
	}
	return ref, nil
}

func (s *Server) accountAvatarAccess(r *http.Request, p Principal, ref accountAvatarReference) error {
	if err := s.memberResultAccess(r, p, gateway.MemberSnapshot{
		AccountEpoch: ref.Epoch, DeploymentVersion: ref.Deployment,
	}); err != nil {
		return err
	}
	var credential []byte
	platform := "wsl"
	if requestWorkspace(r) == "lajiaoliao" {
		platform = "lajiaoliao"
	}
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(credential_sealed,'\x')
        FROM accounts WHERE id=$1 AND platform=$5 AND native_uid=$2
        AND epoch=$3 AND version=$4 AND auth_state='authenticated'`,
		r.PathValue("id"), ref.UID, ref.Epoch, ref.Version, platform).Scan(&credential)
	if err == pgx.ErrNoRows {
		return fail(409, "native_media_scope_changed")
	}
	if err != nil {
		return fail(503, "database_unavailable")
	}
	digest := sha256.Sum256(credential)
	clear(credential)
	if hex.EncodeToString(digest[:]) != ref.Credential {
		return fail(409, "native_media_scope_changed")
	}
	return nil
}

func (s *Server) nativeAccountAvatar(w http.ResponseWriter, r *http.Request, p Principal) error {
	if r.URL.RawQuery != "" {
		return fail(400, "invalid_media_reference")
	}
	ref, err := s.openAccountAvatar(r.PathValue("id"), p.ID, requestWorkspace(r), r.PathValue("reference"))
	if err != nil {
		return err
	}
	if err = s.accountAvatarAccess(r, p, ref); err != nil {
		return err
	}
	select {
	case s.mediaSlots <- struct{}{}:
		defer func() { <-s.mediaSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		return fail(429, "native_media_busy")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if requestWorkspace(r) == "lajiaoliao" {
		file, err := lajiaoliao.FetchResource(ctx, s.chiliMediaClient, lajiaoliao.Resource{URL: ref.Asset, Kind: "avatar"})
		if err != nil {
			return nativeSettingsError(w, err)
		}
		defer clear(file.Data)
		if err = s.accountAvatarAccess(r, p, ref); err != nil {
			return err
		}
		return writeNativeMedia(w, nativeauth.MediaFile{Data: file.Data, MIME: file.MIME})
	}
	deployment, version, err := s.loadNativeDeployment(r.Context())
	if err != nil {
		return err
	}
	if version != ref.Deployment {
		return fail(409, "native_media_scope_changed")
	}
	file, err := nativeauth.FetchImage(ctx, s.mediaClient, deployment, ref.Asset, true, nil)
	if err != nil {
		return fail(502, "native_media_unavailable")
	}
	defer clear(file.Data)
	if err = s.accountAvatarAccess(r, p, ref); err != nil {
		return err
	}
	return writeNativeMedia(w, file)
}
