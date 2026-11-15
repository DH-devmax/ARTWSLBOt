package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/database"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

// NativeTextService admits isolated test-window intents only. Its connection
// owner adapter is internal; production operation endpoints remain closed.
type NativeTextService struct {
	DB    *database.DB
	Vault *security.Vault
}

type TextRequest struct {
	controlledExecutionFence
	AccountID, GroupID, TestWindowID, IdempotencyKey, Text string
	AccountEpoch, BindingEpoch                             int64
	Mentions                                               []nativewire.MessageMention `json:",omitempty"`
	QuoteEventID                                           int64                       `json:",omitempty"`
	ReferenceDigest                                        []byte                      `json:",omitempty"`
	MediaUploadID                                          string                      `json:",omitempty"`
	MediaKind                                              string                      `json:",omitempty"`
	PrivateConversation                                    string                      `json:",omitempty"`
	PrivateEventID                                         int64                       `json:",omitempty"`
	PrivateDigest                                          []byte                      `json:",omitempty"`
	ChiliMentionID                                         string                      `json:",omitempty"`
	// Captured by Stage, never trusted as browser-supplied ownership.
	ReceiverOwner    string
	ReceiverRevision int64
	uploadOnly       bool
}

type TextOperation struct {
	ID            string             `json:"id"`
	Status        string             `json:"status"`
	ResultCode    string             `json:"result_code"`
	EvidenceCheck *TextEvidenceCheck `json:"evidence_check,omitempty"`
}

// Inspect exposes state only, never the sealed body or transport evidence.
// A lost HTTP response must be reconciled by its original request key, not a resend.
func (s *NativeTextService) Inspect(ctx context.Context, ref SessionRef, account, group, id, key string) (TextOperation, error) {
	var out TextOperation
	if !ValidID(account) || !ValidID(group) ||
		(id == "" && (len(key) < 8 || len(key) > 100)) || (id != "" && !ValidID(id)) {
		return out, Denied(400, "invalid_operation")
	}
	return s.inspect(ctx, ref, account, group, "", id, key)
}

func (s *NativeTextService) InspectConversation(ctx context.Context, ref SessionRef, account, conversation, id, key string) (TextOperation, error) {
	if !ValidID(account) || len(conversation) != 64 ||
		(id == "" && (len(key) < 8 || len(key) > 100)) || (id != "" && !ValidID(id)) {
		return TextOperation{}, Denied(400, "invalid_operation")
	}
	return s.inspect(ctx, ref, account, "", conversation, id, key)
}

// InspectAccount survives unbinding, migration and raw-message expiration. It
// exposes only this actor's operation status, never a route or an executable
// authorization, and is not used to admit dispatch.
func (s *NativeTextService) InspectAccount(ctx context.Context, ref SessionRef, account, id, key string) (TextOperation, error) {
	if !ValidID(account) || (id == "" && (len(key) < 8 || len(key) > 100)) || (id != "" && !ValidID(id)) {
		return TextOperation{}, Denied(400, "invalid_operation")
	}
	return s.inspect(ctx, ref, account, "", "*", id, key)
}

func (s *NativeTextService) inspect(ctx context.Context, ref SessionRef, account, group, conversation, id, key string) (TextOperation, error) {
	var out TextOperation
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	err = tx.QueryRow(ctx, `SELECT o.id::text,o.status,o.result_code FROM operations o
        WHERE o.actor_id=$1 AND o.account_id=$2 AND ($3='' OR o.group_id::text=$3)
        AND (EXISTS(SELECT 1 FROM native_text_attempts t WHERE t.operation_id=o.id)
            OR EXISTS(SELECT 1 FROM lajiaoliao_media_attempts t WHERE t.operation_id=o.id))
        AND o.kind IN('send_text','send_image','send_voice','send_video','send_file')
        AND ($6='*' OR o.conversation_key=$6)
        AND (($4<>'' AND o.id::text=$4) OR ($4='' AND o.idempotency_key=$5))`,
		actor.ID, account, group, id, key, conversation).Scan(&out.ID, &out.Status, &out.ResultCode)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "operation_not_found")
	}
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

type TextDispatch struct {
	OperationID, AccountID string
	AccountEpoch           int64
	ReceiverOwner          string
	ReceiverRevision       int64
	Message                nativewire.GroupText
	PrivatePeer            *nativewire.PrivateTarget
	PrivateConversation    string
	PrivateEventID         int64
	QuoteEventID           int64
	Nonce, AAD             uint64
	RecordRoute            func(context.Context, nativewire.TextRoute) error
}

// TextPort must bind this intent to the account's existing connection owner and
// verified group route, then invoke the fence around one write, never a retry.
type TextPort interface {
	SubmitText(context.Context, TextDispatch, nativewire.SubmissionFence) (nativewire.TextSendResult, error)
}

