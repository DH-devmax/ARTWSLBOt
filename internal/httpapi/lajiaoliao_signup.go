package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

func (s *Server) chiliSignupStatus(w http.ResponseWriter, r *http.Request, p Principal) error {
	cookie, _ := r.Cookie("wsl_session")
	if cookie == nil {
		return fail(401, "authentication_required")
	}
	var state string
	var allowed bool
	var expires, resend *time.Time
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT state,coalesce(sms_actor_id=$2 AND sms_session_hash=$3
        AND sms_expires_at>now() AND sms_phone_sealed IS NOT NULL AND sms_attempts<5,false),sms_expires_at,sms_resend_at
        FROM lajiaoliao_auth_state WHERE account_id=$1`, r.PathValue("id"), p.ID, security.Digest(cookie.Value)).
		Scan(&state, &allowed, &expires, &resend)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	send(w, 200, map[string]any{"sms_ready": allowed && state == "ready", "review_required": state != "" && state != "ready",
		"expires_at": expires, "resend_at": resend, "native_ready": false})
	return nil
}

func (s *Server) chiliSignupSMS(w http.ResponseWriter, r *http.Request, p Principal) error {
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		return fail(429, "native_auth_limit")
	}
	var in struct {
		Version int64  `json:"version"`
		Phone   string `json:"phone"`
		Proof   string `json:"captcha_verify_param"`
		Agreed  bool   `json:"agreed"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	defer func() { in.Phone = ""; in.Proof = "" }()
	if !in.Agreed || in.Version < 1 || len(in.Phone) != 11 || in.Phone[0] != '1' ||
		in.Phone[1] < '3' || in.Phone[1] > '9' || strings.Trim(in.Phone, "0123456789") != "" || len(in.Proof) > 32768 {
		return fail(400, "invalid_request")
	}
	cookie, _ := r.Cookie("wsl_session")
	if cookie == nil {
		return fail(401, "authentication_required")
	}
	s.accountLifecycle.RLock()
	defer s.accountLifecycle.RUnlock()
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	// One phone cannot obtain concurrent SMS sends from different empty panels.
	fingerprint := s.Vault.Fingerprint("lajiaoliao-signup-phone", []byte(in.Phone))
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fingerprint); err != nil {
		return err
	}
	var version, attempt int64
	var uid, device, state string
	var connected, cooling bool
	err = tx.QueryRow(r.Context(), `SELECT a.version,coalesce(a.native_uid,''),a.connection_enabled OR EXISTS(
        SELECT 1 FROM account_receivers WHERE account_id=a.id AND enabled),st.device_id,st.state,
        coalesce(st.sms_resend_at>now(),false)
        FROM accounts a JOIN lajiaoliao_auth_state st ON st.account_id=a.id
        WHERE a.id=$1 AND a.platform='lajiaoliao' FOR UPDATE OF a,st`, r.PathValue("id")).
		Scan(&version, &uid, &connected, &device, &state, &cooling)
	if err == pgx.ErrNoRows {
		return fail(409, "lajiaoliao_challenge_required")
	}
	if err != nil {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	if uid != "" || connected {
		return fail(409, "lajiaoliao_signup_existing_identity")
	}
	if state != "ready" {
		return fail(409, "lajiaoliao_login_review_required")
	}
	if cooling {
		return fail(429, "lajiaoliao_sms_cooldown")
	}
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM lajiaoliao_auth_state
        WHERE sms_phone_fingerprint=$1 AND sms_resend_at>now())`, fingerprint).Scan(&cooling); err != nil {
		return err
	}
	if cooling {
		return fail(429, "lajiaoliao_sms_cooldown")
	}
	sealed, err := s.Vault.Seal("lajiaoliao-signup-phone:"+r.PathValue("id"), []byte(in.Phone))
	if err != nil {
		return err
	}
	if err = tx.QueryRow(r.Context(), `UPDATE lajiaoliao_auth_state SET state='submitting',actor_id=$2,
        attempted_at=now(),error='',version=version+1,sms_actor_id=$2,sms_session_hash=$3,
        sms_phone_sealed=$4,sms_phone_fingerprint=$5,sms_resend_at=now()+interval '60 seconds',
        sms_expires_at=NULL,sms_attempts=0 WHERE account_id=$1 RETURNING version`,
		r.PathValue("id"), p.ID, security.Digest(cookie.Value), sealed, fingerprint).Scan(&attempt); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	finalized := false
	defer func() {
		if finalized {
			return
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer cancel()
		_, _ = s.DB.Platform.Exec(ctx, `UPDATE lajiaoliao_auth_state SET state='unknown',error='lajiaoliao_result_unknown'
            WHERE account_id=$1 AND version=$2 AND state='submitting'`, r.PathValue("id"), attempt)
	}()
	client := s.newChiliAuthClient()
	defer client.Close()
	registrar, ok := client.(chiliRegistrar)
	if !ok {
		return fail(503, "lajiaoliao_signup_unavailable")
	}
	if err = registrar.SignupSMS(r.Context(), in.Phone, in.Proof, device); err != nil {
		// Unknown transport outcomes remain fenced; only known rejection allows retry.
		state := "unknown"
		code := "lajiaoliao_result_unknown"
		retry := time.Minute
		if e, ok := err.(*lajiaoliao.Failure); ok {
			code = e.Code
			retry = max(retry, e.RetryAfter)
			if !e.Submitted || e.Code == "lajiaoliao_request_rejected" {
				state = "ready"
			}
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer cancel()
		_, _ = s.DB.Platform.Exec(ctx, `UPDATE lajiaoliao_auth_state SET state=$3,error=$4,sms_phone_sealed=NULL,
            sms_resend_at=greatest(sms_resend_at,now()+$5::bigint*interval '1 second')
            WHERE account_id=$1 AND version=$2 AND state='submitting'`, r.PathValue("id"), attempt, state, code, int64(retry/time.Second))
		return chiliFailure(err)
	}
	// Successful delivery alone does not create or log in a native account.
	var expires, resend time.Time
	tx, err = s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(r.Context(), `UPDATE lajiaoliao_auth_state st SET state='ready',
        sms_expires_at=now()+interval '10 minutes' WHERE account_id=$1 AND version=$2 AND state='submitting'
        AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=st.account_id AND a.version=$3 AND a.native_uid IS NULL)
        AND EXISTS(SELECT 1 FROM users u JOIN sessions se ON se.user_id=u.id
            WHERE u.id=$4 AND NOT u.disabled AND u.role='superadmin' AND se.token_hash=$5 AND se.expires_at>now())
        RETURNING sms_expires_at,sms_resend_at`, r.PathValue("id"), attempt, in.Version, p.ID, security.Digest(cookie.Value)).
		Scan(&expires, &resend)
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "lajiaoliao_signup_sms_requested", r.PathValue("id"),
		map[string]any{"adapter": "desktop", "expires_at": expires}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	finalized = true
	send(w, 200, map[string]any{"status": "sms_required", "expires_at": expires, "resend_at": resend, "native_ready": false})
	return nil
}
