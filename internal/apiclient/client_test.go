package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPermissionsAreServerSideAndSessionIsPrivate(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "wsl_session", Value: strings.Repeat("a", 43), Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "wsl_csrf", Value: strings.Repeat("b", 43), Path: "/"})
			w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
			return
		}
		if r.Header.Get("X-CSRF-Token") != strings.Repeat("b", 43) {
			t.Error("CLI dropped CSRF token")
		}
		w.WriteHeader(403)
		w.Write([]byte(`{"code":403,"msg":"superadmin_required","data":null}`))
	}))
	defer server.Close()
	c, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Login("user", "private-password-not-in-output"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.json")
	if err = c.Save(path); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal("session permissions are not private")
	}
	next, _ := New(server.URL)
	if err = next.Load(path); err != nil {
		t.Fatal(err)
	}
	if _, err = next.Do("GET", "/test-windows", nil); err == nil || !strings.Contains(err.Error(), "superadmin_required") {
		t.Fatal("CLI bypassed server role validation")
	}
	if _, err = next.DoOperation(json.RawMessage(`{}`), "stable-key"); err == nil {
		t.Fatal("CLI converted denied action into success")
	}
	if calls != 3 {
		t.Fatal(calls)
	}
	other, _ := New("http://127.0.0.1:1")
	if err = other.Load(path); err == nil {
		t.Fatal("session reused against another origin")
	}
}

func TestTransportAndSessionGuards(t *testing.T) {
	for _, base := range []string{"http://example.com", "https://name:secret@example.com", "https://example.com/path", "https://example.com?x=1"} {
		if _, err := New(base); err == nil {
			t.Fatal("unsafe base accepted", base)
		}
	}
	c, _ := New("http://127.0.0.1")
	for _, path := range []string{"//evil", "/../x", "/tests?tenant_id=x"} {
		_, err := c.Do("GET", path, nil)
		if err == nil {
			t.Fatal("unsafe path accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "session")
	session, _ := json.Marshal(Session{Base: "http://127.0.0.1", Token: strings.Repeat("a", 43),
		CSRF: strings.Repeat("b", 43), Expires: time.Now().Add(time.Hour)})
	if err := os.WriteFile(path, session, 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.Load(path); err == nil {
		t.Fatal("world-readable session accepted")
	}
}
