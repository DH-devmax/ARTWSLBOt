package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresChiliSecurityStateAndProfileAdmission(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{47}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	account, actor := control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'chili-security-owner','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("test"), actor)
	exec(`INSERT INTO accounts(id,name,workspace,platform,purpose,native_uid,auth_state,credential_sealed)
		VALUES($1,'chili-security','lajiaoliao','lajiaoliao','management','self-string','authenticated',$2)`, account, []byte("synthetic-sealed"))
	req := control.SettingRequest{Account: account, Workspace: "lajiaoliao", Epoch: 1, Version: 1, Digest: strings.Repeat("c", 64)}
	calls := 0
	nativeID := "self-string"
	proof := func() control.SettingEvidence {
		return control.SettingEvidence{UID: nativeID, Epoch: 1, Version: 1, DeploymentVersion: 0,
			Digest: req.Digest, CredentialDigest: sha256.Sum256([]byte("synthetic-sealed")), ObservedAt: time.Now()}
	}
	for _, kind := range []string{"profile_gender", "profile_birth", "profile_signature", "push_notifications"} {
		text := map[string]string{"profile_gender": "0", "profile_birth": "946656000000", "profile_signature": "",
			"push_notifications": `{"showMessageDetail":true,"receiveNewMessage":false,"groupMessageNotification":false,"notificationSound":"normal","vibration":false}`}[kind]
		req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: kind, Text: text}
		out, err := service.Execute(ctx, ref, req, func(ctx context.Context, fence control.SettingFence) error {
			return fence(ctx, proof(), func(context.Context) error { calls++; return nil })
		})
		if err != nil || out.Status != "succeeded" {
			t.Fatal("Chili setting admission", kind, out, err)
		}
	}
	req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: "password_sms"}
	port := func(ctx context.Context, state *nativeauth.SecurityState, fence control.SettingFence) error {
		return fence(ctx, proof(), func(context.Context) error {
			calls++
			state.Phone, state.AreaCode = "13800000000", "+86"
			return nil
		})
	}
	first, err := service.ExecuteSecurity(ctx, ref, req, nativeauth.SecurityInput{}, port)
	if err != nil || first.Flow == nil || first.Flow.Stage != "password_code" {
		t.Fatal("Chili SMS stage", first, err)
	}
	req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: "password_verify"}
	if _, err = service.ExecuteSecurity(ctx, ref, req, nativeauth.SecurityInput{Flow: first.Flow.ID, Code: "123456"}, port); err == nil {
		t.Fatal("WSL-only verification accepted")
	}
	req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: "password"}
	input := nativeauth.SecurityInput{Flow: first.Flow.ID, Code: "123456", NewPassword: "synthetic-private-password"}
	final, err := service.ExecuteSecurity(ctx, ref, req, input, port)
	if err != nil || final.Flow == nil || final.Flow.Stage != "completed" || calls != 6 {
		t.Fatal("Chili direct reset", final, err, calls)
	}
	duplicate, err := service.ExecuteSecurity(ctx, ref, req, input, port)
	if err != nil || duplicate.ID != final.ID || calls != 6 {
		t.Fatal("security replayed", err)
	}
	var state string
	var epoch int64
	if err = db.Platform.QueryRow(ctx, `SELECT auth_state,epoch FROM accounts WHERE id=$1`, account).Scan(&state, &epoch); err != nil || state != "reauth_required" || epoch != 2 {
		t.Fatal("session not invalidated", err, state, epoch)
	}
	var sealed []byte
	if err = db.Platform.QueryRow(ctx, `SELECT payload_sealed FROM native_setting_operations WHERE id=$1`, final.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open("native-setting:"+final.ID, sealed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	for _, secret := range []string{input.Code, input.NewPassword, "13800000000"} {
		if bytes.Contains(plain, []byte(secret)) {
			t.Fatal("security secret in durable request")
		}
	}
	var leaked bool
	if err = db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE resource_id=$1
		AND (detail::text LIKE '%13800000000%' OR detail::text LIKE '%synthetic-private-password%' OR detail::text LIKE '%123456%'))`, account).Scan(&leaked); err != nil || leaked {
		t.Fatal("secret in audit", err)
	}
	account, nativeID = control.NewID(), "phone-self-string"
	exec(`INSERT INTO accounts(id,name,workspace,platform,purpose,native_uid,auth_state,credential_sealed)
		VALUES($1,'chili-phone-security','lajiaoliao','lajiaoliao','management',$2,'authenticated',$3)`,
		account, nativeID, []byte("synthetic-sealed"))
	req.Account = account
	var phoneFlow string
	for index, kind := range []string{"phone_old_sms", "phone_old_verify", "phone_new_sms", "phone"} {
		req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: kind}
		input := nativeauth.SecurityInput{Flow: phoneFlow}
		if index == 1 || index == 3 {
			input.Code = "654321"
		}
		if index == 2 {
			input.Phone = "13900000000"
		}
		beforeCalls := calls
		request := req
		out, err := service.ExecuteSecurity(ctx, ref, request, input,
			func(ctx context.Context, state *nativeauth.SecurityState, fence control.SettingFence) error {
				expectedPhone := "13800000000"
				if index == 3 {
					expectedPhone = "13900000000"
				}
				if index > 0 && (state.AreaCode != "+86" || state.Phone != expectedPhone) {
					t.Fatal("sealed phone stage lost target", index)
				}
				return fence(ctx, proof(), func(context.Context) error {
					calls++
					state.Phone, state.AreaCode = "13800000000", "+86"
					if index >= 2 {
						state.Phone = "13900000000"
					}
					return nil
				})
			})
		if err != nil || out.Status != "succeeded" || out.Flow == nil ||
			out.Flow.Stage != []string{"phone_old_code", "phone_old_verified", "phone_new_code", "completed"}[index] ||
			calls != beforeCalls+1 {
			t.Fatal("Chili phone stage", index, out, err)
		}
		phoneFlow = out.Flow.ID
		duplicate, err := service.ExecuteSecurity(ctx, ref, request, input, port)
		if err != nil || duplicate.ID != out.ID || calls != beforeCalls+1 {
			t.Fatal("phone step replayed", index, err)
		}
	}
}
