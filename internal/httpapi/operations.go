package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func (s *Server) operations(w http.ResponseWriter, r *http.Request, p Principal) error {
	data, err := control.SafeBusinessOperations(r.Context(), s.DB.Platform, principalActor(p), requestWorkspace(r), "", 200)
	if err != nil {
		return err
	}
	permissions, err := s.permissions(r.Context(), p)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, "operations:read") {
		return fail(403, "permission_denied")
	}
	send(w, 200, data)
	return nil
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
	var tenant, group string
	err = tx.QueryRow(r.Context(), `SELECT coalesce(tenant_id::text,''),group_id::text FROM operations WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&tenant, &group)
	if err == pgx.ErrNoRows || (err == nil && p.Role != "superadmin" && tenant != p.TenantID) {
		return fail(404, "operation_not_found")
	}
	if err != nil {
		return err
	}
	if err = control.RequireGroup(r.Context(), tx, principalActor(p), group); err != nil {
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
	return s.list(w, r, p, `SELECT w.* FROM test_windows w JOIN native_groups g ON g.id=w.group_id
        WHERE g.platform=$1 AND w.actor_id=$2 ORDER BY w.created_at DESC LIMIT 100`, requestWorkspace(r), p.ID)
}

func (s *Server) createTestWindow(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) != "wsl" && requestWorkspace(r) != "lajiaoliao" {
		return fail(403, "workspace_route_forbidden")
	}
	var in struct {
		GroupID             string   `json:"group_id"`
		AccountIDs          []string `json:"account_ids"`
		TargetIDs           []string `json:"target_ids"`
		Actions             []string `json:"actions"`
		Seconds             int      `json:"seconds"`
		Budget              int      `json:"budget"`
		ConfirmIsolated     bool     `json:"confirm_isolated"`
		AdmissionMode       string   `json:"admission_mode"`
		FriendApplicationID string   `json:"friend_application_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.AdmissionMode == "" {
		in.AdmissionMode = "package"
	}
	if !validUUID(in.GroupID) || len(in.AccountIDs) < 1 || len(in.AccountIDs) > 10 || in.Seconds < 1 || in.Seconds > 300 ||
		in.Budget < 1 || in.Budget > 10 || len(in.Actions) < 1 || len(in.Actions) > 32 || len(in.TargetIDs) > 10 ||
		in.AdmissionMode != "package" && in.AdmissionMode != "native_debug" {
		return fail(400, "invalid_test_scope")
	}
	seenActions := map[string]bool{}
	for _, action := range in.Actions {
		if !control.TestAction(action) || seenActions[action] ||
			requestWorkspace(r) != "lajiaoliao" && (action == "kick_member_ban" || action == "remove_group_ban") ||
			in.AdmissionMode == "native_debug" && !control.NativeDebugActionForPlatform(requestWorkspace(r), action) ||
			requestWorkspace(r) == "lajiaoliao" && !control.NativeDebugActionForPlatform("lajiaoliao", action) {
			return fail(400, "invalid_test_action")
		}
		seenActions[action] = true
	}
	if seenActions["friend_delete"] && (in.AdmissionMode != "native_debug" || len(in.Actions) != 1 ||
		len(in.AccountIDs) != 1 || len(in.TargetIDs) != 1 || in.Budget != 1) {
		return fail(400, "native_friend_delete_single_scope_required")
	}
	if seenActions["friend_approve"] && (in.AdmissionMode != "native_debug" || len(in.Actions) != 1 ||
		len(in.AccountIDs) != 1 || len(in.TargetIDs) != 1 || in.Budget != 1 ||
		in.FriendApplicationID == "" || len(in.FriendApplicationID) > 200) {
		return fail(400, "native_friend_approve_single_scope_required")
	}
	if !seenActions["friend_approve"] && in.FriendApplicationID != "" {
		return fail(400, "invalid_test_scope")
	}
	seen := map[string]bool{}
	for _, id := range in.AccountIDs {
		if !validUUID(id) || seen[id] {
			return fail(400, "invalid_test_accounts")
		}
		seen[id] = true
	}
	seenTargets := map[string]bool{}
	for _, id := range in.TargetIDs {
		if id == "" || len(id) > 200 || seenTargets[id] {
			return fail(400, "invalid_test_targets")
		}
		seenTargets[id] = true
	}
	acceptProofs := map[string]gateway.SocialSnapshot{}
	deleteProofs := map[string]gateway.SocialSnapshot{}
	approveProofs := map[string]gateway.SocialSnapshot{}
	if seenActions["friend_approve"] {
		if s.Receiver == nil {
			return fail(409, "native_friend_approve_receiver_required")
		}
		var nativeGroup string
		if err := s.DB.Platform.QueryRow(r.Context(), `SELECT g.native_id FROM native_groups g
			JOIN group_bindings b ON b.group_id=g.id AND b.active
			WHERE g.id=$1 AND g.platform=$2`, in.GroupID, requestWorkspace(r)).Scan(&nativeGroup); err != nil {
			return fail(409, "group_authorization_inactive")
		}
		account := in.AccountIDs[0]
		view, err := s.Receiver.SocialApplication(r.Context(), account, nativeGroup)
		if err != nil {
			return nativeSettingsError(w, err)
		}
		if view.Platform != requestWorkspace(r) && !(view.Platform == "" && requestWorkspace(r) == "wsl") ||
			!view.Present || !view.MembersKnown || view.SelfRole != "owner" && view.SelfRole != "admin" {
			return fail(409, "native_social_role_required")
		}
		if _, err := view.ValidateChange(nativeauth.SettingChange{Kind: "friend_approve",
			Group: nativeGroup, Target: in.TargetIDs[0], Text: in.FriendApplicationID}); err != nil {
			return nativeSettingsError(w, err)
		}
		approveProofs[account] = view
	}
	if seenActions["friend_delete"] {
		if s.Receiver == nil {
			return fail(409, "native_friend_delete_receiver_required")
		}
		var nativeGroup string
		if err := s.DB.Platform.QueryRow(r.Context(), `SELECT g.native_id FROM native_groups g
			JOIN group_bindings b ON b.group_id=g.id AND b.active
			WHERE g.id=$1 AND g.platform=$2`, in.GroupID, requestWorkspace(r)).Scan(&nativeGroup); err != nil {
			return fail(409, "group_authorization_inactive")
		}
		account := in.AccountIDs[0]
		view, err := s.Receiver.Social(r.Context(), account, nativeGroup)
		if err != nil {
			return nativeSettingsError(w, err)
		}
		if view.Platform != requestWorkspace(r) && !(view.Platform == "" && requestWorkspace(r) == "wsl") {
			return fail(409, "native_friend_delete_receiver_required")
		}
		if _, err := view.ValidateChange(nativeauth.SettingChange{Kind: "friend_delete",
			Group: nativeGroup, Target: in.TargetIDs[0]}); err != nil {
			return nativeSettingsError(w, err)
		}
		deleteProofs[account] = view
	}
	if seenActions["group_accept"] {
		if len(in.Actions) != 1 || s.Receiver == nil {
			return fail(409, "native_invitation_self_required")
		}
		var nativeGroup string
		if err := s.DB.Platform.QueryRow(r.Context(), `SELECT native_id FROM native_groups
            WHERE id=$1 AND platform=$2`, in.GroupID, requestWorkspace(r)).Scan(&nativeGroup); err != nil {
			return fail(409, "native_invitation_self_required")
		}
		for _, account := range in.AccountIDs {
			view, err := s.Receiver.Social(r.Context(), account, nativeGroup)
			if err != nil {
				return nativeSettingsError(w, err)
			}
			if view.Platform != requestWorkspace(r) && !(view.Platform == "" && requestWorkspace(r) == "wsl") ||
				!selfInvitationGrantAllowed(view) || !seenTargets[view.SelfID] {
				return fail(409, "native_invitation_self_required")
			}
			acceptProofs[account] = view
		}
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var authorized bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM group_bindings b
        WHERE b.group_id=$1 AND b.active AND ($3='native_debug' OR EXISTS(
        SELECT 1 FROM subscriptions sub JOIN plans plan ON plan.id=sub.plan_id
        WHERE sub.tenant_id=b.tenant_id AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
        AND plan.group_limit>0 AND $2=ANY(plan.allowed_platforms))))`,
		in.GroupID, requestWorkspace(r), in.AdmissionMode).Scan(&authorized)
	if err != nil {
		return err
	}
	if !authorized {
		return fail(409, "group_authorization_inactive")
	}
	var test bool
	err = tx.QueryRow(r.Context(), `SELECT isolated_test FROM native_groups WHERE id=$1 AND platform=$2 FOR UPDATE`,
		in.GroupID, requestWorkspace(r)).Scan(&test)
	if err == pgx.ErrNoRows || !test && !in.ConfirmIsolated {
		return fail(409, "isolated_test_group_required")
	}
	if err != nil {
		return err
	}
	if !test {
		if _, err = tx.Exec(r.Context(), `UPDATE native_groups SET isolated_test=true WHERE id=$1`, in.GroupID); err != nil {
			return err
		}
	}
	epochs := map[string]int64{}
	for _, id := range in.AccountIDs {
		var epoch int64
		var uid, role string
		if seenActions["friend_approve"] {
			epoch, err = socialGrantAccountEpoch(r.Context(), tx, id, requestWorkspace(r),
				approveProofs[id], "native_friend_approve_receiver_required")
			if err != nil {
				return err
			}
			epochs[id] = epoch
			continue
		}
		if seenActions["friend_delete"] {
			epoch, err = socialGrantAccountEpoch(r.Context(), tx, id, requestWorkspace(r),
				deleteProofs[id], "native_friend_delete_receiver_required")
			if err != nil {
				return err
			}
			epochs[id] = epoch
			continue
		}
		if seenActions["group_accept"] {
			proof := acceptProofs[id]
			epoch, err = selfInvitationAccountEpoch(r.Context(), tx, id, requestWorkspace(r), proof, seenTargets)
			if err != nil {
				return err
			}
			epochs[id] = epoch
			continue
		}
		err = tx.QueryRow(r.Context(), `SELECT a.epoch,coalesce(a.native_uid,''),ag.role FROM accounts a JOIN account_groups ag ON ag.account_id=a.id
            WHERE a.id=$1 AND ag.group_id=$2 AND a.testing_enabled AND a.connection_enabled
            AND a.workspace=$3 AND a.platform=$3 AND a.purpose='management' AND a.auth_state='authenticated'
            AND a.credential_sealed IS NOT NULL
            AND ag.observation_source='native'
            AND ag.observed_at>now()-interval '60 seconds' FOR UPDATE OF a`, id, in.GroupID, requestWorkspace(r)).Scan(&epoch, &uid, &role)
		if err == pgx.ErrNoRows {
			return fail(409, "fresh_test_account_role_required")
		}
		if err != nil {
			return err
		}
		if seenActions["grant_admin"] || seenActions["revoke_admin"] {
			if role != "owner" || len(in.TargetIDs) == 0 {
				return fail(409, "native_moderation_role_required")
			}
		}
		if seenActions["group_leave"] {
			if !seenTargets[uid] || role != "admin" && !(len(in.Actions) == 1 && role == "member") {
				return fail(409, "fresh_test_account_role_required")
			}
		} else if role != "admin" && role != "owner" &&
			!(role == "member" && len(in.Actions) == 1 && seenActions["member_nickname"] &&
				len(in.TargetIDs) == 1 && seenTargets[uid]) &&
			!(role == "member" && in.AdmissionMode == "native_debug" && len(in.Actions) == 1 && seenActions["send_text"]) {
			return fail(409, "fresh_test_account_role_required")
		}
		epochs[id] = epoch
	}
	if requestWorkspace(r) == "lajiaoliao" {
		// Owned recipients are targets, not additional executors. Snapshot their
		// epochs without enabling their action connection or changing their role.
		rows, e := tx.Query(r.Context(), `SELECT id::text,native_uid,epoch FROM accounts
			WHERE platform='lajiaoliao' AND workspace='lajiaoliao' AND purpose='management'
			AND native_uid=ANY($1) AND auth_state='authenticated' AND credential_sealed IS NOT NULL
			FOR SHARE`, in.TargetIDs)
		if e != nil {
			return e
		}
		owned := map[string]bool{}
		for rows.Next() {
			var id, uid string
			var epoch int64
			if e = rows.Scan(&id, &uid, &epoch); e != nil {
				rows.Close()
				return e
			}
			epochs[id], owned[uid] = epoch, true
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if seenActions["send_private_text"] || seenActions["send_private_image"] || seenActions["upload_private_image"] {
			for _, target := range in.TargetIDs {
				if !owned[target] {
					return fail(409, "native_text_test_target_invalid")
				}
			}
		}
	}
	blob, _ := json.Marshal(epochs)
	var id string
	var expires time.Time
	if in.TargetIDs == nil {
		in.TargetIDs = []string{}
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO test_windows(actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget,admission_mode,friend_application_id)
        VALUES($1,$2,$3::uuid[],$4,$5,$6,now()+make_interval(secs=>$7),$8,$9,$10)
        RETURNING id::text,expires_at`, p.ID, in.GroupID, in.AccountIDs, in.TargetIDs, in.Actions, blob, in.Seconds, in.Budget, in.AdmissionMode, in.FriendApplicationID).Scan(&id, &expires)
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

