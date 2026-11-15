package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type projectionMediaTransport func(*http.Request) (*http.Response, error)

func (f projectionMediaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func exerciseConversationMedia(t *testing.T, s *Server, admin, tenant testClient, id, other, key string, event int64) {
	t.Helper()
	ctx := context.Background()
	var bitmap bytes.Buffer
	if err := png.Encode(&bitmap, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	secret := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{29}, 32))
	d := nativeauth.Deployment{Origin: "https://ifeng-api01.hfjtqy.com", ProtocolVersion: "2.8.3", DesktopPlatform: "macos",
		Metadata: make([]uint64, 14), SigningSeed: secret, BodyKey: secret, ServerKey: secret,
		CaptchaID: "synthetic", DeviceID: "synthetic-media-device"}
	raw, _ := json.Marshal(d)
	sealed, _ := s.Vault.Seal("native-deployment:v1", raw)
	if _, err := s.DB.Platform.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,1)`, sealed); err != nil {
		t.Fatal(err)
	}
	message := gateway.DecodedMessage{TransportTo: "456", Message: nativewire.ApplicationMessage{Format: 1,
		Sender: nativewire.MessageSource{ID: 7, Avatar: "9007199254740993"}, Target: nativewire.MessageSource{ID: 123},
		Image: &nativewire.MessageImage{Asset: "18446744073709551615", Width: 2, Height: 3, Size: uint64(bitmap.Len())}}}
	raw, _ = json.Marshal(message)
	sealed, _ = s.Vault.Seal(gateway.DecodedAAD(id, event), raw)
	if _, err := s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET payload_sealed=$2 WHERE event_id=$1`, event, sealed); err != nil {
		t.Fatal(err)
	}
	requests := 0
	content := bitmap.Bytes()
	expectedURL := "https://images-03003.hfjtqy.com/assets/18446744073709551615"
	var during func()
	s.mediaClient = &http.Client{Transport: projectionMediaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != expectedURL ||
			r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("resource target changed or credentials leaked")
		}
		if during != nil {
			during()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{},
			Body: io.NopCloser(bytes.NewReader(content)), Request: r}, nil
	})}
	path := "/accounts/" + id + "/conversations/" + key + "/messages/" + strconv.FormatInt(event, 10) + "/media/image"
	body := expect(t, admin, "GET", "/accounts/"+id+"/conversations/"+key+"/messages", nil, 200)
	if !bytes.Contains(body, []byte(path)) || !bytes.Contains(body, []byte("18446744073709551615")) {
		t.Fatal("scoped media references or exact string IDs missing")
	}
	request, _ := http.NewRequest("GET", admin.base+"/api/v1"+path, nil)
	response, err := admin.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/png" ||
		response.Header.Get("Cache-Control") != "no-store" || !bytes.Equal(data, bitmap.Bytes()) {
		t.Fatal("scoped binary media response invalid")
	}
	start := requests
	expect(t, tenant, "GET", path, nil, 403)
	expect(t, admin, "GET", strings.Replace(path, id, other, 1), nil, 404)
	expect(t, admin, "GET", strings.Replace(path, key, strings.Repeat("b", 64), 1), nil, 404)
	wrongWorkspace := admin
	wrongWorkspace.workspace = "maibot"
	expect(t, wrongWorkspace, "GET", path, nil, 404)
	expect(t, admin, "GET", path+"?url=https://127.0.0.1", nil, 400)
	expect(t, admin, "GET", strings.TrimSuffix(path, "image")+"entry-999-image", nil, 404)
	if requests != start {
		t.Fatal("invalid scope reached download transport")
	}
	original := sealed
	message.Message.Image.Size = nativewire.MaxImageBytes + 1
	raw, _ = json.Marshal(message)
	oversized, _ := s.Vault.Seal(gateway.DecodedAAD(id, event), raw)
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET payload_sealed=$2 WHERE event_id=$1`, event, oversized); err != nil {
		t.Fatal(err)
	}
	start = requests
	expect(t, admin, "GET", path, nil, 502)
	if requests != start {
		t.Fatal("blocked preview initiated a download")
	}
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET payload_sealed=$2 WHERE event_id=$1`, event, original); err != nil {
		t.Fatal(err)
	}
	message.Message.Format, message.Message.Image = 13, nil
	message.Message.FriendCard = &nativewire.MessageFriendCard{UserID: 88, AccountID: 123456,
		Nickname: "<script>card-subject</script>", Avatar: "9007199254740993.jpg"}
	raw, _ = json.Marshal(message)
	cardSealed, _ := s.Vault.Seal(gateway.DecodedAAD(id, event), raw)
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET payload_sealed=$2 WHERE event_id=$1`, event, cardSealed); err != nil {
		t.Fatal(err)
	}
	body = expect(t, admin, "GET", "/accounts/"+id+"/conversations/"+key+"/messages", nil, 200)
	contactPath := strings.TrimSuffix(path, "image") + "contact-avatar"
	if !bytes.Contains(body, []byte(contactPath)) || !bytes.Contains(body, []byte(`"friend_card"`)) {
		t.Fatal("scoped contact card projection missing")
	}
	expectedURL = "https://images-03003.hfjtqy.com/assets/9007199254740993.jpg"
	assertNativeImage(t, admin, contactPath, bitmap.Bytes())
	message.Message.Format, message.Message.FriendCard = 3, nil
	content = []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp42")
	message.Message.Video = &nativewire.MessageVideo{Asset: "18446744073709551615", Duration: 61, Size: uint64(len(content))}
	raw, _ = json.Marshal(message)
	videoSealed, _ := s.Vault.Seal(gateway.DecodedAAD(id, event), raw)
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET payload_sealed=$2 WHERE event_id=$1`, event, videoSealed); err != nil {
		t.Fatal(err)
	}
	body = expect(t, admin, "GET", "/accounts/"+id+"/conversations/"+key+"/messages", nil, 200)
	videoPath := strings.TrimSuffix(path, "image") + "video"
	if !bytes.Contains(body, []byte(videoPath)) || !bytes.Contains(body, []byte(`"poster"`)) {
		t.Fatal("video projection links missing")
	}
	expectedURL = "https://images-03003.hfjtqy.com/assets/18446744073709551615"
	assertNativeMedia(t, admin, videoPath, content, "video/mp4")
	during = func() {
		if _, e := s.DB.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, id); e != nil {
			t.Error(e)
		}
	}
	expect(t, admin, "GET", videoPath, nil, 409)
	during = nil
	content = bitmap.Bytes()
	expectedURL += ".jpg"
	assertNativeImage(t, admin, videoPath+"-poster", content)
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET payload_sealed=$2 WHERE event_id=$1`, event, original); err != nil {
		t.Fatal(err)
	}
	expectedURL = "https://images-03003.hfjtqy.com/assets/18446744073709551615"
	s.mediaSlots <- struct{}{}
	s.mediaSlots <- struct{}{}
	expect(t, admin, "GET", path, nil, 429)
	<-s.mediaSlots
	<-s.mediaSlots
	during = func() {
		if _, e := s.DB.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, id); e != nil {
			t.Error(e)
		}
	}
	expect(t, admin, "GET", path, nil, 409)
	during = func() {
		if _, e := s.DB.Platform.Exec(ctx, `UPDATE users SET disabled=true WHERE username='message-admin'`); e != nil {
			t.Error(e)
		}
	}
	expect(t, admin, "GET", path, nil, 403)
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE users SET disabled=false WHERE username='message-admin'`); err != nil {
		t.Fatal(err)
	}
	during = nil
	reloginMessageFixture(t, admin)
	if _, err = s.DB.Platform.Exec(ctx, `UPDATE native_decoded_events SET received_at=now()-interval '8 days' WHERE event_id=$1`, event); err != nil {
		t.Fatal(err)
	}
	start = requests
	expect(t, admin, "GET", path, nil, 404)
	if requests != start {
		t.Fatal("expired retained media was downloaded")
	}
}

