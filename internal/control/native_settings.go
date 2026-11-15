package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/database"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type NativeSettingsService struct {
	DB    *database.DB
	Vault *security.Vault
}

type SettingRequest struct {
	Account, Workspace, Key, Digest string
	Epoch, Version                  int64
	Change                          nativeauth.SettingChange
	InputDigest                     string `json:",omitempty"`
	TestWindowID                    string `json:",omitempty"`
	ReceiverOwner                   string `json:",omitempty"`
	ReceiverRevision                int64  `json:",omitempty"`
	business                        *assistantBusiness
	contactRecovery                 *onboardingContactRecovery
}

type SettingEvidence struct {
	UID, Role, Target, TargetRole, Digest string
	OwnerID, ReceiverOwner                string
	ReceiverRevision                      int64
	Locked                                bool
	Epoch, Version, DeploymentVersion     int64
	CredentialDigest                      [32]byte
	ObservedAt                            time.Time
	Matches                               bool
	SocialAllowed                         bool
	ModerationAllowed                     bool
	GroupBanKnown, TargetBanned           bool
}

type SettingFence func(context.Context, SettingEvidence, func(context.Context) error) error
type SettingPort func(context.Context, SettingFence) error

type SettingOperation struct {
	ID     string        `json:"id"`
	Status string        `json:"status"`
	Code   string        `json:"result_code"`
	Flow   *SecurityFlow `json:"flow,omitempty"`
	Kind   string        `json:"kind,omitempty"`
}

func (s *NativeSettingsService) Pending(ctx context.Context, ref SessionRef, account string) ([]map[string]string, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return nil, err
	}
	if actor.Role != "superadmin" {
		return nil, Denied(403, "superadmin_required")
	}
	rows, err := tx.Query(ctx, `SELECT id::text,status,result_code,idempotency_key,kind FROM native_setting_operations
        WHERE actor_id=$1 AND account_id=$2 AND status IN ('preparing','submitting','unknown') ORDER BY created_at DESC LIMIT 10`,
		actor.ID, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var id, status, code, key, kind string
		if err := rows.Scan(&id, &status, &code, &key, &kind); err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"id": id, "status": status, "result_code": code, "key": key, "kind": kind})
	}
	return out, rows.Err()
}

func (s *NativeSettingsService) Inspect(ctx context.Context, ref SessionRef, account, id, key string) (SettingOperation, error) {
	out := SettingOperation{}
	if !ValidID(account) || id != "" && !ValidID(id) || id == "" && (len(key) < 8 || len(key) > 100) {
		return out, Denied(400, "invalid_operation")
	}
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
	var sealed []byte
	var kind string
	err = tx.QueryRow(ctx, `SELECT id::text,status,result_code,kind,payload_sealed FROM native_setting_operations
        WHERE actor_id=$1 AND account_id=$2 AND (($3<>'' AND id::text=$3) OR ($3='' AND idempotency_key=$4))`,
		actor.ID, account, id, key).Scan(&out.ID, &out.Status, &out.Code, &kind, &sealed)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "operation_not_found")
	}
	out.Kind = kind
	if err != nil {
		return out, err
	}
	if nativeauth.SecurityKind(kind) && kind != "avatar" && len(sealed) > 0 {
		plain, err := s.Vault.Open("native-setting:"+out.ID, sealed)
		if err != nil {
			return out, err
		}
		defer clear(plain)
		var req SettingRequest
		if json.Unmarshal(plain, &req) != nil {
			return out, Denied(409, "native_security_flow_expired")
		}
		var flow SecurityFlow
		err = tx.QueryRow(ctx, `SELECT id::text,stage,expires_at FROM native_security_flows
			WHERE id=$1 AND actor_id=$2 AND account_id=$3 AND workspace=$4`,
			req.Change.Text, actor.ID, account, req.Workspace).Scan(&flow.ID, &flow.Stage, &flow.ExpiresAt)
		if err != nil && err != pgx.ErrNoRows {
			return out, err
		}
		if err == nil {
			out.Flow = &flow
		}
	}
	return out, nil
}

