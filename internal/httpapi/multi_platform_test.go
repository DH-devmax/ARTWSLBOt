package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"wangshangliao/internal/security"
)

func TestPostgresPlatformPoolsAndIdentity(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	var management, chat []string
	for i := 0; i < 10; i++ {
		platform := "wsl"
		if i%2 != 0 {
			platform = "lajiaoliao"
		}
		var id string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,platform,purpose,native_uid)
            VALUES($1,$2,'management',$3)RETURNING id::text`, fmt.Sprintf("synthetic-%d", i), platform, fmt.Sprintf("uid-%d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		management = append(management, id)
		if err := db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,platform,purpose,native_uid)
            VALUES($1,$2,'chat',$3)RETURNING id::text`, fmt.Sprintf("synthetic-chat-%d", i), platform, fmt.Sprintf("chat-%d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		chat = append(chat, id)
	}
	for _, purpose := range []string{"management", "chat"} {
		if _, err := db.Platform.Exec(ctx, `INSERT INTO accounts(name,purpose)VALUES('eleventh',$1)`, purpose); err == nil {
			t.Fatal("eleventh account admitted", purpose)
		}
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET native_uid='same-native' WHERE id=ANY($1::uuid[])`, management[:2]); err != nil {
		t.Fatal("cross-platform equal native IDs must stay distinct", err)
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET native_uid='same-native' WHERE id=$1`, chat[0]); err == nil {
		t.Fatal("same native identity admitted in both pools")
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET platform='lajiaoliao' WHERE id=$1`, management[0]); err == nil {
		t.Fatal("account relabeled")
	}
	var tenant string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO tenants(name)VALUES('synthetic')RETURNING id::text`).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		account := i / 30
		platform := "wsl"
		if account%2 != 0 {
			platform = "lajiaoliao"
		}
		var group string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)VALUES($1,$2,$2)RETURNING id::text`,
			platform, fmt.Sprintf("group-%d", i)).Scan(&group); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,ai_account_id,ai_requested)
            VALUES($1,$2,$3,$4,true)`, group, tenant, management[account], chat[account]); err != nil {
			t.Fatal("300-group admission failed", i, err)
		}
		if i == 29 {
			var extra string
			if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)VALUES('wsl','thirty-first','extra')RETURNING id::text`).Scan(&extra); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)VALUES($1,$2,$3)`, extra, tenant, management[0]); err == nil {
				t.Fatal("31st group admitted")
			}
		}
	}
	var extra string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)VALUES('wsl','301','extra')RETURNING id::text`).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled)VALUES($1,$2,$3,false)`, extra, tenant, management[0]); err == nil {
		t.Fatal("301st authorization admitted")
	}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
        SELECT $1,id,'member',now(),'native' FROM native_groups WHERE platform='lajiaoliao' LIMIT 1`, management[0]); err == nil {
		t.Fatal("cross-platform membership accepted")
	}
	// Disabled features retain slots; revoked authorization releases one.
	if _, err := db.Platform.Exec(ctx, `UPDATE group_bindings SET management_enabled=false`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled)VALUES($1,$2,$3,false)`, extra, tenant, management[0]); err == nil {
		t.Fatal("feature disable released authorization capacity")
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE group_bindings SET active=false WHERE group_id=(SELECT group_id FROM group_bindings WHERE primary_account_id=$1 LIMIT 1)`, management[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled)VALUES($1,$2,$3,false)`, extra, tenant, management[0]); err != nil {
		t.Fatal("revocation did not release capacity", err)
	}
}

