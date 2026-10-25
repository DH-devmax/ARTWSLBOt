package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func nativeFailure(err error) error {
	var native *nativeauth.Error
	if errors.As(err, &native) {
		switch native.Code {
		case "native_crypto_unavailable":
			return fail(503, native.Code)
		case "native_auth_not_found":
			return fail(404, native.Code)
		case "native_auth_expired":
			return fail(410, native.Code)
		case "native_auth_state_conflict", "native_auth_busy", "native_auth_account_busy", "native_auth_review_required",
			"native_signup_review_required", "native_signup_already_registered":
			return fail(409, native.Code)
		case "native_auth_cooldown", "native_auth_rate_limited", "native_auth_limit":
			return fail(429, native.Code)
		default:
			return fail(400, native.Code)
		}
	}
	return err
}

func (s *Server) nativeRequestIDs(version int64) nativeauth.RequestIDSource {
	return func(ctx context.Context, current bool, now uint64) (uint64, error) {
		if now > 1<<63-2 {
			return 0, &nativeauth.Error{Code: "native_request_id_exhausted"}
		}
		statement := `UPDATE native_deployment SET request_id_ns=GREATEST(request_id_ns+1,$1)
            WHERE singleton AND version=$2 AND request_id_ns<9223372036854775807 RETURNING request_id_ns`
		if current {
			statement = `UPDATE native_deployment SET request_id_ms=GREATEST(request_id_ms+1,$1)
                WHERE singleton AND version=$2 AND request_id_ms<9223372036854775807 RETURNING request_id_ms`
		}
		var id int64
		err := s.DB.Platform.QueryRow(ctx, statement, int64(now), version).Scan(&id)
		if err == pgx.ErrNoRows {
			return 0, &nativeauth.Error{Code: "native_deployment_changed"}
		}
		if err != nil {
			return 0, &nativeauth.Error{Code: "native_request_id_unavailable"}
		}
		return uint64(id), nil
	}
}

func (s *Server) loadNativeDeployment(ctx context.Context) (nativeauth.Deployment, int64, error) {
	var sealed []byte
	var version int64
	err := s.DB.Platform.QueryRow(ctx, `SELECT sealed,version FROM native_deployment WHERE singleton`).Scan(&sealed, &version)
	if err == pgx.ErrNoRows {
		return nativeauth.Deployment{}, 0, fail(409, "native_deployment_missing")
	}
	if err != nil {
		return nativeauth.Deployment{}, 0, err
	}
	plain, err := s.Vault.Open("native-deployment:v1", sealed)
	if err != nil {
		return nativeauth.Deployment{}, 0, fail(503, "native_deployment_unreadable")
	}
	defer clear(plain)
	deployment, err := nativeauth.ParseDeployment(plain)
	if err != nil {
		return nativeauth.Deployment{}, 0, fail(503, "native_deployment_invalid")
	}
	return deployment, version, nil
}

func (s *Server) nativeDeployment(w http.ResponseWriter, r *http.Request, p Principal) error {
	d, version, err := s.loadNativeDeployment(r.Context())
	if err != nil {
		var api *apiError
		if errors.As(err, &api) && api.code == "native_deployment_missing" {
			send(w, 200, map[string]any{"configured": false, "version": 0, "crypto_ready": nativewire.Available()})
			return nil
		}
		return err
	}
	send(w, 200, map[string]any{"configured": true, "version": version, "origin": d.Origin,
		"protocol_version": d.ProtocolVersion, "crypto_ready": nativewire.Available()})
	return nil
}

