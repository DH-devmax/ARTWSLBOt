package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func signupBlocked(ctx context.Context, tx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, account string) error {
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM native_signup_attempts
        WHERE account_id=$1 AND state IN ('submitted','registered')`, account).Scan(&state)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if state == "submitted" {
		return &nativeauth.Error{Code: "native_signup_review_required"}
	}
	if state == "registered" {
		return &nativeauth.Error{Code: "native_signup_already_registered"}
	}
	return nil
}

func (s *Server) startNativeSignup(w http.ResponseWriter, r *http.Request, p Principal) error {
	s.accountLifecycle.RLock()
	defer s.accountLifecycle.RUnlock()
	var in struct {
		AccountID string `json:"account_id"`
		Version   int64  `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !validUUID(in.AccountID) {
		return fail(400, "invalid_input")
	}
	var version int64
	var empty bool
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT version,
        native_uid IS NULL AND credential_sealed IS NULL AND NOT connection_enabled
        FROM accounts WHERE id=$1 AND workspace=$2`, in.AccountID, requestWorkspace(r)).Scan(&version, &empty)
	if err == pgx.ErrNoRows {
		return fail(404, "account_not_found")
	}
	if err != nil {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	if !empty {
		return fail(409, "native_signup_empty_panel_required")
	}
	d, deploymentVersion, err := s.loadNativeDeployment(r.Context())
	if err != nil {
		return err
	}
	if err = signupBlocked(r.Context(), s.DB.Platform, in.AccountID); err != nil {
		return nativeFailure(err)
	}
	cookie, _ := r.Cookie("wsl_session")
	sessionHash := security.Digest(cookie.Value)
	view, err := s.Native.StartSignup(p.ID, in.AccountID, d, nativeauth.SignupHooks{
		BeforeSubmit: func(ctx context.Context, id string) error {
			tx, err := s.DB.Platform.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			var currentDeployment int64
			if err = tx.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton FOR SHARE`).
				Scan(&currentDeployment); err != nil || currentDeployment != deploymentVersion {
				return &nativeauth.Error{Code: "native_deployment_changed"}
			}
			var actor string
			if err = tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN sessions se ON se.user_id=u.id
                WHERE u.id=$1 AND u.role='superadmin' AND NOT u.disabled AND se.token_hash=$2 AND se.expires_at>now()
                FOR SHARE OF u,se`, p.ID, sessionHash).Scan(&actor); err != nil {
				return &nativeauth.Error{Code: "native_site_session_expired"}
			}
			var current int64
			if err = tx.QueryRow(ctx, `SELECT version FROM accounts WHERE id=$1
                AND native_uid IS NULL AND credential_sealed IS NULL AND NOT connection_enabled
                FOR UPDATE`, in.AccountID).Scan(&current); err != nil || current != version {
				return &nativeauth.Error{Code: "native_account_changed"}
			}
			if err = signupBlocked(ctx, tx, in.AccountID); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO native_signup_attempts
                (transaction_id,account_id,actor_id,deployment_version,account_version,state)
                VALUES($1,$2,$3,$4,$5,'submitted')
                ON CONFLICT(transaction_id) DO UPDATE SET state='submitted',result_code='',updated_at=now()
                WHERE native_signup_attempts.state='rejected'`,
				id, in.AccountID, p.ID, deploymentVersion, version)
			if err != nil {
				return err
			}
			if err = auditTx(ctx, tx, p, "", "native_signup_submitted", in.AccountID,
				map[string]any{"transaction_id": id, "deployment_version": deploymentVersion}); err != nil {
				return err
			}
			// This durable barrier commits before the non-idempotent remote request.
			return tx.Commit(ctx)
		},
		Complete: func(ctx context.Context, id, registered string, outcome error) error {
			state, code := "registered", ""
			if outcome != nil {
				state, code = "submitted", "native_signup_review_required"
				var native *nativeauth.Error
				if errors.As(outcome, &native) {
					code = native.Code
					if !native.Submitted || native.Number != 0 &&
						native.Code != "native_transport_unknown" && native.Code != "native_response_invalid" &&
						native.Code != "native_http_rejected" {
						state = "rejected"
					}
				}
			}
			tx, err := s.DB.Platform.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			tag, err := tx.Exec(ctx, `UPDATE native_signup_attempts SET state=$1,
                registered_account=NULLIF($2,''),result_code=$3,updated_at=now()
                WHERE transaction_id=$4 AND account_id=$5 AND actor_id=$6 AND state='submitted'`,
				state, registered, code, id, in.AccountID, p.ID)
			var postgres *pgconn.PgError
			if errors.As(err, &postgres) && postgres.Code == "23505" {
				return &nativeauth.Error{Code: "native_identity_already_bound"}
			}
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return &nativeauth.Error{Code: "native_signup_record_pending"}
			}
			if err = auditTx(ctx, tx, p, "", "native_signup_"+state, in.AccountID,
				map[string]any{"transaction_id": id, "result_code": code, "registered_account": registered}); err != nil {
				return err
			}
			return tx.Commit(ctx)
		},
	}, s.nativeRequestIDs(deploymentVersion))
	if err != nil {
		return nativeFailure(err)
	}
	send(w, 201, view)
	return nil
}
