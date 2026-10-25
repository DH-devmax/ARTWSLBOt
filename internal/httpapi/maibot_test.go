package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"wangshangliao/internal/security"
)

func TestPostgresGoConsole(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("go-console-isolated-password")
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role) VALUES('console-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{21}, 32)))
	s := New(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "console-admin", "password": "go-console-isolated-password"}, 200)
	chat := admin
	chat.workspace = "maibot"
	a := field(t, expect(t, chat, "POST", "/accounts", map[string]string{"name": "chat-a"}, 201), "id")
	b := field(t, expect(t, chat, "POST", "/accounts", map[string]string{"name": "chat-b"}, 201), "id")
	path := "/accounts/" + a + "/maibot-settings/models"
	body := map[string]any{"version": 0, "config": map[string]any{
		"endpoint": "https://synthetic.invalid/v1", "chat_model": "fixture", "api_key": "sensitive-synthetic-provider-key",
	}}
	reply := expect(t, chat, "PUT", path, body, 200)
	if bytes.Contains(reply, []byte("sensitive-synthetic-provider-key")) || !bytes.Contains(reply, []byte(`"applied":false`)) {
		t.Fatal("configuration response leaked secret or implied activation")
	}
	loaded := expect(t, chat, "GET", path, nil, 200)
	if bytes.Contains(loaded, []byte("sensitive-synthetic-provider-key")) || !bytes.Contains(loaded, []byte(`"has_api_key":true`)) {
		t.Fatal("secret read was not projected")
	}
	var stored []byte
	if err := db.Platform.QueryRow(ctx, `SELECT sealed FROM maibot_settings WHERE account_id=$1 AND section='models'`, a).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("sensitive-synthetic-provider-key")) {
		t.Fatal("model key was not sealed")
	}
	body = map[string]any{"version": 1, "config": map[string]any{"endpoint": "https://synthetic.invalid/v1", "chat_model": "fixture"}}
	expect(t, chat, "PUT", path, body, 200)
	var refreshed []byte
	_ = db.Platform.QueryRow(ctx, `SELECT sealed FROM maibot_settings WHERE account_id=$1 AND section='models'`, a).Scan(&refreshed)
	plain, err := vault.Open("maibot:"+a+":models", refreshed)
	if err != nil || !bytes.Contains(plain, []byte("sensitive-synthetic-provider-key")) {
		t.Fatal("blank key overwrote an existing credential")
	}
	clear(plain)
	expect(t, chat, "PUT", path, body, 409)
	expect(t, admin, "GET", path, nil, 403)
	other := expect(t, chat, "GET", "/accounts/"+b+"/maibot-settings/models", nil, 200)
	if bytes.Contains(other, []byte(`"has_api_key":true`)) {
		t.Fatal("account configuration leaked")
	}
	for _, section := range []string{"learning", "memory"} {
		config := map[string]any{"cross_group": true, "derived_retention_days": 30}
		expect(t, chat, "PUT", "/accounts/"+a+"/maibot-settings/"+section, map[string]any{"version": 0, "config": config}, 400)
	}
	expect(t, chat, "GET", "/maibot/capabilities", nil, 200)
	expect(t, chat, "GET", "/system/monitor", nil, 200)
	var leaked bool
	if err := db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE detail::text LIKE '%sensitive-synthetic%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("audit contains model secret")
	}
	var denied bool
	if err := db.Platform.QueryRow(ctx, `SELECT NOT has_table_privilege('wsl_tenant','maibot_settings','SELECT')`).Scan(&denied); err != nil || !denied {
		t.Fatal("tenant role can read shared model settings")
	}
	// Independent pool caps, including a race for the last slot.
	for i := 0; i < 9; i++ {
		expect(t, admin, "POST", "/accounts", map[string]string{"name": "manager"}, 201)
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _, _ := admin.request("POST", "/accounts", map[string]string{"name": "last-manager"})
			statuses <- status
		}()
	}
	wg.Wait()
	close(statuses)
	accepted := 0
	for status := range statuses {
		if status == 201 {
			accepted++
		} else if status != 409 {
			t.Fatalf("unexpected admission response: %d", status)
		}
	}
	if accepted != 1 {
		t.Fatal("concurrent account cap was exceeded")
	}
	for range 8 {
		expect(t, chat, "POST", "/accounts", map[string]string{"name": "chat"}, 201)
	}
	expect(t, chat, "POST", "/accounts", map[string]string{"name": "eleventh-chat"}, 409)
	plan := expect(t, admin, "POST", "/plans", map[string]any{"name": "100-group-plan", "group_limit": 100, "ai_group_limit": 100, "modules": []string{"ai"}}, 201)
	var planID map[string]json.RawMessage
	_ = json.Unmarshal(plan, &planID)
	if planID["id"] == nil {
		t.Fatal("100-group AI plan rejected")
	}
}
