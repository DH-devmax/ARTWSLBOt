package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

func (s *Server) assignmentPreview(w http.ResponseWriter, r *http.Request, p Principal) error {
	tx, err := s.DB.Platform.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if err = control.RequireGroup(r.Context(), tx, principalActor(p), r.PathValue("id")); err != nil {
		return err
	}
	out, err := control.AssignmentOptions(r.Context(), tx, r.PathValue("id"), requestWorkspace(r))
	if err != nil {
		return err
	}
	for i := range out.Candidates {
		if s.Receiver != nil {
			view := s.Receiver.ReadSnapshot(out.Candidates[i].ID)
			if view.WaitMS != nil {
				out.Candidates[i].WaitMS = *view.WaitMS
			}
		}
	}
	control.SortAssignmentCandidates(out.Candidates)
	out.Recommended = ""
	for _, c := range out.Candidates {
		if c.Eligible {
			out.Recommended = c.ID
			break
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}

func (s *Server) assignGroup(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Target       string `json:"target_account_id"`
		Source       string `json:"source_account_id"`
		SourceEpoch  int64  `json:"source_epoch"`
		TargetEpoch  int64  `json:"target_epoch"`
		Version      int64  `json:"version"`
		BindingEpoch int64  `json:"binding_epoch"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 100 {
		return fail(400, "idempotency_key_required")
	}
	if !validUUID(in.Target) || in.Source != "" && !validUUID(in.Source) || in.Version < 1 || in.TargetEpoch < 1 {
		return fail(400, "invalid_assignment")
	}
	body, _ := json.Marshal(struct {
		Group, Workspace string
		Input            any
	}{r.PathValue("id"), requestWorkspace(r), in})
	digest := security.Digest(string(body))
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	if err = control.RequireGroup(r.Context(), tx, principalActor(p), r.PathValue("id")); err != nil {
		return err
	}
	var priorHash, prior []byte
	err = tx.QueryRow(r.Context(), `SELECT request_hash,result FROM group_assignment_executions WHERE actor_id=$1 AND request_key=$2`, p.ID, key).Scan(&priorHash, &prior)
	if err == nil {
		if !bytes.Equal(digest, priorHash) {
			return fail(409, "idempotency_key_conflict")
		}
		send(w, 200, json.RawMessage(prior))
		return nil
	}
	if err != pgx.ErrNoRows {
		return err
	}
	// Lock accounts in a stable order before the binding, matching role/removal fences.
	_, err = tx.Exec(r.Context(), `SELECT id FROM accounts WHERE id=ANY($1::text[]::uuid[]) ORDER BY id FOR UPDATE`, []string{in.Target, coalesceID(in.Source, in.Target)})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `SELECT group_id FROM group_bindings WHERE group_id=$1 FOR UPDATE`, r.PathValue("id")); err != nil {
		return err
	}
	out, err := control.AssignmentOptions(r.Context(), tx, r.PathValue("id"), requestWorkspace(r))
	if err != nil {
		return err
	}
	if out.Source != in.Source || out.SourceEpoch != in.SourceEpoch || out.Version != in.Version || out.Epoch != in.BindingEpoch {
		return fail(409, "version_conflict")
	}
	if err = control.CheckAssignmentEntitlement(r.Context(), tx, out.Tenant, out.GroupID, out.Workspace); err != nil {
		return err
	}
	found := false
	for _, c := range out.Candidates {
		if c.ID == in.Target {
			if c.Epoch != in.TargetEpoch {
				return fail(409, "version_conflict")
			}
			if !c.Eligible {
				return fail(409, "assignment_target_ineligible")
			}
			found = true
		}
	}
	if !found {
		return fail(404, "account_not_found")
	}
	if err = control.CheckAccountCapacity(r.Context(), tx, in.Target, out.GroupID); err != nil {
		return err
	}
	var pending bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM operations WHERE group_id=$1 AND status IN('running','unknown'))
        OR EXISTS(SELECT 1 FROM native_setting_operations WHERE group_id=$1 AND status IN('preparing','submitting','unknown'))`, out.GroupID).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return fail(409, "assignment_operations_pending")
	}
	version := out.Version
	if in.Target != out.Source {
		column := "primary_account_id"
		if out.Workspace == "maibot" {
			column = "ai_account_id"
		}
		err = tx.QueryRow(r.Context(), `UPDATE group_bindings SET `+column+`=$1,version=version+1,epoch=epoch+1
            WHERE group_id=$2 AND version=$3 RETURNING version`, in.Target, out.GroupID, out.Version).Scan(&version)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='group_account_changed',updated_at=now()
            WHERE group_id=$1 AND status='queued'`, out.GroupID); err != nil {
			return err
		}
		if err = auditTx(r.Context(), tx, p, out.Tenant, "group_account_assigned", out.GroupID, map[string]any{
			"source_account_id": in.Source, "target_account_id": in.Target, "workspace": out.Workspace, "version": version}); err != nil {
			return err
		}
	}
	result := map[string]any{"version": version, "account_id": in.Target, "state": "binding_saved", "executed": false}
	raw, _ := json.Marshal(result)
	if _, err = tx.Exec(r.Context(), `INSERT INTO group_assignment_executions(actor_id,request_key,request_hash,group_id,result)
        VALUES($1,$2,$3,$4,$5)`, p.ID, key, digest, out.GroupID, raw); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return fail(503, "assignment_commit_unknown")
	}
	send(w, 200, result)
	return nil
}

func coalesceID(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
