package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func (s *Server) checkNativeDeployment(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !s.nativeCheckMu.TryLock() {
		return fail(409, "native_check_busy")
	}
	defer s.nativeCheckMu.Unlock()
	if time.Now().Before(s.nativeCheckUntil) {
		w.Header().Set("Retry-After", "30")
		return fail(429, "native_check_cooldown")
	}
	d, version, err := s.loadNativeDeployment(r.Context())
	if err != nil {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	s.nativeCheckUntil = time.Now().Add(30 * time.Second)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := nativeauth.CheckDeployment(ctx, d, s.nativeRequestIDs(version), s.Native.Factory)
	if err != nil {
		var upstream *nativeauth.Error
		if errors.As(err, &upstream) && upstream.RetryAfter > 30*time.Second {
			s.nativeCheckUntil = time.Now().Add(min(upstream.RetryAfter, 24*time.Hour))
		}
		return nativeFailure(err)
	}
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return fail(401, "unauthorized")
	}
	var valid bool
	if err = s.DB.Platform.QueryRow(ctx, `SELECT EXISTS(
        SELECT 1 FROM users u JOIN sessions se ON se.user_id=u.id
        JOIN native_deployment d ON d.singleton AND d.version=$3
        WHERE u.id=$1 AND u.role='superadmin' AND NOT u.disabled
        AND se.token_hash=$2 AND se.expires_at>now())`, p.ID, security.Digest(cookie.Value), version).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return fail(409, "native_check_stale")
	}
	// No upstream settings, identity, keys or payload enter the result or audit.
	send(w, 200, map[string]any{"verified": result.Verified, "captcha_required": result.CaptchaRequired,
		"version": version, "checked_at": time.Now().UTC(), "scope": "anonymous_settings_only"})
	return nil
}
