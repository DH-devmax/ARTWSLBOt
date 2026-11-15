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

func TestPostgresNativeSecurityFlowIsolationSecretsAndRetries(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{44}, 32)))
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
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'security-test-owner','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("test"), actor)
	exec(`INSERT INTO accounts(id,name,native_uid,auth_state,credential_sealed) VALUES($1,'security-test','7','authenticated',$2)`, account, []byte("synthetic-sealed"))
	exec(`UPDATE accounts SET connection_enabled=true WHERE id=$1`, account)
	exec(`INSERT INTO account_receivers(account_id,enabled) VALUES($1,true)`, account)
	exec(`INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,77)`, []byte("synthetic-deployment"))
	req := control.SettingRequest{Account: account, Workspace: "wsl", Epoch: 1, Version: 1, Digest: strings.Repeat("b", 64)}
	count := 0
	port := func(ctx context.Context, state *nativeauth.SecurityState, fence control.SettingFence) error {
		proof := control.SettingEvidence{UID: "7", Epoch: req.Epoch, Version: req.Version, DeploymentVersion: 77, Digest: req.Digest, CredentialDigest: sha256.Sum256([]byte("synthetic-sealed")), ObservedAt: time.Now()}
		return fence(ctx, proof, func(context.Context) error {
			count++
			state.Key = "private-synthetic-key"
			state.Cert = "private-synthetic-cert"
			state.ExpiresSeconds = 300
			return nil
		})
	}
	call := func(kind string, input nativeauth.SecurityInput) (control.SettingOperation, error) {
		req.Key = control.NewID()
		req.Change = nativeauth.SettingChange{Kind: kind}
		return service.ExecuteSecurity(ctx, ref, req, input, port)
	}
	first, err := call("password_sms", nativeauth.SecurityInput{Captcha: "private-captcha"})
	if err != nil || first.Status != "succeeded" || first.Flow == nil || first.Flow.Stage != "password_code" {
		t.Fatal("SMS flow", first, err)
	}
	flow := first.Flow.ID
	if _, err = call("password", nativeauth.SecurityInput{Flow: flow, OldPassword: "private-old", NewPassword: "private-new12"}); err == nil || count != 1 {
		t.Fatal("skipped SMS verification")
	}
	if _, err = call("password_verify", nativeauth.SecurityInput{Flow: control.NewID(), Code: "123456"}); err == nil || count != 1 {
		t.Fatal("replaced flow accepted")
	}
	verify, err := call("password_verify", nativeauth.SecurityInput{Flow: flow, Code: "123456"})
	if err != nil || verify.Flow.Stage != "password_verified" {
		t.Fatal(verify, err)
	}
	change := nativeauth.SecurityInput{Flow: flow, OldPassword: "private-old", NewPassword: "private-new12"}
	final, err := call("password", change)
	if err != nil || final.Status != "succeeded" || final.Flow.Stage != "completed" || count != 3 {
		t.Fatal("password", final, err)
	}
	again, err := service.ExecuteSecurity(ctx, ref, req, change, port)
	if err != nil || again.ID != final.ID || count != 3 {
		t.Fatal("replayed password", again, err)
	}
	change.NewPassword = "another-new12"
	if _, err = service.ExecuteSecurity(ctx, ref, req, change, port); err == nil || count != 3 {
		t.Fatal("idempotency secrets changed")
	}
	var state string
	var epoch int64
	var connectionEnabled, receiverEnabled bool
	if err = db.Platform.QueryRow(ctx, `SELECT a.auth_state,a.epoch,a.connection_enabled,r.enabled FROM accounts a
		JOIN account_receivers r ON r.account_id=a.id WHERE a.id=$1`, account).Scan(&state, &epoch, &connectionEnabled, &receiverEnabled); err != nil || state != "reauth_required" || epoch != 2 || connectionEnabled || receiverEnabled {
		t.Fatal("session not fenced", state, epoch, err)
	}
	rows, err := db.Platform.Query(ctx, `SELECT id::text,payload_sealed FROM native_setting_operations WHERE account_id=$1`, account)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var sealed []byte
		rows.Scan(&id, &sealed)
		plain, err := vault.Open("native-setting:"+id, sealed)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(plain, []byte("private-")) || bytes.Contains(plain, []byte("123456")) {
			t.Fatal("operation retained security input")
		}
		clear(plain)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	var retained []byte
	if err = db.Platform.QueryRow(ctx, `SELECT payload_sealed FROM native_security_flows WHERE id=$1`, flow).Scan(&retained); err != nil || len(retained) != 0 {
		t.Fatal("completed flow retained certificate", err)
	}
	rows.Close()
	exec(`UPDATE accounts SET auth_state='authenticated' WHERE id=$1`, account)
	exec(`UPDATE native_setting_operations SET created_at=now()-interval '2 minutes' WHERE account_id=$1`, account)
	req.Epoch, req.Version = 2, 2
	phoneStart, err := call("phone_old_sms", nativeauth.SecurityInput{Captcha: "private-captcha"})
	if err != nil || phoneStart.Flow.Stage != "phone_old_code" {
		t.Fatal("phone verification", phoneStart, err)
	}
	phoneFlow := phoneStart.Flow.ID
	otherActor, otherToken := control.NewID(), ""
	otherToken, _ = security.Token()
	otherRef, _ := control.WebsiteSession(otherToken)
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'other-security-owner','synthetic','superadmin')`, otherActor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(otherToken), security.Digest("test"), otherActor)
	req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: "phone_old_verify"}
	before := count
	if _, err = service.ExecuteSecurity(ctx, otherRef, req, nativeauth.SecurityInput{Flow: phoneFlow, Code: "123456"}, port); err == nil || count != before {
		t.Fatal("another superadmin reused verification")
	}
	req.Workspace = "maibot"
	if _, err = service.ExecuteSecurity(ctx, ref, req, nativeauth.SecurityInput{Flow: phoneFlow, Code: "123456"}, port); err == nil || count != before {
		t.Fatal("cross-workspace verification accepted")
	}
	req.Workspace = "wsl"
	oldVerified, err := call("phone_old_verify", nativeauth.SecurityInput{Flow: phoneFlow, Code: "123456"})
	if err != nil || oldVerified.Flow.Stage != "phone_old_verified" {
		t.Fatal("old phone", oldVerified, err)
	}
	newSMS, err := call("phone_new_sms", nativeauth.SecurityInput{Flow: phoneFlow, Phone: "13800000001", Captcha: "private-captcha"})
	if err != nil || newSMS.Flow.Stage != "phone_new_code" {
		t.Fatal("new phone", newSMS, err)
	}
	// Expiry is checked before the native port, even with a valid OTP.
	exec(`UPDATE native_security_flows SET expires_at=now()-interval '1 second' WHERE id=$1`, phoneFlow)
	before = count
	if _, err = call("phone", nativeauth.SecurityInput{Flow: phoneFlow, Code: "123456"}); err == nil || count != before {
		t.Fatal("expired verification submitted")
	}
	exec(`UPDATE native_security_flows SET expires_at=now()+interval '1 minute' WHERE id=$1`, phoneFlow)
	req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: "phone"}
	unknownPort := func(ctx context.Context, state *nativeauth.SecurityState, fence control.SettingFence) error {
		return fence(ctx, control.SettingEvidence{UID: "7", Epoch: req.Epoch, Version: req.Version, DeploymentVersion: 77,
			Digest: req.Digest, CredentialDigest: sha256.Sum256([]byte("synthetic-sealed")), ObservedAt: time.Now()},
			func(context.Context) error {
				count++
				return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
			})
	}
	lastInput := nativeauth.SecurityInput{Flow: phoneFlow, Code: "123456"}
	unknown, err := service.ExecuteSecurity(ctx, ref, req, lastInput, unknownPort)
	if err != nil || unknown.Status != "unknown" || unknown.Flow.Stage != "unknown" || count != before+1 {
		t.Fatal("unknown security outcome", unknown, err)
	}
	replayed, err := service.ExecuteSecurity(ctx, ref, req, lastInput, unknownPort)
	if err != nil || replayed.ID != unknown.ID || count != before+1 {
		t.Fatal("unknown change replayed", replayed, err)
	}
	if _, err = call("password_sms", nativeauth.SecurityInput{Captcha: "private-captcha"}); err == nil || count != before+1 {
		t.Fatal("unknown phone change allowed another mutation")
	}
}
