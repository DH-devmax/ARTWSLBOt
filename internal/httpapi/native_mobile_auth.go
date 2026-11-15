package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func (s *Server) mobileAuthRoutes() {
	s.handle("GET /api/v1/native-mobile-deployment", true, s.nativeMobileDeployment)
	s.handle("PUT /api/v1/native-mobile-deployment", true, s.saveNativeMobileDeployment)
	s.handle("GET /api/v1/accounts/{id}/native-mobile-auth", true, s.nativeMobileStatus)
	s.handle("POST /api/v1/accounts/{id}/native-mobile-auth", true, s.startNativeMobileAuth)
	s.handle("GET /api/v1/accounts/{id}/native-mobile-auth/{transaction}", true, s.pollNativeMobileAuth)
	s.handle("POST /api/v1/accounts/{id}/native-mobile-auth/{transaction}", true, s.nativeMobileAuthAction)
	s.handle("DELETE /api/v1/accounts/{id}/native-mobile-auth/{transaction}", true, s.cancelNativeMobileAuth)
	s.handle("DELETE /api/v1/accounts/{id}/native-mobile-session", true, s.forgetNativeMobileSession)
}

func (s *Server) loadNativeMobileDeployment(ctx context.Context) (nativeauth.Deployment, int64, error) {
	var sealed []byte
	var version int64
	err := s.DB.Platform.QueryRow(ctx, `SELECT sealed,version FROM native_mobile_deployment WHERE singleton`).Scan(&sealed, &version)
	if err == pgx.ErrNoRows {
		return nativeauth.Deployment{}, 0, fail(409, "native_mobile_configuration_missing")
	}
	if err != nil {
		return nativeauth.Deployment{}, 0, err
	}
	plain, err := s.Vault.Open("native-mobile-deployment:v1", sealed)
	if err != nil {
		return nativeauth.Deployment{}, 0, fail(503, "native_mobile_configuration_invalid")
	}
	defer clear(plain)
	d, err := nativeauth.ParseMobileDeployment(plain)
	if err != nil {
		return nativeauth.Deployment{}, 0, fail(503, "native_mobile_configuration_invalid")
	}
	return d, version, nil
}

func (s *Server) nativeMobileDeployment(w http.ResponseWriter, r *http.Request, p Principal) error {
	d, version, err := s.loadNativeMobileDeployment(r.Context())
	var api *apiError
	if errors.As(err, &api) && api.code == "native_mobile_configuration_missing" {
		send(w, 200, map[string]any{"configured": false, "version": 0,
			"crypto_ready": nativewire.Available(), "protocol_version": nativeauth.MobileProtocolVersion})
		return nil
	}
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"configured": true, "version": version, "origin": d.Origin,
		"crypto_ready": nativewire.Available(), "protocol_version": d.ProtocolVersion})
	return nil
}

func mobileUnresolved(ctx context.Context, query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, account string) error {
	var pending bool
	err := query.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_outbound_attempts
		WHERE platform='wsl' AND ($1='' OR account_id::text=$1) AND status IN ('submitting','unknown'))`, account).Scan(&pending)
	if err != nil {
		return err
	}
	if pending {
		return fail(409, "group_binding_outbound_operation_unresolved")
	}
	return nil
}

func (s *Server) saveNativeMobileDeployment(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version       int64  `json:"version"`
		Configuration string `json:"configuration"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	d, err := nativeauth.ParseMobileDeployment([]byte(in.Configuration))
	if err != nil {
		return fail(400, "native_mobile_configuration_invalid")
	}
	plain, err := json.Marshal(d)
	if err != nil {
		return err
	}
	defer clear(plain)
	sealed, err := s.Vault.Seal("native-mobile-deployment:v1", plain)
	if err != nil {
		return err
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SET LOCAL lock_timeout='3s'`); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798427)`); err != nil {
		return err
	}
	var version int64
	err = tx.QueryRow(r.Context(), `SELECT version FROM native_mobile_deployment WHERE singleton FOR UPDATE`).Scan(&version)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if in.Version < 0 || in.Version != version {
		return fail(409, "version_conflict")
	}
	if err = mobileUnresolved(r.Context(), tx, ""); err != nil {
		return err
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO native_mobile_deployment(singleton,sealed) VALUES(true,$1)
		ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,
		version=native_mobile_deployment.version+1,updated_at=now() RETURNING version`, sealed).Scan(&version)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE native_mobile_sessions SET credential_sealed=NULL,
		state='reauth_required',version=version+1,updated_at=now()`); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "native_mobile_deployment_updated", "platform",
		map[string]any{"version": version, "protocol_version": d.ProtocolVersion}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	s.NativeMobile.Close()
	send(w, 200, map[string]any{"configured": true, "version": version})
	return nil
}