func TestPostgresUserCenterPlatformAccess(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("synthetic-platform-password")
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)VALUES('platform-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{73}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{"username": name, "password": "synthetic-platform-password"}, 200)
		return c
	}
	admin := login("platform-admin")
	user := expect(t, admin, "POST", "/users", map[string]string{"username": "platform-user", "password": "synthetic-platform-password", "tenant_name": "synthetic"}, 201)
	tenant := field(t, user, "tenant_id")
	explicitTestCeiling(t, admin, tenant)
	alice := login("platform-user")
	plan := field(t, expect(t, admin, "POST", "/plans", map[string]any{"name": "chili-only", "group_limit": 300, "ai_group_limit": 300,
		"monthly_ai_tokens": 10000, "modules": []string{"ai", "moderation"}, "allowed_platforms": []string{"lajiaoliao"}}, 201), "id")
	expect(t, admin, "PUT", "/subscriptions/"+tenant, map[string]any{"plan_id": plan, "expires_at": time.Now().Add(time.Hour), "enabled": true, "version": 0}, 200)
	chili := admin
	chili.workspace = "lajiaoliao"
	management := field(t, expect(t, chili, "POST", "/accounts", map[string]any{"name": "synthetic-chili"}, 201), "id")
	if _, err := db.Platform.Exec(ctx, `INSERT INTO lajiaoliao_auth_state(account_id,device_id,state,attempted_at,stream_initialized)
        VALUES($1,'synthetic-stable-device','submitting',now(),true)`, management); err != nil {
		t.Fatal(err)
	}
	expect(t, chili, "POST", "/accounts/"+management+"/forget-session", map[string]any{"version": 1}, 409)
	if _, err := db.Platform.Exec(ctx, `UPDATE lajiaoliao_auth_state SET attempted_at=now()-interval '2 minutes' WHERE account_id=$1`, management); err != nil {
		t.Fatal(err)
	}
	expect(t, chili, "POST", "/accounts/"+management+"/forget-session", map[string]any{"version": 1}, 200)
	var reset bool
	if err := db.Platform.QueryRow(ctx, `SELECT st.state='ready' AND st.device_id='synthetic-stable-device' AND st.stream_initialized
        AND a.epoch=2 AND a.version=2 AND a.credential_sealed IS NULL
        FROM accounts a JOIN lajiaoliao_auth_state st ON st.account_id=a.id WHERE a.id=$1`, management).Scan(&reset); err != nil || !reset {
		t.Fatal("review reset lost identity/cursor or did not invalidate the old login", err)
	}
	chat := admin
	chat.workspace = "maibot"
	chatID := field(t, expect(t, chat, "POST", "/accounts", map[string]any{"name": "synthetic-chat", "platform": "lajiaoliao"}, 201), "id")
	expect(t, chat, "PUT", "/personas/"+chatID, map[string]any{"nickname": "published-name", "personality": "never-disclose-prompt", "reply_style": "", "published": true, "version": 0}, 200)
	var group string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)VALUES('lajiaoliao','same-native','my-chili-group')RETURNING id::text`).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)VALUES($1,$2,$3)`, group, tenant, management); err != nil {
		t.Fatal(err)
	}
	panels := expect(t, alice, "GET", "/workspaces", nil, 200)
	if bytes.Contains(panels, []byte(`"id":"maibot"`)) || bytes.Contains(panels, []byte(`"id":"wsl"`)) || !bytes.Contains(panels, []byte(`"id":"user"`)) {
		t.Fatal("tenant panel grants", string(panels))
	}
	center := expect(t, alice, "GET", "/user-center", nil, 200)
	if bytes.Contains(center, []byte("never-disclose-prompt")) || !bytes.Contains(center, []byte("published-name")) || !bytes.Contains(center, []byte("my-chili-group")) {
		t.Fatal("unsafe/incomplete user projection")
	}
	alice.workspace = "lajiaoliao"
	expect(t, alice, "GET", "/accounts/"+management+"/lajiaoliao-challenge", nil, 405)
	expect(t, alice, "POST", "/accounts/"+management+"/lajiaoliao-challenge", map[string]any{}, 403)
	expect(t, alice, "PUT", "/groups/"+group+"/chat-preferences", map[string]any{"version": 1, "preference_version": 0, "activity": 20,
		"start_minute": 0, "end_minute": 1439, "monthly_token_limit": 1000, "persona_id": chatID}, 200)
	expect(t, alice, "PUT", "/groups/"+group+"/chat-preferences", map[string]any{"version": 1, "preference_version": 0, "activity": 30}, 409)
	var enabled bool
	if err := db.Platform.QueryRow(ctx, `SELECT ai_enabled FROM group_bindings WHERE group_id=$1`, group).Scan(&enabled); err != nil || enabled {
		t.Fatal("preferences activated engine", err)
	}
	var state map[string]any
	json.Unmarshal(expect(t, alice, "GET", "/user-center", nil, 200), &state)
	alice.workspace = "wsl"
	expect(t, alice, "GET", "/groups", nil, 403)
	expect(t, admin, "PATCH", "/accounts/"+management, map[string]any{"platform": "wsl", "version": 1}, 404)
}
