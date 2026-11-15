package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

func exerciseGroupAvatar(t *testing.T, s *Server, admin, tenant testClient, id, other, key string, event int64) {
	t.Helper()
	ctx := context.Background()
	group := nativeauth.DirectoryGroup{ID: "123", Team: "456", Name: "synthetic-directory", Avatar: "9007199254740993"}
	saveDirectory := func(group nativeauth.DirectoryGroup) {
		t.Helper()
		raw, _ := json.Marshal([]nativeauth.DirectoryGroup{group})
		sealed, err := s.Vault.Seal("native-directory:"+id, raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.DB.Platform.Exec(ctx, `UPDATE native_directory_snapshots SET payload_sealed=$2,sampled_at=now()
            WHERE account_id=$1`, id, sealed); err != nil {
			t.Fatal(err)
		}
	}
	saveDirectory(group)
	route := strings.TrimPrefix(nativeGroupAvatarURL(id, group), "/api/v1")
	body := expect(t, admin, "GET", "/accounts/"+id+"/conversations", nil, 200)
	if !bytes.Contains(body, []byte(route)) {
		t.Fatal("directory without retained messages lost group avatar")
	}
	// Directory metadata overrides a missing message target avatar only after
	// matching both business and transport group identity.
	if _, err := s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET received_at=now() WHERE event_id=$1`, event); err != nil {
		t.Fatal(err)
	}
	body = expect(t, admin, "GET", "/accounts/"+id+"/conversations", nil, 200)
	var listing struct {
		Items []nativeConversation `json:"items"`
	}
	if err := json.Unmarshal(body, &listing); err != nil || len(listing.Items) != 1 ||
		listing.Items[0].AvatarURL != "/api/v1"+route {
		t.Fatal("group conversation did not use its directory avatar")
	}
	var sealed []byte
	if err := s.DB.Platform.QueryRow(ctx, `SELECT payload_sealed FROM native_decoded_events WHERE event_id=$1`, event).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, _ := s.Vault.Open(gateway.DecodedAAD(id, event), sealed)
	var projection gateway.DecodedMessage
	if err := json.Unmarshal(raw, &projection); err != nil {
		t.Fatal(err)
	}
	clear(raw)
	projection.TransportTo = "999"
	raw, _ = json.Marshal(projection)
	mismatch, _ := s.Vault.Seal(gateway.DecodedAAD(id, event), raw)
	if _, err := s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET payload_sealed=$2 WHERE event_id=$1`, event, mismatch); err != nil {
		t.Fatal(err)
	}
	body = expect(t, admin, "GET", "/accounts/"+id+"/conversations", nil, 200)
	listing.Items = nil
	if err := json.Unmarshal(body, &listing); err != nil || len(listing.Items) != 1 ||
		listing.Items[0].AvatarURL == "/api/v1"+route {
		t.Fatal("directory avatar crossed transport group mapping")
	}
	var bitmap bytes.Buffer
	if err := png.Encode(&bitmap, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	requests := 0
	var during func()
	s.mediaClient = &http.Client{Transport: projectionMediaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != "https://images-03003.hfjtqy.com/assets/9007199254740993.jpg" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("group avatar target or credential boundary changed")
		}
		if during != nil {
			during()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(bitmap.Bytes())), Request: r}, nil
	})}
	assertNativeImage(t, admin, route, bitmap.Bytes())
	start := requests
	expect(t, tenant, "GET", route, nil, 403)
	expect(t, admin, "GET", strings.Replace(route, id, other, 1), nil, 404)
	expect(t, admin, "GET", strings.Replace(route, "/123/", "/124/", 1), nil, 404)
	expect(t, admin, "GET", route+"?url=https://127.0.0.1", nil, 400)
	expect(t, admin, "GET", strings.TrimSuffix(route, groupAvatarReference(group))+strings.Repeat("a", 64), nil, 404)
	wrongWorkspace := admin
	wrongWorkspace.workspace = "maibot"
	expect(t, wrongWorkspace, "GET", route, nil, 404)
	if requests != start {
		t.Fatal("invalid group avatar scope reached upstream")
	}
	s.mediaSlots <- struct{}{}
	s.mediaSlots <- struct{}{}
	expect(t, admin, "GET", route, nil, 429)
	<-s.mediaSlots
	<-s.mediaSlots
	during = func() {
		changed := group
		changed.Avatar = "987"
		saveDirectory(changed)
	}
	expect(t, admin, "GET", route, nil, 409)
	during = nil
	start = requests
	expect(t, admin, "GET", route, nil, 404)
	if requests != start {
		t.Fatal("old directory avatar reference was reused")
	}
	saveDirectory(group)
	during = func() {
		if _, err := s.DB.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, id); err != nil {
			t.Error(err)
		}
	}
	expect(t, admin, "GET", route, nil, 409)
	during = func() {
		if _, err := s.DB.Platform.Exec(ctx, `UPDATE users SET disabled=true WHERE username='message-admin'`); err != nil {
			t.Error(err)
		}
	}
	expect(t, admin, "GET", route, nil, 403)
	if _, err := s.DB.Platform.Exec(ctx, `UPDATE users SET disabled=false WHERE username='message-admin'`); err != nil {
		t.Fatal(err)
	}
	during = nil
	reloginMessageFixture(t, admin)
	if _, err := s.DB.Platform.Exec(ctx, `UPDATE native_directory_snapshots SET sampled_at=now()-interval '8 days' WHERE account_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	start = requests
	expect(t, admin, "GET", route, nil, 404)
	if requests != start {
		t.Fatal("expired directory avatar reached upstream")
	}
}

func TestGroupAvatarReferenceBoundaries(t *testing.T) {
	base := nativeauth.DirectoryGroup{ID: "123", Team: "456", Avatar: "9007199254740993"}
	want := groupAvatarReference(base)
	base.Avatar += ".jpg"
	if want == "" || groupAvatarReference(base) != want {
		t.Fatal("canonical avatar suffix changed identity")
	}
	base.Team = "457"
	if groupAvatarReference(base) == want {
		t.Fatal("avatar reference did not bind transport group")
	}
	for _, avatar := range []string{"", "0", "01", "../123", "https://example.com/a"} {
		base.Avatar = avatar
		if nativeGroupAvatarURL("account", base) != "" {
			t.Fatal("unsafe avatar created a download link")
		}
	}
}
