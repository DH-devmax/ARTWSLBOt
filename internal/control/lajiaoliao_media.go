package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

type ChiliMediaDispatch struct {
	ID, Sender, Group, Peer, ClientID string
	Request                           TextRequest
	Asset                             lajiaoliao.MediaAsset
	Input                             nativeauth.MessageUpload
	Message                           lajiaoliao.Outbound
}

type ChiliMediaPort interface {
	UploadChiliMedia(context.Context, ChiliMediaDispatch, func(context.Context) error) (lajiaoliao.MediaAsset, error)
	SubmitChiliMedia(context.Context, ChiliMediaDispatch, nativewire.SubmissionFence) (lajiaoliao.Receipt, error)
}

type ChiliMediaEvidencePort interface {
	ChiliMediaEvidence(context.Context, lajiaoliao.MediaEvidenceQuery) (lajiaoliao.MediaEvidence, error)
}

type TextEvidenceCheck = lajiaoliao.EvidenceCheck

func validChiliMediaRequest(req TextRequest) bool {
	return validChiliMessageRequest(req) && req.Text == "" && len(req.Mentions) == 0 &&
		req.QuoteEventID == 0 && req.ChiliMentionID == ""
}

func (s *NativeTextService) UploadChiliMedia(ctx context.Context, ref SessionRef, req TextRequest,
	input nativeauth.MessageUpload, port ChiliMediaPort) (MediaUploadOperation, error) {
	var out MediaUploadOperation
	if input.Kind != "image" && input.Kind != "voice" && input.Kind != "video" {
		if input.Kind == "file" {
			return out, Denied(409, "native_capability_not_in_product")
		}
		return out, Denied(409, "native_media_platform_unavailable")
	}
	if port == nil {
		return out, Denied(409, "native_media_acceptance_pending")
	}
	req.MediaKind = input.Kind
	if !validChiliMediaRequest(req) || req.MediaUploadID != "" {
		return out, Denied(400, "native_upload_invalid")
	}
	meta, err := nativeauth.InspectUpload(input)
	if err != nil {
		return out, err
	}
	if input.Kind == "video" {
		width, height, err := lajiaoliao.SnapshotDimensions(input.Thumbnail)
		if err != nil {
			return out, err
		}
		meta.Width, meta.Height = uint64(width), uint64(height)
	}
	ctx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1`, req.AccountID).
		Scan(&req.ReceiverOwner, &req.ReceiverRevision); err != nil {
		return out, Denied(409, "native_text_connection_changed")
	}
	scope, err := s.authorizeSendPlatform(ctx, tx, ref, req, false, "lajiaoliao")
	if err != nil {
		return out, err
	}
	req.controlledExecutionFence = scope.controlledExecutionFence
	req.PrivateDigest = scope.privateDigest
	data, cover := sha256.Sum256(input.Data), sha256.Sum256(input.Thumbnail)
	hash := security.Digest(digest(struct {
		Request     TextRequest
		Data, Cover [32]byte
		Name        string
	}{req, data, cover, input.Name}))
	var previous []byte
	err = tx.QueryRow(ctx, `SELECT id::text,status,result_code,kind,request_hash FROM native_media_uploads
		WHERE actor_id=$1 AND idempotency_key=$2`, scope.actor.ID, req.IdempotencyKey).
		Scan(&out.ID, &out.Status, &out.ResultCode, &out.Kind, &previous)
	if err == nil {
		if !bytes.Equal(hash, previous) {
			return out, Denied(409, "idempotency_conflict")
		}
		return out, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if err = s.chiliMediaAvailable(ctx, tx, req, scope, ""); err != nil {
		return out, err
	}
	out = MediaUploadOperation{ID: NewID(), Kind: input.Kind, Status: "preparing", TestWindowID: req.TestWindowID}
	_, err = tx.Exec(ctx, `INSERT INTO native_media_uploads(id,tenant_id,actor_id,account_id,group_id,
		test_window_id,idempotency_key,request_hash,kind,account_epoch,binding_epoch,receiver_owner,receiver_revision,status,conversation_key)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'preparing',$14)`,
		out.ID, scope.tenant, scope.actor.ID, req.AccountID, req.GroupID, req.TestWindowID, req.IdempotencyKey,
		hash, input.Kind, req.AccountEpoch, req.BindingEpoch, req.ReceiverOwner, req.ReceiverRevision, req.PrivateConversation)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	var called, submitted atomic.Bool
	guard := func(work context.Context) error {
		if !called.CompareAndSwap(false, true) {
			return errors.New("native_submission_repeated")
		}
		check, err := s.DB.Platform.Begin(work)
		if err != nil {
			return err
		}
		defer check.Rollback(work)
		current, err := s.authorizeSendPlatform(work, check, ref, req, false, "lajiaoliao")
		if err != nil {
			return err
		}
		if current.actor.ID != scope.actor.ID || current.tenant != scope.tenant ||
			current.nativeSender != scope.nativeSender || current.nativeGroup != scope.nativeGroup || current.chiliPeer != scope.chiliPeer {
			return Denied(409, "native_upload_scope_changed")
		}
		if current.controlledExecutionFence != scope.controlledExecutionFence {
			return Denied(409, "controlled_onboarding_scope_changed")
		}
		if err = scope.controlledExecutionFence.requireSubmission(work, check, 0); err != nil {
			return err
		}
		if err = ReserveControlledNativeTx(work, check, "lajiaoliao", req.GroupID, "native_upload", out.ID); err != nil {
			return err
		}
		tag, err := check.Exec(work, `UPDATE native_media_uploads SET status='submitting'
			WHERE id=$1 AND status='preparing' AND expires_at>clock_timestamp()+interval '3 seconds'`, out.ID)
		if err != nil || tag.RowsAffected() != 1 {
			return Denied(409, "native_upload_scope_changed")
		}
		if _, err = check.Exec(work, `UPDATE test_windows SET used=used+1 WHERE id=$1`, req.TestWindowID); err != nil {
			return err
		}
		submitted.Store(true)
		return check.Commit(work)
	}
	asset, uploadErr := port.UploadChiliMedia(ctx, ChiliMediaDispatch{ID: out.ID, Sender: scope.nativeSender,
		Group: scope.nativeGroup, Peer: scope.chiliPeer, Request: req, Input: input}, guard)
	out.Status, out.ResultCode = "failed", "native_upload_not_submitted"
	if submitted.Load() {
		out.Status, out.ResultCode = "unknown", "native_upload_result_unknown"
		if uploadErr == nil && asset.Validate() == nil && asset.ID == out.ID && asset.Kind == input.Kind &&
			asset.Size == int64(len(input.Data)) && asset.Duration == int64(meta.Duration) &&
			asset.Width == int64(meta.Width) && asset.Height == int64(meta.Height) &&
			asset.SnapshotSize == int64(len(input.Thumbnail)) {
			out.Status, out.ResultCode = "succeeded", "native_upload_received"
		}
	}
	var sealed []byte
	if out.Status == "succeeded" {
		raw, _ := json.Marshal(asset)
		sealed, err = s.Vault.Seal("lajiaoliao-media:"+out.ID, raw)
		clear(raw)
		if err != nil {
			return MediaUploadOperation{ID: out.ID, Kind: out.Kind, Status: "unknown", ResultCode: "native_upload_result_unknown"}, err
		}
	}
	save, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	persistenceFailure := func(err error) (MediaUploadOperation, error) {
		return MediaUploadOperation{ID: out.ID, Kind: out.Kind, Status: "unknown", ResultCode: "native_upload_result_unknown"}, err
	}
	finish, err := s.DB.Platform.Begin(save)
	if err != nil {
		return persistenceFailure(err)
	}
	defer finish.Rollback(save)
	tag, err := finish.Exec(save, `UPDATE native_media_uploads SET status=$2,result_code=$3,
		payload_sealed=$4,finished_at=clock_timestamp() WHERE id=$1 AND status IN('preparing','submitting')`,
		out.ID, out.Status, out.ResultCode, sealed)
	if err != nil || tag.RowsAffected() != 1 {
		return persistenceFailure(Denied(503, "native_upload_result_unknown"))
	}
	if _, err = finish.Exec(save, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		VALUES($1,$2,'native_media_uploaded',$3,jsonb_build_object('platform','lajiaoliao','kind',$4::text,'status',$5::text))`,
		scope.tenant, scope.actor.ID, out.ID, out.Kind, out.Status); err != nil {
		return persistenceFailure(err)
	}
	if err = finish.Commit(save); err != nil {
		return persistenceFailure(err)
	}
	return s.InspectMediaUpload(ctx, ref, req.AccountID, out.ID, "")
}