type textScope struct {
	controlledExecutionFence
	actor         Actor
	tenant        string
	sender        uint64
	group         uint64
	peer          *nativewire.PrivateTarget
	privateDigest []byte
	nativeSender  string
	nativeGroup   string
	chiliPeer     string
}

func (s *NativeTextService) authorize(ctx context.Context, tx pgx.Tx, ref SessionRef, req TextRequest, reserved bool) (textScope, error) {
	return s.authorizeSendPlatform(ctx, tx, ref, req, reserved, "wsl")
}

func (s *NativeTextService) authorizeSendPlatform(ctx context.Context, tx pgx.Tx, ref SessionRef, req TextRequest, reserved bool, expectedPlatform string) (textScope, error) {
	var out textScope
	// Fixed order and bounded lock acquisition. Configuration/role mutations
	// cannot pass these row locks during the socket write. Time-based grants
	// need three seconds of slack for the final two-second write fence.
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='750ms'`); err != nil {
		return out, err
	}
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	out.actor = a
	var nativeUID, workspace, auth, platform, purpose string
	var connected, testing, credential bool
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT coalesce(native_uid,''),workspace,auth_state,connection_enabled,
        testing_enabled,credential_sealed IS NOT NULL,epoch,platform,purpose FROM accounts WHERE id=$1 FOR SHARE`, req.AccountID).
		Scan(&nativeUID, &workspace, &auth, &connected, &testing, &credential, &epoch, &platform, &purpose)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "account_not_found")
	}
	if err != nil {
		return out, err
	}
	if expectedPlatform != "wsl" && expectedPlatform != "lajiaoliao" ||
		workspace != expectedPlatform || platform != expectedPlatform || purpose != "management" ||
		auth != "authenticated" || !connected || !testing || !credential || epoch != req.AccountEpoch {
		return out, Denied(409, "native_text_account_changed")
	}
	if expectedPlatform == "lajiaoliao" && !validChiliMessageRequest(req) ||
		expectedPlatform == "wsl" && req.ChiliMentionID != "" {
		return out, Denied(400, "native_text_request_invalid")
	}
	var snapshot []byte
	err = tx.QueryRow(ctx, `SELECT snapshot FROM account_receivers WHERE account_id=$1
        AND enabled AND owner=$2 AND owner<>'' AND revision=$3
        AND lease_until>clock_timestamp()+interval '3 seconds'
        AND observed_at>clock_timestamp()-interval '5 seconds' AND observed_at<=clock_timestamp() FOR SHARE`,
		req.AccountID, req.ReceiverOwner, req.ReceiverRevision).Scan(&snapshot)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "native_text_connection_changed")
	}
	if err != nil {
		return out, err
	}
	var live struct {
		State     string     `json:"state"`
		Heartbeat *time.Time `json:"heartbeat_at"`
	}
	if json.Unmarshal(snapshot, &live) != nil || live.State != "connected" || live.Heartbeat == nil ||
		time.Since(*live.Heartbeat) > 37*time.Second || live.Heartbeat.After(time.Now()) {
		return out, Denied(409, "native_text_connection_changed")
	}
	var primary, nativeID, groupPlatform string
	var active, management, isolated bool
	var bindingEpoch int64
	err = tx.QueryRow(ctx, `SELECT b.tenant_id::text,coalesce(b.primary_account_id::text,''),b.active,
        b.management_enabled,b.epoch,g.native_id,g.isolated_test,g.platform FROM group_bindings b
        JOIN native_groups g ON g.id=b.group_id WHERE b.group_id=$1 FOR SHARE OF b,g`, req.GroupID).
		Scan(&out.tenant, &primary, &active, &management, &bindingEpoch, &nativeID, &isolated, &groupPlatform)
	if err == pgx.ErrNoRows || err == nil && a.Role != "superadmin" && a.TenantID != out.tenant {
		return out, Denied(404, "group_not_found")
	}
	if err != nil {
		return out, err
	}
	if !active || !isolated || bindingEpoch != req.BindingEpoch || groupPlatform != expectedPlatform {
		return out, Denied(409, "native_text_binding_changed")
	}
	// Only the separate superadmin test surface can use these grants.
	if a.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	var role, source string
	var observed time.Time
	err = tx.QueryRow(ctx, `SELECT role,observation_source,observed_at FROM account_groups
        WHERE account_id=$1 AND group_id=$2 FOR SHARE`, req.AccountID, req.GroupID).Scan(&role, &source, &observed)
	if err == pgx.ErrNoRows {
		return out, ErrFreshRole
	}
	if err != nil {
		return out, err
	}
	now := time.Now()
	if source != "native" || role != "member" && role != "admin" && role != "owner" || now.Sub(observed) > 57*time.Second || observed.After(now) {
		return out, ErrFreshRole
	}
	var allowed bool
	var targets []string
	var admission string
	action := req.action()
	if req.uploadOnly {
		action = "upload_" + req.MediaKind
		if req.PrivateConversation != "" {
			action = "upload_private_" + req.MediaKind
		}
	}
	err = tx.QueryRow(ctx, `SELECT actor_id=$2 AND group_id=$3 AND $4::uuid=ANY(account_ids)
        AND (CASE WHEN admission_mode='native_debug' THEN $7 ELSE $8 END)=ANY(actions)
        AND account_epochs->>($4::text)=$5::text AND NOT cancelled
        AND expires_at>clock_timestamp()+interval '3 seconds' AND (used<budget OR ($6 AND used<=budget))
        AND (admission_mode='package' OR actions <@ $9::text[])
        ,target_ids,admission_mode FROM test_windows WHERE id=$1 FOR UPDATE`,
		req.TestWindowID, a.ID, req.GroupID, req.AccountID, strconv.FormatInt(req.AccountEpoch, 10), reserved,
		action, req.action(), []string{"upload_image", "upload_voice", "upload_video", "upload_private_image",
			"send_text", "send_image", "send_voice", "send_video", "recall_message",
			"send_private_text", "send_private_image"}).
		Scan(&allowed, &targets, &admission)
	if err == pgx.ErrNoRows || err == nil && !allowed {
		return out, Denied(409, "native_text_test_window_invalid")
	}
	if err != nil {
		return out, err
	}
	var proofWindow bool
	out.controlledExecutionFence, proofWindow, err = controlledProofTextScope(ctx, tx, s.Vault, a.ID, expectedPlatform, req, reserved, targets)
	if err != nil {
		return out, err
	}
	if !proofWindow {
		out.controlledExecutionFence, err = controlledExecutionScope(ctx, tx, a.ID, expectedPlatform, req.GroupID)
		if err != nil {
			return out, err
		}
	}
	if reserved && out.controlledExecutionFence != req.controlledExecutionFence {
		return out, Denied(409, "controlled_onboarding_scope_changed")
	}
	if role == "member" && (admission != "native_debug" || req.uploadOnly || req.MediaKind != "") {
		return out, ErrFreshRole
	}
	if admission != "native_debug" {
		if !management || primary != req.AccountID {
			return out, Denied(409, "native_text_binding_changed")
		}
		var modules []string
		err = tx.QueryRow(ctx, `SELECT p.modules FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
            WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
            AND p.group_limit>0 AND $2=ANY(p.allowed_platforms) FOR SHARE OF sub,p`, out.tenant, expectedPlatform).Scan(&modules)
		if err == pgx.ErrNoRows {
			return out, Denied(403, "active_subscription_required")
		}
		if err != nil {
			return out, err
		}
		if !slices.Contains(modules, "moderation") {
			return out, Denied(403, "module_not_in_plan")
		}
	}
	for _, mention := range req.Mentions {
		if !slices.Contains(targets, strconv.FormatUint(mention.UID, 10)) {
			return out, Denied(409, "native_text_test_target_invalid")
		}
	}
	if req.PrivateConversation != "" {
		if req.PrivateEventID < 1 || len(req.PrivateConversation) != 64 || len(req.Mentions) != 0 ||
			req.QuoteEventID != 0 || req.MediaKind != "" && req.MediaKind != "image" {
			return out, Denied(400, "native_private_message_invalid")
		}
		if expectedPlatform == "lajiaoliao" {
			peer, fingerprint, err := s.chiliPrivateTargetForSend(ctx, tx, req, nativeUID, a.ID, reserved)
			if err != nil {
				return out, err
			}
			out.chiliPeer, out.privateDigest = peer, fingerprint
		} else {
			peer, fingerprint, err := s.privateTarget(ctx, tx, req)
			if err != nil {
				return out, err
			}
			if !slices.Contains(targets, strconv.FormatUint(peer.ID, 10)) {
				return out, Denied(409, "native_text_test_target_invalid")
			}
			out.peer, out.privateDigest = &peer, fingerprint
		}
	} else if req.PrivateEventID != 0 || len(req.PrivateDigest) != 0 {
		return out, Denied(400, "native_private_message_invalid")
	}
	var sessionFresh bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash=$1
        AND expires_at>clock_timestamp()+interval '3 seconds')`, ref.hash).Scan(&sessionFresh); err != nil {
		return out, err
	}
	if !sessionFresh {
		return out, Denied(401, "authentication_required")
	}
	out.nativeSender, out.nativeGroup = nativeUID, nativeID
	if proofWindow {
		target := nativeID
		if req.PrivateConversation != "" {
			target = out.chiliPeer
			if out.peer != nil {
				target = strconv.FormatUint(out.peer.ID, 10)
			}
		}
		if target != out.ProofTarget {
			return out, Denied(409, "onboarding_controlled_proof_window_invalid")
		}
	}
	if expectedPlatform == "lajiaoliao" {
		if !lajiaoliao.ValidID(nativeUID) || !lajiaoliao.ValidID(nativeID) ||
			!chiliTargetsAllowed(req, targets, out.chiliPeer) || primary != req.AccountID && !proofWindow {
			return out, Denied(409, "native_text_identity_unverified")
		}
		return out, nil
	}
	out.sender, err = strconv.ParseUint(nativeUID, 10, 30)
	if err != nil || out.sender == 0 || strconv.FormatUint(out.sender, 10) != nativeUID {
		return out, Denied(409, "native_text_identity_unverified")
	}
	out.group, err = strconv.ParseUint(nativeID, 10, 30)
	if err != nil || out.group == 0 || strconv.FormatUint(out.group, 10) != nativeID {
		return out, Denied(409, "native_text_identity_unverified")
	}
	return out, nil
}

func (s *NativeTextService) Stage(ctx context.Context, ref SessionRef, req TextRequest) (TextOperation, error) {
	var out TextOperation
	if !ValidID(req.AccountID) || !ValidID(req.GroupID) || !ValidID(req.TestWindowID) ||
		len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 100 || req.AccountEpoch < 1 || req.BindingEpoch < 1 ||
		req.QuoteEventID < 0 || len(req.Mentions) > 32 {
		return out, Denied(400, "invalid_operation")
	}
	req.Mentions = slices.Clone(req.Mentions)
	req.ReferenceDigest = nil // Captured only from server-held source evidence.
	req.PrivateDigest = nil
	req.MediaKind = ""
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if req.MediaUploadID != "" {
		if !ValidID(req.MediaUploadID) || req.Text != "" || req.QuoteEventID != 0 || len(req.Mentions) != 0 {
			return out, Denied(400, "native_upload_invalid")
		}
		if err = tx.QueryRow(ctx, `SELECT kind FROM native_media_uploads WHERE id=$1
            AND account_id=$2 AND group_id=$3 AND conversation_key=$4`, req.MediaUploadID, req.AccountID, req.GroupID,
			req.PrivateConversation).Scan(&req.MediaKind); err != nil {
			return out, Denied(409, "native_upload_unavailable")
		}
	}
	if err = tx.QueryRow(ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1`,
		req.AccountID).Scan(&req.ReceiverOwner, &req.ReceiverRevision); err != nil {
		return out, Denied(409, "native_text_connection_changed")
	}
	scope, err := s.authorize(ctx, tx, ref, req, false)
	if err != nil {
		return out, err
	}
	req.controlledExecutionFence = scope.controlledExecutionFence
	req.PrivateDigest = scope.privateDigest
	message := nativewire.GroupText{SenderID: scope.sender, GroupID: scope.group, ClientID: NewID(),
		Text: req.Text, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if req.MediaUploadID != "" {
		message.Media, err = s.mediaReceipt(ctx, tx, scope.actor.ID, req, "")
		if err != nil {
			return out, err
		}
	}
	reference, err := s.textComponents(ctx, tx, req, scope, &message)
	if err != nil {
		return out, err
	}
	if reference != nil {
		req.ReferenceDigest = reference.digest()
	}
	plain, err := req.encode(message, scope.peer)
	clear(plain)
	if err != nil {
		return out, Denied(400, "invalid_text_message")
	}
	hash := security.Digest(digest(req))
	var oldHash []byte
	err = tx.QueryRow(ctx, `SELECT id::text,status,result_code,request_hash FROM operations
        WHERE actor_id=$1 AND idempotency_key=$2`, scope.actor.ID, req.IdempotencyKey).
		Scan(&out.ID, &out.Status, &out.ResultCode, &oldHash)
	if err == nil {
		if !bytes.Equal(hash, oldHash) {
			return out, Denied(409, "idempotency_conflict")
		}
		return out, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,798451))`, req.AccountID); err != nil {
		return out, err
	}
	var queued int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM operations WHERE account_id=$1 AND status='queued'`,
		req.AccountID).Scan(&queued); err != nil {
		return out, err
	}
	if queued >= 16 {
		return out, Denied(429, "native_text_queue_full")
	}
	out.ID, out.Status = NewID(), "queued"
	body, _ := json.Marshal(req)
	sealed, err := s.Vault.Seal("native-text:"+out.ID, body)
	clear(body)
	if err != nil {
		return out, err
	}
	nonce, err := reserveNativeNonce(ctx, tx)
	if err != nil {
		return out, err
	}
	var aad [8]byte
	if _, err = rand.Read(aad[:]); err != nil {
		return out, err
	}
	aadNumber := binary.LittleEndian.Uint64(aad[:])
	if aadNumber == 0 {
		return out, errors.New("native_random_unavailable")
	}
	_, err = tx.Exec(ctx, `INSERT INTO operations(id,tenant_id,actor_id,account_id,group_id,test_window_id,
        idempotency_key,request_hash,kind,payload_sealed,account_epoch,binding_epoch,expires_at,conversation_key)
        SELECT $1,$2,$3,$4,$5,$6,$7,$8,$12,$9,$10,$11,least(expires_at,clock_timestamp()+interval '60 seconds'),$13
        FROM test_windows WHERE id=$6`, out.ID, scope.tenant, scope.actor.ID, req.AccountID, req.GroupID,
		req.TestWindowID, req.IdempotencyKey, hash, sealed, req.AccountEpoch, req.BindingEpoch, req.kind(), req.PrivateConversation)
	if err != nil {
		return out, err
	}
	if req.MediaUploadID != "" {
		tag, e := tx.Exec(ctx, `UPDATE native_media_uploads SET operation_id=$2 WHERE id=$1 AND operation_id IS NULL`,
			req.MediaUploadID, out.ID)
		if e != nil {
			return out, e
		}
		if tag.RowsAffected() != 1 {
			return out, Denied(409, "native_upload_already_used")
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO native_text_attempts(operation_id,client_id,nonce,aad,message_at)
        VALUES($1,$2,$3::numeric,$4::numeric,$5)`, out.ID, message.ClientID, nonce, strconv.FormatUint(aadNumber, 10), message.CreatedAt)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        VALUES($1,$2,'native_text_staged',$3,'{"mode":"isolated_test"}')`, req.auditTenant(scope.tenant), scope.actor.ID, out.ID)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// Dispatch persists running before transport and never retries running/unknown.
// The final transaction holds authorization locks only across one bounded write,
// not encryption, HTTP directory/model work or the receipt wait.
func (s *NativeTextService) Dispatch(ctx context.Context, ref SessionRef, id string, port TextPort) (TextOperation, error) {
	out := TextOperation{ID: id}
	if !ValidID(id) || port == nil {
		return out, Denied(409, "native_text_transport_unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='750ms'`); err != nil {
		return out, err
	}
	var sealed []byte
	var nonce, aad string
	var dispatch TextDispatch
	var deadline time.Time
	err = tx.QueryRow(ctx, `SELECT o.status,o.result_code,o.payload_sealed,o.expires_at,
        t.client_id::text,t.nonce::text,t.aad::text,t.message_at FROM operations o
        JOIN native_text_attempts t ON t.operation_id=o.id
        WHERE o.id=$1 AND o.actor_id=$2 AND o.kind IN('send_text','send_image','send_voice','send_video','send_file')
        FOR UPDATE OF o,t`, id, a.ID).
		Scan(&out.Status, &out.ResultCode, &sealed, &deadline, &dispatch.Message.ClientID, &nonce, &aad, &dispatch.Message.CreatedAt)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "operation_not_found")
	}
	if err != nil {
		return out, err
	}
	if out.Status != "queued" {
		return out, nil
	}
	raw, err := s.Vault.Open("native-text:"+id, sealed)
	if err != nil {
		return out, err
	}
	var req TextRequest
	err = json.Unmarshal(raw, &req)
	clear(raw)
	if err != nil {
		return out, err
	}
	scope, err := s.authorize(ctx, tx, ref, req, false)
	if err != nil {
		return out, err
	}
	if scope.controlledExecutionFence != req.controlledExecutionFence {
		return out, Denied(409, "controlled_onboarding_scope_changed")
	}
	if !deadline.After(time.Now()) {
		return out, Denied(409, "operation_expired")
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,798451))`, req.AccountID); err != nil {
		return out, err
	}
	var blocked bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE account_id=$1
        AND id<>$2 AND status IN('running','unknown')) OR EXISTS(SELECT 1 FROM native_setting_operations
        WHERE account_id=$1 AND status IN('preparing','submitting','unknown'))`, req.AccountID, id).Scan(&blocked)
	if err != nil {
		return out, err
	}
	if blocked {
		return out, Denied(409, "native_text_unresolved_submission")
	}
	dispatch.OperationID, dispatch.AccountID, dispatch.AccountEpoch = id, req.AccountID, req.AccountEpoch
	dispatch.ReceiverOwner, dispatch.ReceiverRevision = req.ReceiverOwner, req.ReceiverRevision
	dispatch.Message.SenderID, dispatch.Message.GroupID, dispatch.Message.Text = scope.sender, scope.group, req.Text
	dispatch.PrivatePeer, dispatch.PrivateConversation, dispatch.PrivateEventID = scope.peer, req.PrivateConversation, req.PrivateEventID
	if req.MediaUploadID != "" {
		dispatch.Message.Media, err = s.mediaReceipt(ctx, tx, a.ID, req, id)
		if err != nil {
			return out, err
		}
	}
	if req.QuoteEventID > 0 && len(req.ReferenceDigest) != 64 {
		return out, Denied(409, "native_text_reference_changed")
	}
	reference, err := s.textComponents(ctx, tx, req, scope, &dispatch.Message)
	if err != nil {
		return out, err
	}
	dispatch.QuoteEventID = req.QuoteEventID
	encoded, err := req.encode(dispatch.Message, scope.peer)
	clear(encoded)
	if err != nil {
		return out, Denied(400, "invalid_text_message")
	}
	dispatch.Nonce, err = strconv.ParseUint(nonce, 10, 64)
	if err != nil {
		return out, err
	}
	dispatch.AAD, err = strconv.ParseUint(aad, 10, 64)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE test_windows SET used=used+1 WHERE id=$1`, req.TestWindowID); err != nil {
		return out, err
	}
	if err = req.controlledExecutionFence.reserveNative(ctx, tx, a.ID, "wsl", req.GroupID, "native_text", id); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE operations SET status='running',updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return out, err
	}
	// This timestamp is a durable possible-submission boundary, not proof of a write.
	if _, err = tx.Exec(ctx, `UPDATE native_text_attempts SET submitted_at=clock_timestamp() WHERE operation_id=$1`, id); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err // no native call on an uncertain intent commit
	}
	ctx, stop := context.WithDeadline(ctx, deadline)
	defer stop()
	var called, attempted, fenced, routed atomic.Bool
	var recorded atomic.Pointer[nativewire.TextRoute]
	dispatch.RecordRoute = func(work context.Context, proof nativewire.TextRoute) error {
		team, e := strconv.ParseUint(proof.Team, 10, 64)
		if !routed.CompareAndSwap(false, true) || e != nil || team == 0 ||
			strconv.FormatUint(team, 10) != proof.Team || proof.Sender == "" || len(proof.Sender) > 128 ||
			proof.DeploymentVersion < 1 || len(proof.Digest) != 32 || proof.ObservedAt.After(time.Now()) ||
			time.Since(proof.ObservedAt) > 5*time.Second {
			return errors.New("native_text_route_unverified")
		}
		if scope.peer == nil && proof.PrivatePeer != "" || scope.peer != nil && proof.PrivatePeer != scope.peer.Transport {
			return errors.New("native_text_route_unverified")
		}
		// Persist routing evidence before any socket write. The body stays in
		// the sealed intent; neither the model nor browser can supply this.
		tag, e := s.DB.Platform.Exec(work, `INSERT INTO native_text_routes(operation_id,team,sender,
            deployment_version,attachment_digest,observed_at,private_peer)
            SELECT o.id,$2,$3,$4,$5,$6,$11 FROM operations o JOIN account_receivers r ON r.account_id=o.account_id
            WHERE o.id=$1 AND o.status='running' AND o.account_id=$7 AND o.account_epoch=$8
            AND r.owner=$9 AND r.revision=$10 AND r.enabled AND r.lease_until>clock_timestamp()`,
			id, proof.Team, proof.Sender, proof.DeploymentVersion, proof.Digest, proof.ObservedAt,
			req.AccountID, req.AccountEpoch, req.ReceiverOwner, req.ReceiverRevision, proof.PrivatePeer)
		if e != nil || tag.RowsAffected() != 1 {
			return errors.New("native_text_route_unavailable")
		}
		proof.Digest = bytes.Clone(proof.Digest)
		recorded.Store(&proof)
		return nil
	}
	fence := func(work context.Context, write func(context.Context) error) error {
		route := recorded.Load()
		if !called.CompareAndSwap(false, true) || write == nil || route == nil {
			return errors.New("native_submission_repeated")
		}
		work, done := context.WithTimeout(work, 2*time.Second)
		defer done()
		check, e := s.DB.Platform.Begin(work)
		if e != nil {
			return e
		}
		defer check.Rollback(work)
		// The reservation consumed one test action. Temporarily allow exactly
		// that reserved slot in the final check, never another dispatch.
		finalScope, e := s.authorizeReserved(work, check, ref, req, id)
		if e != nil {
			return e
		}
		if finalScope.actor.ID != a.ID || finalScope.tenant != scope.tenant ||
			finalScope.sender != scope.sender || finalScope.group != scope.group || !deadline.After(time.Now().Add(2*time.Second)) {
			return errors.New("native_text_scope_changed")
		}
		if (finalScope.peer == nil) != (scope.peer == nil) || scope.peer != nil && *scope.peer != *finalScope.peer {
			return errors.New("native_private_target_changed")
		}
		if reference != nil {
			current, e := s.textReference(work, check, req)
			if e != nil || !reference.equal(current) {
				return errors.New("native_text_reference_changed")
			}
		}
		if req.MediaUploadID != "" {
			current, e := s.mediaReceipt(work, check, a.ID, req, id)
			if e != nil || dispatch.Message.Media == nil || *current != *dispatch.Message.Media {
				return errors.New("native_upload_scope_changed")
			}
		}
		var version int64
		if e = check.QueryRow(work, `SELECT version FROM native_deployment FOR SHARE`).Scan(&version); e != nil ||
			version != route.DeploymentVersion || route.ObservedAt.After(time.Now()) ||
			time.Since(route.ObservedAt) > 5*time.Second {
			return errors.New("native_text_route_changed")
		}
		if e = req.controlledExecutionFence.requireSubmission(work, check, 2*time.Second); e != nil {
			return e
		}
		if e = work.Err(); e != nil {
			return e
		}
		attempted.Store(true)
		e = write(work)
		if e != nil {
			return e
		}
		e = check.Commit(work)
		if e == nil {
			fenced.Store(true)
		}
		return e
	}
	result, sendErr := port.SubmitText(ctx, dispatch, fence)
	out.Status, out.ResultCode = "failed", "native_text_not_submitted"
	if attempted.Load() || result.Attempted {
		out.Status, out.ResultCode = "unknown", "native_text_result_unknown"
		if sendErr == nil && fenced.Load() && attempted.Load() && result.Attempted {
			if result.Code != 200 && result.Code >= 100 {
				out.Status, out.ResultCode = "failed", "native_text_rejected"
			} else if result.Code == 200 && result.Receipt.ClientID == dispatch.Message.ClientID &&
				validReceipt(result.Receipt) {
				out.Status, out.ResultCode = "succeeded", "im_server_accepted"
			}
		}
	}
	// Cancellation or expiry must not erase evidence of an attempted submission.
	save, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer done()
	finish, err := s.DB.Platform.Begin(save)
	if err != nil {
		return TextOperation{ID: id, Status: "unknown", ResultCode: "native_text_reconciliation_required"}, err
	}
	defer finish.Rollback(save)
	tag, err := finish.Exec(save, `UPDATE operations SET status=$2,result_code=$3,updated_at=clock_timestamp()
        WHERE id=$1 AND status='running'`, id, out.Status, out.ResultCode)
	if err != nil || tag.RowsAffected() != 1 {
		return TextOperation{ID: id, Status: "unknown", ResultCode: "native_text_reconciliation_required"}, errors.New("native_text_finish_unavailable")
	}
	var serverID, serverTime *string
	if out.Status == "succeeded" {
		serverID, serverTime = &result.Receipt.ServerID, &result.Receipt.TimeMS
	}
	if _, err = finish.Exec(save, `UPDATE native_text_attempts SET server_id=$2::numeric,
        server_time_ms=$3::numeric,finished_at=clock_timestamp() WHERE operation_id=$1`, id, serverID, serverTime); err != nil {
		return TextOperation{ID: id, Status: "unknown", ResultCode: "native_text_reconciliation_required"}, err
	}
	_, err = finish.Exec(save, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        VALUES($1,$2,'native_text_finished',$3,jsonb_build_object('status',$4::text,'result_code',$5::text))`,
		req.auditTenant(scope.tenant), a.ID, id, out.Status, out.ResultCode)
	if err != nil {
		return TextOperation{ID: id, Status: "unknown", ResultCode: "native_text_reconciliation_required"}, err
	}
	if err = finish.Commit(save); err != nil {
		return TextOperation{ID: id, Status: "unknown", ResultCode: "native_text_reconciliation_required"}, err
	}
	return out, nil
}