// Execute is an explicit, single-setting manual action, never an automation
// worker. A durable submitting marker precedes the final authorization fence.
func (s *NativeSettingsService) Execute(ctx context.Context, ref SessionRef, req SettingRequest, port SettingPort) (SettingOperation, error) {
	if nativeauth.SocialKind(req.Change.Kind) || nativeauth.ModerationKind(req.Change.Kind) ||
		lajiaoliao.BanAction(req.Change.Kind) || req.TestWindowID != "" {
		return SettingOperation{}, Denied(400, "native_social_endpoint_required")
	}
	if nativeauth.SecurityKind(req.Change.Kind) {
		return SettingOperation{}, Denied(400, "native_security_flow_required")
	}
	return s.execute(ctx, ref, req, port, nil)
}

func (s *NativeSettingsService) execute(ctx context.Context, ref SessionRef, req SettingRequest, port SettingPort, security *securityStep) (SettingOperation, error) {
	out := SettingOperation{Kind: req.Change.Kind}
	if (req.Change.Kind == "send_text" || req.Change.Kind == "recall_message") && req.business == nil &&
		!((req.Change.Kind == "recall_message" && (req.Workspace == "wsl" || req.Workspace == "lajiaoliao") ||
			req.Change.Kind == "send_text" && req.Workspace == "lajiaoliao") && ValidID(req.TestWindowID)) {
		return out, Denied(403, "assistant_business_admission_required")
	}
	digest, err := hex.DecodeString(req.Digest)
	if !ValidID(req.Account) || len(req.Key) < 8 || len(req.Key) > 100 ||
		err != nil || len(digest) != sha256.Size || req.Epoch < 1 || req.Version < 1 {
		return out, Denied(400, "native_setting_request_invalid")
	}
	if err := ValidateNativeChange(req.Workspace, req.Change); err != nil {
		return out, Denied(400, err.Error())
	}
	if port == nil {
		return out, Denied(503, "native_read_owner_unavailable")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" && req.business == nil {
		return out, Denied(403, "superadmin_required")
	}
	// Only this synchronous controlled-delete path can resolve an exception.
	// Browser fields and decoded historical requests cannot supply it.
	req.contactRecovery = nil
	if actor.Role == "superadmin" && req.business == nil && req.Workspace == "lajiaoliao" &&
		req.Change.Kind == "friend_delete" && ValidID(req.TestWindowID) {
		req.contactRecovery = &onboardingContactRecovery{actor: actor.ID}
	}
	plain, _ := json.Marshal(req)
	hash := sha256.Sum256(plain)
	defer clear(plain)
	var oldHash []byte
	err = tx.QueryRow(ctx, `SELECT id::text,status,result_code,request_hash FROM native_setting_operations
        WHERE actor_id=$1 AND idempotency_key=$2`, actor.ID, req.Key).Scan(&out.ID, &out.Status, &out.Code, &oldHash)
	if err == nil {
		if !bytes.Equal(oldHash, hash[:]) {
			return SettingOperation{}, Denied(409, "idempotency_conflict")
		}
		return out, nil
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	group, binding, err := s.authorize(ctx, tx, ref, req, nil, "", 0, false)
	if err != nil {
		return out, err
	}
	// These account contact operations have already passed their independent,
	// singleton native_debug grant in authorize; they do not use claim proofs.
	independentContact := req.business == nil && ValidID(req.TestWindowID) &&
		(req.Change.Kind == "friend_delete" || req.Change.Kind == "friend_approve")
	var controlled controlledExecutionFence
	if !independentContact {
		var proofWindow bool
		if req.TestWindowID != "" {
			controlled, _, proofWindow, err = controlledProofSettingScope(ctx, tx, s.Vault, actor.ID, req, false)
		}
		if err == nil && !proofWindow {
			controlled, err = controlledExecutionScope(ctx, tx, actor.ID, req.Workspace, group)
		}
		if err != nil {
			return out, err
		}
	}
	// The account row serializes duplicate keys as well as different settings.
	err = tx.QueryRow(ctx, `SELECT id::text,status,result_code,request_hash FROM native_setting_operations
        WHERE actor_id=$1 AND idempotency_key=$2`, actor.ID, req.Key).Scan(&out.ID, &out.Status, &out.Code, &oldHash)
	if err == nil {
		if !bytes.Equal(oldHash, hash[:]) {
			return SettingOperation{}, Denied(409, "idempotency_conflict")
		}
		return out, nil
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1
        AND status IN ('preparing','submitting','unknown')) OR EXISTS(SELECT 1 FROM operations
        WHERE account_id=$1 AND status IN ('running','unknown') AND id::text<>$2)`,
		req.Account, req.contactRecovery.excludedOperation()).Scan(&pending); err != nil {
		return out, err
	}
	if pending {
		return out, Denied(409, "native_setting_pending")
	}
	if req.business != nil {
		// authorize holds the account UPDATE lock, shared with automatic work
		// preparation, until this durable preparing record has committed.
		if err = req.business.prepareAvailable(ctx, tx, req.Account); err != nil {
			return out, err
		}
	}
	if security != nil {
		if err = s.prepareSecurity(ctx, tx, actor.ID, req, security); err != nil {
			return out, err
		}
	}
	out.ID, out.Status = NewID(), "preparing"
	payload := plain
	if req.contactRecovery != nil && req.contactRecovery.snapshot != nil {
		payload, err = json.Marshal(struct {
			SettingRequest
			ContactRecovery onboardingContactSnapshot `json:"onboarding_contact_recovery"`
		}{req, *req.contactRecovery.snapshot})
		if err != nil {
			return out, err
		}
		defer clear(payload)
	}
	sealed, err := s.Vault.Seal("native-setting:"+out.ID, payload)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO native_setting_operations
        (id,actor_id,account_id,group_id,kind,idempotency_key,request_hash,payload_sealed,account_epoch,status,expires_at,test_window_id)
        VALUES($1,$2,$3,nullif($4,'')::uuid,$5,$6,$7,$8,$9,'preparing',now()+interval '15 seconds',nullif($10,'')::uuid)`,
		out.ID, actor.ID, req.Account, group, req.Change.Kind, req.Key, hash[:], sealed, req.Epoch, req.TestWindowID)
	if err != nil {
		return out, err
	}
	if req.business != nil {
		if err = req.business.prepareMessage(ctx, tx, out.ID); err != nil {
			return out, err
		}
	}
	if req.TestWindowID != "" {
		tag, err := tx.Exec(ctx, `UPDATE test_windows SET used=used+1 WHERE id=$1 AND used<budget AND NOT cancelled
            AND expires_at>clock_timestamp()+interval '3 seconds'`, req.TestWindowID)
		if err != nil {
			return out, err
		}
		if tag.RowsAffected() != 1 {
			return out, Denied(409, "native_test_window_invalid")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	attempted := false
	submissions := 0
	var changedOwner *SettingEvidence
	err = port(ctx, func(work context.Context, proof SettingEvidence, submit func(context.Context) error) error {
		submissions++
		limit := 1
		if security != nil && req.Change.Kind == "avatar" {
			limit = 3
		}
		if submissions > limit {
			return Denied(409, "native_setting_submission_limit")
		}
		duration := settingFenceDuration(req)
		// Commit the shared attempt reservation before transport I/O. A failed
		// socket write or uncertain commit must never refund this scope budget.
		reserve, err := s.DB.Platform.Begin(work)
		if err != nil {
			return err
		}
		defer reserve.Rollback(context.WithoutCancel(work))
		if _, _, err = s.authorize(work, reserve, ref, req, &proof, group, binding, false); err != nil {
			var denied *Failure
			if req.business != nil && errors.As(err, &denied) && denied.Code == "group_ownership_changed" {
				changedOwner = &proof
			}
			return err
		}
		if !independentContact {
			if err = controlled.require(work, reserve, actor.ID, req.Workspace, group); err != nil {
				return err
			}
			if err = controlled.reserveNative(work, reserve, actor.ID, req.Workspace, group, "native_settings", out.ID); err != nil {
				return err
			}
		}
		tag, err := reserve.Exec(work, `UPDATE native_setting_operations SET status='submitting',updated_at=now()
            WHERE id=$1 AND status IN ('preparing','submitting') AND expires_at>clock_timestamp()+make_interval(secs=>$2)`,
			out.ID, duration.Seconds()+1)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return Denied(409, "native_setting_expired")
		}
		if err = reserve.Commit(work); err != nil {
			return err
		}
		final, cancel := context.WithTimeout(work, duration)
		defer cancel()
		fenceTx, err := s.DB.Platform.Begin(final)
		if err != nil {
			return err
		}
		defer fenceTx.Rollback(context.WithoutCancel(final))
		if _, _, err = s.authorize(final, fenceTx, ref, req, &proof, group, binding, false); err != nil {
			var denied *Failure
			if req.business != nil && errors.As(err, &denied) && denied.Code == "group_ownership_changed" {
				changedOwner = &proof
			}
			return err
		}
		if !independentContact {
			if err = controlled.require(final, fenceTx, actor.ID, req.Workspace, group); err != nil {
				return err
			}
		}
		if security != nil && req.Change.Kind != "avatar" {
			var valid bool
			if err = fenceTx.QueryRow(final, `SELECT EXISTS(SELECT 1 FROM native_security_flows
				WHERE id=$1 AND actor_id=$2 AND account_id=$3 AND account_epoch=$4 AND workspace=$5
				AND expires_at>clock_timestamp()+interval '3 seconds' AND stage=$6)`,
				req.Change.Text, actor.ID, req.Account, req.Epoch, req.Workspace, security.expected).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return Denied(409, "native_security_flow_expired")
			}
		}
		if _, err = fenceTx.Exec(final, `SET LOCAL lock_timeout='500ms'`); err != nil {
			return err
		}
		if err = controlled.requireSubmission(final, fenceTx, duration); err != nil {
			return err
		}
		attempted = true
		if err = submit(final); err != nil {
			return err
		}
		return fenceTx.Commit(final)
	})
	if err == nil && submissions == 0 {
		err = Denied(409, "native_setting_submission_missing")
	}
	var ownershipErr error
	if changedOwner != nil {
		observe, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		ownershipErr = req.business.observeOwnership(observe, req, *changedOwner)
		stop()
	}
	out.Status, out.Code = settingOutcome(err, attempted)
	if req.Workspace == "lajiaoliao" && req.Change.Kind == "avatar" && security != nil &&
		security.state.Asset == "" && out.Status == "unknown" {
		out.Status, out.Code = "failed", "lajiaoliao_avatar_upload_unconfirmed"
	}
	if out.Status == "succeeded" && nativeauth.SocialKind(req.Change.Kind) {
		out.Code = map[string]string{"friend_approve": "native_friend_decision_acknowledged",
			"friend_reject": "native_friend_decision_acknowledged", "friend_delete": "native_friend_delete_acknowledged", "group_invite": "native_invitation_requested",
			"group_leave": "native_leave_acknowledged", "group_accept": "native_invitation_accept_acknowledged"}[req.Change.Kind]
	}
	if out.Status == "succeeded" && (nativeauth.ModerationKind(req.Change.Kind) || lajiaoliao.BanAction(req.Change.Kind)) {
		out.Code = "native_moderation_acknowledged"
	}
	// HTTP cancellation does not erase an already submitted outcome.
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	tx, saveErr := s.DB.Platform.Begin(save)
	if saveErr != nil {
		return out, saveErr
	}
	defer tx.Rollback(save)
	_, saveErr = tx.Exec(save, `UPDATE native_setting_operations SET status=$2,result_code=$3,updated_at=now() WHERE id=$1`,
		out.ID, out.Status, out.Code)
	if saveErr == nil && out.Status == "succeeded" && req.Change.Kind == "nickname" {
		_, saveErr = tx.Exec(save, `UPDATE accounts SET native_nickname=$2 WHERE id=$1 AND epoch=$3`,
			req.Account, req.Change.Text, req.Epoch)
	}
	if saveErr == nil && security != nil {
		saveErr = s.finishSecurity(save, tx, req, security, &out)
	}
	if saveErr == nil && out.Status == "succeeded" && req.Change.Kind == "group_leave" {
		_, saveErr = applyNativeRoleTx(save, tx, req.Account, group, "left", time.Now().UTC())
	}
	if saveErr == nil {
		_, saveErr = tx.Exec(save, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
            VALUES($1,'native_setting_result',$2,jsonb_build_object('kind',$3::text,'status',$4::text,'code',$5::text))`,
			actor.ID, req.Account, req.Change.Kind, out.Status, out.Code)
	}
	if saveErr != nil {
		return out, saveErr
	}
	if err = tx.Commit(save); err != nil {
		return out, err
	}
	var denied *Failure
	if ownershipErr != nil && !errors.As(ownershipErr, &denied) {
		return out, ownershipErr
	}
	return out, nil
}

func settingOutcome(err error, attempted bool) (string, string) {
	if err == nil {
		return "succeeded", "native_acknowledged"
	}
	var denied *Failure
	if errors.As(err, &denied) && !attempted {
		return "failed", denied.Code
	}
	var chili *lajiaoliao.Failure
	if errors.As(err, &chili) {
		if attempted && (chili.Code == "lajiaoliao_result_unknown" || chili.Code == "lajiaoliao_response_invalid") {
			return "unknown", chili.Code
		}
		return "failed", chili.Code
	}
	var native *nativeauth.Error
	if errors.As(err, &native) {
		switch native.Code {
		case "native_transport_unknown", "native_response_invalid":
			if attempted {
				return "unknown", native.Code
			}
		}
		return "failed", native.Code
	}
	if attempted {
		return "unknown", "native_setting_result_unknown"
	}
	return "failed", "native_setting_unavailable"
}

func (s *NativeSettingsService) authorize(ctx context.Context, tx pgx.Tx, ref SessionRef, req SettingRequest,
	proof *SettingEvidence, expectedGroup string, expectedBinding int64, observation bool) (string, int64, error) {
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='500ms'`); err != nil {
		return "", 0, err
	}
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return "", 0, err
	}
	if actor.Role != "superadmin" && req.business == nil {
		return "", 0, Denied(403, "superadmin_required")
	}
	var uid string
	var credential []byte
	lock := " FOR UPDATE"
	if proof != nil {
		// SHARE fences account changes without blocking ingress foreign-key
		// KEY SHARE locks while a bounded native HTTP request completes.
		lock = " FOR SHARE"
	}
	err = tx.QueryRow(ctx, `SELECT native_uid,credential_sealed FROM accounts WHERE id=$1 AND workspace=$2
        AND platform=$6 AND auth_state='authenticated' AND ($5 OR connection_enabled) AND epoch=$3 AND version=$4
        AND credential_sealed IS NOT NULL`+lock, req.Account, req.Workspace, req.Epoch, req.Version,
		// Private business admission below owns production authorization. The
		// legacy connection flag remains mandatory for controlled test actions.
		observation || req.business != nil || req.Change.Kind != "send_text" && req.Change.Kind != "recall_message" && req.Change.Kind != "member_nickname" && !nativeauth.SocialKind(req.Change.Kind) &&
			!NativeModerationActionForPlatform(req.Workspace, req.Change.Kind), nativePlatform(req.Workspace)).Scan(&uid, &credential)
	if err != nil {
		return "", 0, Denied(409, "native_setting_account_changed")
	}
	if req.business != nil {
		if err := req.business.authorize(ctx, tx, ref, req); err != nil {
			return "", 0, err
		}
	}
	var sessionFresh bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash=$1
        AND expires_at>clock_timestamp()+make_interval(secs=>$2))`, ref.hash, settingFenceDuration(req).Seconds()+1).Scan(&sessionFresh); err != nil {
		return "", 0, err
	}
	if !sessionFresh {
		return "", 0, Denied(401, "authentication_required")
	}
	if proof != nil {
		var version int64
		if req.Workspace == "lajiaoliao" {
			version = 0
		} else {
			err = tx.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton FOR SHARE`).Scan(&version)
		}
		if err != nil || version != proof.DeploymentVersion || proof.UID != uid || proof.Epoch != req.Epoch ||
			proof.Version != req.Version || proof.Digest != req.Digest || proof.CredentialDigest != sha256.Sum256(credential) ||
			proof.ObservedAt.After(time.Now()) || time.Since(proof.ObservedAt) > 5*time.Second {
			return "", 0, Denied(409, "native_setting_snapshot_changed")
		}
	}
	if req.business != nil {
		if err = req.business.authorizeOwnership(ctx, tx, req, proof); err != nil {
			return "", 0, err
		}
	}
	if req.business != nil && (req.Change.Kind == "send_text" || req.Change.Kind == "recall_message") {
		return req.business.messageFence(ctx, tx, req, proof)
	}
	if req.business != nil {
		return req.business.roleFence(req, proof)
	}
	debug := false
	if req.TestWindowID != "" && !observation {
		if _, scope, found, err := controlledProofSettingScope(ctx, tx, s.Vault, actor.ID, req, proof != nil); found || err != nil {
			if err != nil {
				return "", 0, err
			}
			if err = scope.requireSettingConnection(ctx, tx, req, proof, expectedGroup, expectedBinding); err != nil {
				return "", 0, err
			}
			return scope.Group, scope.BindingEpoch, nil
		}
		target := req.Change.Target
		if req.Change.Kind == "group_leave" {
			target = uid
		}
		if req.Change.Kind == "recall_message" {
			reference, err := nativeauth.ReadMessageReference(req.Change.Text, req.Workspace, req.Change.Group)
			if err != nil || !reference.Fresh(time.Now()) {
				return "", 0, Denied(409, "native_message_reference_expired")
			}
			target = reference.Sender
		}
		if req.Change.Kind == "send_text" {
			target = uid
		}
		scope, err := verifyTestWindow(ctx, tx, actor.ID, req.Workspace, req.TestWindowID, req.Account,
			req.Change.Kind, target, req.Change.Text, req.Epoch, proof != nil)
		if err != nil {
			return "", 0, err
		}
		if scope.NativeGroup != req.Change.Group {
			return "", 0, Denied(409, "native_test_window_invalid")
		}
		debug = scope.AdmissionMode == "native_debug"
		if req.Change.Minutes > 5 {
			return "", 0, Denied(400, "native_test_mute_limit")
		}
	}
	if req.business == nil && (req.Change.Kind == "recall_message" || req.Change.Kind == "send_text") && !observation {
		return s.authorizeTestRecall(ctx, tx, req, proof, expectedGroup, expectedBinding, debug)
	}
	if nativeauth.SocialKind(req.Change.Kind) || NativeModerationActionForPlatform(req.Workspace, req.Change.Kind) && req.Change.Kind != "member_nickname" {
		return s.authorizeSocial(ctx, tx, req, proof, expectedGroup, expectedBinding, observation, debug)
	}
	// Superadmin observations follow the existing read-only member boundary;
	// stopping management must not make an earlier outcome impossible to inspect.
	if observation || req.Change.Kind != "member_nickname" {
		return "", 0, nil
	}
	var group, tenant string
	var binding int64
	err = tx.QueryRow(ctx, `SELECT g.id::text,b.tenant_id::text,b.epoch FROM native_groups g
        JOIN group_bindings b ON b.group_id=g.id JOIN accounts a ON a.id=$2
        WHERE g.platform=$3 AND a.platform=g.platform AND g.native_id=$1 AND b.active AND ($4 OR b.management_enabled)
        AND ((a.purpose='management' AND b.primary_account_id=a.id) OR (a.purpose='chat' AND b.ai_account_id=a.id)
            OR ($4 AND a.purpose='management' AND EXISTS(SELECT 1 FROM test_windows w
                WHERE w.id::text=$5 AND w.group_id=g.id AND a.id=ANY(w.account_ids))))
        FOR SHARE OF g,b`, req.Change.Group, req.Account, nativePlatform(req.Workspace), debug, req.TestWindowID).Scan(&group, &tenant, &binding)
	if err != nil || expectedGroup != "" && (group != expectedGroup || binding != expectedBinding) {
		return "", 0, Denied(409, "native_setting_group_binding_required")
	}
	if !debug {
		var modules, platforms []string
		err = tx.QueryRow(ctx, `SELECT p.modules,p.allowed_platforms FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
        WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
        AND p.group_limit>0 FOR SHARE OF sub,p`, tenant).Scan(&modules, &platforms)
		if err != nil || !slices.Contains(modules, "cards") || !slices.Contains(platforms, nativePlatform(req.Workspace)) {
			return "", 0, Denied(403, "native_setting_cards_not_in_plan")
		}
	}
	if proof != nil && !settingRoleAllowed(uid, req.Change.Target, proof.Role, proof.TargetRole, proof.Locked) {
		return "", 0, Denied(403, "native_setting_role_required")
	}
	if proof != nil && proof.Target != req.Change.Target {
		return "", 0, Denied(409, "native_setting_snapshot_changed")
	}
	return group, binding, nil
}

