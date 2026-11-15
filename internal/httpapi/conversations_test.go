package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func TestPostgresNativeConversationsIsolation(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-conversation-password"
	hash, _ := security.Password(password)
	_, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role) VALUES('message-admin',$1,'superadmin')`, hash)
	if err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{21}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	s.Receiver = gateway.NewReceiverManager(db.Platform, vault, s.nativeRequestIDs)
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "message-admin", "password": password}, 200)
	id := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "message-one"}, 201), "id")
	other := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "message-two"}, 201), "id")
	key := strings.Repeat("a", 64)
	var event int64
	err = db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
        VALUES($1,'synthetic-key','group',now(),'\x0102') RETURNING id`, id).Scan(&event)
	if err != nil {
		t.Fatal(err)
	}
	content := gateway.DecodedMessage{Message: nativewire.ApplicationMessage{
		Sender: nativewire.MessageSource{ID: 7, Name: "synthetic-sender"}, Target: nativewire.MessageSource{ID: 123},
		Text: "<script>synthetic-not-html</script>", Session: 2}, TransportTo: "456", Mapping: "unverified"}
	plain, _ := json.Marshal(content)
	sealed, _ := vault.Seal(gateway.DecodedAAD(id, event), plain)
	_, err = db.Platform.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
        deployment_version,payload_sealed,received_at,decoded_at,decode_ms)
        VALUES($1,$2,'decoded','group',$3,1,$4,now(),now(),2.5)`, event, id, key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	directory, _ := json.Marshal([]nativeauth.DirectoryGroup{{ID: "123", Team: "456", Name: "synthetic-directory", Role: "unknown"}})
	dirSealed, _ := vault.Seal("native-directory:"+id, directory)
	_, err = db.Platform.Exec(ctx, `INSERT INTO native_directory_snapshots(account_id,payload_sealed,sampled_at,group_count)
        VALUES($1,$2,now(),1)`, id, dirSealed)
	if err != nil {
		t.Fatal(err)
	}
	path := "/accounts/" + id
	body := expect(t, admin, "GET", path+"/conversations", nil, 200)
	if !bytes.Contains(body, []byte("synthetic-directory")) || !bytes.Contains(body, []byte("directory_matched")) {
		t.Fatal("directory and authenticated routing not matched")
	}
	body = expect(t, admin, "GET", path+"/conversations/"+key+"/sending", nil, 200)
	if !bytes.Contains(body, []byte("native_text_group_unbound")) || bytes.Contains(body, []byte(`"enabled":true`)) {
		t.Fatal("observed native group was treated as an authorized send target")
	}
	expect(t, admin, "POST", path+"/conversations/"+key+"/text-operations",
		map[string]any{"text": "synthetic-not-sent"}, 409)
	expect(t, admin, "POST", path+"/conversations/"+key+"/text-operations/"+other+"/dispatch", map[string]any{}, 409)
	expect(t, admin, "GET", "/accounts/"+other+"/conversations/"+key+"/sending", nil, 404)
	body = expect(t, admin, "GET", path+"/conversations/"+key+"/messages", nil, 200)
	if !bytes.Contains(body, []byte("synthetic-not-html")) {
		t.Fatal("decoded message missing")
	}
	body = expect(t, admin, "GET", path+"/conversations/"+key+"/messages?ids="+strconv.FormatInt(event, 10), nil, 200)
	if !bytes.Contains(body, []byte("synthetic-not-html")) {
		t.Fatal("committed message delta missing")
	}
	body = expect(t, admin, "GET", "/accounts/"+other+"/conversations/"+key+"/messages?ids="+strconv.FormatInt(event, 10), nil, 200)
	if bytes.Contains(body, []byte("synthetic-not-html")) {
		t.Fatal("delta query crossed account scope")
	}
	expect(t, admin, "GET", path+"/conversations/"+key+"/messages?ids=1,bad", nil, 400)
	expect(t, admin, "GET", path+"/conversations/"+key+"/messages?ids=1&before=10", nil, 400)
	body = expect(t, admin, "GET", "/accounts/"+other+"/conversations/"+key+"/messages", nil, 200)
	if bytes.Contains(body, []byte("synthetic-not-html")) {
		t.Fatal("account scope leak")
	}
	expect(t, admin, "GET", path+"/conversations?scene=invalid", nil, 400)
	expect(t, admin, "GET", path+"/conversations?limit=0", nil, 400)
	expect(t, admin, "GET", path+"/conversations/bad/messages", nil, 400)
	expect(t, admin, "GET", path+"/decoder-health", nil, 200)
	// Workspace is verified server-side even for a superadmin.
	request, _ := http.NewRequest("GET", server.URL+"/api/v1"+path+"/conversations", nil)
	request.Header.Set("X-Workspace", "maibot")
	response, err := admin.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("workspace scope bypass")
	}
	expect(t, admin, "POST", "/users", map[string]string{"username": "message-tenant", "password": password, "tenant_name": "message-tenant"}, 201)
	jar, _ = cookiejar.New(nil)
	tenant := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	expect(t, tenant, "POST", "/auth/login", map[string]string{"username": "message-tenant", "password": password}, 200)
	for _, suffix := range []string{"/conversations", "/decoder-health", "/conversations/" + key + "/messages",
		"/conversations/" + key + "/sending", "/conversations/" + key + "/text-operations?key=synthetic-request"} {
		expect(t, tenant, "GET", path+suffix, nil, 403)
	}
	expect(t, tenant, "POST", path+"/conversations/"+key+"/text-operations", map[string]any{"text": "forbidden"}, 403)
	for _, table := range []string{"native_decoded_events", "native_directory_snapshots"} {
		var n int
		if err = db.Tenant.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err == nil {
			t.Fatal("tenant database role can read shared native projections")
		}
	}
	exerciseConversationMedia(t, s, admin, tenant, id, other, key, event)
	exerciseGroupAvatar(t, s, admin, tenant, id, other, key, event)
	exerciseAccountAvatar(t, s, admin, tenant, id, other)
	_, err = db.Platform.Exec(ctx, `DELETE FROM native_received_events WHERE id=$1`, event)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.Platform.QueryRow(ctx, `SELECT count(*) FROM native_decoded_events WHERE event_id=$1`, event).Scan(&n); err != nil || n != 0 {
		t.Fatal("retention orphaned a plaintext projection")
	}
	// The avatar revocation test changed the account version during download.
	expect(t, admin, "DELETE", path, map[string]any{"version": 2, "confirm_name": "message-one"}, 200)
	if err = db.Platform.QueryRow(ctx, `SELECT count(*) FROM native_directory_snapshots WHERE account_id=$1`, id).Scan(&n); err != nil || n != 0 {
		t.Fatal("account removal retained native directory")
	}
}

func TestConversationTextLimits(t *testing.T) {
	if text, truncated := boundedText("汉字abc", 2); text != "汉字" || !truncated {
		t.Fatal("UTF-8 preview limit")
	}
	if text, truncated := boundedText("ab", 2); text != "ab" || truncated {
		t.Fatal("exact preview limit")
	}
}
