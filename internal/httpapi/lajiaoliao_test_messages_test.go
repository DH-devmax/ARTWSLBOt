package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresChiliTextRecallKeepDebugAndDurableFences(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{84}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	actor, account, group, tenant := control.NewID(), control.NewID(), control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	credential := []byte("synthetic-session")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'chili-message-test','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO accounts(id,name,workspace,platform,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
		VALUES($1,'chili-message-test','lajiaoliao','lajiaoliao','self-string','authenticated',$2,true,true)`, account, credential)
	exec(`INSERT INTO tenants(id,name) VALUES($1,'chili-message-tenant')`, tenant)
	exec(`INSERT INTO native_groups(id,platform,native_id,name,isolated_test)
		VALUES($1,'lajiaoliao','group-string','group',true)`, group)
	exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,active,management_enabled,ai_enabled)
		VALUES($1,$2,$3,true,false,false)`, group, tenant, account)
	exec(`INSERT INTO account_groups(account_id,group_id,role,observation_source,observed_at)
		VALUES($1,$2,'owner','native',now())`, account, group)
	exec(`INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at)
		VALUES($1,true,'synthetic-owner',now()+interval '5 minutes',now())`, account)
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(server.URL)
	jar.SetCookies(origin, []*http.Cookie{{Name: "wsl_session", Value: token}, {Name: "wsl_csrf", Value: "csrf"}})
	client := testClient{client: &http.Client{Jar: jar}, base: server.URL, workspace: "lajiaoliao"}
	input := map[string]any{"group_id": group, "account_ids": []string{account},
		"target_ids": []string{"self-string", "target-string"}, "actions": []string{"send_text", "recall_message"},
		"admission_mode": "native_debug", "seconds": 300, "budget": 8}
	window := field(t, expect(t, client, "POST", "/test-windows", input, 201), "id")
	req := control.SettingRequest{Account: account, Workspace: "lajiaoliao", TestWindowID: window, Epoch: 1, Version: 1,
		Key: control.NewID(), Digest: strings.Repeat("b", 64), ReceiverOwner: "synthetic-owner",
		Change: nativeauth.SettingChange{Kind: "send_text", Group: "group-string", Text: "synthetic text"}}
	if err := db.Platform.QueryRow(ctx, `SELECT revision FROM account_receivers WHERE account_id=$1`, account).Scan(&req.ReceiverRevision); err != nil {
		t.Fatal(err)
	}
	role, targetRole, calls := "owner", "member", 0
	var before func()
	port := func(work context.Context, fence control.SettingFence) error {
		if before != nil {
			before()
		}
		proof := control.SettingEvidence{UID: "self-string", Role: role, Target: "target-string", TargetRole: targetRole,
			Epoch: 1, Version: 1, DeploymentVersion: 0, Digest: req.Digest, ObservedAt: time.Now(),
			CredentialDigest: sha256.Sum256(credential), ModerationAllowed: true}
		return fence(work, proof, func(context.Context) error { calls++; return nil })
	}
	if _, err := service.Execute(ctx, ref, req, port); err == nil {
		t.Fatal("ordinary setting endpoint borrowed debug grant")
	}
	out, err := service.ExecuteTestChiliText(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || calls != 1 {
		t.Fatal("durable text rejected", out, err)
	}
	again, err := service.ExecuteTestChiliText(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || calls != 1 {
		t.Fatal("text replayed", err)
	}
	req.Key = control.NewID()
	unknown, err := service.ExecuteTestChiliText(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &lajiaoliao.Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	})
	if err != nil || unknown.Status != "unknown" || calls != 2 {
		t.Fatal("unknown text not frozen", unknown, err)
	}
	again, err = service.ExecuteTestChiliText(ctx, ref, req, port)
	if err != nil || again.ID != unknown.ID || calls != 2 {
		t.Fatal("unknown text replayed", again, err)
	}
	req.Key = control.NewID()
	if _, err = service.ExecuteTestChiliText(ctx, ref, req, port); err == nil || calls != 2 {
		t.Fatal("new text bypassed pending submission", err)
	}
	// The remaining cases use a separate synthetic operation after explicitly
	// observing the first result, never retrying a native write.
	_, err = service.Reconcile(ctx, ref, account, unknown.ID, func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
		return control.SettingEvidence{UID: "self-string", Epoch: 1, Version: 1, Digest: req.Digest,
			CredentialDigest: sha256.Sum256(credential), ObservedAt: time.Now(), Matches: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	refMessage := nativeauth.MessageReference{Platform: "lajiaoliao", EventID: 1, Group: "group-string",
		Sender: "target-string", SourceDigest: strings.Repeat("a", 64), CreatedAt: time.Now().UnixMilli(),
		Sequence: 3, Conversation: "sg_group-string"}
	encoded, _ := json.Marshal(refMessage)
	req.Change = nativeauth.SettingChange{Kind: "recall_message", Group: "group-string", Text: string(encoded)}
	role = "member"
	out, err = service.ExecuteTestRecall(ctx, ref, req, port)
	if err != nil || out.Status != "failed" || calls != 2 {
		t.Fatal("ordinary member recalled another person's message", out, err)
	}
	role, req.Key = "owner", control.NewID()
	out, err = service.ExecuteTestRecall(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || calls != 3 {
		t.Fatal("Chili recall rejected", out, err)
	}
	for _, test := range []struct{ name, change string }{
		{"revoked", `UPDATE test_windows SET cancelled=true`},
		{"epoch", `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`},
		{"connection", `UPDATE account_receivers SET revision=revision+1 WHERE account_id=$1`},
	} {
		req.Key = control.NewID()
		before = func() {
			if test.name == "revoked" {
				exec(test.change)
			} else {
				exec(test.change, account)
			}
		}
		out, err = service.ExecuteTestRecall(ctx, ref, req, port)
		if err != nil || out.Status != "failed" || calls != 3 {
			t.Fatal("late scope change escaped fence", test.name, out, err)
		}
		before = nil
		exec(`UPDATE test_windows SET cancelled=false`)
		exec(`UPDATE accounts SET epoch=1 WHERE id=$1`, account)
		exec(`UPDATE account_receivers SET revision=$2 WHERE account_id=$1`, account, req.ReceiverRevision)
	}
	history, err := service.TestOperations(ctx, ref, account)
	if err != nil || len(history) != 7 {
		t.Fatal("test message history incomplete", len(history), err)
	}
}
