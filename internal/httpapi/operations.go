package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/security"
)

func (s *Server) operations(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT id,tenant_id,actor_id,group_id,account_id,test_window_id,kind,target_id,
        account_epoch,binding_epoch,status,expires_at,result_code,created_at,updated_at
        FROM operations ORDER BY created_at DESC LIMIT 200`)
}

func (s *Server) createOperation(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		GroupID      string `json:"group_id"`
		AccountID    string `json:"account_id"`
		Kind         string `json:"kind"`
		TargetID     string `json:"target_id"`
		Text         string `json:"text"`
		TestWindowID string `json:"test_window_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !validUUID(in.GroupID) || !validUUID(in.AccountID) || !slices.Contains([]string{"send_text", "recall", "mute", "unmute"}, in.Kind) ||
		len(in.Text) > 4000 || len(in.TargetID) > 200 {
		return fail(400, "invalid_operation")
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 100 {
		return fail(400, "idempotency_key_required")
	}
	// Never enqueue a production effect until there is an audited, compatible sender.
	return fail(409, "native_compatibility_not_verified")
}

func (s *Server) cancelOperation(w http.ResponseWriter, r *http.Request, p Principal) error {
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var tenant string
	err = tx.QueryRow(r.Context(), `SELECT coalesce(tenant_id::text,'') FROM operations WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&tenant)
	if err == pgx.ErrNoRows || (err == nil && p.Role != "superadmin" && tenant != p.TenantID) {
		return fail(404, "operation_not_found")
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='user_cancelled',updated_at=now()
        WHERE id=$1 AND status='queued'`, r.PathValue("id"))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fail(409, "operation_already_started")
	}
	if err = auditTx(r.Context(), tx, p, tenant, "operation_cancelled", r.PathValue("id"), map[string]any{}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]string{"status": "cancelled"})
	return nil
}