func (s *Server) saveNativeDeployment(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version       int64  `json:"version"`
		Configuration string `json:"configuration"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	d, err := nativeauth.ParseDeployment([]byte(in.Configuration))
	if err != nil {
		return fail(400, "native_deployment_invalid")
	}
	canonical, err := json.Marshal(d)
	if err != nil {
		return err
	}
	defer clear(canonical)
	sealed, err := s.Vault.Seal("native-deployment:v1", canonical)
	if err != nil {
		return err
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798426)`); err != nil {
		return err
	}
	var version int64
	err = tx.QueryRow(r.Context(), `SELECT version FROM native_deployment WHERE singleton FOR UPDATE`).Scan(&version)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if version != in.Version || in.Version < 0 {
		return fail(409, "version_conflict")
	}
	if err = tx.QueryRow(r.Context(), `INSERT INTO native_deployment(singleton,sealed) VALUES(true,$1)
        ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,version=native_deployment.version+1,updated_at=now()
        RETURNING version`, sealed).Scan(&version); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE accounts SET auth_state='reauth_required',epoch=epoch+1,version=version+1
        WHERE credential_sealed IS NOT NULL`); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "native_deployment_updated", "platform",
		map[string]any{"version": version, "protocol_version": d.ProtocolVersion}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	s.Native.Close()
	send(w, 200, map[string]any{"version": version, "configured": true})
	return nil
}

func (s *Server) startNativeAuth(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	d, deploymentVersion, err := s.loadNativeDeployment(r.Context())
	if err != nil {
		return err
	}
	account := r.PathValue("id")
	var version int64
	var connected bool
	if err = s.DB.Platform.QueryRow(r.Context(), `SELECT version,connection_enabled FROM accounts WHERE id=$1`, account).
		Scan(&version, &connected); err == pgx.ErrNoRows {
		return fail(404, "account_not_found")
	} else if err != nil {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	if connected {
		return fail(409, "native_disconnect_before_login")
	}
	var unresolved bool
	if err = s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM native_signup_attempts
        WHERE account_id=$1 AND state='submitted')`, account).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved {
		return fail(409, "native_signup_review_required")
	}
	cookie, _ := r.Cookie("wsl_session")
	sessionHash := security.Digest(cookie.Value)
	view, err := s.Native.Start(p.ID, account, d, func(ctx context.Context, session nativeauth.Session, nickname string) error {
		// Recheck the site session, account and deployment at the only credential commit boundary.
		tx, err := s.DB.Platform.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var currentDeployment int64
		if err = tx.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton FOR SHARE`).Scan(&currentDeployment); err != nil || currentDeployment != deploymentVersion {
			return &nativeauth.Error{Code: "native_deployment_changed"}
		}
		var actor string
		if err = tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN sessions se ON se.user_id=u.id
            WHERE u.id=$1 AND u.role='superadmin' AND NOT u.disabled AND se.token_hash=$2 AND se.expires_at>now()
            FOR SHARE OF u,se`, p.ID, sessionHash).Scan(&actor); err != nil {
			return &nativeauth.Error{Code: "native_site_session_expired"}
		}
		if session.UID == 0 || session.UID >= 1<<31 || session.JWT == "" || session.NIMID == "" || session.NIMToken == "" {
			return &nativeauth.Error{Code: "native_login_identity"}
		}
		var registered string
		err = tx.QueryRow(ctx, `SELECT registered_account FROM native_signup_attempts
            WHERE account_id=$1 AND state='registered'`, account).Scan(&registered)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if registered != "" && registered != session.AccountID {
			return &nativeauth.Error{Code: "native_signup_identity_mismatch"}
		}
		payload, err := json.Marshal(struct {
			DeploymentVersion int64              `json:"deployment_version"`
			Session           nativeauth.Session `json:"session"`
		}{deploymentVersion, session})
		if err != nil {
			return err
		}
		defer clear(payload)
		sealed, err := s.Vault.Seal("native-session:"+account, payload)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE accounts SET native_uid=$1,credential_sealed=$2,auth_state='authenticated',
            authenticated_at=now(),native_nickname=$3,version=version+1,epoch=epoch+1
            WHERE id=$4 AND version=$5 AND NOT connection_enabled AND (native_uid IS NULL OR native_uid=$1)`,
			strconv.FormatUint(session.UID, 10), sealed, nickname, account, version)
		var postgres *pgconn.PgError
		if errors.As(err, &postgres) && postgres.Code == "23505" {
			return &nativeauth.Error{Code: "native_identity_already_bound"}
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return &nativeauth.Error{Code: "native_account_changed"}
		}
		if _, err = tx.Exec(ctx, `UPDATE operations SET status='cancelled',result_code='account_reauthenticated',updated_at=now()
            WHERE account_id=$1 AND status='queued'`, account); err != nil {
			return err
		}
		if err = auditTx(ctx, tx, p, "", "native_authenticated", account,
			map[string]any{"native_uid": session.UID, "deployment_version": deploymentVersion}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}, s.nativeRequestIDs(deploymentVersion))
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 201, view)
	return nil
}

func (s *Server) pollNativeAuth(w http.ResponseWriter, r *http.Request, p Principal) error {
	view, err := s.Native.Poll(p.ID, r.PathValue("id"), r.PathValue("transaction"))
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 200, view)
	return nil
}

func (s *Server) nativeAuthAction(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in nativeauth.Input
	if err := decode(w, r, &in); err != nil {
		return err
	}
	view, err := s.Native.Action(p.ID, r.PathValue("id"), r.PathValue("transaction"), in)
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 202, view)
	return nil
}

func (s *Server) cancelNativeAuth(w http.ResponseWriter, r *http.Request, p Principal) error {
	view, err := s.Native.Cancel(p.ID, r.PathValue("id"), r.PathValue("transaction"))
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 200, view)
	return nil
}

func (s *Server) forgetNativeSession(w http.ResponseWriter, r *http.Request, p Principal) error {
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
	tag, err := tx.Exec(r.Context(), `UPDATE accounts SET credential_sealed=NULL,auth_state='not_authenticated',
        authenticated_at=NULL,version=version+1,epoch=epoch+1
        WHERE id=$1 AND version=$2 AND NOT connection_enabled`, r.PathValue("id"), in.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fail(409, "version_conflict")
	}
	if err = auditTx(r.Context(), tx, p, "", "native_session_forgotten", r.PathValue("id"),
		map[string]any{"previous_version": in.Version, "at": time.Now().UTC()}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	s.Native.CancelAccount(r.PathValue("id"))
	send(w, 200, map[string]bool{"forgotten": true})
	return nil
}
