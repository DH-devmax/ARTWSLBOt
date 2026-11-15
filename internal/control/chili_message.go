package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/lajiaoliao"
)

func validChiliMessageRequest(req TextRequest) bool {
	if !ValidID(req.AccountID) || !ValidID(req.GroupID) || !ValidID(req.TestWindowID) ||
		len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 100 || req.AccountEpoch < 1 || req.BindingEpoch < 1 ||
		len(req.Mentions) != 0 || req.QuoteEventID < 0 ||
		req.ChiliMentionID != "" && (!lajiaoliao.ValidID(req.ChiliMentionID) || req.QuoteEventID != 0) {
		return false
	}
	if req.PrivateConversation != "" {
		if len(req.PrivateConversation) != 64 || strings.Trim(req.PrivateConversation, "0123456789abcdef") != "" ||
			req.PrivateEventID < 1 || req.QuoteEventID != 0 || req.ChiliMentionID != "" ||
			req.MediaKind != "" && req.MediaKind != "image" {
			return false
		}
	} else if req.PrivateEventID != 0 || len(req.PrivateDigest) != 0 {
		return false
	}
	if req.MediaKind != "" {
		return (req.MediaKind == "image" || req.MediaKind == "voice" || req.MediaKind == "video") &&
			req.Text == "" && req.QuoteEventID == 0 && req.ChiliMentionID == ""
	}
	_, _, err := (lajiaoliao.Outbound{Text: req.Text}).Content()
	return err == nil
}

func (s *NativeTextService) chiliSource(ctx context.Context, tx pgx.Tx, req TextRequest, event int64, scene, conversation string) (lajiaoliao.OutboundSource, []byte, error) {
	var sealed []byte
	var key string
	err := tx.QueryRow(ctx, `SELECT coalesce(sx.payload_sealed,e.payload_sealed),coalesce(sx.event_key,e.event_key)
		FROM native_received_events e
		JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
		JOIN accounts a ON a.id=e.account_id
		LEFT JOIN native_received_events sx ON e.event_key LIKE 'lajiao:message:%'
			AND sx.account_id=e.account_id AND sx.kind='notice'
			AND sx.event_key=replace(e.event_key,'lajiao:message:','lajiao:sequence:')
			AND sx.received_at>clock_timestamp()-interval '7 days' AND sx.payload_sealed IS NOT NULL
		WHERE e.account_id=$1 AND e.id=$2 AND a.platform='lajiaoliao' AND a.epoch=$3
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND (d.state='decoded' OR ($4='private' AND d.state='unsupported' AND d.reason='lajiaoliao_content_type'))
		AND d.scene=$4 AND ($5='' OR d.conversation_key=$5)
		AND e.received_at>clock_timestamp()-interval '7 days'
		AND d.received_at>clock_timestamp()-interval '7 days' AND e.payload_sealed IS NOT NULL`,
		req.AccountID, event, req.AccountEpoch, scene, conversation).Scan(&sealed, &key)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return lajiaoliao.OutboundSource{}, nil, err
	}
	if err != nil || len(sealed) > 128<<10+64 {
		return lajiaoliao.OutboundSource{}, nil, Denied(409, "native_message_reference_expired")
	}
	raw, err := s.Vault.Open("native-event:"+req.AccountID+":"+key, sealed)
	if err != nil {
		return lajiaoliao.OutboundSource{}, nil, err
	}
	defer clear(raw)
	out, err := lajiaoliao.ReadOutboundSource(raw)
	if err != nil {
		return out, nil, Denied(409, "native_message_reference_expired")
	}
	return out, chiliSourceFingerprint(raw), nil
}

func chiliSourceFingerprint(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}

