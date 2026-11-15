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
	service, err := control.HeatToggleEntitlement(r.Context(), tx, principalActor(p), tenantPermissions,
		requestWorkspace(r), r.PathValue("id"))
	if err != nil {
		return err
	}
	tenant := service.TenantID
	var currentVersion int64
	var currentWorkspace, currentAccount string
	if err = tx.QueryRow(r.Context(), `SELECT b.version,coalesce(a.workspace,''),coalesce(b.ai_account_id::text,'') FROM group_bindings b
        LEFT JOIN accounts a ON a.id=b.ai_account_id WHERE b.group_id=$1`,
		r.PathValue("id")).Scan(&currentVersion, &currentWorkspace, &currentAccount); err != nil {
		return err
	}
	if currentVersion != in.Version {
		return fail(409, "version_conflict")
	}
	if in.Enabled && currentWorkspace != "" && currentWorkspace != "maibot" {
		return fail(409, "ai_binding_requires_review")
	}
	if in.Enabled {
		if err = control.CheckAssignmentEntitlement(r.Context(), tx, tenant, r.PathValue("id"), "maibot"); err != nil {
			if denied, ok := err.(*control.Failure); ok && denied.Code == "group_quota_exceeded" {
				return fail(409, "ai_group_quota_exceeded")
			}
			return err
		}
	}
	account := currentAccount
	if in.Enabled && account == "" {
		options, optionErr := control.AssignmentOptions(r.Context(), tx, r.PathValue("id"), "maibot")
		if optionErr != nil {
			return optionErr
		}
		account = options.Recommended
	}
	if in.Enabled && account != "" {
		if err = control.CheckAccountCapacity(r.Context(), tx, account, r.PathValue("id")); err != nil {
			return err
		}
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
	running, reason := false, "heat_chat_group_not_enabled"
	if in.Enabled && !in.Invite && s.HeatChat != nil {
		running, reason, err = s.HeatChat.ActivateRequested(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if running {
			state = "ready"
		}
	}
	if err = auditTx(r.Context(), tx, p, tenant, "group_ai_requested", r.PathValue("id"),
		map[string]any{"requested": in.Enabled, "enabled": running, "invite": in.Invite, "state": state, "executed": false}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"version": version, "state": state, "enabled": running, "requested": in.Enabled,
		"executed": false, "reason": reason})
	return nil
}