// The desktop and mobile profiles can share a body key. Both allocate from the
// existing durable nonce high-water mark, including across restarts.
func (s *Server) nativeMobileRequestIDs(version int64) nativeauth.RequestIDSource {
	return func(ctx context.Context, now uint64) (uint64, error) {
		if now > 1<<63-2 {
			return 0, &nativeauth.Error{Code: "native_request_id_exhausted"}
		}
		var id int64
		err := s.DB.Platform.QueryRow(ctx, `UPDATE native_deployment
			SET request_id_ns=GREATEST(request_id_ns+1,$1)
			WHERE singleton AND request_id_ns<9223372036854775807 AND EXISTS(
				SELECT 1 FROM native_mobile_deployment WHERE singleton AND version=$2)
			RETURNING request_id_ns`, int64(now), version).Scan(&id)
		if err == pgx.ErrNoRows {
			return 0, &nativeauth.Error{Code: "native_mobile_configuration_changed"}
		}
		if err != nil {
			return 0, &nativeauth.Error{Code: "native_request_id_unavailable"}
		}
		return uint64(id), nil
	}
}

func (s *Server) nativeMobileStatus(w http.ResponseWriter, r *http.Request, p Principal) error {
	var version int64
	var available bool
	var state string
	var authenticated any
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT COALESCE(m.version,0),
		COALESCE(m.state,'not_authenticated'),m.authenticated_at,
		COALESCE(a.auth_state='authenticated' AND m.state='authenticated'
			AND m.account_epoch=a.epoch AND m.native_uid=a.native_uid
			AND m.deployment_version=d.version AND m.credential_sealed IS NOT NULL,false)
		FROM accounts a LEFT JOIN native_mobile_sessions m ON m.account_id=a.id
		LEFT JOIN native_mobile_deployment d ON d.singleton
		WHERE a.id=$1 AND a.platform='wsl'`, r.PathValue("id")).Scan(&version, &state, &authenticated, &available)
	if err == pgx.ErrNoRows {
		return fail(404, "account_not_found")
	}
	if err != nil {
		return err
	}
	reason := ""
	if !available {
		reason = "native_mobile_search_required"
	}
	send(w, 200, map[string]any{"available": available, "configured": version > 0,
		"version": version, "state": state, "authenticated_at": authenticated, "blocked_reason": reason})
	return nil
}

func (s *Server) startNativeMobileAuth(w http.ResponseWriter, r *http.Request, p Principal) error {
	s.accountLifecycle.RLock()
	defer s.accountLifecycle.RUnlock()
	var in struct {
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	d, deploymentVersion, err := s.loadNativeMobileDeployment(r.Context())
	if err != nil {
		return err
	}
	account := r.PathValue("id")
	var version, epoch int64
	var uid string
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT version,epoch,native_uid FROM accounts
		WHERE id=$1 AND platform='wsl' AND purpose='management'
		AND auth_state='authenticated' AND credential_sealed IS NOT NULL`, account).Scan(&version, &epoch, &uid)
	if err == pgx.ErrNoRows {
		return fail(409, "native_mobile_account_unavailable")
	}
	if err != nil {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	if s.Native.ActiveAccount(account) {
		return fail(409, "native_auth_account_busy")
	}
	if err = mobileUnresolved(r.Context(), s.DB.Platform, account); err != nil {
		return err
	}
	cookie, _ := r.Cookie("wsl_session")
	sessionHash := security.Digest(cookie.Value)
	view, err := s.NativeMobile.Start(p.ID, account, d, func(ctx context.Context, session nativeauth.Session, nickname string) error {
		if session.ProtocolVersion != nativeauth.MobileProtocolVersion ||
			strconv.FormatUint(session.UID, 10) != uid || session.JWT == "" ||
			len(session.MAC) != 16 || session.Kind >= 1<<32 || session.NIMID != "" || session.NIMToken != "" {
			return &nativeauth.Error{Code: "native_mobile_identity_mismatch"}
		}
		tx, err := s.DB.Platform.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='3s'`); err != nil {
			return err
		}
		var current int64
		if err = tx.QueryRow(ctx, `SELECT version FROM native_mobile_deployment WHERE singleton FOR SHARE`).Scan(&current); err != nil ||
			current != deploymentVersion {
			return &nativeauth.Error{Code: "native_mobile_configuration_changed"}
		}
		var actor string
		err = tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN sessions se ON se.user_id=u.id
			WHERE u.id=$1 AND u.role='superadmin' AND NOT u.disabled
			AND se.token_hash=$2 AND se.expires_at>now() FOR SHARE OF u,se`, p.ID, sessionHash).Scan(&actor)
		if err != nil {
			return &nativeauth.Error{Code: "native_site_session_expired"}
		}
		var currentUID string
		err = tx.QueryRow(ctx, `SELECT native_uid FROM accounts WHERE id=$1 AND platform='wsl'
			AND purpose='management' AND version=$2 AND epoch=$3 AND auth_state='authenticated'
			AND credential_sealed IS NOT NULL FOR SHARE`, account, version, epoch).Scan(&currentUID)
		if err != nil || currentUID != uid {
			return &nativeauth.Error{Code: "native_mobile_account_changed"}
		}
		if err = tx.QueryRow(ctx, `SELECT version FROM native_mobile_sessions WHERE account_id=$1
			FOR UPDATE`, account).Scan(&current); err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err = mobileUnresolved(ctx, tx, account); err != nil {
			return err
		}
		plain, err := json.Marshal(session)
		if err != nil {
			return err
		}
		defer clear(plain)
		sealed, err := s.Vault.Seal("native-mobile-session:"+account, plain)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO native_mobile_sessions
			(account_id,native_uid,account_epoch,deployment_version,credential_sealed,state,request_count)
			VALUES($1,$2,$3,$4,$5,'authenticated',$6) ON CONFLICT(account_id) DO UPDATE SET
			native_uid=excluded.native_uid,account_epoch=excluded.account_epoch,
			deployment_version=excluded.deployment_version,credential_sealed=excluded.credential_sealed,
			request_count=excluded.request_count,
			state='authenticated',version=native_mobile_sessions.version+1,authenticated_at=now(),updated_at=now()`,
			account, uid, epoch, deploymentVersion, sealed, int64(session.Kind))
		if err != nil {
			return err
		}
		if err = auditTx(ctx, tx, p, "", "native_mobile_authenticated", account,
			map[string]any{"mobile_deployment_version": deploymentVersion}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}, s.nativeMobileRequestIDs(deploymentVersion))
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 201, view)
	return nil
}

func (s *Server) pollNativeMobileAuth(w http.ResponseWriter, r *http.Request, p Principal) error {
	view, err := s.NativeMobile.Poll(p.ID, r.PathValue("id"), r.PathValue("transaction"))
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 200, view)
	return nil
}

func (s *Server) nativeMobileAuthAction(w http.ResponseWriter, r *http.Request, p Principal) error {
	s.accountLifecycle.RLock()
	defer s.accountLifecycle.RUnlock()
	var in nativeauth.Input
	if err := decode(w, r, &in); err != nil {
		return err
	}
	switch in.Action {
	case "password", "sms_request", "sms_verify", "sms_resend", "security_verify", "retry_finish":
	default:
		return fail(400, "native_invalid_input")
	}
	view, err := s.NativeMobile.Action(p.ID, r.PathValue("id"), r.PathValue("transaction"), in)
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 202, view)
	return nil
}

func (s *Server) cancelNativeMobileAuth(w http.ResponseWriter, r *http.Request, p Principal) error {
	view, err := s.NativeMobile.Cancel(p.ID, r.PathValue("id"), r.PathValue("transaction"))
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 200, view)
	return nil
}

func (s *Server) forgetNativeMobileSession(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SET LOCAL lock_timeout='3s'`); err != nil {
		return err
	}
	var version int64
	err = tx.QueryRow(r.Context(), `SELECT version FROM native_mobile_sessions
		WHERE account_id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&version)
	if err == pgx.ErrNoRows || err == nil && version != in.Version {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if err = mobileUnresolved(r.Context(), tx, r.PathValue("id")); err != nil {
		return err
	}
	tag, err := tx.Exec(r.Context(), `UPDATE native_mobile_sessions SET credential_sealed=NULL,
		state='revoked',version=version+1,updated_at=now() WHERE account_id=$1 AND version=$2`,
		r.PathValue("id"), in.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fail(409, "version_conflict")
	}
	if err = auditTx(r.Context(), tx, p, "", "native_mobile_session_revoked", r.PathValue("id"), nil); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	s.NativeMobile.CancelAccount(r.PathValue("id"))
	send(w, 200, map[string]any{"forgotten": true})
	return nil
}
