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
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestAccountAvatarReferenceScopeAndExpiry(t *testing.T) {
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{41}, 32)))
	s := &Server{Vault: vault}
	view := gateway.SettingSnapshot{SelfID: "7", AccountEpoch: 1, AccountVersion: 1, DeploymentVersion: 1,
		Settings: &nativeauth.AccountSettings{UID: "7", Avatar: "9007199254740993.jpg"}}
	url, err := s.nativeAccountAvatarURL("account", "actor", "wsl", view)
	if err != nil || url == "" {
		t.Fatal("current avatar did not receive a scoped resource", err)
	}
	token := url[strings.LastIndex(url, "/")+1:]
	ref, err := s.openAccountAvatar("account", "actor", "wsl", token)
	if err != nil || ref.Asset != view.Settings.Avatar || ref.UID != "7" {
		t.Fatal("valid avatar reference lost its exact asset", err)
	}
	for _, scope := range [][3]string{{"other", "actor", "wsl"}, {"account", "other", "wsl"}, {"account", "actor", "maibot"}} {
		if _, err := s.openAccountAvatar(scope[0], scope[1], scope[2], token); err == nil {
			t.Fatal("avatar reference escaped account, actor or workspace")
		}
	}
	for _, invalid := range []string{"bad", strings.Repeat("a", 1025), strings.Repeat("a", len(token))} {
		if _, err := s.openAccountAvatar("account", "actor", "wsl", invalid); err == nil {
			t.Fatal("malformed avatar reference accepted")
		}
	}
	for _, expires := range []int64{time.Now().Add(-time.Second).Unix(), time.Now().Add(2 * accountAvatarLifetime).Unix()} {
		ref.Expires = expires
		plain, _ := json.Marshal(ref)
		sealed, _ := vault.Seal(accountAvatarScope("account", "actor", "wsl"), plain)
		if _, err := s.openAccountAvatar("account", "actor", "wsl", base64.RawURLEncoding.EncodeToString(sealed)); err == nil {
			t.Fatal("expired or excessive lifetime accepted")
		}
	}
	for _, asset := range []string{"", "https://example.com/avatar", "../123", "0", "123.png"} {
		view.Settings.Avatar = asset
		if url, err := s.nativeAccountAvatarURL("account", "actor", "wsl", view); url != "" || err != nil {
			t.Fatal("unsupported asset became a downloadable resource")
		}
	}
	view.Settings = nil
	if url, _ := s.nativeAccountAvatarURL("account", "actor", "wsl", view); url != "" {
		t.Fatal("group member settings generated a personal avatar")
	}
}

func TestChiliAccountAvatarReferenceKeepsStringIdentityAndPrivateURL(t *testing.T) {
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32)))
	s := &Server{Vault: vault}
	view := gateway.SettingSnapshot{Platform: "lajiaoliao", SelfID: "self-string", AccountEpoch: 1, AccountVersion: 2,
		AvatarResource: "https://lj-im-chat-01.lajiaoliao.com/avatar?signature=private",
		Settings:       &nativeauth.AccountSettings{UID: "self-string", Avatar: "configured"}}
	url, err := s.nativeAccountAvatarURL("account", "actor", "lajiaoliao", view)
	if err != nil || url == "" || strings.Contains(url, "private") || strings.Contains(url, "https") {
		t.Fatal("avatar URL escaped sealing", err)
	}
	encoded := url[strings.LastIndex(url, "/")+1:]
	ref, err := s.openAccountAvatar("account", "actor", "lajiaoliao", encoded)
	if err != nil || ref.Asset != view.AvatarResource || ref.UID != "self-string" || ref.Deployment != 0 {
		t.Fatal("Chili identity or deployment was coerced", err)
	}
	raw, _ := json.Marshal(view)
	if bytes.Contains(raw, []byte("private")) || bytes.Contains(raw, []byte("https://")) {
		t.Fatal("native avatar credential leaked from snapshot")
	}
	for _, scope := range [][3]string{{"other", "actor", "lajiaoliao"}, {"account", "other", "lajiaoliao"}, {"account", "actor", "wsl"}} {
		if _, err := s.openAccountAvatar(scope[0], scope[1], scope[2], encoded); err == nil {
			t.Fatal("Chili avatar escaped scope")
		}
	}
	for _, bad := range []string{"https://127.0.0.1/avatar", "https://evil.example/avatar", "", strings.Repeat("a", 2049)} {
		view.AvatarResource = bad
		if url, err := s.nativeAccountAvatarURL("account", "actor", "lajiaoliao", view); url != "" || err != nil {
			t.Fatal("unverified avatar origin became a resource")
		}
	}
}