func selfInvitationAccountEpoch(ctx context.Context, tx pgx.Tx, account, platform string,
	proof gateway.SocialSnapshot, targets map[string]bool) (int64, error) {
	if !targets[proof.SelfID] {
		return 0, fail(409, "native_invitation_self_required")
	}
	return socialGrantAccountEpoch(ctx, tx, account, platform, proof, "native_invitation_self_required")
}

func socialGrantAccountEpoch(ctx context.Context, tx pgx.Tx, account, platform string,
	proof gateway.SocialSnapshot, code string) (int64, error) {
	var epoch, accountVersion, deploymentVersion int64
	var uid string
	var credential []byte
	err := tx.QueryRow(ctx, `SELECT a.epoch,a.native_uid,a.credential_sealed,a.version,
        CASE WHEN a.platform='wsl' THEN d.version ELSE 0 END
        FROM accounts a LEFT JOIN native_deployment d ON a.platform='wsl'
        JOIN account_receivers ar ON ar.account_id=a.id
        WHERE a.id=$1 AND a.testing_enabled AND a.connection_enabled
        AND a.workspace=$2 AND a.platform=$2 AND a.purpose='management'
        AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
        AND ar.enabled AND ar.lease_until>clock_timestamp()
        AND ar.snapshot->>'state'='connected' AND ar.observed_at>now()-interval '15 seconds'
        FOR UPDATE OF a`, account, platform).Scan(&epoch, &uid, &credential, &accountVersion, &deploymentVersion)
	if err != nil || proof.SelfID != uid ||
		proof.AccountEpoch != epoch || proof.AccountVersion != accountVersion ||
		proof.DeploymentVersion != deploymentVersion || proof.CredentialDigest != sha256.Sum256(credential) ||
		proof.SampledAt.After(time.Now()) || time.Since(proof.SampledAt) > 5*time.Second {
		return 0, fail(409, code)
	}
	return epoch, nil
}

