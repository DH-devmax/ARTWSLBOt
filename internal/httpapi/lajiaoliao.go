package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

type chiliAuthenticator interface {
	Challenge(context.Context, string) (lajiaoliao.Challenge, error)
	Login(context.Context, string, string, string, string, string) (lajiaoliao.Session, error)
	Close()
}

type chiliRegistrar interface {
	SignupSMS(context.Context, string, string, string) error
	Signup(context.Context, string, string, string) (lajiaoliao.Session, error)
}

// The API owns one bounded transport, not a client/connection per dialog.
// Account tokens and devices remain request-local. Idle sockets expire in 1m.
type borrowedChiliAuth struct{ *lajiaoliao.Client }

func (borrowedChiliAuth) Close() {}

func (s *Server) newChiliAuthClient() chiliAuthenticator {
	if s.chiliAuthClient != nil {
		return s.chiliAuthClient()
	}
	s.chiliAuthOnce.Do(func() { s.chiliSharedAuth = borrowedChiliAuth{lajiaoliao.NewClient()} })
	return s.chiliSharedAuth
}

func chiliFailure(err error) error {
	var e *lajiaoliao.Failure
	if errors.As(err, &e) {
		status := 400
		if e.Code == "lajiaoliao_rate_limited" {
			status = 429
		}
		if e.Code == "lajiaoliao_result_unknown" || e.Code == "lajiaoliao_device_confirmation_required" {
			status = 409
		}
		return fail(status, e.Code)
	}
	return fail(503, "lajiaoliao_unavailable")
}

func (s *Server) chiliChallenge(w http.ResponseWriter, r *http.Request, p Principal) error {
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		return fail(429, "native_auth_limit")
	}
	id, err := lajiaoliao.NewDeviceID()
	if err != nil {
		return err
	}
	var device, state string
	err = s.DB.Platform.QueryRow(r.Context(), `INSERT INTO lajiaoliao_auth_state(account_id,device_id)
        SELECT id,$2 FROM accounts WHERE id=$1 AND platform='lajiaoliao'
        ON CONFLICT(account_id) DO UPDATE SET device_id=lajiaoliao_auth_state.device_id
        RETURNING device_id,state`, r.PathValue("id"), id).Scan(&device, &state)
	if err == pgx.ErrNoRows {
		return fail(404, "account_not_found")
	}
	if err != nil {
		return err
	}
	if state != "ready" {
		return fail(409, "lajiaoliao_login_review_required")
	}
	if _, err = s.DB.Platform.Exec(r.Context(), `UPDATE lajiaoliao_auth_state SET sms_phone_sealed=NULL,
        sms_actor_id=NULL,sms_session_hash=NULL,sms_expires_at=NULL
        WHERE account_id=$1 AND sms_expires_at<=now()`, r.PathValue("id")); err != nil {
		return err
	}
	client := s.newChiliAuthClient()
	defer client.Close()
	challenge, err := client.Challenge(r.Context(), device)
	if err != nil {
		return chiliFailure(err)
	}
	send(w, 200, map[string]any{"challenge": challenge, "native_ready": false, "adapter": "desktop"})
	return nil
}

