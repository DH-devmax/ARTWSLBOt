package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresNativeSettingsManualFenceRecoveryAndIdempotency(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{43}, 32)))
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
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'native-settings-owner','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
		security.Digest(token), security.Digest("synthetic-csrf"), actor)
	exec(`INSERT INTO accounts(id,name,native_uid,auth_state,credential_sealed,connection_enabled)
        VALUES($1,'synthetic-settings','7','authenticated',$2,true)`, account, []byte("synthetic-sealed-session"))
	exec(`INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,77)`, []byte("synthetic-deployment"))
	req := control.SettingRequest{Account: account, Workspace: "wsl", Epoch: 1, Version: 1,
		Key: control.NewID(), Digest: strings.Repeat("a", 64), Change: nativeauth.SettingChange{Kind: "nickname", Text: "new-name"}}
	proof := func() control.SettingEvidence {
		return control.SettingEvidence{UID: "7", Epoch: 1, Version: 1, DeploymentVersion: 77,
			Digest: req.Digest, CredentialDigest: sha256.Sum256([]byte("synthetic-sealed-session")), ObservedAt: time.Now()}
	}
	var submissions atomic.Int32
	port := func(work context.Context, fence control.SettingFence) error {
		return fence(work, proof(), func(context.Context) error { submissions.Add(1); return nil })
	}
	op, err := service.Execute(ctx, ref, req, port)
	if err != nil || op.Status != "succeeded" || submissions.Load() != 1 {
		t.Fatal("manual setting did not reach exactly one synthetic submission", op, err)
	}
	again, err := service.Execute(ctx, ref, req, port)
	if err != nil || again.ID != op.ID || submissions.Load() != 1 {
		t.Fatal("duplicate setting replayed native HTTP", again, err)
	}
	changed := req
	changed.Change.Text = "other"
	if _, err = service.Execute(ctx, ref, changed, port); err == nil {
		t.Fatal("idempotency key changed contents")
	}
	// Same-key callers converge while one operation is still preparing.
	req.Key = control.NewID()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := service.Execute(ctx, ref, req, port); results <- err }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent duplicate", err)
		}
	}
	if submissions.Load() != 2 {
		t.Fatal("concurrent duplicate produced another submission")
	}
	// Revocation during a fresh read must win at the final submission fence.
	req.Key = control.NewID()
	blocked, err := service.Execute(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		exec(`UPDATE users SET disabled=true WHERE id=$1`, actor)
		return port(work, fence)
	})
	if err != nil || blocked.Status != "failed" || submissions.Load() != 2 {
		t.Fatal("revoked session submitted a native mutation", blocked, err)
	}
	exec(`UPDATE users SET disabled=false WHERE id=$1`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
		security.Digest(token), security.Digest("synthetic-csrf"), actor)
	// A response lost after submission is durable unknown and never retried.
	req.Key = control.NewID()
	unknown, err := service.Execute(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	})
	if err != nil || unknown.Status != "unknown" || submissions.Load() != 3 {
		t.Fatal("lost response was not preserved", unknown, err)
	}
	pending, err := service.Pending(ctx, ref, account)
	if err != nil || len(pending) != 1 || pending[0]["id"] != unknown.ID || pending[0]["key"] != req.Key {
		t.Fatal("reopening could not discover the unresolved original operation", err)
	}
	req.Key = control.NewID()
	if _, err = service.Execute(ctx, ref, req, port); err == nil || submissions.Load() != 3 {
		t.Fatal("unknown request did not block a new modification")
	}
	observed, err := service.Reconcile(ctx, ref, account, unknown.ID, func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
		value := proof()
		value.Matches, value.Digest = true, strings.Repeat("b", 64)
		return value, nil
	})
	if err != nil || observed.Status != "succeeded" || observed.Code != "desired_state_observed" || submissions.Load() != 3 {
		t.Fatal("read-only reconciliation mutated or lied about execution", observed, err)
	}
	// No plaintext nickname is stored in audit or operation metadata.
	var leaked bool
	if err := db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE detail::text LIKE '%new-name%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("nickname leaked into audit", err)
	}
	if err = db.TenantTx(ctx, control.NewID(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT * FROM native_setting_operations`)
		return err
	}); err == nil {
		t.Fatal("tenant database role could read shared account modifications")
	}
	tenant, plan, group := control.NewID(), control.NewID(), control.NewID()
	exec(`INSERT INTO tenants(id,name) VALUES($1,'settings-group-tenant')`, tenant)
	exec(`INSERT INTO plans(id,name,group_limit,ai_group_limit,modules) VALUES($1,'settings-cards',10,0,ARRAY['cards'])`, plan)
	exec(`INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, tenant, plan)
	exec(`INSERT INTO native_groups(id,native_id,name) VALUES($1,'123','synthetic-group')`, group)
	exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,active,management_enabled) VALUES($1,$2,$3,true,true)`, group, tenant, account)
	req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: "member_nickname", Group: "123", Target: "7", Text: "self-card"}
	selfPort := func(work context.Context, fence control.SettingFence) error {
		value := proof()
		value.Role, value.Target, value.TargetRole = "member", "7", "member"
		return fence(work, value, func(context.Context) error { submissions.Add(1); return nil })
	}
	exec(`UPDATE accounts SET connection_enabled=false WHERE id=$1`, account)
	if _, err = service.Execute(ctx, ref, req, selfPort); err == nil || submissions.Load() != 3 {
		t.Fatal("closed native action gate admitted a group nickname update")
	}
	exec(`UPDATE accounts SET connection_enabled=true WHERE id=$1`, account)
	req.Key = control.NewID()
	self, err := service.Execute(ctx, ref, req, selfPort)
	if err != nil || self.Status != "succeeded" || submissions.Load() != 4 {
		t.Fatal("robot could not update its own authorized group nickname", self, err)
	}
	req.Key = control.NewID()
	exec(`UPDATE group_bindings SET active=false,epoch=epoch+1 WHERE group_id=$1`, group)
	if _, err = service.Execute(ctx, ref, req, selfPort); err == nil || submissions.Load() != 4 {
		t.Fatal("revoked binding admitted a group nickname update")
	}
}

// Keep denied/unknown classification stable even when errors wrap other layers.
func TestNativeSettingUnsupportedInputStaysLocal(t *testing.T) {
	for _, change := range []nativeauth.SettingChange{
		{Kind: "avatar", Text: "https://attacker.invalid/x"},
		{Kind: "nickname", Group: "123", Text: "unrelated"},
	} {
		if err := change.Validate(); err == nil || errors.Is(err, context.Canceled) {
			t.Fatal("unsupported account setting was admitted")
		}
	}
}