func (s *NativeTextService) chiliMediaAvailable(ctx context.Context, tx pgx.Tx, req TextRequest, scope textScope, operation string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,798451))`, req.AccountID); err != nil {
		return err
	}
	var blocked bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM native_media_uploads WHERE account_id=$1
		AND status IN('preparing','submitting')) OR EXISTS(SELECT 1 FROM operations WHERE account_id=$1
		AND ($2='' OR id::text<>$2) AND status='running') OR EXISTS(SELECT 1 FROM native_setting_operations
		WHERE account_id=$1 AND status IN('preparing','submitting','unknown'))`, req.AccountID, operation).Scan(&blocked)
	if err != nil {
		return err
	}
	if blocked {
		return Denied(409, "native_text_unresolved_submission")
	}
	return s.chiliUnknownProofAvailable(ctx, tx, req, scope, operation)
}

func (s *NativeTextService) chiliMediaReceipt(ctx context.Context, tx pgx.Tx, actor string, req TextRequest, operation string) (lajiaoliao.MediaAsset, error) {
	var out lajiaoliao.MediaAsset
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT payload_sealed FROM native_media_uploads WHERE id=$1 AND actor_id=$2
		AND account_id=$3 AND group_id=$4 AND test_window_id=$5 AND kind=$6 AND account_epoch=$7 AND binding_epoch=$8
		AND receiver_owner=$9 AND receiver_revision=$10 AND status='succeeded' AND conversation_key=$12
		AND (operation_id IS NULL OR $11<>'' AND operation_id::text=$11)
		AND expires_at>clock_timestamp()+interval '3 seconds' FOR UPDATE`, req.MediaUploadID, actor,
		req.AccountID, req.GroupID, req.TestWindowID, req.MediaKind, req.AccountEpoch, req.BindingEpoch,
		req.ReceiverOwner, req.ReceiverRevision, operation, req.PrivateConversation).Scan(&sealed)
	if err != nil {
		return out, Denied(409, "native_upload_unavailable")
	}
	raw, err := s.Vault.Open("lajiaoliao-media:"+req.MediaUploadID, sealed)
	if err != nil {
		return out, Denied(409, "native_upload_unavailable")
	}
	defer clear(raw)
	if json.Unmarshal(raw, &out) != nil || out.Validate() != nil || out.Kind != req.MediaKind || out.ID != req.MediaUploadID {
		return out, Denied(409, "native_upload_unavailable")
	}
	return out, nil
}

func (s *NativeTextService) StageChiliMedia(ctx context.Context, ref SessionRef, req TextRequest) (TextOperation, error) {
	if !ValidID(req.MediaUploadID) {
		return TextOperation{}, Denied(400, "native_upload_invalid")
	}
	return s.StageChiliMessage(ctx, ref, req)
}

func (s *NativeTextService) StageChiliMessage(ctx context.Context, ref SessionRef, req TextRequest) (TextOperation, error) {
	var out TextOperation
	if req.MediaUploadID != "" && !ValidID(req.MediaUploadID) {
		return out, Denied(400, "native_upload_invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	req.MediaKind, req.ReferenceDigest, req.PrivateDigest = "", nil, nil
	if req.MediaUploadID != "" {
		if err = tx.QueryRow(ctx, `SELECT kind FROM native_media_uploads WHERE id=$1 AND account_id=$2
			AND group_id=$3 AND conversation_key=$4`, req.MediaUploadID, req.AccountID, req.GroupID, req.PrivateConversation).
			Scan(&req.MediaKind); err != nil {
			return out, Denied(409, "native_upload_unavailable")
		}
	}
	if !validChiliMessageRequest(req) {
		return out, Denied(400, "native_text_request_invalid")
	}
	if err = tx.QueryRow(ctx, `SELECT owner,revision FROM account_receivers WHERE account_id=$1`, req.AccountID).
		Scan(&req.ReceiverOwner, &req.ReceiverRevision); err != nil {
		return out, Denied(409, "native_text_connection_changed")
	}
	scope, err := s.authorizeSendPlatform(ctx, tx, ref, req, false, "lajiaoliao")
	if err != nil {
		return out, err
	}
	req.controlledExecutionFence = scope.controlledExecutionFence
	req.PrivateDigest = scope.privateDigest
	if req.QuoteEventID > 0 {
		_, req.ReferenceDigest, err = s.chiliSource(ctx, tx, req, req.QuoteEventID, "group", "")
		if err != nil {
			return out, err
		}
	}
	hash := security.Digest(digest(req))
	var previous []byte
	err = tx.QueryRow(ctx, `SELECT id::text,status,result_code,request_hash FROM operations
		WHERE actor_id=$1 AND idempotency_key=$2`, scope.actor.ID, req.IdempotencyKey).
		Scan(&out.ID, &out.Status, &out.ResultCode, &previous)
	if err == nil {
		if !bytes.Equal(hash, previous) {
			return out, Denied(409, "idempotency_conflict")
		}
		return out, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	message, err := s.chiliMessage(ctx, tx, req, scope, "")
	if err != nil {
		return out, err
	}
	content, _, err := message.Content()
	if err != nil {
		return out, err
	}
	contentHash := sha256.Sum256(content)
	clear(content)
	if err = s.chiliMediaAvailable(ctx, tx, req, scope, ""); err != nil {
		return out, err
	}
	var queued int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM operations WHERE account_id=$1 AND status='queued'`, req.AccountID).
		Scan(&queued); err != nil {
		return out, err
	}
	if queued >= 16 {
		return out, Denied(429, "native_text_queue_full")
	}
	out = TextOperation{ID: NewID(), Status: "queued"}
	raw, _ := json.Marshal(req)
	sealed, err := s.Vault.Seal("lajiaoliao-media-intent:"+out.ID, raw)
	clear(raw)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO operations(id,tenant_id,actor_id,account_id,group_id,test_window_id,
		idempotency_key,request_hash,kind,payload_sealed,account_epoch,binding_epoch,expires_at,conversation_key)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,least(expires_at,clock_timestamp()+interval '60 seconds'),$13
		FROM test_windows WHERE id=$6`, out.ID, scope.tenant, scope.actor.ID, req.AccountID, req.GroupID, req.TestWindowID,
		req.IdempotencyKey, hash, req.kind(), sealed, req.AccountEpoch, req.BindingEpoch, req.PrivateConversation)
	if err != nil {
		return out, err
	}
	if req.MediaUploadID != "" {
		tag, err := tx.Exec(ctx, `UPDATE native_media_uploads SET operation_id=$2 WHERE id=$1 AND operation_id IS NULL`, req.MediaUploadID, out.ID)
		if err != nil || tag.RowsAffected() != 1 {
			return out, Denied(409, "native_upload_already_used")
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO lajiaoliao_media_attempts(operation_id,client_id,native_sender,native_group,content_digest)
		VALUES($1,$2,$3,$4,$5)`, out.ID, NewID(), scope.nativeSender, scope.nativeGroup, contentHash[:])
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		VALUES($1,$2,'native_media_staged',$3,'{"platform":"lajiaoliao","mode":"isolated_test"}')`,
		scope.tenant, scope.actor.ID, out.ID); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *NativeTextService) DispatchChiliMedia(ctx context.Context, ref SessionRef, id string, port ChiliMediaPort) (TextOperation, error) {
	out := TextOperation{ID: id}
	if !ValidID(id) || port == nil {
		return out, Denied(409, "native_media_acceptance_pending")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
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
	var sealed, contentDigest []byte
	var deadline time.Time
	dispatch := ChiliMediaDispatch{ID: id}
	err = tx.QueryRow(ctx, `SELECT o.status,o.result_code,o.payload_sealed,o.expires_at,
		t.client_id,t.native_sender,t.native_group,t.content_digest FROM operations o
		JOIN lajiaoliao_media_attempts t ON t.operation_id=o.id WHERE o.id=$1 AND o.actor_id=$2
		AND o.kind IN('send_text','send_image','send_voice','send_video') FOR UPDATE OF o,t`, id, actor.ID).
		Scan(&out.Status, &out.ResultCode, &sealed, &deadline, &dispatch.ClientID, &dispatch.Sender, &dispatch.Group, &contentDigest)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "operation_not_found")
	}
	if err != nil {
		return out, err
	}
	if out.Status != "queued" {
		return out, nil
	}
	raw, err := s.Vault.Open("lajiaoliao-media-intent:"+id, sealed)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &dispatch.Request)
	clear(raw)
	if err != nil {
		return out, err
	}
	req := dispatch.Request
	scope, err := s.authorizeSendPlatform(ctx, tx, ref, req, false, "lajiaoliao")
	if err != nil {
		return out, err
	}
	if scope.controlledExecutionFence != req.controlledExecutionFence {
		return out, Denied(409, "controlled_onboarding_scope_changed")
	}
	dispatch.Peer = scope.chiliPeer
	if dispatch.Sender != scope.nativeSender || dispatch.Group != scope.nativeGroup ||
		!deadline.After(time.Now().Add(3*time.Second)) {
		return out, Denied(409, "native_text_scope_changed")
	}
	if err = s.chiliMediaAvailable(ctx, tx, req, scope, id); err != nil {
		return out, err
	}
	dispatch.Message, err = s.chiliMessage(ctx, tx, req, scope, id)
	if err != nil {
		return out, err
	}
	if dispatch.Message.Media != nil {
		dispatch.Asset = *dispatch.Message.Media
	}
	content, _, err := dispatch.Message.Content()
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(content)
	clear(content)
	if !bytes.Equal(sum[:], contentDigest) {
		return out, Denied(409, "native_upload_scope_changed")
	}
	if _, err = tx.Exec(ctx, `UPDATE test_windows SET used=used+1 WHERE id=$1`, req.TestWindowID); err != nil {
		return out, err
	}
	if err = req.controlledExecutionFence.reserveNative(ctx, tx, actor.ID, "lajiaoliao", req.GroupID, "native_text", id); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE operations SET status='running',updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE lajiaoliao_media_attempts SET submitted_at=clock_timestamp() WHERE operation_id=$1`, id); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	ctx, stop := context.WithDeadline(ctx, deadline)
	defer stop()
	var called, attempted, fenced atomic.Bool
	fence := func(work context.Context, write func(context.Context) error) error {
		if !called.CompareAndSwap(false, true) || write == nil {
			return errors.New("native_submission_repeated")
		}
		work, done := context.WithTimeout(work, 2*time.Second)
		defer done()
		check, err := s.DB.Platform.Begin(work)
		if err != nil {
			return err
		}
		defer check.Rollback(work)
		current, err := s.authorizeSendPlatform(work, check, ref, req, true, "lajiaoliao")
		if err != nil {
			return err
		}
		if current.actor.ID != actor.ID || current.tenant != scope.tenant ||
			current.nativeSender != dispatch.Sender || current.nativeGroup != dispatch.Group || current.chiliPeer != dispatch.Peer ||
			!deadline.After(time.Now().Add(2*time.Second)) {
			return errors.New("native_text_scope_changed")
		}
		var reserved bool
		err = check.QueryRow(work, `SELECT o.status='running' AND o.test_window_id=w.id AND o.actor_id=w.actor_id AND w.used>=1
			FROM operations o JOIN test_windows w ON w.id=$2 WHERE o.id=$1 FOR SHARE OF o`, id, req.TestWindowID).Scan(&reserved)
		if err != nil || !reserved {
			return errors.New("native_text_reservation_changed")
		}
		message, err := s.chiliMessage(work, check, req, current, id)
		if err != nil {
			return errors.New("native_upload_scope_changed")
		}
		raw, _, err := message.Content()
		if err != nil {
			return err
		}
		checked := sha256.Sum256(raw)
		clear(raw)
		if !bytes.Equal(checked[:], contentDigest) {
			return errors.New("native_text_scope_changed")
		}
		if err = req.controlledExecutionFence.requireSubmission(work, check, 2*time.Second); err != nil {
			return err
		}
		if err = work.Err(); err != nil {
			return err
		}
		attempted.Store(true)
		if err = write(work); err != nil {
			return err
		}
		err = check.Commit(work)
		if err == nil {
			fenced.Store(true)
		}
		return err
	}
	result, sendErr := port.SubmitChiliMedia(ctx, dispatch, fence)
	out.Status, out.ResultCode = "failed", "native_text_not_submitted"
	var submission *lajiaoliao.Failure
	notSubmitted := errors.As(sendErr, &submission) && !submission.Submitted &&
		submission.Code == "lajiaoliao_not_submitted"
	if attempted.Load() && !notSubmitted {
		out.Status, out.ResultCode = "unknown", "native_text_result_unknown"
		if sendErr == nil && fenced.Load() && result.ClientID == dispatch.ClientID &&
			lajiaoliao.ValidID(result.ServerID) && result.Timestamp > 0 {
			out.Status, out.ResultCode = "succeeded", "im_server_accepted"
		}
	}
	var receipt []byte
	if out.Status == "succeeded" {
		raw, _ := json.Marshal(result)
		receipt, err = s.Vault.Seal("lajiaoliao-media-receipt:"+id, raw)
		clear(raw)
		if err != nil {
			return TextOperation{ID: id, Status: "unknown", ResultCode: "native_text_reconciliation_required"}, err
		}
	}
	save, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer done()
	persistenceFailure := func(err error) (TextOperation, error) {
		return TextOperation{ID: id, Status: "unknown", ResultCode: "native_text_reconciliation_required"}, err
	}
	finish, err := s.DB.Platform.Begin(save)
	if err != nil {
		return persistenceFailure(err)
	}
	defer finish.Rollback(save)
	tag, err := finish.Exec(save, `UPDATE operations SET status=$2,result_code=$3,updated_at=clock_timestamp()
		WHERE id=$1 AND status='running'`, id, out.Status, out.ResultCode)
	if err != nil || tag.RowsAffected() != 1 {
		return persistenceFailure(errors.New("native_text_finish_unavailable"))
	}
	if _, err = finish.Exec(save, `UPDATE lajiaoliao_media_attempts SET receipt_sealed=$2 WHERE operation_id=$1`, id, receipt); err != nil {
		return persistenceFailure(err)
	}
	if _, err = finish.Exec(save, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		VALUES($1,$2,'native_media_finished',$3,jsonb_build_object('platform','lajiaoliao','status',$4::text,'result_code',$5::text))`,
		scope.tenant, actor.ID, id, out.Status, out.ResultCode); err != nil {
		return persistenceFailure(err)
	}
	if err = finish.Commit(save); err != nil {
		return persistenceFailure(err)
	}
	return out, nil
}

func (s *NativeTextService) ReconcileChiliMedia(ctx context.Context, ref SessionRef, id string, port ChiliMediaEvidencePort) (TextOperation, error) {
	out := TextOperation{ID: id}
	if !ValidID(id) || port == nil {
		return out, Denied(400, "invalid_operation")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
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
	var q lajiaoliao.MediaEvidenceQuery
	var tenant string
	var submittedAt *time.Time
	var intent []byte
	err = tx.QueryRow(ctx, `SELECT o.status,o.result_code,o.tenant_id::text,o.account_id::text,o.account_epoch,
		t.native_sender,t.native_group,t.client_id,t.content_digest,t.submitted_at,o.payload_sealed FROM operations o
		JOIN lajiaoliao_media_attempts t ON t.operation_id=o.id WHERE o.id=$1 AND o.actor_id=$2`,
		id, actor.ID).Scan(&out.Status, &out.ResultCode, &tenant, &q.Account, &q.Epoch, &q.Sender, &q.Group, &q.ClientID, &q.Digest, &submittedAt, &intent)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "operation_not_found")
	}
	if err != nil {
		return out, err
	}
	if out.Status != "unknown" {
		return out, nil
	}
	if submittedAt == nil {
		return out, Denied(409, "native_text_evidence_conflict")
	}
	// A closed test may advance its account epoch. Historical observation keeps
	// the immutable original intent but fences this read to the same live identity.
	if err = tx.QueryRow(ctx, `SELECT epoch FROM accounts WHERE id=$1 AND platform='lajiaoliao'
		AND native_uid=$2 AND auth_state='authenticated' AND credential_sealed IS NOT NULL FOR SHARE`,
		q.Account, q.Sender).Scan(&q.CurrentEpoch); err != nil {
		return out, Denied(409, "native_text_account_changed")
	}
	q.SubmittedAt = *submittedAt
	plain, err := s.Vault.Open("lajiaoliao-media-intent:"+id, intent)
	if err != nil {
		return out, Denied(409, "native_text_evidence_unavailable")
	}
	var req TextRequest
	err = json.Unmarshal(plain, &req)
	clear(plain)
	if err != nil {
		return out, err
	}
	q.ContentType = 101
	switch {
	case req.MediaKind == "image":
		q.ContentType = 102
	case req.MediaKind == "voice":
		q.ContentType = 103
	case req.MediaKind == "video":
		q.ContentType = 104
	case req.QuoteEventID > 0:
		q.ContentType = 114
	case req.ChiliMentionID != "":
		q.ContentType = 106
	}
	if req.PrivateConversation != "" {
		q.Peer, _, err = s.chiliPrivateTargetForReconcile(ctx, tx, req, q.Sender, actor.ID, id, q.CurrentEpoch)
		if err != nil {
			return out, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	evidence, err := port.ChiliMediaEvidence(ctx, q)
	if err != nil {
		return out, nativeTextEvidenceError(err)
	}
	if !evidence.Found || evidence.Truncated {
		if evidence.Check != nil && evidence.Check.Valid() && evidence.Check.State != "matched" &&
			(evidence.Check.State == "scan_truncated") == evidence.Truncated {
			check := *evidence.Check
			out.EvidenceCheck = &check
		}
		return out, nil
	}
	source := evidence.Source
	if source == "" && evidence.EventID > 0 {
		source = "durable_echo"
	}
	if (source != "durable_echo" || evidence.EventID < 1) &&
		(source != "server_history" || evidence.EventID != 0) ||
		evidence.Receipt.ClientID != q.ClientID ||
		!lajiaoliao.ValidID(evidence.Receipt.ServerID) || evidence.Receipt.Timestamp <= 0 {
		return out, Denied(409, "native_text_evidence_conflict")
	}
	raw, _ := json.Marshal(evidence.Receipt)
	sealed, err := s.Vault.Seal("lajiaoliao-media-receipt:"+id, raw)
	clear(raw)
	if err != nil {
		return out, err
	}
	final, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer final.Rollback(ctx)
	current, err := sessionActor(ctx, final, ref, true)
	if err != nil {
		return out, err
	}
	if current.ID != actor.ID || current.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	var account string
	if err = final.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND epoch=$2 AND platform='lajiaoliao'
		AND native_uid=$3 AND auth_state='authenticated' AND credential_sealed IS NOT NULL FOR SHARE`,
		q.Account, q.CurrentEpoch, q.Sender).Scan(&account); err != nil {
		return out, Denied(409, "native_text_account_changed")
	}
	if err = final.QueryRow(ctx, `SELECT status,result_code FROM operations WHERE id=$1 AND actor_id=$2 FOR UPDATE`,
		id, actor.ID).Scan(&out.Status, &out.ResultCode); err != nil {
		return out, err
	}
	if out.Status != "unknown" {
		return out, nil
	}
	if _, err = final.Exec(ctx, `UPDATE operations SET status='succeeded',result_code='im_server_accepted_reconciled',
		updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return out, err
	}
	if _, err = final.Exec(ctx, `UPDATE lajiaoliao_media_attempts SET receipt_sealed=$2 WHERE operation_id=$1`, id, sealed); err != nil {
		return out, err
	}
	if _, err = final.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		VALUES($1,$2,'native_media_reconciled',$3,jsonb_build_object('platform','lajiaoliao','event_id',$4::bigint,
		'evidence_source',$5::text))`,
		tenant, actor.ID, id, evidence.EventID, source); err != nil {
		return out, err
	}
	if err = final.Commit(ctx); err != nil {
		return out, err
	}
	out.Status, out.ResultCode = "succeeded", "im_server_accepted_reconciled"
	return out, nil
}
