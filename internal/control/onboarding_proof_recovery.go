package control

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/security"
)

type retiredChiliProofContext struct {
	Actor, Tenant, Account, Group        string
	NativeSender, NativeGroup, Recipient string
	Challenge, Scope, Window             string
	Text                                 string
}

// A new, separately authorized challenge is not a retry of a retired proof.
// Preserve the old unknown operation and keep every ordinary send blocked.
func (s *NativeTextService) chiliUnknownProofAvailable(ctx context.Context, tx pgx.Tx,
	req TextRequest, scope textScope, operation string) error {
	// Ordinary sends retain the original pending-work fence. This classifier
	// can only consider a new proof whose own window was already authorized.
	if scope.ProofWindowID == "" || scope.ProofAccountID != req.AccountID ||
		!controlledProofKind(scope.ProofKind) || req.MediaUploadID != "" || req.MediaKind != "" ||
		req.QuoteEventID != 0 || len(req.Mentions) != 0 || req.ChiliMentionID != "" {
		var pending bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE account_id=$1
			AND ($2='' OR id::text<>$2) AND status='unknown')`, req.AccountID, operation).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return Denied(409, "native_text_unresolved_submission")
		}
		return nil
	}
	peer := ""
	if scope.ProofKind == "private" {
		peer = scope.chiliPeer
	}
	_, err := retiredChiliPrivateProof(ctx, tx, s.Vault, retiredChiliProofContext{
		Actor: scope.actor.ID, Tenant: scope.tenant, Account: req.AccountID, Group: req.GroupID,
		NativeSender: scope.nativeSender, NativeGroup: scope.nativeGroup, Recipient: peer,
		Challenge: scope.ProofChallengeID, Scope: scope.ControlledScopeID, Window: req.TestWindowID,
		Text: req.Text,
	}, operation)
	return err
}

// The caller owns the current authorization. This function only classifies the
// immutable predecessor; it never resolves, retries or changes that operation.
func retiredChiliPrivateProof(ctx context.Context, tx pgx.Tx, vault *security.Vault,
	current retiredChiliProofContext, operation string) (string, error) {
	deny := func() (string, error) { return "", Denied(409, "native_text_unresolved_submission") }
	rows, err := tx.Query(ctx, `SELECT id::text FROM operations WHERE account_id=$1
		AND ($2='' OR id::text<>$2) AND status='unknown' ORDER BY id LIMIT 2`, current.Account, operation)
	if err != nil {
		return "", err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(ids) == 0 {
		return "", err
	}
	if len(ids) != 1 || vault == nil || !ValidID(current.Actor) || !ValidID(current.Tenant) ||
		!ValidID(current.Account) || !ValidID(current.Group) || !ValidID(current.Challenge) ||
		!ValidID(current.Scope) || !ValidID(current.Window) ||
		current.NativeSender == "" || current.NativeGroup == "" {
		return deny()
	}
	var sealed []byte
	err = tx.QueryRow(ctx, `SELECT payload_sealed FROM operations WHERE id=$1 AND status='unknown'
		AND kind='send_text' AND result_code='native_text_result_unknown'
		AND actor_id=$2 AND tenant_id=$3 AND account_id=$4 AND group_id=$5
		AND expires_at<=clock_timestamp() FOR SHARE`, ids[0], current.Actor, current.Tenant,
		current.Account, current.Group).Scan(&sealed)
	if err == pgx.ErrNoRows {
		return deny()
	}
	if err != nil {
		return "", err
	}
	raw, err := vault.Open("lajiaoliao-media-intent:"+ids[0], sealed)
	if err != nil {
		return deny()
	}
	var old TextRequest
	err = json.Unmarshal(raw, &old)
	clear(raw)
	if err != nil || old.ProofKind != "private" || !ValidID(old.ProofChallengeID) ||
		!ValidID(old.ControlledScopeID) || old.ProofWindowID != old.TestWindowID ||
		old.ProofAccountID != old.AccountID || old.AccountID != current.Account || old.GroupID != current.Group ||
		old.ProofChallengeID == current.Challenge || old.ControlledScopeID == current.Scope ||
		old.TestWindowID == current.Window || old.Text == current.Text ||
		old.ProofTextDigest != controlledProofDigest([]byte(old.Text)) ||
		old.PrivateConversation == "" || old.PrivateEventID < 1 || old.ProofTarget == "" ||
		old.MediaUploadID != "" || old.MediaKind != "" || old.QuoteEventID != 0 ||
		len(old.Mentions) != 0 || old.ChiliMentionID != "" ||
		old.ProofChallengeVersion < 1 || old.ControlledScopeVersion < 1 {
		return deny()
	}
	var tokenHash []byte
	var targets, accounts, actions []string
	err = tx.QueryRow(ctx, `SELECT c.private_token_hash,w.target_ids,w.account_ids::text[],w.actions
		FROM operations o JOIN lajiaoliao_media_attempts a ON a.operation_id=o.id
		JOIN test_windows w ON w.id=o.test_window_id
		JOIN group_binding_challenges c ON c.id=$2
		JOIN onboarding_test_scopes s ON s.id=c.controlled_scope_id
		JOIN group_binding_challenges fresh ON fresh.id=$3
		JOIN accounts receiver ON receiver.id=c.account_id
		WHERE o.id=$1 AND o.test_window_id=$4 AND o.account_epoch=$5 AND o.binding_epoch=$6
		AND o.conversation_key=$7 AND a.native_sender=$8 AND a.native_group=$9 AND a.submitted_at IS NOT NULL
		AND c.controlled_scope_id=$10 AND c.platform='lajiaoliao' AND c.applicant_mode='operator_demo'
		AND c.friend_flow='incoming_contact' AND c.applicant_id=o.actor_id AND c.tenant_id=o.tenant_id
		AND c.group_id=o.group_id AND c.account_id=s.account_id
		AND (c.expires_at<=clock_timestamp() OR c.state IN('cancelled','expired','rejected'))
		AND s.claimant_account_id=o.account_id AND s.claimant_native_id=a.native_sender
		AND s.actor_id=o.actor_id AND s.tenant_id=o.tenant_id AND s.group_id=o.group_id
		AND s.platform='lajiaoliao' AND (s.state IN('closed','expired') OR s.expires_at<=clock_timestamp())
		AND fresh.account_id=c.account_id AND fresh.private_token_hash<>c.private_token_hash
		AND fresh.controlled_scope_id=$12 AND fresh.platform=c.platform
		AND fresh.applicant_id=o.actor_id AND fresh.tenant_id=o.tenant_id
		AND receiver.platform='lajiaoliao' AND receiver.native_uid=$11
		AND w.actor_id=o.actor_id AND w.group_id=o.group_id AND w.admission_mode='native_debug'
		AND w.budget=1 AND w.used=1 AND w.expires_at<=clock_timestamp()
		AND w.account_epochs->>o.account_id::text=o.account_epoch::text
		AND EXISTS(SELECT 1 FROM audit_events e WHERE e.action='onboarding_controlled_proof_window_created'
			AND e.resource_id=w.id::text AND e.actor_id=o.actor_id AND e.tenant_id=o.tenant_id
			AND e.detail->>'challenge_id'=c.id::text AND e.detail->>'proof_kind'='private')
		FOR SHARE OF o,a,w,c,s,fresh`, ids[0], old.ProofChallengeID, current.Challenge,
		old.TestWindowID, old.AccountEpoch, old.BindingEpoch, old.PrivateConversation,
		current.NativeSender, current.NativeGroup, old.ControlledScopeID, old.ProofTarget, current.Scope).
		Scan(&tokenHash, &targets, &accounts, &actions)
	if err == pgx.ErrNoRows {
		return deny()
	}
	if err != nil {
		return "", err
	}
	if !bindingTokenMatches(tokenHash, old.Text, "群接入 ") ||
		!slices.Equal(accounts, []string{old.AccountID}) ||
		!slices.Equal(actions, []string{"send_private_text"}) || len(targets) != 9 ||
		targets[0] != old.ProofTarget ||
		targets[1] != controlledProofMarker(old.ProofChallengeID, "private") ||
		targets[2] != "onboarding_proof_version:"+strconv.FormatInt(old.ProofChallengeVersion, 10) ||
		targets[3] != "onboarding_proof_scope:"+old.ControlledScopeID+":"+strconv.FormatInt(old.ControlledScopeVersion, 10) ||
		!strings.HasPrefix(targets[4], "onboarding_proof_binding:"+strconv.FormatInt(old.BindingEpoch, 10)+":") ||
		targets[5] != "onboarding_proof_text:"+old.ProofTextDigest ||
		(current.Recipient != "" && current.Recipient != old.ProofTarget) {
		return deny()
	}
	return ids[0], nil
}