func assertNativeImage(t *testing.T, client testClient, path string, expected []byte) {
	t.Helper()
	assertNativeMedia(t, client, path, expected, "image/png")
}

func assertNativeMedia(t *testing.T, client testClient, path string, expected []byte, mime string) {
	t.Helper()
	request, _ := http.NewRequest("GET", client.base+"/api/v1"+path, nil)
	if client.workspace != "" {
		request.Header.Set("X-Workspace", client.workspace)
	}
	response, err := client.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != mime ||
		response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Accel-Buffering") != "no" ||
		!bytes.Equal(data, expected) {
		t.Fatal("scoped binary image response invalid")
	}
}

func TestMediaSelectorsAndProjectionBounds(t *testing.T) {
	f := &nativewire.MessageForward{Title: strings.Repeat("title", 100), Total: 102}
	for range 102 {
		f.Entries = append(f.Entries, nativewire.MessageForwardEntry{Text: strings.Repeat("文", 3000),
			Image: &nativewire.MessageImage{Asset: "123"}, Sender: nativewire.MessageSource{Avatar: "456"}})
	}
	if !clipForward(f) || len(f.Entries) != 100 {
		t.Fatal("forward preview was not bounded")
	}
	length := 0
	for _, entry := range f.Entries {
		length += len([]rune(entry.Text))
	}
	if length > 32768 || len(previewForward(f).Entries) != 0 {
		t.Fatal("preview exposed full forwarded records")
	}
	m := nativewire.ApplicationMessage{Forward: f}
	asset, avatar, _, err := messageMedia(m, "entry-0-image")
	if err != nil || avatar || asset != "123" {
		t.Fatal("forward attachment selector mismatch")
	}
	for _, part := range []string{"entry--1-image", "entry-01-image", "entry-100-image", "https://example.com", "entry-0-unknown"} {
		if _, _, _, err = messageMedia(m, part); err == nil {
			t.Fatal("unbounded or arbitrary media selector accepted")
		}
	}
	m.FriendCard = &nativewire.MessageFriendCard{Avatar: "789.jpg"}
	asset, avatar, _, err = messageMedia(m, "contact-avatar")
	if err != nil || !avatar || asset != "789.jpg" {
		t.Fatal("contact avatar selector mismatch")
	}
	links := nativeMediaLinks("account", "conversation", 1, m)
	if !strings.HasSuffix(links.Contact, "/media/contact-avatar") {
		t.Fatal("contact avatar scoped link missing")
	}
	m.Video = &nativewire.MessageVideo{Asset: "123", Size: nativewire.MaxVideoBytes + 1}
	links = nativeMediaLinks("account", "conversation", 1, m)
	if links.Video != "" || links.Poster == "" {
		t.Fatal("large video did not keep metadata and poster only")
	}
	m.Video = &nativewire.MessageVideo{External: true}
	links = nativeMediaLinks("account", "conversation", 1, m)
	if links.Video != "" || links.Poster != "" {
		t.Fatal("external video created a resource link")
	}
	m.Forward.Entries[0].Video = &nativewire.MessageVideo{Asset: "987"}
	if messageVideo(m, "entry-0-video") == nil || messageVideo(m, "entry-00-video") != nil ||
		messageVideo(m, "entry-100-video") != nil || messageVideo(m, "image") != nil {
		t.Fatal("forward video selector escaped bounds")
	}
}
