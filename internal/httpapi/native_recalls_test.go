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
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func TestPostgresRecallReadScopesAndRetainsOriginal(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-recall-password"
	hash, _ := security.Password(password)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
		VALUES('recall-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{62}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "recall-admin", "password": password}, 200)
	account := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "recall-one"}, 201), "id")
	other := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "recall-two"}, 201), "id")
	key := strings.Repeat("a", 64)
	var event int64
	if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,payload_sealed,received_at)
		VALUES($1,'original','group',$2,now()) RETURNING id`, account, []byte("synthetic")).Scan(&event); err != nil {
		t.Fatal(err)
	}
	selected := gateway.DecodedMessage{TransportTo: "team", TransportPeer: "sender", Message: nativewire.ApplicationMessage{
		ClientID: "original-client", Text: "original-retained", Target: nativewire.MessageSource{ID: 123}}}
	plain, _ := json.Marshal(selected)
	sealed, _ := vault.Seal(gateway.DecodedAAD(account, event), plain)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
		deployment_version,payload_sealed,received_at) VALUES($1,$2,'decoded','group',$3,1,$4,now())`, event, account, key, sealed); err != nil {
		t.Fatal(err)
	}
	insert := func(owner, eventKey, target string) {
		t.Helper()
		props, _ := nativewire.Properties([]nativewire.Property{
			{Key: 0, Value: []byte("1700000000000")}, {Key: 1, Value: []byte("8")},
			{Key: 2, Value: []byte(target)}, {Key: 3, Value: []byte("sender")}, {Key: 10, Value: []byte("original-client")},
			{Key: 11, Value: []byte("99")}, {Key: 16, Value: []byte("actor")}})
		frame, _ := json.Marshal(nativewire.Frame{Service: 7, Command: 14, Code: 200, Body: props})
		payload, _ := vault.Seal("native-event:"+owner+":"+eventKey, frame)
		if _, err := db.Platform.Exec(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,payload_sealed,received_at)
			VALUES($1,$2,'notice',$3,now())`, owner, eventKey, payload); err != nil {
			t.Fatal(err)
		}
	}
	insert(account, "own-notice", "team")
	insert(account, "other-group", "foreign-team")
	insert(other, "other-account", "team")
	path := "/accounts/" + account + "/conversations/" + key
	body := expect(t, admin, "GET", path+"/recalls", nil, 200)
	var result struct {
		Items []map[string]any `json:"items"`
	}
	json.Unmarshal(body, &result)
	if len(result.Items) != 1 || !bytes.Contains(body, []byte("original-client")) ||
		bytes.Contains(body, []byte("foreign-team")) || bytes.Contains(body, []byte("other-account")) {
		t.Fatal("recall read crossed scope")
	}
	body = expect(t, admin, "GET", path+"/messages", nil, 200)
	if !bytes.Contains(body, []byte("original-retained")) {
		t.Fatal("notification mutated message history")
	}
	expect(t, admin, "GET", "/accounts/"+other+"/conversations/"+key+"/recalls", nil, 404)
	expect(t, admin, "GET", path+"/recalls?before=bad", nil, 400)
	expect(t, admin, "GET", "/accounts/"+account+"/conversations/bad/recalls", nil, 400)
	expect(t, admin, "POST", path+"/recalls", map[string]any{}, 405)

	admin.workspace = "lajiaoliao"
	chili := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "chili-recall"}, 201), "id")
	if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,payload_sealed,received_at)
		VALUES($1,'chili-original','group',$2,now()) RETURNING id`, chili, []byte("synthetic")).Scan(&event); err != nil {
		t.Fatal(err)
	}
	selected.NativeTarget = "chili-group"
	plain, _ = json.Marshal(selected)
	sealed, _ = vault.Seal(gateway.DecodedAAD(chili, event), plain)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
		deployment_version,payload_sealed,received_at) VALUES($1,$2,'decoded','group',$3,1,$4,now())`, event, chili, key, sealed); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"chili-group", "foreign-group"} {
		content, _ := json.Marshal(map[string]string{"detail": `{"revokerUserID":"actor","clientMsgID":"original-client",
			"revokeTime":1700000000,"sesstionType":3,"seq":7,"conversationID":"sg_` + group + `","isAdminRevoke":true}`})
		raw, _ := proto.Marshal(&sdkws.MsgData{SendID: "actor", RecvID: "self", GroupID: group,
			SessionType: 3, MsgFrom: 200, ContentType: 2101, Content: content})
		payload, _ := vault.Seal("native-event:"+chili+":"+group, raw)
		if _, err := db.Platform.Exec(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,payload_sealed,received_at)
			VALUES($1,$2,'group',$3,now())`, chili, group, payload); err != nil {
			t.Fatal(err)
		}
	}
	body = expect(t, admin, "GET", "/accounts/"+chili+"/conversations/"+key+"/recalls", nil, 200)
	if json.Unmarshal(body, &result) != nil || len(result.Items) != 1 ||
		!bytes.Contains(body, []byte("original-client")) || bytes.Contains(body, []byte("foreign-group")) {
		t.Fatal("Chili recall read lost scope")
	}
	expect(t, admin, "GET", path+"/recalls", nil, 404)
}

func TestRecallRoutingNeverUsesActorAsGroupPermission(t *testing.T) {
	selected := gateway.DecodedMessage{NativeTarget: "group", TransportTo: "team", TransportPeer: "peer"}
	for _, row := range []struct {
		platform string
		scene    string
		notice   nativewire.RecallNotice
		want     bool
	}{
		{"wsl", "group", nativewire.RecallNotice{Scene: 1, Target: "team", Actor: "unknown"}, true},
		{"wsl", "group", nativewire.RecallNotice{Scene: 1, Target: "other", Actor: "owner"}, false},
		{"lajiaoliao", "group", nativewire.RecallNotice{Scene: 1, Target: "group"}, true},
		{"lajiaoliao", "group", nativewire.RecallNotice{Scene: 1, Target: "team"}, false},
		{"wsl", "private", nativewire.RecallNotice{Target: "team", Sender: "peer"}, true},
		{"wsl", "private", nativewire.RecallNotice{Target: "other", Sender: "peer"}, false},
	} {
		if recallMatches(selected, row.scene, row.platform, row.notice) != row.want {
			t.Fatal("scope mismatch", row.platform, row.scene)
		}
	}
}
