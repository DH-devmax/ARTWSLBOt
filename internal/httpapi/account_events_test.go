package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/security"
)

func TestPostgresAccountEventsIsolationAndRevocation(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-live-password"
	hash, _ := security.Password(password)
	_, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role) VALUES('live-admin',$1,'superadmin')`, hash)
	if err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{27}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar, Timeout: 12 * time.Second}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "live-admin", "password": password}, 200)
	account := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "live-one"}, 201), "id")
	other := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "live-other"}, 201), "id")
	path := "/accounts/" + account + "/events"
	expect(t, admin, "GET", path+"?view=invalid", nil, 400)
	chat := admin
	chat.workspace = "maibot"
	expect(t, chat, "GET", path+"?view=messages", nil, 404)
	open := func(view string) *http.Response {
		t.Helper()
		response, err := admin.client.Get(server.URL + "/api/v1" + path + "?view=" + view)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			response.Body.Close()
			t.Fatal("not a live stream", response.StatusCode)
		}
		return response
	}
	read := func(scanner *bufio.Scanner) (string, map[string]json.RawMessage) {
		t.Helper()
		kind, raw := "", ""
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" && raw != "" {
				var data map[string]json.RawMessage
				if err := json.Unmarshal([]byte(raw), &data); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(raw, "payload_sealed") || strings.Contains(raw, "credential") {
					t.Fatal("unsafe stream projection")
				}
				return kind, data
			}
			if strings.HasPrefix(line, "event: ") {
				kind = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				raw = strings.TrimPrefix(line, "data: ")
			}
		}
		t.Fatal("stream ended early", scanner.Err())
		return "", nil
	}
	response := open("messages")
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 128*1024)
	kind, first := read(scanner)
	if kind != "update" || string(first["reset"]) != "true" || string(first["account_id"]) != `"`+account+`"` {
		t.Fatal("missing explicit initial resync")
	}
	s.Changes.Publish(gateway.Change{Account: other, Kind: "decoded", EventID: 999})
	s.Changes.Publish(gateway.Change{Account: account, Kind: "decoded", EventID: 9})
	s.Changes.Publish(gateway.Change{Account: account, Kind: "decoded", EventID: 1})
	_, update := read(scanner)
	var ids []string
	json.Unmarshal(update["decoded_ids"], &ids)
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["9"] || !found["1"] || found["999"] {
		t.Fatal("account filter or late lower event ID lost", ids)
	}
	second := open("dashboard")
	defer second.Body.Close()
	expect(t, admin, "GET", path+"?view=dashboard", nil, 429)
	for i := 1; i <= 200; i++ {
		s.Changes.Publish(gateway.Change{Account: account, Kind: "decoded", EventID: int64(i)})
	}
	_, overflow := read(scanner)
	if string(overflow["reset"]) != "true" {
		t.Fatal("overflow was silently skipped")
	}
	second.Body.Close()
	u, _ := url.Parse(server.URL)
	for _, cookie := range jar.Cookies(u) {
		if cookie.Name == "wsl_session" {
			if _, err = db.Platform.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, security.Digest(cookie.Value)); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.Changes.Publish(gateway.Change{Account: account, Kind: "receiver"})
	kind, revoked := read(scanner)
	if kind != "error" || len(revoked["decoded_ids"]) != 0 || len(revoked["receiver"]) != 0 {
		t.Fatal("revoked stream delivered resource data")
	}
	response.Body.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.liveMu.Lock()
		n := len(s.liveActors)
		s.liveMu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stream slots leaked")
}
