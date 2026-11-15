package control

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Only an unchanged demo binding can recover around a retired private proof.
// Transfers and legacy/manual binding retain the strict unresolved-work fence.
func (s *GroupBindingService) bindProvenGroup(ctx context.Context, tx pgx.Tx, c bindingRecord, proof bindingProof) error {
	retired, err := s.retiredPrivateProofForBinding(ctx, tx, c, proof)
	if err != nil {
		return err
	}
	return bindCustomerGroupTx(ctx, tx, c.Tenant, c.Platform, c.Account, c.Group, c.BindingVersion, c.ID, retired)
}

func (s *GroupBindingService) retiredPrivateProofForBinding(ctx context.Context, tx pgx.Tx,
	c bindingRecord, proof bindingProof) (string, error) {
	if c.Platform != "lajiaoliao" || c.Mode != "operator_demo" || c.ControlledScope == "" ||
		c.TransferFrom != "" || c.ProofEvent == nil || len(c.Identity) == 0 || s.Vault == nil {
		return "", nil
	}
	var claimant, sender, group string
	err := tx.QueryRow(ctx, `SELECT s.claimant_account_id::text,s.claimant_native_id,g.native_id
		FROM onboarding_test_scopes s JOIN group_bindings b ON b.group_id=s.group_id
		JOIN group_binding_ownership o ON o.group_id=b.group_id JOIN native_groups g ON g.id=b.group_id
		WHERE s.id=$1 AND s.actor_id=$2 AND s.tenant_id=$3 AND s.platform='lajiaoliao'
		AND s.account_id=$4 AND s.group_id=$5 AND onboarding_test_scope_active(s.id)
		AND b.active AND b.tenant_id=s.tenant_id AND b.primary_account_id=s.account_id AND b.version=$6
		AND o.tenant_id=s.tenant_id AND o.platform=s.platform AND o.proof_kind='controlled_test' AND o.state='paused'
		FOR SHARE OF s,b,o,g`, c.ControlledScope, c.Applicant, c.Tenant, c.Account, c.Group, c.BindingVersion).
		Scan(&claimant, &sender, &group)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if sender != proof.Sender || group != proof.Group {
		return "", Denied(409, "group_binding_evidence_changed")
	}
	rows, err := tx.Query(ctx, `SELECT id::text,account_id::text,status FROM operations
		WHERE group_id=$1 AND status IN('running','unknown') ORDER BY id LIMIT 2 FOR SHARE`, c.Group)
	if err != nil {
		return "", err
	}
	type pendingOperation struct{ id, account, status string }
	var pending []pendingOperation
	for rows.Next() {
		var op pendingOperation
		if err = rows.Scan(&op.id, &op.account, &op.status); err != nil {
			rows.Close()
			return "", err
		}
		pending = append(pending, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if len(pending) != 1 || pending[0].status != "unknown" || pending[0].account != claimant {
		return "", nil
	}
	// The successful new group proof supplies the sealed authorization snapshot;
	// authenticated private/group ingress was independently checked by the caller.
	var sealed []byte
	var operation, window string
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.test_window_id::text,o.payload_sealed
		FROM operations o JOIN test_windows w ON w.id=o.test_window_id
		JOIN lajiaoliao_media_attempts a ON a.operation_id=o.id
		WHERE o.actor_id=$1 AND o.tenant_id=$2 AND o.account_id=$3 AND o.group_id=$4
		AND o.status='succeeded' AND o.kind='send_text' AND o.conversation_key=''
		AND o.result_code IN('im_server_accepted','im_server_accepted_reconciled')
		AND a.native_sender=$5 AND a.native_group=$6 AND a.submitted_at IS NOT NULL AND a.receipt_sealed IS NOT NULL
		AND w.actor_id=o.actor_id AND w.group_id=o.group_id AND w.admission_mode='native_debug'
		AND w.budget=1 AND w.used=1 AND $7=ANY(w.target_ids)
		AND EXISTS(SELECT 1 FROM audit_events e WHERE e.action='onboarding_controlled_proof_window_created'
			AND e.resource_id=w.id::text AND e.actor_id=o.actor_id AND e.tenant_id=o.tenant_id
			AND e.detail->>'challenge_id'=$8 AND e.detail->>'proof_kind'='group')
		ORDER BY o.id LIMIT 1 FOR SHARE OF o,w,a`, c.Applicant, c.Tenant, claimant, c.Group,
		sender, group, controlledProofMarker(c.ID, "group"), c.ID).Scan(&operation, &window, &sealed)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	raw, err := s.Vault.Open("lajiaoliao-media-intent:"+operation, sealed)
	if err != nil {
		return "", Denied(409, "group_binding_evidence_changed")
	}
	var req TextRequest
	err = json.Unmarshal(raw, &req)
	clear(raw)
	if err != nil || req.ProofKind != "group" || req.ProofChallengeID != c.ID ||
		req.ControlledScopeID != c.ControlledScope || req.ProofAccountID != claimant ||
		req.AccountID != claimant || req.GroupID != c.Group || req.TestWindowID != window ||
		req.ProofWindowID != window || req.PrivateConversation != "" || req.PrivateEventID != 0 ||
		!bindingTokenMatches(c.TokenHash, req.Text, "群绑定 ") ||
		req.ProofTextDigest != controlledProofDigest([]byte(req.Text)) {
		return "", Denied(409, "group_binding_evidence_changed")
	}
	scope := textScope{actor: Actor{ID: c.Applicant}, tenant: c.Tenant,
		nativeSender: sender, nativeGroup: group, controlledExecutionFence: req.controlledExecutionFence}
	if err = (&NativeTextService{Vault: s.Vault}).chiliUnknownProofAvailable(ctx, tx, req, scope, operation); err != nil {
		return "", err
	}
	return pending[0].id, nil
}