func (s *Server) chiliLogin(w http.ResponseWriter, r *http.Request, p Principal) error {
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		return fail(429, "native_auth_limit")
	}
	var in struct {
		Version  int64  `json:"version"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
		Proof    string `json:"captcha_verify_param"`
		Code     string `json:"code"`
		Agreed   bool   `json:"agreed"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	defer func() { in.Password = ""; in.Proof = ""; in.Code = "" }()
	signup := strings.HasSuffix(r.URL.Path, "/lajiaoliao-signup")
	if in.Version < 1 || len(in.Phone) > 20 || len(in.Password) > 256 || len(in.Proof) > 32768 ||
		(signup && (!in.Agreed || len(in.Code) != 6 || strings.Trim(in.Code, "0123456789") != "" ||
			in.Phone != "" || in.Password != "" || in.Proof != "")) {
		return fail(400, "invalid_request")
	}
	s.accountLifecycle.RLock()
	defer s.accountLifecycle.RUnlock()
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var uid, device, state string
	var version int64
	var attempt int64
	var connected bool
	var smsAllowed bool
	var phoneSealed []byte
	cookie, _ := r.Cookie("wsl_session")
	if cookie == nil {
		return fail(401, "authentication_required")
	}
	err = tx.QueryRow(r.Context(), `SELECT a.version,coalesce(a.native_uid,''),a.connection_enabled OR EXISTS(
        SELECT 1 FROM account_receivers WHERE account_id=a.id AND enabled),st.device_id,st.state,
        st.sms_phone_sealed,coalesce(st.sms_actor_id=$2 AND st.sms_session_hash=$3
            AND st.sms_expires_at>now() AND st.sms_attempts<5,false)
        FROM accounts a JOIN lajiaoliao_auth_state st ON st.account_id=a.id
        WHERE a.id=$1 AND a.platform='lajiaoliao' FOR UPDATE OF a,st`,
		r.PathValue("id"), p.ID, security.Digest(cookie.Value)).Scan(&version, &uid, &connected, &device, &state, &phoneSealed, &smsAllowed)
	if err == pgx.ErrNoRows {
		return fail(409, "lajiaoliao_challenge_required")
	}
	if err != nil {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	if connected {
		return fail(409, "native_disconnect_before_login")
	}
	if state != "ready" {
		return fail(409, "lajiaoliao_login_review_required")
	}
	if signup {
		if uid != "" || !smsAllowed || len(phoneSealed) == 0 {
			return fail(409, "lajiaoliao_sms_flow_expired")
		}
		phone, err := s.Vault.Open("lajiaoliao-signup-phone:"+r.PathValue("id"), phoneSealed)
		if err != nil {
			return err
		}
		in.Phone = string(phone)
		clear(phone)
	}
	if err = tx.QueryRow(r.Context(), `UPDATE lajiaoliao_auth_state SET state='submitting',actor_id=$2,
        attempted_at=now(),version=version+1,error='',sms_attempts=sms_attempts+CASE WHEN $3 THEN 1 ELSE 0 END
        WHERE account_id=$1 RETURNING version`, r.PathValue("id"), p.ID, signup).Scan(&attempt); err != nil {
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
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer cancel()
		_, _ = s.DB.Platform.Exec(cleanup, `UPDATE lajiaoliao_auth_state SET state='unknown',error='lajiaoliao_result_unknown'
            WHERE account_id=$1 AND state='submitting' AND version=$2`, r.PathValue("id"), attempt)
	}()
	client := s.newChiliAuthClient()
	defer client.Close()
	var session lajiaoliao.Session
	var loginErr error
	if signup {
		if registrar, ok := client.(chiliRegistrar); ok {
			session, loginErr = registrar.Signup(r.Context(), in.Phone, in.Code, device)
		} else {
			loginErr = &lajiaoliao.Failure{Code: "lajiaoliao_signup_unavailable"}
		}
	} else {
		session, loginErr = client.Login(r.Context(), in.Phone, in.Password, in.Proof, device, uid)
	}
	in.Password = ""
	in.Proof = ""
	if loginErr != nil {
		// A transport loss after submission is never turned into a retryable login.
		state = "unknown"
		code := "lajiaoliao_result_unknown"
		var failure *lajiaoliao.Failure
		if errors.As(loginErr, &failure) {
			code = failure.Code
			if !failure.Submitted || code == "lajiaoliao_request_rejected" {
				state = "ready"
			}
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer cancel()
		_, _ = s.DB.Platform.Exec(cleanup, `UPDATE lajiaoliao_auth_state SET state=$2,error=$3
            WHERE account_id=$1 AND state='submitting' AND version=$4`, r.PathValue("id"), state, code, attempt)
		return chiliFailure(loginErr)
	}
	plain, err := json.Marshal(session)
	if err != nil {
		return err
	}
	defer clear(plain)
	sealed, err := s.Vault.Seal("lajiaoliao-session:"+r.PathValue("id"), plain)
	if err != nil {
		return err
	}
	tx, err = s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var actor string
	if err = tx.QueryRow(r.Context(), `SELECT u.id::text FROM users u JOIN sessions se ON se.user_id=u.id
        WHERE u.id=$1 AND u.role='superadmin' AND NOT u.disabled AND se.token_hash=$2 AND se.expires_at>now()
        FOR SHARE OF u,se`, p.ID, security.Digest(cookie.Value)).Scan(&actor); err != nil {
		return fail(401, "authentication_required")
	}
	err = tx.QueryRow(r.Context(), `UPDATE accounts SET native_uid=$3,credential_sealed=$4,auth_state='authenticated',
        authenticated_at=now(),epoch=epoch+1,version=version+1 WHERE id=$1 AND version=$2 AND platform='lajiaoliao'
        AND NOT connection_enabled AND (native_uid IS NULL OR native_uid=$3)
        AND NOT EXISTS(SELECT 1 FROM account_receivers WHERE account_id=accounts.id AND enabled)
        RETURNING version`, r.PathValue("id"), in.Version, session.UserID, sealed).Scan(&version)
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return fail(409, "native_identity_already_connected")
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(r.Context(), `UPDATE lajiaoliao_auth_state SET state='ready',error='',
        sms_phone_sealed=NULL,sms_expires_at=NULL,sms_session_hash=NULL,sms_actor_id=NULL
        WHERE account_id=$1 AND version=$2 AND state='submitting'`, r.PathValue("id"), attempt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fail(409, "version_conflict")
	}
	if err = auditTx(r.Context(), tx, p, "", "lajiaoliao_authenticated", r.PathValue("id"), map[string]any{"version": version, "adapter": "desktop"}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	finalized = true
	send(w, 200, map[string]any{"version": version, "auth_state": "authenticated", "native_ready": false})
	return nil
}
