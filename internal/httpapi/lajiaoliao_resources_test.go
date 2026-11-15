package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresChiliResourceScopeRetentionAndLateEpoch(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{83}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	actor, account, other := control.NewID(), control.NewID(), control.NewID()
	token, _ := security.Token()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'chili-resource','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,now()+interval '1 hour')`, security.Digest(token), security.Digest("csrf"), actor)
	for _, id := range []string{account, other} {
		exec(`INSERT INTO accounts(id,name,workspace,platform) VALUES($1,'chili-resource-account','lajiaoliao','lajiaoliao')`, id)
	}
	var picture bytes.Buffer
	png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 3)))
	content, _ := json.Marshal(map[string]any{"sourcePicture": map[string]any{
		"url":   "https://lj-im-chat-01.lajiaoliao.com/synthetic?signature=hidden",
		"width": 2, "height": 3, "size": picture.Len()}})
	raw, _ := proto.Marshal(&sdkws.MsgData{SendID: "sender", RecvID: "group", GroupID: "group",
		ClientMsgID: "message", SessionType: 3, MsgFrom: 100, ContentType: 102, Content: content})
	source, _ := vault.Seal("native-event:"+account+":source-key", raw)
	var event int64
	if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,payload_sealed,received_at)
		VALUES($1,'source-key','group',$2,now()) RETURNING id`, account, source).Scan(&event); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("d", 64)
	selected := gateway.DecodedMessage{Platform: "lajiaoliao", NativeSender: "sender", NativeTarget: "group",
		TransportPeer: "sender", TransportTo: "group"}
	selected.Message.ClientID, selected.Message.Session = "message", 2
	plain, _ := json.Marshal(selected)
	projection, _ := vault.Seal(gateway.DecodedAAD(account, event), plain)
	exec(`INSERT INTO native_decoded_events(event_id,account_id,state,reason,scene,conversation_key,
		deployment_version,payload_sealed,received_at)
		VALUES($1,$2,'unsupported','lajiaoliao_content_type','group',$3,0,$4,now())`, event, account, key, projection)
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(server.URL)
	jar.SetCookies(origin, []*http.Cookie{{Name: "wsl_session", Value: token}})
	client := testClient{client: &http.Client{Jar: jar}, base: server.URL, workspace: "lajiaoliao"}
	requests := 0
	var during func()
	s.chiliMediaClient = &http.Client{Transport: projectionMediaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "lj-im-chat-01.lajiaoliao.com" || r.Header.Get("Cookie") != "" ||
			r.Header.Get("token") != "" || r.Header.Get("Authorization") != "" {
			t.Error("resource scope or credentials changed")
		}
		if during != nil {
			during()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{},
			Body: io.NopCloser(bytes.NewReader(picture.Bytes())), Request: r}, nil
	})}
	path := "/accounts/" + account + "/conversations/" + key + "/messages/" + strconv.FormatInt(event, 10) + "/media/node-0-image"
	body := expect(t, client, "GET", "/accounts/"+account+"/conversations/"+key+"/messages", nil, 200)
	if !bytes.Contains(body, []byte(path)) || bytes.Contains(body, []byte("hidden")) || bytes.Contains(body, []byte("https://")) {
		t.Fatal("scoped image link missing or source leaked")
	}
	assertNativeImage(t, client, path, picture.Bytes())
	calls := requests
	for _, bad := range []string{strings.Replace(path, account, other, 1), strings.Replace(path, key, strings.Repeat("e", 64), 1),
		strings.TrimSuffix(path, "node-0-image") + "node-01-image"} {
		expect(t, client, "GET", bad, nil, 404)
	}
	expect(t, client, "GET", path+"?url=https://127.0.0.1", nil, 400)
	wrong := client
	wrong.workspace = "wsl"
	expect(t, wrong, "GET", path, nil, 404)
	if requests != calls {
		t.Fatal("invalid scope fetched media")
	}
	s.mediaSlots <- struct{}{}
	s.mediaSlots <- struct{}{}
	expect(t, client, "GET", path, nil, 429)
	<-s.mediaSlots
	<-s.mediaSlots
	during = func() { exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account) }
	expect(t, client, "GET", path, nil, 409)
	during = func() {
		exec(`UPDATE native_received_events SET received_at=now()-interval '8 days' WHERE id=$1`, event)
	}
	expect(t, client, "GET", path, nil, 409)
	during = nil
	calls = requests
	expect(t, client, "GET", path, nil, 404)
	if requests != calls {
		t.Fatal("expired source fetched")
	}
	var untouched bool
	if err := db.Platform.QueryRow(ctx, `SELECT state='unsupported' AND reason='lajiaoliao_content_type'
		FROM native_decoded_events WHERE event_id=$1`, event).Scan(&untouched); err != nil || !untouched {
		t.Fatal("preview rewrote decoder history", err)
	}
	credential := []byte("synthetic-avatar-session")
	exec(`UPDATE accounts SET native_uid='self-string',auth_state='authenticated',credential_sealed=$2 WHERE id=$1`, account, credential)
	var epoch, version int64
	if err := db.Platform.QueryRow(ctx, `SELECT epoch,version FROM accounts WHERE id=$1`, account).Scan(&epoch, &version); err != nil {
		t.Fatal(err)
	}
	view := gateway.SettingSnapshot{SelfID: "self-string", AccountEpoch: epoch, AccountVersion: version,
		CredentialDigest: sha256.Sum256(credential), AvatarResource: "https://lj-im-chat-01.lajiaoliao.com/avatar",
		Settings: &nativeauth.AccountSettings{UID: "self-string", Avatar: "configured"}}
	avatar, err := s.nativeAccountAvatarURL(account, actor, "lajiaoliao", view)
	if err != nil {
		t.Fatal(err)
	}
	avatar = strings.TrimPrefix(avatar, "/api/v1")
	assertNativeImage(t, client, avatar, picture.Bytes())
	calls = requests
	expect(t, client, "GET", strings.Replace(avatar, account, other, 1), nil, 404)
	expect(t, wrong, "GET", avatar, nil, 404)
	expect(t, client, "GET", avatar+"?url=https://127.0.0.1", nil, 400)
	if calls != requests {
		t.Fatal("invalid account avatar scope fetched a resource")
	}
	during = func() { exec(`UPDATE accounts SET version=version+1 WHERE id=$1`, account) }
	expect(t, client, "GET", avatar, nil, 409)
	during = nil
	calls = requests
	expect(t, client, "GET", avatar, nil, 409)
	if calls != requests {
		t.Fatal("stale account avatar fetched a resource")
	}
}
