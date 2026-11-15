package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCLITwoStepLoginAndExistingAuthenticatedTools(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.json")
	statePath := sessionPath + ".challenge.json"
	oldSession := []byte("existing authenticated session is preserved until login succeeds")
	if err := os.WriteFile(sessionPath, oldSession, 0600); err != nil {
		t.Fatal(err)
	}
	human, rotated := strings.Repeat("h", 43), strings.Repeat("r", 43)
	challenge, ticket := strings.Repeat("c", 43), strings.Repeat("t", 43)
	session, csrf := strings.Repeat("s", 43), strings.Repeat("f", 43)
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/captcha/challenge":
			var in map[string]string
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&in) != nil ||
				len(in) != 1 || in["username"] != "synthetic" {
				t.Error("challenge request widened scope")
			}
			http.SetCookie(w, &http.Cookie{Name: "wsl_human", Value: human, Path: "/api/v1", HttpOnly: true})
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"challenge_id": challenge, "expires_at": time.Now().Add(time.Minute),
				"answer": "server-only-answer", "x": 137, "y": 61,
				"image": map[string]any{
					"background": "data:image/png;base64,synthetic-background", "tile": "data:image/png;base64,synthetic-tile",
					"width": 300, "height": 220, "tile_width": 50, "tile_height": 50,
					"initial_x": 0, "initial_y": 0, "answer": "nested-server-only-answer",
				},
			}})
		case "/api/v1/auth/captcha/verify":
			if _, err := os.Lstat(statePath); !errors.Is(err, os.ErrNotExist) {
				t.Error("challenge state remains at verification", err)
			}
			proof, err := r.Cookie("wsl_human")
			if err != nil || proof.Value != human {
				t.Error("challenge browser cookie was not restored at /api/v1")
			}
			var in map[string]any
			if json.NewDecoder(r.Body).Decode(&in) != nil || len(in) != 4 ||
				in["username"] != "synthetic" || in["challenge_id"] != challenge ||
				in["x"] != float64(0) || in["y"] != float64(61) {
				t.Error("human coordinates or verification scope changed")
			}
			http.SetCookie(w, &http.Cookie{Name: "wsl_human", Value: rotated, Path: "/api/v1", HttpOnly: true})
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"ticket": ticket, "expires_at": time.Now().Add(time.Minute),
			}})
		case "/api/v1/auth/login":
			proof, err := r.Cookie("wsl_human")
			if err != nil || proof.Value != rotated {
				t.Error("verify and login did not share a cookie jar")
			}
			var in map[string]string
			if json.NewDecoder(r.Body).Decode(&in) != nil || len(in) != 3 ||
				in["username"] != "synthetic" || in["password"] != "synthetic-password" || in["captcha_ticket"] != ticket {
				t.Error("login bypassed verification or changed credentials")
			}
			http.SetCookie(w, &http.Cookie{Name: "wsl_session", Value: session, Path: "/", HttpOnly: true})
			http.SetCookie(w, &http.Cookie{Name: "wsl_csrf", Value: csrf, Path: "/"})
			io.WriteString(w, `{"code":0,"data":{}}`)
		case "/api/v1/test-tools", "/api/v1/test-tools/call":
			cookie, err := r.Cookie("wsl_session")
			if err != nil || cookie.Value != session || r.Header.Get("X-CSRF-Token") != csrf {
				t.Error("existing Load or Tools session behavior changed")
			}
			if r.URL.Path == "/api/v1/test-tools/call" && (r.Method != "POST" || r.Header.Get("Idempotency-Key") != "synthetic-stable-key") {
				t.Error("Tools lost its stable key")
			}
			io.WriteString(w, `{"code":0,"data":{"synthetic":true}}`)
		default:
			t.Error("unexpected CLI request", r.URL.Path)
			w.WriteHeader(404)
			io.WriteString(w, `{"code":404,"msg":"not_found"}`)
		}
	}))
	defer server.Close()
	global := []string{"--url", server.URL, "--session", sessionPath}
	var output bytes.Buffer
	if err := runCLI(append(append([]string{}, global...), "login-challenge", "--username", "synthetic"), strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	var public map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &public); err != nil || len(public) != 3 {
		t.Fatal("challenge output is not public JSON", output.String(), err)
	}
	var image map[string]json.RawMessage
	if err := json.Unmarshal(public["image"], &image); err != nil || len(image) != 8 {
		t.Fatal("unexpected public image fields", err)
	}
	for _, secret := range []string{"server-only-answer", human, ticket, "synthetic-password"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("private value leaked in challenge output")
		}
	}
	stat, err := os.Stat(statePath)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("login state is not private", err)
	}
	b, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]json.RawMessage
	if json.Unmarshal(b, &state) != nil || len(state) != 5 {
		t.Fatal("unexpected private state fields")
	}
	for _, key := range []string{"base", "username", "challenge_id", "expires_at", "wsl_human"} {
		if _, ok := state[key]; !ok {
			t.Fatal("missing state field", key)
		}
	}
	b, err = os.ReadFile(sessionPath)
	if err != nil || !bytes.Equal(b, oldSession) {
		t.Fatal("challenge overwrote existing session", err)
	}
	output.Reset()
	login := append(append([]string{}, global...), "login", "--username", "synthetic", "--password-stdin", "--captcha-x", "0", "--captcha-y", "61")
	if err := runCLI(login, strings.NewReader("synthetic-password\r\n"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "Authenticated.") {
		t.Fatal("login did not report success")
	}
	for _, secret := range []string{"synthetic-password", human, rotated, ticket, session, csrf} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("private value leaked in login output")
		}
	}
	b, err = os.ReadFile(sessionPath)
	if err != nil || strings.Contains(string(b), rotated) || strings.Contains(string(b), "synthetic-password") ||
		strings.Contains(string(b), ticket) {
		t.Fatal("temporary login secrets persisted in session", err)
	}
	if err := runCLI(login, strings.NewReader("synthetic-password"), io.Discard); err == nil {
		t.Fatal("used challenge replayed")
	}
	for _, command := range [][]string{{"tools"}, {"tool-call", "synthetic-stable-key"}} {
		if err := runCLI(append(append([]string{}, global...), command...), strings.NewReader(`{"name":"inspect_operation","arguments":{}}`), io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"/api/v1/auth/captcha/challenge", "/api/v1/auth/captcha/verify", "/api/v1/auth/login", "/api/v1/test-tools", "/api/v1/test-tools/call"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatal("unexpected request order or retry", paths)
	}
}