func selfInvitationGrantAllowed(view gateway.SocialSnapshot) bool {
	for _, record := range view.Invitations {
		if _, err := view.ValidateChange(nativeauth.SettingChange{Kind: "group_accept",
			Group: view.Group, Target: view.SelfID, Text: record.ID}); err == nil {
			return true
		}
	}
	return false
}

func (s *Server) cancelTestWindow(w http.ResponseWriter, r *http.Request, p Principal) error {
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE test_windows w SET cancelled=true WHERE id=$1 AND actor_id=$2
        AND EXISTS(SELECT 1 FROM native_groups g WHERE g.id=w.group_id AND g.platform=$3)`,
		r.PathValue("id"), p.ID, requestWorkspace(r))
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
	if err := control.RequireGroup(r.Context(), tx, control.Actor{ID: p.ID, TenantID: p.TenantID, Role: p.Role}, group); err != nil {
		return "", err
	}
	var tenant string
	var modules []string
	err := tx.QueryRow(r.Context(), `SELECT b.tenant_id::text,p.modules FROM group_bindings b
        JOIN native_groups g ON g.id=b.group_id
        JOIN subscriptions s ON s.tenant_id=b.tenant_id JOIN plans p ON p.id=s.plan_id
        WHERE b.group_id=$1 AND b.active AND s.enabled AND s.expires_at>now()
        AND g.platform=ANY(p.allowed_platforms) FOR UPDATE OF b`, group).Scan(&tenant, &modules)
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