// Reconcile observes the desired state only; it never invokes a mutation or
// claims that an unknown request was executed exactly once.
func (s *NativeSettingsService) Reconcile(ctx context.Context, ref SessionRef, account, id string,
	read func(context.Context, SettingRequest) (SettingEvidence, error)) (SettingOperation, error) {
	out, err := s.Inspect(ctx, ref, account, id, "")
	if err != nil || out.Status != "unknown" {
		return out, err
	}
	if nativeauth.SecurityKind(out.Kind) && out.Kind != "avatar" {
		return out, Denied(409, "native_security_manual_review_required")
	}
	var sealed []byte
	err = s.DB.Platform.QueryRow(ctx, `SELECT payload_sealed FROM native_setting_operations
        WHERE id=$1 AND account_id=$2 AND created_at>now()-interval '7 days'`, id, account).Scan(&sealed)
	if err != nil || len(sealed) == 0 {
		return out, Denied(409, "native_setting_evidence_expired")
	}
	plain, err := s.Vault.Open("native-setting:"+id, sealed)
	if err != nil {
		return out, err
	}
	defer clear(plain)
	var req SettingRequest
	if json.Unmarshal(plain, &req) != nil || req.Account != account {
		return out, Denied(409, "native_setting_snapshot_changed")
	}
	proof, err := read(ctx, req)
	if err != nil {
		return out, err
	}
	// A newer website name/version is harmless to read-only reconciliation;
	// a different native login epoch cannot resolve an old identity's action.
	req.Version, req.Digest = proof.Version, proof.Digest
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = s.authorize(ctx, tx, ref, req, &proof, "", 0, true); err != nil {
		return out, err
	}
	if !proof.Matches {
		return out, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE native_setting_operations SET status='succeeded',
        result_code='desired_state_observed',updated_at=now() WHERE id=$1 AND status='unknown'`, id); err != nil {
		return out, err
	}
	if req.Change.Kind == "group_leave" {
		var group string
		if err = tx.QueryRow(ctx, `SELECT group_id::text FROM native_setting_operations WHERE id=$1`, id).Scan(&group); err != nil {
			return out, err
		}
		if _, err = applyNativeRoleTx(ctx, tx, account, group, "left", time.Now().UTC()); err != nil {
			return out, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
        SELECT actor_id,'native_setting_reconciled',account_id::text,
        jsonb_build_object('operation_id',id,'result','desired_state_observed') FROM native_setting_operations WHERE id=$1`, id); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	out.Status, out.Code = "succeeded", "desired_state_observed"
	return out, nil
}

func settingRoleAllowed(uid, target, role, targetRole string, locked bool) bool {
	if locked || role != "member" && role != "admin" && role != "owner" ||
		targetRole != "member" && targetRole != "admin" && targetRole != "owner" {
		return false
	}
	if uid == target {
		return true
	}
	return role == "owner" && targetRole != "owner" || role == "admin" && targetRole == "member"
}