func TestCLILoginRequiresCompleteHumanInput(t *testing.T) {
	for _, args := range [][]string{
		{"login", "--username", "synthetic", "--password-stdin"},
		{"login", "--username", "synthetic", "--password-stdin", "--captcha-x", "1"},
		{"login", "--username", "synthetic", "--password-stdin", "--captcha-y", "1"},
		{"login", "--username", "synthetic", "--captcha-x", "1", "--captcha-y", "1"},
		{"login", "--password-stdin", "--captcha-x", "1", "--captcha-y", "1"},
		{"login", "--username", "synthetic", "--password-stdin", "--captcha-x", "-1", "--captcha-y", "1"},
		{"login", "--username", "synthetic", "--password-stdin", "--captcha-x", "1", "--captcha-y", "1", "extra"},
		{"login-challenge"},
		{"login-challenge", "--username", "synthetic", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.json")
			state := path + ".challenge.json"
			if err := os.WriteFile(state, []byte("unconsumed"), 0600); err != nil {
				t.Fatal(err)
			}
			argv := append([]string{"--url", "http://127.0.0.1:1", "--session", path}, args...)
			err := runCLI(argv, strings.NewReader("synthetic-password"), io.Discard)
			if err == nil || strings.Contains(err.Error(), "transport") {
				t.Fatal("incomplete input reached HTTP", err)
			}
			b, err := os.ReadFile(state)
			if err != nil || string(b) != "unconsumed" {
				t.Fatal("incomplete input consumed state", err)
			}
		})
	}
}

func TestCLILoginStateIsSeparateAndPasswordIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	for _, command := range []string{"login-challenge", "login"} {
		args := []string{"--session", path, "--challenge-state", filepath.Join(filepath.Dir(path), ".", filepath.Base(path)), command}
		if err := runCLI(args, strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "separate") {
			t.Fatal("session path accepted as challenge state", err)
		}
	}
	state := path + ".challenge.json"
	if err := os.WriteFile(state, []byte("unconsumed"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--session", path, "login", "--username", "synthetic", "--password-stdin", "--captcha-x", "0", "--captcha-y", "0"}
	if err := runCLI(args, strings.NewReader(strings.Repeat("p", 257)), io.Discard); err == nil || !strings.Contains(err.Error(), "password input") {
		t.Fatal("unbounded password accepted", err)
	}
	b, err := os.ReadFile(state)
	if err != nil || string(b) != "unconsumed" {
		t.Fatal("invalid password consumed challenge", err)
	}
}

func TestCLIChallengeStateRejectsSessionDirectoryAlias(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "session.json")
	original := []byte("existing private session")
	if err := os.WriteFile(session, original, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	args := []string{"--session", session, "--challenge-state", filepath.Join(alias, "session.json"),
		"login-challenge", "--username", "synthetic"}
	if err := runCLI(args, strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "separate") {
		t.Fatal("session alias accepted as challenge state", err)
	}
	b, err := os.ReadFile(session)
	if err != nil || !bytes.Equal(b, original) {
		t.Fatal("existing session changed", err)
	}
}
