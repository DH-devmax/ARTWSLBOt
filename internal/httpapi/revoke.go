package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (s *Server) revokeGroup(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 1 {
		return fail(400, "invalid_request")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	var tenant string
	var version int64
	err = tx.QueryRow(r.Context(), `SELECT tenant_id::text,version FROM group_bindings WHERE group_id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&tenant, &version)
	if err == pgx.ErrNoRows || (err == nil && p.Role != "superadmin" && tenant != p.TenantID) {
		return fail(404, "group_not_found")
	}
	if err != nil {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	if _, err = tx.Exec(r.Context(), `UPDATE group_bindings SET active=false,management_enabled=false,ai_enabled=false,
        ai_requested=false,ai_request_state='disabled',version=version+1,epoch=epoch+1 WHERE group_id=$1`, r.PathValue("id")); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='authorization_revoked',updated_at=now()
        WHERE group_id=$1 AND status='queued'`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "group_authorization_revoked", r.PathValue("id"), map[string]any{"version": version + 1}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"active": false, "version": version + 1})
	return nil
}