func (s *NativeTextService) chiliPrivateTarget(ctx context.Context, tx pgx.Tx, req TextRequest, self string) (string, []byte, error) {
	source, fingerprint, err := s.chiliSource(ctx, tx, req, req.PrivateEventID, "private", req.PrivateConversation)
	if err != nil || source.Session != 1 || source.Group != "" {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	peer := source.Sender
	if peer == self {
		peer = source.Recipient
	} else if source.Recipient != self {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	if peer == self || !lajiaoliao.ValidID(peer) ||
		len(req.PrivateDigest) != 0 && !bytes.Equal(req.PrivateDigest, fingerprint) {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	// This surface accepts only a second authenticated, owner-controlled account
	// explicitly named in the same isolated grant, never an arbitrary contact.
	var owned bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a JOIN test_windows w ON w.id=$1
		WHERE a.native_uid=ANY(w.target_ids) AND a.id<>$2 AND a.platform='lajiaoliao'
		AND a.auth_state='authenticated' AND a.native_uid=$3 AND a.credential_sealed IS NOT NULL
		AND w.account_epochs->>a.id::text=a.epoch::text)`, req.TestWindowID, req.AccountID, peer).Scan(&owned)
	if err != nil || !owned {
		return "", nil, Denied(409, "native_text_test_target_invalid")
	}
	return peer, fingerprint, nil
}

// The caller supplies its authenticated actor and reservation state. Ordinary
// sources keep the old path; notice routes require the exact live proof grant.
func (s *NativeTextService) chiliPrivateTargetForSend(ctx context.Context, tx pgx.Tx, req TextRequest,
	self, actor string, reserved bool) (string, []byte, error) {
	peer, fingerprint, err := s.chiliPrivateTarget(ctx, tx, req, self)
	if err == nil {
		return peer, fingerprint, nil
	}
	fence, state, found, proofErr := controlledProofExecution(ctx, tx, req.TestWindowID, reserved)
	if proofErr != nil {
		return "", nil, proofErr
	}
	if !found || actor != state.Actor || state.Platform != "lajiaoliao" || state.Account != req.AccountID ||
		state.Sender != self || fence.ProofKind != "private" || req.AccountEpoch != state.Epoch ||
		req.GroupID != state.Group || req.BindingEpoch != state.BindingEpoch ||
		req.PrivateEventID < 1 || req.PrivateConversation == "" || req.uploadOnly ||
		req.MediaKind != "" || req.MediaUploadID != "" || req.QuoteEventID != 0 ||
		len(req.Mentions) != 0 || req.ChiliMentionID != "" || len(req.ReferenceDigest) != 0 ||
		controlledProofDigest([]byte(req.Text)) != fence.ProofTextDigest ||
		req.ProofWindowID != "" && req.controlledExecutionFence != fence ||
		reserved && (req.controlledExecutionFence != fence || len(req.PrivateDigest) != sha256.Size) {
		return "", nil, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	if err = state.checkText(s.Vault, "private", req.Text); err != nil {
		return "", nil, err
	}
	peer, fingerprint, err = s.chiliPrivateNoticeSource(ctx, tx, req, self)
	if err != nil || peer != state.Recipient || peer != fence.ProofTarget {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	return peer, fingerprint, nil
}

// Reconciliation can only inspect an already submitted, sealed operation. It
// does not renew its proof window or require that window/scope to remain live.
func (s *NativeTextService) chiliPrivateTargetForReconcile(ctx context.Context, tx pgx.Tx, req TextRequest,
	self, actor, operation string, currentEpoch int64) (string, []byte, error) {
	peer, fingerprint, err := s.chiliPrivateTarget(ctx, tx, req, self)
	if err == nil {
		return peer, fingerprint, nil
	}
	if !ValidID(operation) || !ValidID(actor) || req.ProofWindowID != req.TestWindowID ||
		req.ProofKind != "private" || req.ProofAccountID != req.AccountID ||
		!ValidID(req.ProofChallengeID) || !ValidID(req.ControlledScopeID) ||
		req.ControlledScopeVersion < 1 || req.ProofChallengeVersion < 1 ||
		len(req.PrivateDigest) != sha256.Size || req.PrivateEventID < 1 || req.uploadOnly ||
		req.MediaKind != "" || req.MediaUploadID != "" || req.QuoteEventID != 0 ||
		len(req.Mentions) != 0 || req.ChiliMentionID != "" || len(req.ReferenceDigest) != 0 {
		return "", nil, Denied(409, "native_text_evidence_unavailable")
	}
	var sealed []byte
	var targets []string
	err = tx.QueryRow(ctx, `SELECT o.payload_sealed,w.target_ids FROM operations o
		JOIN lajiaoliao_media_attempts t ON t.operation_id=o.id
		JOIN test_windows w ON w.id=o.test_window_id
		JOIN group_binding_challenges pc ON pc.id=$10
		JOIN onboarding_test_scopes ps ON ps.id=pc.controlled_scope_id
		WHERE o.id=$1 AND o.actor_id=$2 AND o.status='unknown' AND o.kind='send_text'
		AND o.account_id=$3 AND o.group_id=$4 AND o.test_window_id=$5
		AND o.account_epoch=$6 AND o.binding_epoch=$7 AND o.conversation_key=$8
		AND t.submitted_at IS NOT NULL AND t.native_sender=$9
		AND w.actor_id=o.actor_id AND w.group_id=o.group_id AND w.account_ids=ARRAY[o.account_id]
		AND w.actions=ARRAY['send_private_text']::text[] AND w.admission_mode='native_debug'
		AND w.budget=1 AND w.used=1 AND w.account_epochs->>o.account_id::text=o.account_epoch::text
		AND pc.applicant_id=o.actor_id AND pc.platform='lajiaoliao' AND pc.applicant_mode='operator_demo'
		AND pc.tenant_id=ps.tenant_id AND pc.applicant_id=ps.actor_id AND pc.group_id=o.group_id
		AND ps.id=$11 AND ps.claimant_account_id=o.account_id AND ps.account_id=pc.account_id
		AND (o.tenant_id=pc.tenant_id OR split_part(w.target_ids[5],':',4)=o.tenant_id::text)
		AND EXISTS(SELECT 1 FROM audit_events e
			WHERE e.action='onboarding_controlled_proof_window_created' AND e.resource_id=w.id::text
			AND e.actor_id=o.actor_id AND e.tenant_id=pc.tenant_id
			AND e.detail->>'challenge_id'=$10::text AND e.detail->>'proof_kind'='private'
			AND e.detail->>'account_id'=o.account_id::text)`,
		operation, actor, req.AccountID, req.GroupID, req.TestWindowID, req.AccountEpoch,
		req.BindingEpoch, req.PrivateConversation, self, req.ProofChallengeID, req.ControlledScopeID).Scan(&sealed, &targets)
	if err != nil {
		return "", nil, Denied(409, "native_text_evidence_unavailable")
	}
	plain, err := s.Vault.Open("lajiaoliao-media-intent:"+operation, sealed)
	if err != nil {
		return "", nil, Denied(409, "native_text_evidence_unavailable")
	}
	expected, marshalErr := json.Marshal(req)
	matches := marshalErr == nil && bytes.Equal(plain, expected)
	clear(plain)
	clear(expected)
	if !matches || len(targets) != 9 || targets[0] != req.ProofTarget ||
		targets[1] != controlledProofMarker(req.ProofChallengeID, "private") ||
		targets[2] != "onboarding_proof_version:"+strconv.FormatInt(req.ProofChallengeVersion, 10) ||
		targets[3] != "onboarding_proof_scope:"+req.ControlledScopeID+":"+strconv.FormatInt(req.ControlledScopeVersion, 10) ||
		targets[5] != "onboarding_proof_text:"+req.ProofTextDigest ||
		controlledProofDigest([]byte(req.Text)) != req.ProofTextDigest {
		return "", nil, Denied(409, "native_text_evidence_unavailable")
	}
	peer, fingerprint, err = s.chiliPrivateNoticeSourceAtEpoch(ctx, tx, req, self, currentEpoch)
	if err != nil || peer != req.ProofTarget {
		return "", nil, Denied(409, "native_text_evidence_unavailable")
	}
	return peer, fingerprint, nil
}

func (s *NativeTextService) chiliPrivateNoticeSource(ctx context.Context, tx pgx.Tx, req TextRequest,
	self string) (string, []byte, error) {
	return s.chiliPrivateNoticeSourceAtEpoch(ctx, tx, req, self, req.AccountEpoch)
}

func (s *NativeTextService) chiliPrivateNoticeSourceAtEpoch(ctx context.Context, tx pgx.Tx, req TextRequest,
	self string, epoch int64) (string, []byte, error) {
	var sealed []byte
	var key string
	err := tx.QueryRow(ctx, `SELECT e.payload_sealed,e.event_key FROM native_received_events e
		JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
		JOIN accounts a ON a.id=e.account_id
		WHERE e.account_id=$1 AND e.id=$2 AND a.platform='lajiaoliao' AND a.epoch=$3
		AND a.native_uid=$4 AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND e.kind='private' AND d.scene='private' AND d.conversation_key=$5
		AND d.state='unsupported' AND d.reason='lajiaoliao_content_type'
		AND e.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
		AND d.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
		AND e.received_at<=clock_timestamp() AND d.received_at<=clock_timestamp()
		AND e.payload_sealed IS NOT NULL FOR SHARE OF e,d,a`,
		req.AccountID, req.PrivateEventID, epoch, self, req.PrivateConversation).Scan(&sealed, &key)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	if err != nil || len(sealed) > 128<<10+64 {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	raw, err := s.Vault.Open("native-event:"+req.AccountID+":"+key, sealed)
	if err != nil {
		return "", nil, err
	}
	defer clear(raw)
	route, err := lajiaoliao.ReadPrivateFriendNoticeRoute(raw)
	if err != nil {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	peer := route.Sender
	if peer == self {
		peer = route.Recipient
	} else if route.Recipient != self {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	// Match the decoder's existing account-local key without treating the notice
	// as a message body, identity proof or a newly manufactured receive event.
	if controlledProofDigest([]byte("lajiaoliao:private\x000\x00"+peer)) != req.PrivateConversation {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	fingerprint := chiliSourceFingerprint(raw)
	if len(req.PrivateDigest) != 0 && !bytes.Equal(req.PrivateDigest, fingerprint) {
		return "", nil, Denied(409, "native_private_message_invalid")
	}
	return peer, fingerprint, nil
}

func (s *NativeTextService) ChiliPrivateConversation(ctx context.Context, ref SessionRef, account, conversation string) (string, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return "", 0, err
	}
	if actor.Role != "superadmin" {
		return "", 0, Denied(403, "superadmin_required")
	}
	rows, err := tx.Query(ctx, `SELECT d.event_id,a.epoch,a.native_uid FROM native_decoded_events d
		JOIN accounts a ON a.id=d.account_id WHERE d.account_id=$1 AND d.conversation_key=$2
		AND a.platform='lajiaoliao' AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND (d.state='decoded' OR
		    (d.state='unsupported' AND d.reason='lajiaoliao_content_type')) AND d.scene='private'
		AND d.received_at>clock_timestamp()-interval '7 days' ORDER BY event_id DESC LIMIT 32`,
		account, conversation)
	if err != nil {
		return "", 0, err
	}
	type candidate struct {
		event, epoch int64
		self         string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.event, &c.epoch, &c.self); err != nil {
			rows.Close()
			return "", 0, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", 0, err
	}
	if len(candidates) == 0 {
		return "", 0, Denied(404, "conversation_not_found")
	}
	var notice candidate
	noticePeer := ""
	for _, c := range candidates {
		req := TextRequest{AccountID: account, AccountEpoch: c.epoch,
			PrivateEventID: c.event, PrivateConversation: conversation}
		source, _, sourceErr := s.chiliSource(ctx, tx, req, c.event, "private", conversation)
		var rejected *Failure
		if sourceErr != nil && (!errors.As(sourceErr, &rejected) || rejected.Code != "native_message_reference_expired") {
			return "", 0, sourceErr
		}
		if sourceErr == nil && source.Session == 1 && source.Group == "" {
			peer := source.Sender
			if peer == c.self {
				peer = source.Recipient
			} else if source.Recipient != c.self {
				continue
			}
			if lajiaoliao.ValidID(peer) && peer != c.self {
				return peer, c.event, nil
			}
		}
		if noticePeer != "" {
			continue
		}
		peer, _, routeErr := s.chiliPrivateNoticeSource(ctx, tx, req, c.self)
		if routeErr != nil {
			if !errors.As(routeErr, &rejected) || rejected.Code != "native_private_message_invalid" {
				return "", 0, routeErr
			}
			continue
		}
		notice, noticePeer = c, peer
	}
	if noticePeer == "" {
		return "", 0, Denied(409, "native_private_message_invalid")
	}
	// Ordinary sources take precedence. A retained 1201/1204 notice still needs
	// its exact live proof grant; later unsupported notices confer no authority.
	// Release locks before discovery; Stage and submission pin the chosen event.
	if err = tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	windows, proofErr := s.OnboardingControlledProofWindows(ctx, ref, account, "private", noticePeer)
	if proofErr != nil {
		return "", 0, proofErr
	}
	matched := slices.ContainsFunc(windows, func(w OnboardingControlledProofChoice) bool {
		return w.AccountEpoch == notice.epoch
	})
	if !matched {
		return "", 0, Denied(409, "onboarding_controlled_proof_window_invalid")
	}
	return noticePeer, notice.event, nil
}

func (s *NativeTextService) chiliMessage(ctx context.Context, tx pgx.Tx, req TextRequest, scope textScope, operation string) (lajiaoliao.Outbound, error) {
	out := lajiaoliao.Outbound{Text: req.Text}
	if req.MediaUploadID != "" {
		asset, err := s.chiliMediaReceipt(ctx, tx, scope.actor.ID, req, operation)
		if err != nil {
			return out, err
		}
		out.Media = &asset
	}
	if req.ChiliMentionID != "" {
		var granted bool
		err := tx.QueryRow(ctx, `SELECT $2=ANY(target_ids) FROM test_windows WHERE id=$1`,
			req.TestWindowID, req.ChiliMentionID).Scan(&granted)
		if err != nil || !granted {
			return out, Denied(409, "native_text_test_target_invalid")
		}
		out.Mention = &lajiaoliao.MessagePerson{ID: req.ChiliMentionID}
	}
	if req.QuoteEventID > 0 {
		source, fingerprint, err := s.chiliSource(ctx, tx, req, req.QuoteEventID, "group", "")
		if err != nil || source.Session != 3 || source.Group != scope.nativeGroup ||
			!source.CanQuote || len(req.ReferenceDigest) != 0 && !bytes.Equal(req.ReferenceDigest, fingerprint) {
			return out, Denied(409, "native_message_reference_expired")
		}
		out.Quote = &lajiaoliao.TextQuote{ClientID: source.ClientID, ServerID: source.ServerID,
			Sender: source.Sender, Group: source.Group, Time: source.Time, Seq: source.Seq, Text: source.Text}
	}
	_, _, err := out.Content()
	return out, err
}

func chiliTargetsAllowed(req TextRequest, targets []string, peer string) bool {
	return (req.ChiliMentionID == "" || slices.Contains(targets, req.ChiliMentionID)) &&
		(peer == "" || slices.Contains(targets, peer))
}

type ChiliSendTarget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ChiliSendQuote struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (s *NativeTextService) ChiliSendChoices(ctx context.Context, ref SessionRef, req TextRequest, conversation string) ([]ChiliSendTarget, []ChiliSendQuote, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)
	req.Text = "read-only choices"
	if err = tx.QueryRow(ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1`, req.AccountID).
		Scan(&req.ReceiverOwner, &req.ReceiverRevision); err != nil {
		return nil, nil, err
	}
	scope, err := s.authorizeSendPlatform(ctx, tx, ref, req, false, "lajiaoliao")
	if err != nil {
		return nil, nil, err
	}
	members := []ChiliSendTarget{}
	rows, err := tx.Query(ctx, `SELECT a.native_uid,coalesce(nullif(a.native_nickname,''),a.name)
		FROM accounts a JOIN test_windows w ON w.id=$1 WHERE a.native_uid=ANY(w.target_ids)
		AND a.id<>$2 AND a.platform='lajiaoliao'
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND w.account_epochs->>a.id::text=a.epoch::text ORDER BY a.id LIMIT 20`, req.TestWindowID, req.AccountID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var member ChiliSendTarget
		if err = rows.Scan(&member.ID, &member.Name); err != nil {
			rows.Close()
			return nil, nil, err
		}
		members = append(members, member)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	ids, err := tx.Query(ctx, `SELECT e.id,coalesce(sx.event_key,e.event_key),coalesce(sx.payload_sealed,e.payload_sealed)
		FROM native_received_events e
		JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
		LEFT JOIN native_received_events sx ON e.event_key LIKE 'lajiao:message:%'
			AND sx.account_id=e.account_id AND sx.kind='notice'
			AND sx.event_key=replace(e.event_key,'lajiao:message:','lajiao:sequence:')
			AND sx.received_at>clock_timestamp()-interval '7 days' AND sx.payload_sealed IS NOT NULL
		WHERE e.account_id=$1 AND d.conversation_key=$2 AND d.state='decoded' AND d.scene='group'
		AND e.received_at>clock_timestamp()-interval '7 days' AND d.received_at>clock_timestamp()-interval '7 days'
		ORDER BY e.id DESC LIMIT 30`, req.AccountID, conversation)
	if err != nil {
		return nil, nil, err
	}
	quotes, total := []ChiliSendQuote{}, 0
	for ids.Next() {
		var event int64
		var key string
		var sealed []byte
		if err = ids.Scan(&event, &key, &sealed); err != nil {
			ids.Close()
			return nil, nil, err
		}
		total += len(sealed)
		if total > 2<<20 {
			break
		}
		if len(sealed) > 128<<10+64 {
			continue
		}
		raw, err := s.Vault.Open("native-event:"+req.AccountID+":"+key, sealed)
		if err != nil {
			continue
		}
		source, err := lajiaoliao.ReadOutboundSource(raw)
		clear(raw)
		if err != nil || !source.CanQuote || source.Group != scope.nativeGroup {
			continue
		}
		text := []rune(source.Text)
		if len(text) > 80 {
			text = text[:80]
		}
		quotes = append(quotes, ChiliSendQuote{ID: strconv.FormatInt(event, 10), Text: string(text)})
		if len(quotes) == 20 {
			break
		}
	}
	ids.Close()
	if err = ids.Err(); err != nil {
		return nil, nil, err
	}
	return members, quotes, tx.Commit(ctx)
}
