package control

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

func settingFenceDuration(req SettingRequest) time.Duration {
	if req.Workspace == "lajiaoliao" && req.Change.Kind == "avatar" {
		return 8 * time.Second
	}
	return 2 * time.Second
}

func avatarUploadUnsubmitted(req SettingRequest, account, key, code string) bool {
	return req.Workspace == "lajiaoliao" && req.Account == account && req.Key == key &&
		ValidID(key) && req.Change.Kind == "avatar" && req.Change.Text == key &&
		req.InputDigest != "" && code == "lajiaoliao_result_unknown"
}

// A finished avatar operation seals the uploaded URL before recording an
// uncertain final update. Its original key proves that update was never called.
// Interrupted submissions cannot use this recovery path.
func (s *NativeSettingsService) ResolveAvatarUpload(ctx context.Context, ref SessionRef, account, id string) (SettingOperation, error) {
	out := SettingOperation{}
	if !ValidID(account) || !ValidID(id) {
		return out, Denied(400, "invalid_operation")
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
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	var sealed []byte
	var key string
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT id::text,status,result_code,kind,idempotency_key,payload_sealed,account_epoch
		FROM native_setting_operations WHERE id=$1 AND account_id=$2 AND actor_id=$3 FOR UPDATE`,
		id, account, actor.ID).Scan(&out.ID, &out.Status, &out.Code, &out.Kind, &key, &sealed, &epoch)
	if err != nil {
		if err == pgx.ErrNoRows {
			return out, Denied(404, "operation_not_found")
		}
		return out, err
	}
	if out.Status != "unknown" || out.Kind != "avatar" {
		return out, Denied(409, "native_avatar_final_submission_unresolved")
	}
	plain, err := s.Vault.Open("native-setting:"+id, sealed)
	if err != nil {
		return out, err
	}
	defer clear(plain)
	var req SettingRequest
	if json.Unmarshal(plain, &req) != nil || req.Epoch != epoch || !avatarUploadUnsubmitted(req, account, key, out.Code) {
		return out, Denied(409, "native_avatar_final_submission_unresolved")
	}
	// This only closes a proven unsubmitted historical update. Authorize the
	// current account view; test-switch epoch changes cannot change that fact.
	if err = tx.QueryRow(ctx, `SELECT version,epoch FROM accounts WHERE id=$1 AND platform='lajiaoliao'
		AND auth_state='authenticated' FOR SHARE`, account).Scan(&req.Version, &req.Epoch); err != nil {
		return out, Denied(409, "native_setting_account_changed")
	}
	if _, _, err = s.authorize(ctx, tx, ref, req, nil, "", 0, true); err != nil {
		return out, err
	}
	out.Status, out.Code = "failed", "lajiaoliao_avatar_upload_unconfirmed"
	if _, err = tx.Exec(ctx, `UPDATE native_setting_operations SET status='failed',result_code=$2,updated_at=now()
		WHERE id=$1 AND status='unknown'`, id, out.Code); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
		VALUES($1,'native_setting_reconciled',$2,jsonb_build_object('kind','avatar','status','failed',
		'code',$3::text,'operation_id',$4::text,'native_update_submitted',false))`, actor.ID, account, out.Code, id); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
