package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type removalPreview struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	Workspace          string           `json:"workspace"`
	Version            int64            `json:"version"`
	Cleanup            map[string]int64 `json:"cleanup"`
	Retained           map[string]int64 `json:"retained"`
	Blockers           []string         `json:"blockers"`
	ManagementGroupIDs []string         `json:"management_group_ids"`
}

func readRemovalPreview(r *http.Request, tx pgx.Tx, lock bool) (removalPreview, error) {
	view := removalPreview{Blockers: []string{}, ManagementGroupIDs: []string{}}
	query := `SELECT id::text,name,workspace,version FROM accounts WHERE id=$1 AND workspace=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRow(r.Context(), query, r.PathValue("id"), requestWorkspace(r)).Scan(
		&view.ID, &view.Name, &view.Workspace, &view.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, fail(404, "account_not_found")
	}
	if err != nil {
		return view, err
	}
	rows, err := tx.Query(r.Context(), `SELECT group_id::text FROM group_bindings WHERE primary_account_id=$1 ORDER BY group_id`, view.ID)
	if err != nil {
		return view, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return view, err
		}
		view.ManagementGroupIDs = append(view.ManagementGroupIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return view, err
	}
	var cleanup, retained []byte
	var submitted, unresolved int64
	err = tx.QueryRow(r.Context(), `SELECT
        jsonb_build_object(
          'messages',(SELECT count(*) FROM inbox WHERE account_id=$1),
          'memberships',(SELECT count(*) FROM account_groups WHERE account_id=$1),
          'personas',(SELECT count(*) FROM personas WHERE account_id=$1),
          'settings',(SELECT count(*) FROM maibot_settings WHERE account_id=$1),
          'claim_codes',(SELECT count(*) FROM claim_codes WHERE account_id=$1),
          'claims',(SELECT count(*) FROM group_claims WHERE account_id=$1),
          'signup_attempts',(SELECT count(*) FROM native_signup_attempts WHERE account_id=$1),
          'operations',(SELECT count(*) FROM operations WHERE account_id=$1),
          'test_windows',(SELECT count(*) FROM test_windows WHERE account_ids=ARRAY[$1::uuid]),
          'shared_test_windows',(SELECT count(*) FROM test_windows WHERE account_ids @> ARRAY[$1::uuid] AND cardinality(account_ids)>1),
          'management_groups',(SELECT count(*) FROM group_bindings WHERE primary_account_id=$1),
          'ai_groups',(SELECT count(*) FROM group_bindings WHERE ai_account_id=$1)),
        jsonb_build_object(
          'group_ownership',(SELECT count(*) FROM group_bindings WHERE primary_account_id=$1 OR ai_account_id=$1),
          'ledger_entries',(SELECT count(*) FROM ledger_entries WHERE group_id IN
            (SELECT group_id FROM group_bindings WHERE primary_account_id=$1 OR ai_account_id=$1)),
          'activities',(SELECT count(*) FROM activities WHERE group_id IN
            (SELECT group_id FROM group_bindings WHERE primary_account_id=$1 OR ai_account_id=$1))),
        (SELECT count(*) FROM native_signup_attempts WHERE account_id=$1 AND state='submitted'),
        (SELECT count(*) FROM operations WHERE status IN ('running','unknown') AND
          (account_id=$1 OR group_id=ANY($2::uuid[]) OR test_window_id IN
            (SELECT id FROM test_windows WHERE account_ids @> ARRAY[$1::uuid])))`,
		view.ID, view.ManagementGroupIDs).Scan(&cleanup, &retained, &submitted, &unresolved)
	if err != nil {
		return view, err
	}
	if err = json.Unmarshal(cleanup, &view.Cleanup); err != nil {
		return view, err
	}
	if err = json.Unmarshal(retained, &view.Retained); err != nil {
		return view, err
	}
	if submitted > 0 {
		view.Blockers = append(view.Blockers, "account_removal_signup_pending")
	}
	if unresolved > 0 {
		view.Blockers = append(view.Blockers, "account_removal_operations_pending")
	}
	return view, nil
}

func (s *Server) accountRemovalPreview(w http.ResponseWriter, r *http.Request, p Principal) error {
	tx, err := s.DB.Platform.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	view, err := readRemovalPreview(r, tx, false)
	if err != nil {
		return err
	}
	send(w, 200, view)
	return nil
}

func (s *Server) removeAccount(w http.ResponseWriter, r *http.Request, p Principal) (err error) {
	var in struct {
		Version     int64  `json:"version"`
		ConfirmName string `json:"confirm_name"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	s.accountLifecycle.Lock()
	defer s.accountLifecycle.Unlock()
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	defer func() {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "40P01") {
			err = fail(409, "account_removal_busy")
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			err = fail(409, "account_removal_dependencies_changed")
		}
	}()
	if _, err = tx.Exec(r.Context(), `SET LOCAL lock_timeout='3s'`); err != nil {
		return err
	}
	// Same group-allocation lock as claims and AI changes. Account row locks
	// fence signup commits, role observations, inbox inserts and test creation.
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return err
	}
	view, err := readRemovalPreview(r, tx, true)
	if err != nil {
		return err
	}
	if in.Version != view.Version {
		return fail(409, "version_conflict")
	}
	if in.ConfirmName != view.Name {
		return fail(400, "account_removal_name_mismatch")
	}
	rows, err := tx.Query(r.Context(), `SELECT group_id FROM group_bindings
        WHERE primary_account_id=$1 OR ai_account_id=$1 ORDER BY group_id FOR UPDATE`, view.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Lock tasks before the final check so a queued task cannot start between
	// checking its outcome and removing its evidence.
	rows, err = tx.Query(r.Context(), `SELECT id FROM test_windows WHERE account_ids @> ARRAY[$1::uuid] ORDER BY id FOR UPDATE`, view.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query(r.Context(), `SELECT id FROM operations WHERE account_id=$1 OR group_id=ANY($2::uuid[])
        OR test_window_id IN (SELECT id FROM test_windows WHERE account_ids @> ARRAY[$1::uuid]) ORDER BY id FOR UPDATE`,
		view.ID, view.ManagementGroupIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	view, err = readRemovalPreview(r, tx, true)
	if err != nil {
		return err
	}
	if len(view.Blockers) > 0 {
		return fail(409, view.Blockers[0])
	}
	// Only prune native directories that belonged to this account and have
	// become unreferenced. Shared directories and ownership anchors survive.
	var candidates []string
	rows, err = tx.Query(r.Context(), `SELECT group_id::text FROM account_groups WHERE account_id=$1
        UNION SELECT group_id::text FROM inbox WHERE account_id=$1
        UNION SELECT group_id::text FROM claim_codes WHERE account_id=$1
        UNION SELECT group_id::text FROM group_claims WHERE account_id=$1
        UNION SELECT group_id::text FROM operations WHERE account_id=$1
        UNION SELECT group_id::text FROM test_windows WHERE account_ids @> ARRAY[$1::uuid]`, view.ID)
	if err != nil {
		return err
	}
	candidates = []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	statements := []string{
		`UPDATE accounts SET connection_enabled=false,testing_enabled=false,credential_sealed=NULL,
            epoch=epoch+1,version=version+1 WHERE id=$1`,
		`UPDATE group_bindings SET primary_account_id=NULL,ai_account_id=NULL,active=false,
            management_enabled=false,ai_enabled=false,ai_requested=false,ai_request_state='disabled',
            config='{}',epoch=epoch+1,version=version+1 WHERE primary_account_id=$1`,
		`UPDATE group_bindings SET ai_account_id=NULL,ai_enabled=false,ai_requested=false,
            ai_request_state='disabled',epoch=epoch+1,version=version+1 WHERE ai_account_id=$1`,
		`UPDATE operations SET status='cancelled',result_code='account_removed',updated_at=now()
            WHERE status='queued' AND (group_id=ANY($2::uuid[]) OR test_window_id IN
              (SELECT id FROM test_windows WHERE account_ids @> ARRAY[$1::uuid]))`,
		`DELETE FROM audit_events WHERE resource_id=$1::uuid::text OR detail->>'account_id'=$1::uuid::text
            OR (detail->'account_ids') @> jsonb_build_array($1::uuid::text)
            OR (detail->'AccountIDs') @> jsonb_build_array($1::uuid::text)
            OR resource_id IN (SELECT id::text FROM operations WHERE account_id=$1)
            OR resource_id IN (SELECT id::text FROM group_claims WHERE account_id=$1)
            OR resource_id IN (SELECT transaction_id FROM native_signup_attempts WHERE account_id=$1)
            OR resource_id IN (SELECT id::text FROM test_windows WHERE account_ids=ARRAY[$1::uuid])`,
		`DELETE FROM operations WHERE account_id=$1`,
		`DELETE FROM test_windows WHERE account_ids=ARRAY[$1::uuid]`,
		`UPDATE test_windows SET cancelled=true,account_ids=array_remove(account_ids,$1::uuid),
            account_epochs=account_epochs-$1::text WHERE account_ids @> ARRAY[$1::uuid]`,
		`DELETE FROM inbox WHERE account_id=$1`,
		`DELETE FROM personas WHERE account_id=$1`,
		`DELETE FROM maibot_settings WHERE account_id=$1`,
		`DELETE FROM native_signup_attempts WHERE account_id=$1`,
		`DELETE FROM claim_codes WHERE account_id=$1`,
		`DELETE FROM group_claims WHERE account_id=$1`,
		`DELETE FROM account_groups WHERE account_id=$1`,
		`DELETE FROM accounts WHERE id=$1`,
	}
	for i, statement := range statements {
		args := []any{view.ID}
		// Only the operation cancellation statement needs the captured groups.
		if i == 3 {
			args = append(args, view.ManagementGroupIDs)
		}
		if _, err = tx.Exec(r.Context(), statement, args...); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM native_groups g WHERE g.id=ANY($1::uuid[])
        AND NOT EXISTS(SELECT 1 FROM account_groups WHERE group_id=g.id)
        AND NOT EXISTS(SELECT 1 FROM group_bindings WHERE group_id=g.id)
        AND NOT EXISTS(SELECT 1 FROM claim_codes WHERE group_id=g.id)
        AND NOT EXISTS(SELECT 1 FROM group_claims WHERE group_id=g.id)
        AND NOT EXISTS(SELECT 1 FROM inbox WHERE group_id=g.id)
        AND NOT EXISTS(SELECT 1 FROM operations WHERE group_id=g.id)
        AND NOT EXISTS(SELECT 1 FROM test_windows WHERE group_id=g.id)`, candidates); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "account_deleted", view.ID, map[string]any{
		"workspace": view.Workspace, "cleanup": view.Cleanup, "retained": view.Retained,
	}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	s.Native.ForgetAccount(view.ID)
	send(w, 200, view)
	return nil
}