// Expire records abandoned attempts conservatively. It does not resend, query a
// native endpoint or claim delivery. The grace exceeds Dispatch's maximum eight
// seconds plus its three-second evidence write. Each pass is bounded and skips
// rows held by a live submission or another recovery pass.
// RecoverInterrupted records process loss without replaying any submission.
func (s *NativeTextService) RecoverInterrupted(ctx context.Context) error {
	_, err := s.DB.Platform.Exec(ctx, `WITH changed AS (
        UPDATE operations SET status='unknown',result_code='process_restarted',updated_at=now()
        WHERE status='running'
        RETURNING id,CASE WHEN conversation_key='' THEN tenant_id ELSE NULL END AS tenant_id,actor_id,status,result_code)
        INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        SELECT c.tenant_id,c.actor_id,'native_text_recovered',c.id::text,
        jsonb_build_object('status',c.status,'result_code',c.result_code)
        FROM changed c WHERE EXISTS(SELECT 1 FROM native_text_attempts t WHERE t.operation_id=c.id)
            OR EXISTS(SELECT 1 FROM lajiaoliao_media_attempts t WHERE t.operation_id=c.id)`)
	return err
}

func (s *NativeTextService) Expire(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='750ms'`); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `WITH candidates AS (
        SELECT o.id FROM operations o
        WHERE (EXISTS(SELECT 1 FROM native_text_attempts t WHERE t.operation_id=o.id)
            OR EXISTS(SELECT 1 FROM lajiaoliao_media_attempts t WHERE t.operation_id=o.id))
        AND o.kind IN('send_text','send_image','send_voice','send_video','send_file') AND
        ((o.status='queued' AND o.expires_at<clock_timestamp())
        OR (o.status='running' AND o.expires_at<clock_timestamp()-interval '15 seconds'))
        ORDER BY o.expires_at,o.id LIMIT 100 FOR UPDATE OF o SKIP LOCKED
    ), recovered AS (
        UPDATE operations o SET
        status=CASE WHEN o.status='queued' THEN 'expired' ELSE 'unknown' END,
        result_code=CASE WHEN o.status='queued' THEN 'native_text_expired' ELSE 'native_text_reconciliation_required' END,
        updated_at=clock_timestamp() FROM candidates c WHERE o.id=c.id
        RETURNING o.id,CASE WHEN o.conversation_key='' THEN o.tenant_id ELSE NULL END AS tenant_id,o.actor_id,o.status,o.result_code
    ) INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        SELECT tenant_id,actor_id,'native_text_recovered',id,
        jsonb_build_object('status',status,'result_code',result_code) FROM recovered`)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `WITH batch AS (
        SELECT id FROM native_media_uploads
        WHERE status IN('preparing','submitting') AND created_at<clock_timestamp()-interval '30 seconds'
        ORDER BY created_at,id LIMIT 100 FOR UPDATE SKIP LOCKED
    ) UPDATE native_media_uploads SET
        status=CASE WHEN status='preparing' THEN 'expired' ELSE 'unknown' END,
        result_code='native_upload_result_unknown',finished_at=clock_timestamp()
        WHERE id IN(SELECT id FROM batch)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `WITH batch AS (
        SELECT id FROM native_media_uploads WHERE payload_sealed IS NOT NULL
        AND created_at<clock_timestamp()-interval '7 days'
        ORDER BY created_at,id LIMIT 100 FOR UPDATE SKIP LOCKED
    ) UPDATE native_media_uploads SET payload_sealed=NULL WHERE id IN(SELECT id FROM batch)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `WITH batch AS (
        SELECT t.operation_id FROM lajiaoliao_media_attempts t JOIN operations o ON o.id=t.operation_id
        WHERE t.receipt_sealed IS NOT NULL AND o.created_at<clock_timestamp()-interval '7 days'
        ORDER BY o.created_at,o.id LIMIT 100 FOR UPDATE OF t SKIP LOCKED
    ) UPDATE lajiaoliao_media_attempts SET receipt_sealed=NULL WHERE operation_id IN(SELECT operation_id FROM batch)`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r TextRequest) action() string {
	if r.PrivateConversation != "" {
		return "send_private_" + r.mediaOrText()
	}
	return r.kind()
}

func (r TextRequest) kind() string { return "send_" + r.mediaOrText() }

func (r TextRequest) mediaOrText() string {
	if nativewire.MediaKind(r.MediaKind) {
		return r.MediaKind
	}
	return "text"
}

func (r TextRequest) auditTenant(tenant string) any {
	if r.PrivateConversation != "" {
		return nil
	}
	return tenant
}

func validReceipt(r nativewire.SendReceipt) bool {
	for _, value := range []string{r.ServerID, r.TimeMS} {
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != value {
			return false
		}
	}
	return true
}

func (s *NativeTextService) authorizeReserved(ctx context.Context, tx pgx.Tx, ref SessionRef, req TextRequest, id string) (textScope, error) {
	scope, err := s.authorize(ctx, tx, ref, req, true)
	if err != nil {
		return scope, err
	}
	// Authorize against the reserved budget without mutating the window. Its
	// row is locked; only this already-running intent may consume its own slot.
	var used int
	var running bool
	err = tx.QueryRow(ctx, `SELECT w.used,o.status='running' AND o.test_window_id=w.id
        AND o.actor_id=w.actor_id FROM test_windows w JOIN operations o ON o.id=$2
        WHERE w.id=$1 FOR UPDATE OF w FOR SHARE OF o`, req.TestWindowID, id).Scan(&used, &running)
	if err != nil || !running || used < 1 {
		return textScope{}, errors.New("native_text_reservation_changed")
	}
	return scope, nil
}
