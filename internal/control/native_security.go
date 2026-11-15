package control

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativeauth"
)

type SecurityFlow struct {
	ID        string    `json:"id"`
	Stage     string    `json:"stage"`
	ExpiresAt time.Time `json:"expires_at"`
}
type securityStep struct {
	state    nativeauth.SecurityState
	expected string
}
type SecurityPort func(context.Context, *nativeauth.SecurityState, SettingFence) error

func (s *NativeSettingsService) ExecuteSecurity(ctx context.Context, ref SessionRef, req SettingRequest, in nativeauth.SecurityInput, port SecurityPort) (SettingOperation, error) {
	if err := ValidateSecurityInput(req.Workspace, req.Change.Kind, in); err != nil {
		return SettingOperation{}, Denied(400, err.Error())
	}
	if !nativeauth.SecurityKind(req.Change.Kind) || port == nil {
		return SettingOperation{}, Denied(400, "native_security_flow_required")
	}
	plain, _ := json.Marshal(in)
	req.InputDigest = s.Vault.Fingerprint("native-security:"+req.Account, plain)
	clear(plain)
	req.Change.Text = in.Flow
	if in.Flow == "" {
		req.Change.Text = req.Key
	}
	if !ValidID(req.Change.Text) {
		return SettingOperation{}, Denied(400, "native_security_flow_required")
	}
	step := &securityStep{}
	result, err := s.execute(ctx, ref, req, func(ctx context.Context, fence SettingFence) error {
		return port(ctx, &step.state, fence)
	}, step)
	// Duplicate operation responses also recover the safe workflow status.
	if err == nil && req.Change.Kind != "avatar" {
		var flow SecurityFlow
		e := s.DB.Platform.QueryRow(ctx, `SELECT id::text,stage,expires_at FROM native_security_flows
			WHERE id=$1 AND account_id=$2`, req.Change.Text, req.Account).Scan(&flow.ID, &flow.Stage, &flow.ExpiresAt)
		if e != nil && e != pgx.ErrNoRows {
			return result, e
		}
		if e == nil {
			result.Flow = &flow
		}
	}
	return result, err
}

func securityStages(kind string) (before, after string) {
	switch kind {
	case "password_sms":
		return "new", "password_code"
	case "password_verify":
		return "password_code", "password_verified"
	case "password":
		return "password_verified", "completed"
	case "phone_old_sms":
		return "new", "phone_old_code"
	case "phone_old_verify":
		return "phone_old_code", "phone_old_verified"
	case "phone_new_sms":
		return "phone_old_verified", "phone_new_code"
	case "phone":
		return "phone_new_code", "completed"
	}
	return "", ""
}

func securityStageFor(workspace, kind string) (string, string) {
	if workspace == "lajiaoliao" && kind == "password" {
		return "password_code", "completed"
	}
	return securityStages(kind)
}

func (s *NativeSettingsService) prepareSecurity(ctx context.Context, tx pgx.Tx, actor string, req SettingRequest, step *securityStep) error {
	if req.Change.Kind == "avatar" {
		return nil
	}
	step.expected, _ = securityStageFor(req.Workspace, req.Change.Kind)
	if step.expected == "new" {
		var recent bool
		if strings.HasSuffix(req.Change.Kind, "_sms") {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1
			AND kind IN ('password_sms','phone_old_sms','phone_new_sms') AND created_at>now()-interval '60 seconds')`, req.Account).Scan(&recent); err != nil {
				return err
			}
		}
		if recent {
			return Denied(429, "native_security_sms_cooldown")
		}
		_, err := tx.Exec(ctx, `INSERT INTO native_security_flows(id,account_id,actor_id,workspace,account_epoch,stage)
			VALUES($1,$2,$3,$4,$5,'new')`, req.Change.Text, req.Account, actor, req.Workspace, req.Epoch)
		return err
	}
	var sealed []byte
	if err := tx.QueryRow(ctx, `SELECT payload_sealed FROM native_security_flows WHERE id=$1 AND actor_id=$2
		AND account_id=$3 AND workspace=$4 AND account_epoch=$5 AND stage=$6
		AND expires_at>clock_timestamp()+interval '3 seconds' FOR UPDATE`,
		req.Change.Text, actor, req.Account, req.Workspace, req.Epoch, step.expected).Scan(&sealed); err != nil {
		return Denied(409, "native_security_flow_expired")
	}
	plain, err := s.Vault.Open("native-security:"+req.Change.Text, sealed)
	if err != nil {
		return err
	}
	defer clear(plain)
	if json.Unmarshal(plain, &step.state) != nil {
		return Denied(409, "native_security_flow_expired")
	}
	return nil
}

func (s *NativeSettingsService) finishSecurity(ctx context.Context, tx pgx.Tx, req SettingRequest, step *securityStep, out *SettingOperation) error {
	if req.Change.Kind == "avatar" {
		if step.state.Asset != "" {
			req.Change.Text = step.state.Asset
			plain, _ := json.Marshal(req)
			defer clear(plain)
			sealed, err := s.Vault.Seal("native-setting:"+out.ID, plain)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE native_setting_operations SET payload_sealed=$2 WHERE id=$1`, out.ID, sealed)
			return err
		}
		return nil
	}
	_, stage := securityStageFor(req.Workspace, req.Change.Kind)
	if out.Status != "succeeded" {
		stage = out.Status
	}
	plain, _ := json.Marshal(step.state)
	defer clear(plain)
	var sealed []byte
	if out.Status == "succeeded" && stage != "completed" {
		var err error
		sealed, err = s.Vault.Seal("native-security:"+req.Change.Text, plain)
		if err != nil {
			return err
		}
	}
	// A missing expiry in the SMS response is not assumed to mean unlimited.
	var expiry any
	if strings.HasSuffix(req.Change.Kind, "_sms") {
		expiry = time.Now().Add(time.Duration(max(1, min(step.state.ExpiresSeconds, 600))) * time.Second)
		if step.state.ExpiresSeconds == 0 {
			expiry = time.Now().Add(5 * time.Minute)
		}
	}
	_, err := tx.Exec(ctx, `UPDATE native_security_flows SET stage=$2,payload_sealed=$3,
		expires_at=CASE WHEN $4::timestamptz IS NULL THEN expires_at ELSE least(expires_at,$4) END WHERE id=$1`,
		req.Change.Text, stage, sealed, expiry)
	if err != nil {
		return err
	}
	if out.Status == "succeeded" && (req.Change.Kind == "password" || req.Change.Kind == "phone") {
		_, err = tx.Exec(ctx, `UPDATE accounts SET auth_state='reauth_required',connection_enabled=false,epoch=epoch+1,version=version+1
			WHERE id=$1 AND epoch=$2`, req.Account, req.Epoch)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE account_receivers SET enabled=false,revision=revision+1
				WHERE account_id=$1`, req.Account)
		}
	}
	return err
}