func (s *Server) testWindows(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT * FROM test_windows ORDER BY created_at DESC LIMIT 100`)
}

func (s *Server) createTestWindow(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		GroupID    string   `json:"group_id"`
		AccountIDs []string `json:"account_ids"`
		TargetIDs  []string `json:"target_ids"`
		Actions    []string `json:"actions"`
		Seconds    int      `json:"seconds"`
		Budget     int      `json:"budget"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !validUUID(in.GroupID) || len(in.AccountIDs) < 1 || len(in.AccountIDs) > 10 || in.Seconds < 1 || in.Seconds > 300 ||
		in.Budget < 1 || in.Budget > 10 || len(in.Actions) < 1 || len(in.Actions) > 4 || len(in.TargetIDs) > 10 {
		return fail(400, "invalid_test_scope")
	}
	for _, action := range in.Actions {
		if !slices.Contains([]string{"send_text", "recall", "mute", "unmute"}, action) {
			return fail(400, "invalid_test_action")
		}
	}
	seen := map[string]bool{}
	for _, id := range in.AccountIDs {
		if !validUUID(id) || seen[id] {
			return fail(400, "invalid_test_accounts")
		}
		seen[id] = true
	}
	for _, id := range in.TargetIDs {
		if id == "" || len(id) > 200 {
			return fail(400, "invalid_test_targets")
		}
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var test bool
	err = tx.QueryRow(r.Context(), `SELECT isolated_test FROM native_groups WHERE id=$1`, in.GroupID).Scan(&test)
	if err == pgx.ErrNoRows || !test {
		return fail(409, "isolated_test_group_required")
	}
	if err != nil {
		return err
	}
	epochs := map[string]int64{}
	for _, id := range in.AccountIDs {
		var epoch int64
		err = tx.QueryRow(r.Context(), `SELECT a.epoch FROM accounts a JOIN account_groups ag ON ag.account_id=a.id
            WHERE a.id=$1 AND ag.group_id=$2 AND a.testing_enabled AND a.connection_enabled
            AND ag.role IN('admin','owner') AND ag.observation_source='native'
            AND ag.observed_at>now()-interval '60 seconds' FOR UPDATE OF a`, id, in.GroupID).Scan(&epoch)
		if err == pgx.ErrNoRows {
			return fail(409, "fresh_test_account_role_required")
		}
		if err != nil {
			return err
		}
		epochs[id] = epoch
	}
	blob, _ := json.Marshal(epochs)
	var id string
	var expires time.Time
	if in.TargetIDs == nil {
		in.TargetIDs = []string{}
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO test_windows(actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget)
        VALUES($1,$2,$3::uuid[],$4,$5,$6,now()+make_interval(secs=>$7),$8)
        RETURNING id::text,expires_at`, p.ID, in.GroupID, in.AccountIDs, in.TargetIDs, in.Actions, blob, in.Seconds, in.Budget).Scan(&id, &expires)
	if err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "test_window_created", id, in); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 201, map[string]any{"id": id, "expires_at": expires})
	return nil
}

func (s *Server) cancelTestWindow(w http.ResponseWriter, r *http.Request, p Principal) error {
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE test_windows SET cancelled=true WHERE id=$1`, r.PathValue("id"))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fail(404, "test_window_not_found")
	}
	if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='test_window_cancelled',updated_at=now()
        WHERE test_window_id=$1 AND status='queued'`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "test_window_cancelled", r.PathValue("id"), map[string]any{}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]bool{"cancelled": true})
	return nil
}

func (s *Server) ledger(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.list(w, r, p, `SELECT id,tenant_id,group_id,member_id,kind,amount_cents::text amount_cents,
        external_key,evidence,actor_id,created_at FROM ledger_entries ORDER BY created_at DESC LIMIT 200`)
}

func (s *Server) createLedgerEntry(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		GroupID     string `json:"group_id"`
		MemberID    string `json:"member_id"`
		Kind        string `json:"kind"`
		Amount      string `json:"amount"`
		ExternalKey string `json:"external_key"`
		Note        string `json:"note"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	amount, err := security.Money(in.Amount)
	if err != nil || !validUUID(in.GroupID) || in.MemberID == "" || len(in.MemberID) > 200 || len(in.ExternalKey) < 8 || len(in.ExternalKey) > 100 ||
		!slices.Contains([]string{"reward", "adjustment", "paid"}, in.Kind) || in.Note == "" || len(in.Note) > 2000 {
		return fail(400, "invalid_ledger_entry")
	}
	if in.Kind != "adjustment" && amount <= 0 {
		return fail(400, "positive_amount_required")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	tenant, err := authorizeBusiness(r, tx, p, in.GroupID, "finance")
	if err != nil {
		return err
	}
	var id string
	blob, _ := json.Marshal(map[string]string{"note": in.Note, "source": "web_manual"})
	err = tx.QueryRow(r.Context(), `INSERT INTO ledger_entries(tenant_id,group_id,member_id,kind,amount_cents,external_key,evidence,actor_id)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING RETURNING id::text`,
		tenant, in.GroupID, in.MemberID, in.Kind, amount, in.ExternalKey, blob, p.ID).Scan(&id)
	if err == pgx.ErrNoRows {
		return fail(409, "ledger_reference_exists")
	}
	if err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "ledger_entry_created", id, map[string]any{"kind": in.Kind, "amount_cents": amount}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 201, map[string]string{"id": id})
	return nil
}

func authorizeBusiness(r *http.Request, tx pgx.Tx, p Principal, group, module string) (string, error) {
	var tenant string
	var modules []string
	err := tx.QueryRow(r.Context(), `SELECT b.tenant_id::text,p.modules FROM group_bindings b
        JOIN subscriptions s ON s.tenant_id=b.tenant_id JOIN plans p ON p.id=s.plan_id
        WHERE b.group_id=$1 AND b.active AND s.enabled AND s.expires_at>now() FOR UPDATE OF b`, group).Scan(&tenant, &modules)
	if err == pgx.ErrNoRows || (err == nil && p.Role != "superadmin" && tenant != p.TenantID) {
		return "", fail(404, "group_not_found")
	}
	if err != nil {
		return "", err
	}
	if !slices.Contains(modules, module) {
		return "", fail(403, "module_not_in_plan")
	}
	return tenant, nil
}
