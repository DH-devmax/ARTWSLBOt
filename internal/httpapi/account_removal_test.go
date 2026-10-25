package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresAccountRemoval(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "removal-isolated-password-123"
	hash, _ := security.Password(password)
	var actor string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('removal-admin',$1,'superadmin') RETURNING id::text`, hash).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{21}, 32)))
	s := New(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{"username": name, "password": password}, 200)
		return c
	}
	admin := login("removal-admin")
	chat := admin
	chat.workspace = "maibot"
	tenant := field(t, expect(t, admin, "POST", "/users", map[string]string{
		"username": "removal-user", "password": password, "tenant_name": "removal-tenant",
	}, 201), "tenant_id")
	user := login("removal-user")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(sql string, want int, args ...any) {
		t.Helper()
		var got int
		if err := db.Platform.QueryRow(ctx, sql, args...).Scan(&got); err != nil || got != want {
			t.Fatalf("count got=%d want=%d err=%v query=%s", got, want, err, sql)
		}
	}
	account := func(c testClient, name string) string {
		return field(t, expect(t, c, "POST", "/accounts", map[string]string{"name": name}, 201), "id")
	}
	group := func(name string) string {
		var id string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name,isolated_test)
            VALUES($1,$1,true) RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	removeBody := func(name string) map[string]any { return map[string]any{"version": 1, "confirm_name": name} }
	primary, survivor, ai := account(admin, "remove-manager"), account(admin, "keep-manager"), account(chat, "remove-chat")
	owned, shared, orphan := group("owned-removal"), group("shared-removal"), group("orphan-removal")
	exec(`UPDATE accounts SET native_uid='7654321',credential_sealed=$2,auth_state='authenticated' WHERE id=$1`, primary, []byte("sealed-fixture"))
	for _, pair := range [][2]string{{primary, owned}, {primary, shared}, {primary, orphan}, {survivor, shared}, {ai, owned}} {
		exec(`INSERT INTO account_groups VALUES($1,$2,'admin',now(),'replay')`, pair[0], pair[1])
	}
	exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,ai_account_id,management_enabled,ai_enabled,ai_requested,ai_request_state,config)
        VALUES($1,$2,$3,$4,true,true,true,'ready','{"blocked_words":["private-fixture"]}')`, owned, tenant, primary, ai)
	exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,ai_account_id,management_enabled,ai_enabled,ai_requested,ai_request_state)
        VALUES($1,$2,$3,$4,true,true,true,'ready')`, shared, tenant, survivor, ai)
	for _, id := range []string{primary, ai} {
		exec(`INSERT INTO personas VALUES($1,'fixture','private persona','fixture',1)`, id)
		exec(`INSERT INTO maibot_settings(account_id,section,sealed,updated_by) VALUES($1,'models',$2,$3)`, id, []byte("sealed-model-fixture"), actor)
		exec(`INSERT INTO inbox(account_id,group_id,native_message_id,sender_id,received_at,payload_sealed,state)
            VALUES($1,$2,'fixture-message','fixture-member',now(),$3,'pending')`, id, owned, []byte("sealed-message-fixture"))
	}
	exec(`INSERT INTO inbox(account_id,group_id,native_message_id,sender_id,received_at,state)
        VALUES($1,$2,'surviving-message','fixture-member',now(),'processed')`, survivor, shared)
	exec(`INSERT INTO claim_codes VALUES($1,$2,$3,1,now()+interval '10 minutes',NULL)`, []byte("fixture-code-hash"), orphan, primary)
	exec(`INSERT INTO group_claims(tenant_id,group_id,account_id,applicant_id,native_identity)
        VALUES($1,$2,$3,$4,'private-identity')`, tenant, orphan, primary, actor)
	exec(`INSERT INTO native_signup_attempts(transaction_id,account_id,actor_id,deployment_version,account_version,state)
        VALUES($1,$2,$3,1,1,'rejected')`, strings.Repeat("r", 43), primary, actor)
	exec(`INSERT INTO ledger_entries(tenant_id,group_id,member_id,kind,amount_cents,external_key,evidence,actor_id)
        VALUES($1,$2,'fixture-member','paid',12345,'removal-ledger-fixture','{"source":"fixture"}',$3)`, tenant, owned, actor)
	exec(`INSERT INTO activities(tenant_id,group_id,name,closes_at,winners)
        VALUES($1,$2,'retained-activity',now()+interval '1 hour',1)`, tenant, owned)
	var single, multiple string
	for _, fixture := range []struct {
		accounts []string
		id       *string
	}{{[]string{primary}, &single}, {[]string{primary, survivor}, &multiple}} {
		epochs, _ := json.Marshal(map[string]int{primary: 1, survivor: 1})
		err := db.Platform.QueryRow(ctx, `INSERT INTO test_windows(actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget)
            VALUES($1,$2,$3::uuid[],'{}','{send_text}',$4,now()+interval '240 seconds',10) RETURNING id::text`,
			actor, shared, fixture.accounts, epochs).Scan(fixture.id)
		if err != nil {
			t.Fatal(err)
		}
	}
	op := func(acc, window, state, key string) {
		exec(`INSERT INTO operations(actor_id,group_id,account_id,test_window_id,idempotency_key,request_hash,kind,account_epoch,status,expires_at)
            VALUES($1,$2,$3,nullif($4,'')::uuid,$5,$6,'send_text',1,$7,now()+interval '2 minutes')`,
			actor, shared, acc, window, key, []byte("fixture"), state)
	}
	op(primary, single, "succeeded", "remove-operation")
	op(survivor, multiple, "queued", "shared-operation")
	exec(`INSERT INTO audit_events(actor_id,action,resource_id,detail) VALUES($1,'fixture',$2,jsonb_build_object('account_id',$2::text))`, actor, primary)
	exec(`INSERT INTO audit_events(actor_id,action,resource_id,detail) VALUES($1,'test_window_created',$2,jsonb_build_object('AccountIDs',jsonb_build_array($3::text,$4::text)))`, actor, multiple, primary, survivor)
	preview := expect(t, admin, "GET", "/accounts/"+primary+"/removal-preview", nil, 200)
	if bytes.Contains(preview, []byte("sealed-fixture")) || bytes.Contains(preview, []byte("private-identity")) ||
		!bytes.Contains(preview, []byte(`"ledger_entries":1`)) {
		t.Fatal("preview leaked content or omitted retention")
	}
	count(`SELECT count(*) FROM accounts WHERE id=$1`, 1, primary)
	expect(t, user, "GET", "/accounts/"+primary+"/removal-preview", nil, 403)
	expect(t, user, "DELETE", "/accounts/"+primary, removeBody("remove-manager"), 403)
	expect(t, chat, "DELETE", "/accounts/"+primary, removeBody("remove-manager"), 404)
	expect(t, admin, "DELETE", "/accounts/"+primary, removeBody("wrong-name"), 400)
	expect(t, admin, "DELETE", "/accounts/"+primary, map[string]any{"version": 99, "confirm_name": "remove-manager"}, 409)
	// A session cookie alone must not authorize removal.
	req, _ := http.NewRequest("DELETE", server.URL+"/api/v1/accounts/"+primary, strings.NewReader(`{"version":1,"confirm_name":"remove-manager"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := admin.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("removal bypassed CSRF")
	}
	t.Run("uncertain outcomes block the entire transaction", func(t *testing.T) {
		op(primary, "", "unknown", "unknown-operation")
		status, _, code := admin.request("DELETE", "/accounts/"+primary, removeBody("remove-manager"))
		if status != 409 || code != "account_removal_operations_pending" {
			t.Fatal("unknown evidence was not protected", status, code)
		}
		count(`SELECT count(*) FROM accounts WHERE id=$1 AND version=1 AND credential_sealed IS NOT NULL`, 1, primary)
		count(`SELECT count(*) FROM group_bindings WHERE group_id=$1 AND management_enabled`, 1, owned)
		exec(`UPDATE operations SET status='succeeded' WHERE account_id=$1 AND status='unknown'`, primary)
		op(survivor, multiple, "running", "shared-running-operation")
		status, _, code = admin.request("DELETE", "/accounts/"+primary, removeBody("remove-manager"))
		if status != 409 || code != "account_removal_operations_pending" {
			t.Fatal("another participant's running result was not protected", status, code)
		}
		exec(`UPDATE operations SET status='failed' WHERE idempotency_key='shared-running-operation'`)
		exec(`UPDATE native_signup_attempts SET state='submitted' WHERE account_id=$1`, primary)
		status, _, code = admin.request("DELETE", "/accounts/"+primary, removeBody("remove-manager"))
		if status != 409 || code != "account_removal_signup_pending" {
			t.Fatal("unresolved registration was erased", status, code)
		}
		exec(`UPDATE native_signup_attempts SET state='rejected' WHERE account_id=$1`, primary)
	})
	// Synthetic auth manager flow only; never construct a native network client.
	s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return &isolatedAuth{uid: 7654321}, nil }
	flow, err := s.Native.Start(actor, primary, nativeauth.Deployment{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "DELETE", "/accounts/"+primary, removeBody("remove-manager"), 200)
	if _, err := s.Native.Poll(actor, primary, flow.ID); err == nil {
		t.Fatal("deleted account retains a verification flow")
	}
	for _, table := range []string{"accounts", "account_groups", "inbox", "personas", "maibot_settings", "claim_codes", "group_claims", "native_signup_attempts", "operations"} {
		column := "account_id"
		if table == "accounts" {
			column = "id"
		}
		count(`SELECT count(*) FROM `+table+` WHERE `+column+`=$1`, 0, primary)
	}
	count(`SELECT count(*) FROM test_windows WHERE $1::uuid=ANY(account_ids) OR account_epochs ? $1::text`, 0, primary)
	count(`SELECT count(*) FROM test_windows WHERE id=$1`, 0, single)
	count(`SELECT count(*) FROM test_windows WHERE id=$1 AND cancelled AND account_ids=ARRAY[$2::uuid] AND NOT account_epochs ? $3::text`, 1, multiple, survivor, primary)
	count(`SELECT count(*) FROM operations WHERE account_id=$1 AND status='cancelled' AND result_code='account_removed'`, 1, survivor)
	count(`SELECT count(*) FROM native_groups WHERE id=$1`, 0, orphan)
	count(`SELECT count(*) FROM native_groups WHERE id=$1`, 1, shared)
	count(`SELECT count(*) FROM group_bindings WHERE group_id=$1 AND primary_account_id IS NULL AND NOT active
        AND NOT management_enabled AND NOT ai_enabled AND ai_account_id IS NULL AND config='{}' AND version=2 AND epoch=2`, 1, owned)
	count(`SELECT count(*) FROM ledger_entries WHERE group_id=$1 AND amount_cents=12345`, 1, owned)
	count(`SELECT count(*) FROM activities WHERE group_id=$1`, 1, owned)
	count(`SELECT count(*) FROM inbox WHERE account_id=$1`, 1, survivor)
	count(`SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='account_deleted'`, 1, primary)
	count(`SELECT count(*) FROM audit_events WHERE detail->>'account_id'=$1 OR detail->'AccountIDs' @> jsonb_build_array($1::text)`, 0, primary)
	if !bytes.Contains(expect(t, admin, "GET", "/groups", nil, 200), []byte(owned)) {
		t.Fatal("detached ownership disappeared from the management UI")
	}
	status, _, code := admin.request("PATCH", "/groups/"+owned, map[string]any{"version": 2, "config": map[string]any{}})
	if status != 409 || code != "group_robot_removed" {
		t.Fatal("detached group can be reconfigured", status, code)
	}
	expect(t, admin, "GET", "/accounts/"+primary+"/removal-preview", nil, 404)
	expect(t, admin, "POST", "/accounts/"+primary+"/native-auth", map[string]int{"version": 1}, 404)
	count(`SELECT count(*) FROM accounts WHERE id=$1 AND version=1`, 1, survivor)
	expect(t, chat, "DELETE", "/accounts/"+ai, removeBody("remove-chat"), 200)
	count(`SELECT count(*) FROM group_bindings WHERE group_id=$1 AND primary_account_id=$2 AND active
        AND management_enabled AND ai_account_id IS NULL AND NOT ai_enabled AND NOT ai_requested AND version=2`, 1, shared, survivor)
	count(`SELECT count(*) FROM ledger_entries WHERE group_id=$1`, 1, owned)
	t.Run("dependency failure rolls back credentials bindings and cleanup", func(t *testing.T) {
		target := account(admin, "rollback-manager")
		exec(`UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, target, []byte("rollback-sealed-fixture"))
		var window string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO test_windows(actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget)
            VALUES($1,$2,ARRAY[$3::uuid],'{}','{send_text}','{}',now()+interval '240 seconds',10) RETURNING id::text`,
			actor, shared, target).Scan(&window); err != nil {
			t.Fatal(err)
		}
		// Unexpected legacy reference: deleting it would erase another robot's evidence.
		op(survivor, window, "succeeded", "rollback-other-operation")
		status, _, code := admin.request("DELETE", "/accounts/"+target, removeBody("rollback-manager"))
		if status != 409 || code != "account_removal_dependencies_changed" {
			t.Fatal("dependency failure did not report a rollback", status, code)
		}
		count(`SELECT count(*) FROM accounts WHERE id=$1 AND credential_sealed IS NOT NULL AND version=1`, 1, target)
		count(`SELECT count(*) FROM test_windows WHERE id=$1 AND NOT cancelled`, 1, window)
		count(`SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='account_deleted'`, 0, target)
	})
	t.Run("inflight login cannot recreate a deleted panel", func(t *testing.T) {
		target := account(admin, "inflight-manager")
		key := base64.StdEncoding.EncodeToString(make([]byte, 32))
		d := nativeauth.Deployment{Origin: "https://example.invalid", Headers: map[string]string{},
			Metadata: make([]uint64, 14), SigningSeed: key, BodyKey: key, ServerKey: key,
			CaptchaID: "synthetic-captcha", DeviceID: "synthetic-device", ProtocolVersion: "3.7.3"}
		encoded, _ := json.Marshal(d)
		expect(t, admin, "PUT", "/native-deployment", map[string]any{"version": 0, "configuration": string(encoded)}, 200)
		fake := &isolatedAuth{block: true, start: make(chan struct{}), done: make(chan struct{}), uid: 7654321}
		s.Native.Factory = func(nativeauth.Deployment) (nativeauth.AuthClient, error) { return fake, nil }
		txID := field(t, expect(t, admin, "POST", "/accounts/"+target+"/native-auth", map[string]int{"version": 1}, 201), "transaction_id")
		expect(t, admin, "POST", "/accounts/"+target+"/native-auth/"+txID, map[string]string{
			"action": "password", "account": "synthetic", "password": "synthetic-not-live", "captcha": "synthetic-human",
		}, 202)
		select {
		case <-fake.start:
		case <-time.After(2 * time.Second):
			t.Fatal("synthetic login did not start")
		}
		expect(t, admin, "DELETE", "/accounts/"+target, removeBody("inflight-manager"), 200)
		close(fake.done)
		count(`SELECT count(*) FROM accounts WHERE id=$1`, 0, target)
		expect(t, admin, "GET", "/accounts/"+target+"/native-auth/"+txID, nil, 404)
	})
	// Identity uniqueness and admission slots are released, without unregistering upstream.
	replacement := account(admin, "replacement")
	exec(`UPDATE accounts SET native_uid='7654321' WHERE id=$1`, replacement)
	t.Run("concurrent removal has one winner", func(t *testing.T) {
		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, _, _ := admin.request("DELETE", "/accounts/"+replacement, removeBody("replacement"))
				statuses <- status
			}()
		}
		wg.Wait()
		close(statuses)
		results := map[int]int{}
		for status := range statuses {
			results[status]++
		}
		if results[200] != 1 || results[404] != 1 {
			t.Fatal("concurrent deletion did not fence stale callers", results)
		}
	})
}
