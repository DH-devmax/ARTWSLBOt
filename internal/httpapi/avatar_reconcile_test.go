package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestChiliAvatarRecoveryCannotClearFinalOrInterruptedSubmissions(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{63}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	actor, account := control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	ref = ref.InPlatform("lajiaoliao")
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,$2,'synthetic','superadmin')`, actor, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
		security.Digest(token), security.Digest("synthetic"), actor)
	exec(`INSERT INTO accounts(id,name,platform,workspace,native_uid,auth_state,credential_sealed)
		VALUES($1,'synthetic','lajiaoliao','lajiaoliao','self','authenticated','\x01')`, account)
	for _, mode := range []string{"upload", "rotated_epoch", "final", "interrupted", "submitting", "other_account", "other_platform", "epoch"} {
		t.Run(mode, func(t *testing.T) {
			id, key := control.NewID(), control.NewID()
			req := control.SettingRequest{Account: account, Workspace: "lajiaoliao", Key: key,
				Epoch: 1, Version: 1, Digest: strings.Repeat("a", 64), InputDigest: "synthetic",
				Change: nativeauth.SettingChange{Kind: "avatar", Text: key}}
			status, code := "unknown", "lajiaoliao_result_unknown"
			switch mode {
			case "final":
				req.Change.Text = "https://lj-im-chat-01.lajiaoliao.com/synthetic.jpg"
			case "interrupted":
				code = "process_restarted"
			case "submitting":
				status = "submitting"
			case "other_account":
				req.Account = control.NewID()
			case "other_platform":
				req.Workspace = "wsl"
			case "epoch":
				req.Epoch = 2
			}
			plain, _ := json.Marshal(req)
			sealed, _ := vault.Seal("native-setting:"+id, plain)
			clear(plain)
			exec(`INSERT INTO native_setting_operations(id,actor_id,account_id,kind,idempotency_key,request_hash,
				payload_sealed,account_epoch,status,result_code,expires_at)
				VALUES($1,$2,$3,'avatar',$4,$5,$6,1,$7,$8,now()+interval '2 minutes')`,
				id, actor, account, key, []byte("synthetic"), sealed, status, code)
			if mode == "rotated_epoch" {
				exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account)
				defer exec(`UPDATE accounts SET epoch=1 WHERE id=$1`, account)
			}
			result, err := service.ResolveAvatarUpload(ctx, ref, account, id)
			if mode == "upload" || mode == "rotated_epoch" {
				if err != nil || result.Status != "failed" || result.Code != "lajiaoliao_avatar_upload_unconfirmed" {
					t.Fatal("known unsubmitted avatar not resolved", result, err)
				}
			} else if err == nil {
				t.Fatal("unresolved final update cleared", mode, result)
			}
		})
	}
}

func TestChiliAvatarUploadUsesBoundedDedicatedFenceAndPreservesFinalUnknown(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{64}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	actor, account := control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,$2,'synthetic','superadmin')`, actor, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
		security.Digest(token), security.Digest("synthetic"), actor)
	credential := []byte("synthetic")
	exec(`INSERT INTO accounts(id,name,platform,workspace,native_uid,auth_state,credential_sealed)
		VALUES($1,'synthetic','lajiaoliao','lajiaoliao','self','authenticated',$2)`, account, credential)
	for _, final := range []bool{false, true} {
		req := control.SettingRequest{Account: account, Workspace: "lajiaoliao", Key: control.NewID(),
			Epoch: 1, Version: 1, Digest: strings.Repeat("a", 64), Change: nativeauth.SettingChange{Kind: "avatar"}}
		result, err := service.ExecuteSecurity(ctx, ref.InPlatform("lajiaoliao"), req,
			nativeauth.SecurityInput{Image: bytes.Repeat([]byte{1}, 64)},
			func(work context.Context, state *nativeauth.SecurityState, fence control.SettingFence) error {
				proof := control.SettingEvidence{UID: "self", Epoch: 1, Version: 1, Digest: req.Digest,
					CredentialDigest: sha256.Sum256(credential), ObservedAt: time.Now()}
				return fence(work, proof, func(bounded context.Context) error {
					deadline, ok := bounded.Deadline()
					if !ok || time.Until(deadline) < 7*time.Second || time.Until(deadline) > 8*time.Second {
						t.Fatal("upload fence did not get its bounded timeout")
					}
					if final {
						state.Asset = "https://lj-im-chat-01.lajiaoliao.com/synthetic.jpg"
					}
					return &lajiaoliao.Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
				})
			})
		if err != nil || (!final && (result.Status != "failed" || result.Code != "lajiaoliao_avatar_upload_unconfirmed")) ||
			(final && result.Status != "unknown") {
			t.Fatal("upload uncertainty conflated with final account update", final, result, err)
		}
	}
}