func exerciseAccountAvatar(t *testing.T, s *Server, admin, tenant testClient, account, other string) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.DB.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	credential := []byte("synthetic-avatar-credential")
	exec(`UPDATE accounts SET auth_state='authenticated',native_uid='7',credential_sealed=$2 WHERE id=$1`, account, credential)
	var actor string
	var epoch, version int64
	if err := s.DB.Platform.QueryRow(ctx, `SELECT id::text FROM users WHERE username='message-admin'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Platform.QueryRow(ctx, `SELECT epoch,version FROM accounts WHERE id=$1`, account).Scan(&epoch, &version); err != nil {
		t.Fatal(err)
	}
	view := gateway.SettingSnapshot{SelfID: "7", AccountEpoch: epoch, AccountVersion: version, DeploymentVersion: 1,
		CredentialDigest: sha256.Sum256(credential), Settings: &nativeauth.AccountSettings{UID: "7", Avatar: "9007199254740993.jpg"}}
	fresh := func() string {
		t.Helper()
		url, err := s.nativeAccountAvatarURL(account, actor, "wsl", view)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimPrefix(url, "/api/v1")
	}
	route := fresh()
	var bitmap bytes.Buffer
	if err := png.Encode(&bitmap, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	requests := 0
	var during func()
	s.mediaClient = &http.Client{Transport: projectionMediaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != "https://images-03003.hfjtqy.com/assets/9007199254740993.jpg" ||
			r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("avatar download target changed or credentials leaked")
		}
		if during != nil {
			during()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r,
			Body: io.NopCloser(bytes.NewReader(bitmap.Bytes()))}, nil
	})}
	assertNativeImage(t, admin, route, bitmap.Bytes())
	start := requests
	expect(t, tenant, "GET", route, nil, 403)
	expect(t, admin, "GET", strings.Replace(route, account, other, 1), nil, 404)
	expect(t, admin, "GET", route+"?url=https://127.0.0.1", nil, 400)
	wrongWorkspace := admin
	wrongWorkspace.workspace = "maibot"
	expect(t, wrongWorkspace, "GET", route, nil, 404)
	if requests != start {
		t.Fatal("invalid scope downloaded an avatar")
	}
	s.mediaSlots <- struct{}{}
	s.mediaSlots <- struct{}{}
	expect(t, admin, "GET", route, nil, 429)
	<-s.mediaSlots
	<-s.mediaSlots
	during = func() { exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account) }
	expect(t, admin, "GET", route, nil, 403)
	during = nil
	start = requests
	expect(t, admin, "GET", route, nil, 403)
	if requests != start {
		t.Fatal("old epoch downloaded an avatar")
	}
	view.AccountEpoch++
	route = fresh()
	exec(`UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, account, []byte("different-synthetic-session"))
	expect(t, admin, "GET", route, nil, 409)
	exec(`UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, account, credential)
	during = func() { exec(`UPDATE accounts SET version=version+1 WHERE id=$1`, account) }
	expect(t, admin, "GET", route, nil, 409)
	during = nil
	view.AccountVersion++
	route = fresh()
	during = func() { exec(`UPDATE users SET disabled=true WHERE id=$1`, actor) }
	expect(t, admin, "GET", route, nil, 403)
	during = nil
	exec(`UPDATE users SET disabled=false WHERE id=$1`, actor)
	reloginMessageFixture(t, admin)
	during = func() { exec(`UPDATE native_deployment SET version=version+1`) }
	expect(t, admin, "GET", route, nil, 403)
	during = nil
}
