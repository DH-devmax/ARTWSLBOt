package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
)

// This records intent only. An unverified sender must never turn it into an invite or message.
func (s *Server) requestGroupAI(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64 `json:"version"`
		Enabled bool  `json:"enabled"`
		Invite  bool  `json:"invite"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 1 {
		return fail(400, "invalid_group_config")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	tenant, err := authorizeBusiness(r, tx, p, r.PathValue("id"), "ai")
	if err != nil {
		return err
	}
	var currentVersion int64
	var currentWorkspace string
	if err = tx.QueryRow(r.Context(), `SELECT b.version,coalesce(a.workspace,'') FROM group_bindings b
        LEFT JOIN accounts a ON a.id=b.ai_account_id WHERE b.group_id=$1`,
		r.PathValue("id")).Scan(&currentVersion, &currentWorkspace); err != nil {
		return err
	}
	if currentVersion != in.Version {
		return fail(409, "version_conflict")
	}
	if in.Enabled && currentWorkspace != "" && currentWorkspace != "maibot" {
		return fail(409, "ai_binding_requires_review")
	}
	var limit int
	if err = tx.QueryRow(r.Context(), `SELECT p.ai_group_limit FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
        WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>now()`, tenant).Scan(&limit); err != nil {
		return err
	}
	if in.Enabled {
		var used, total int
		if err = tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE tenant_id=$1),count(*) FROM group_bindings
            WHERE active AND (ai_requested OR ai_enabled) AND group_id<>$2`, tenant, r.PathValue("id")).Scan(&used, &total); err != nil {
			return err
		}
		if used >= limit || total >= control.MaxAuthorizedGroups {
			return fail(409, "ai_group_quota_exceeded")
		}
	}
	var account string
	err = tx.QueryRow(r.Context(), `SELECT a.id::text FROM accounts a LEFT JOIN account_groups ag
        ON ag.account_id=a.id AND ag.group_id=$1 WHERE a.workspace='maibot' AND a.auth_state='authenticated'
        AND ($2 OR a.id=(SELECT ai_account_id FROM group_bindings WHERE group_id=$1) OR (
          ag.role IN('member','admin','owner') AND ag.observation_source='native'
          AND ag.observed_at>now()-interval '60 seconds'))
        ORDER BY
          CASE WHEN a.id=(SELECT ai_account_id FROM group_bindings WHERE group_id=$1) THEN 0 ELSE 1 END,
          CASE WHEN ag.role IN('member','admin','owner') AND ag.observation_source='native'
            AND ag.observed_at>now()-interval '60 seconds' THEN 0 ELSE 1 END,
          (SELECT count(*) FROM group_bindings b WHERE b.ai_account_id=a.id AND b.active AND (b.ai_requested OR b.ai_enabled)),
          a.created_at,a.id LIMIT 1`, r.PathValue("id"), in.Invite).Scan(&account)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	state := "disabled"
	if in.Enabled {
		state = "pending_verification"
		if in.Invite {
			state = "pending_invite"
		}
	}
	var version int64
	err = tx.QueryRow(r.Context(), `UPDATE group_bindings SET ai_requested=$1,ai_request_state=$2,ai_enabled=false,
        ai_account_id=CASE WHEN $1 THEN coalesce(ai_account_id,NULLIF($3,'')::uuid) ELSE ai_account_id END,
        version=version+1,epoch=epoch+1 WHERE group_id=$4 AND version=$5 RETURNING version`,
		in.Enabled, state, account, r.PathValue("id"), in.Version).Scan(&version)
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "group_ai_requested", r.PathValue("id"),
		map[string]any{"enabled": in.Enabled, "invite": in.Invite, "state": state, "executed": false}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"version": version, "state": state, "enabled": false, "requested": in.Enabled,
		"executed": false, "reason": "ai_isolation_not_verified"})
	return nil
}
