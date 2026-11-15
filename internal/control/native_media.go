package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

type MediaUploadOperation struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ResultCode   string `json:"result_code"`
	Kind         string `json:"kind"`
	TestWindowID string `json:"test_window_id,omitempty"`
}

type MediaUploadDispatch struct {
	ID      string
	Request TextRequest
	Sender  uint64
	Peer    *nativewire.PrivateTarget
	Input   nativeauth.MessageUpload
}

type MediaUploadPort interface {
	UploadMedia(context.Context, MediaUploadDispatch, func(context.Context) error) (nativewire.OutboundMedia, error)
}

func (s *NativeTextService) InspectMediaUpload(ctx context.Context, ref SessionRef, account, id, key string) (MediaUploadOperation, error) {
	var out MediaUploadOperation
	if !ValidID(account) || id != "" && !ValidID(id) || id == "" && (len(key) < 8 || len(key) > 100) {
		return out, Denied(400, "invalid_operation")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return out, err
	}
	if a.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	err = tx.QueryRow(ctx, `SELECT id::text,status,result_code,kind,test_window_id::text FROM native_media_uploads
        WHERE actor_id=$1 AND account_id=$2 AND (($3<>'' AND id::text=$3) OR ($3='' AND idempotency_key=$4))`,
		a.ID, account, id, key).Scan(&out.ID, &out.Status, &out.ResultCode, &out.Kind, &out.TestWindowID)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "operation_not_found")
	}
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// UploadMedia persists only hashes and a sealed receipt; file/thumbnail bodies
// never enter PostgreSQL, logs, caches or a filesystem. A lost result is inspected
// with the original key and is never an automatic new upload.
func (s *NativeTextService) UploadMedia(ctx context.Context, ref SessionRef, req TextRequest,
	input nativeauth.MessageUpload, port MediaUploadPort) (MediaUploadOperation, error) {
	var out MediaUploadOperation
	if port == nil {
		return out, Denied(409, "native_media_acceptance_pending")
	}
	if !ValidID(req.AccountID) || !ValidID(req.GroupID) || !ValidID(req.TestWindowID) ||
		len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 100 || req.AccountEpoch < 1 || req.BindingEpoch < 1 ||
		req.Text != "" || len(req.Mentions) != 0 || req.QuoteEventID != 0 || req.MediaUploadID != "" {
		return out, Denied(400, "native_upload_invalid")
	}
	meta, err := nativeauth.InspectUpload(input)
	if err != nil {
		return out, err
	}
	req.MediaKind = input.Kind
	// Set only by the upload entry point, never by a browser or staged send.
	req.uploadOnly = true
	ctx, stop := context.WithTimeout(ctx, 14*time.Second)
	defer stop()
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
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
	dataHash, thumbnailHash := sha256.Sum256(input.Data), sha256.Sum256(input.Thumbnail)
	hash := security.Digest(digest(struct {
		Request         TextRequest
		Meta            nativewire.OutboundMedia
		Name            string
		Data, Thumbnail [32]byte
	}{req, meta, input.Name, dataHash, thumbnailHash}))
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
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,798451))`, req.AccountID); err != nil {
		return out, err
	}
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM native_media_uploads WHERE account_id=$1
        AND status IN('preparing','submitting')) OR EXISTS(SELECT 1 FROM operations WHERE account_id=$1
        AND status IN('running','unknown')) OR EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1
        AND status IN('preparing','submitting','unknown'))`, req.AccountID).Scan(&blocked); err != nil {
		return out, err
	}
	if blocked {
		return out, Denied(409, "native_text_unresolved_submission")
	}
	out.ID, out.Kind, out.Status = NewID(), input.Kind, "preparing"
	_, err = tx.Exec(ctx, `INSERT INTO native_media_uploads(id,tenant_id,actor_id,account_id,group_id,
        test_window_id,idempotency_key,request_hash,kind,account_epoch,binding_epoch,receiver_owner,receiver_revision,status,conversation_key)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'preparing',$14)`, out.ID, scope.tenant,
		scope.actor.ID, req.AccountID, req.GroupID, req.TestWindowID, req.IdempotencyKey, hash,
		req.MediaKind, req.AccountEpoch, req.BindingEpoch, req.ReceiverOwner, req.ReceiverRevision, req.PrivateConversation)
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
		current, err := s.authorize(work, check, ref, req, false)
		if err != nil || current.actor.ID != scope.actor.ID || current.tenant != scope.tenant ||
			current.sender != scope.sender || current.group != scope.group {
			return Denied(409, "native_upload_scope_changed")
		}
		if (current.peer == nil) != (scope.peer == nil) || scope.peer != nil && *scope.peer != *current.peer {
			return Denied(409, "native_private_target_changed")
		}
		if current.controlledExecutionFence != scope.controlledExecutionFence {
			return Denied(409, "controlled_onboarding_scope_changed")
		}
		if err = scope.controlledExecutionFence.requireSubmission(work, check, 0); err != nil {
			return err
		}
		if err = ReserveControlledNativeTx(work, check, "wsl", req.GroupID, "native_upload", out.ID); err != nil {
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
		// An uncertain COMMIT cannot be retried. No network call is authorized
		// unless this returns success.
		submitted.Store(true)
		return check.Commit(work)
	}
	result, uploadErr := port.UploadMedia(ctx, MediaUploadDispatch{
		ID: out.ID, Request: req, Sender: scope.sender, Peer: scope.peer, Input: input,
	}, guard)
	out.Status, out.ResultCode = "failed", "native_upload_not_submitted"
	if submitted.Load() {
		out.Status, out.ResultCode = "unknown", "native_upload_result_unknown"
		want := meta
		want.Asset = result.Asset
		if uploadErr == nil && result.Validate() == nil && result == want {
			out.Status, out.ResultCode = "succeeded", "native_upload_received"
		} else {
			var nativeErr *nativeauth.Error
			if errors.As(uploadErr, &nativeErr) && (nativeErr.Code == "native_business_rejected" ||
				nativeErr.Code == "native_http_rejected" || nativeErr.Code == "native_rate_limited") {
				out.Status, out.ResultCode = "failed", "native_upload_rejected"
				switch nativeErr.Code {
				case "native_business_rejected":
					if nativeErr.Number > 0 && nativeErr.Number <= 999999 {
						out.ResultCode = "native_upload_business_rejected_" + strconv.Itoa(nativeErr.Number)
					}
				case "native_rate_limited":
					out.ResultCode = "native_upload_rate_limited"
				case "native_http_rejected":
					out.ResultCode = "native_upload_http_rejected"
				}
			}
		}
	}
	var sealed []byte
	if out.Status == "succeeded" {
		encoded, _ := json.Marshal(result)
		sealed, err = s.Vault.Seal("native-media:"+out.ID, encoded)
		clear(encoded)
		if err != nil {
			return MediaUploadOperation{ID: out.ID, Kind: out.Kind, Status: "unknown", ResultCode: "native_upload_result_unknown"}, err
		}
	}
	save, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer done()
	finish, err := s.DB.Platform.Begin(save)
	if err != nil {
		return out, err
	}
	defer finish.Rollback(save)
	tag, err := finish.Exec(save, `UPDATE native_media_uploads SET status=$2,result_code=$3,
        payload_sealed=$4,finished_at=clock_timestamp() WHERE id=$1 AND status IN('preparing','submitting')`,
		out.ID, out.Status, out.ResultCode, sealed)
	if err != nil || tag.RowsAffected() != 1 {
		return MediaUploadOperation{ID: out.ID, Kind: out.Kind, Status: "unknown", ResultCode: "native_upload_result_unknown"},
			Denied(503, "native_upload_result_unknown")
	}
	if _, err = finish.Exec(save, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
        VALUES($1,$2,'native_media_uploaded',$3,jsonb_build_object('kind',$4::text,'status',$5::text))`,
		req.auditTenant(scope.tenant), scope.actor.ID, out.ID, out.Kind, out.Status); err != nil {
		return out, err
	}
	if err = finish.Commit(save); err != nil {
		return out, err
	}
	// A revoked session may not obtain a result, even after a completed upload.
	return s.InspectMediaUpload(ctx, ref, req.AccountID, out.ID, "")
}

func (s *NativeTextService) mediaReceipt(ctx context.Context, tx pgx.Tx, actor string, req TextRequest, operation string) (*nativewire.OutboundMedia, error) {
	var sealed []byte
	err := tx.QueryRow(ctx, `SELECT payload_sealed FROM native_media_uploads WHERE id=$1 AND actor_id=$2
        AND account_id=$3 AND group_id=$4 AND test_window_id=$5 AND kind=$6 AND account_epoch=$7 AND binding_epoch=$8
        AND receiver_owner=$9 AND receiver_revision=$10 AND status='succeeded'
        AND (operation_id IS NULL OR $11='' OR operation_id::text=$11)
        AND expires_at>clock_timestamp()+interval '3 seconds' AND conversation_key=$12 FOR UPDATE`, req.MediaUploadID, actor,
		req.AccountID, req.GroupID, req.TestWindowID, req.MediaKind, req.AccountEpoch, req.BindingEpoch,
		req.ReceiverOwner, req.ReceiverRevision, operation, req.PrivateConversation).Scan(&sealed)
	if err != nil {
		return nil, Denied(409, "native_upload_unavailable")
	}
	raw, err := s.Vault.Open("native-media:"+req.MediaUploadID, sealed)
	if err != nil {
		return nil, Denied(409, "native_upload_unavailable")
	}
	defer clear(raw)
	var out nativewire.OutboundMedia
	if json.Unmarshal(raw, &out) != nil || out.Kind != req.MediaKind || out.Validate() != nil {
		return nil, Denied(409, "native_upload_unavailable")
	}
	return &out, nil
}
