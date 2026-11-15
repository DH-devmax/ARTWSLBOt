package apiclient

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type loginTestTransport func(*http.Request) (*http.Response, error)

func (f loginTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func loginTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func loginTestState() loginChallengeState {
	return loginChallengeState{Base: "https://cli.example.test", Username: "synthetic",
		ChallengeID: strings.Repeat("c", 43), ExpiresAt: time.Now().Add(time.Minute),
		HumanCookie: strings.Repeat("h", 43)}
}

func writeLoginTestState(t *testing.T, path string, state loginChallengeState) {
	t.Helper()
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func loginTestTicket() string {
	b, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{
		"ticket": strings.Repeat("t", 43), "expires_at": time.Now().Add(time.Minute),
	}})
	return string(b)
}

func TestLoginChallengeRejectsInvalidScopeAndUnsafeState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*loginChallengeState)
		file   string
	}{
		{name: "origin", mutate: func(s *loginChallengeState) { s.Base = "https://other.example.test" }},
		{name: "username", mutate: func(s *loginChallengeState) { s.Username = "Synthetic" }},
		{name: "expired", mutate: func(s *loginChallengeState) { s.ExpiresAt = time.Now().Add(-time.Second) }},
		{name: "zero expiry", mutate: func(s *loginChallengeState) { s.ExpiresAt = time.Time{} }},
		{name: "challenge ID", mutate: func(s *loginChallengeState) { s.ChallengeID = "" }},
		{name: "browser proof", mutate: func(s *loginChallengeState) { s.HumanCookie = "" }},
		{name: "invalid cookie", mutate: func(s *loginChallengeState) { s.HumanCookie = strings.Repeat(";", 43) }},
		{name: "malformed", file: "malformed"},
		{name: "oversized", file: "oversized"},
		{name: "permissions", file: "permissions"},
		{name: "symlink", file: "symlink"},
		{name: "directory", file: "directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := loginTestState()
			if tc.mutate != nil {
				tc.mutate(&state)
			}
			path := filepath.Join(t.TempDir(), "challenge.json")
			writeLoginTestState(t, path, state)
			switch tc.file {
			case "malformed", "oversized":
				value := []byte("{")
				if tc.file == "oversized" {
					value = []byte(strings.Repeat(" ", 4097))
				}
				if err := os.WriteFile(path, value, 0600); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := path + ".target"
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int64
			c, _ := New("https://cli.example.test")
			c.http.Transport = loginTestTransport(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, errors.New("unexpected transport call")
			})
			if err := c.LoginWithChallenge("synthetic", "synthetic-password", path, 0, 61); err == nil {
				t.Fatal("invalid state admitted")
			}
			if calls.Load() != 0 {
				t.Fatal("invalid state reached HTTP")
			}
			if tc.file == "" || tc.file == "malformed" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("consumed state was retained", err)
				}
			}
		})
	}
}

func TestLoginChallengeFailuresAndUnknownResultsAreNotRetried(t *testing.T) {
	for _, failure := range []string{
		"verify transport", "verify rejection", "verify malformed", "verify missing ticket",
		"verify expired ticket", "login transport", "login rejection", "login malformed",
	} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "challenge.json")
			writeLoginTestState(t, path, loginTestState())
			var calls atomic.Int64
			c, _ := New("https://cli.example.test")
			c.http.Transport = loginTestTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Error("HTTP submitted before removing state", err)
				}
				proof, err := r.Cookie("wsl_human")
				if err != nil || proof.Value != strings.Repeat("h", 43) {
					t.Error("browser proof missing")
				}
				switch r.URL.Path {
				case "/api/v1/auth/captcha/verify":
					var in map[string]any
					if json.NewDecoder(r.Body).Decode(&in) != nil || len(in) != 4 ||
						in["username"] != "synthetic" || in["challenge_id"] != strings.Repeat("c", 43) ||
						in["x"] != float64(0) || in["y"] != float64(61) {
						t.Error("verify scope or manual coordinates changed")
					}
					switch failure {
					case "verify transport":
						return nil, errors.New("synthetic unknown verification")
					case "verify rejection":
						return loginTestResponse(422, `{"code":422,"msg":"captcha_invalid"}`), nil
					case "verify malformed":
						return loginTestResponse(200, `{`), nil
					case "verify missing ticket":
						return loginTestResponse(200, `{"code":0,"data":{}}`), nil
					case "verify expired ticket":
						return loginTestResponse(200, `{"code":0,"data":{"ticket":"`+strings.Repeat("t", 43)+`","expires_at":"2000-01-01T00:00:00Z"}}`), nil
					}
					return loginTestResponse(200, loginTestTicket()), nil
				case "/api/v1/auth/login":
					var in map[string]string
					if json.NewDecoder(r.Body).Decode(&in) != nil || len(in) != 3 ||
						in["captcha_ticket"] != strings.Repeat("t", 43) || in["username"] != "synthetic" ||
						in["password"] != "synthetic-password" {
						t.Error("login lost ticket or credentials")
					}
					switch failure {
					case "login transport":
						return nil, errors.New("synthetic unknown login")
					case "login rejection":
						return loginTestResponse(401, `{"code":401,"msg":"invalid_credentials"}`), nil
					case "login malformed":
						return loginTestResponse(200, `{`), nil
					}
				}
				t.Error("unexpected request", r.URL.Path)
				return nil, errors.New("unexpected request")
			})
			if err := c.LoginWithChallenge("synthetic", "synthetic-password", path, 0, 61); err == nil {
				t.Fatal("failure became success")
			}
			want := int64(1)
			if strings.HasPrefix(failure, "login ") {
				want = 2
			}
			if calls.Load() != want {
				t.Fatal("request retried or login submitted after failed verify", calls.Load())
			}
			if err := c.LoginWithChallenge("synthetic", "synthetic-password", path, 0, 61); err == nil || calls.Load() != want {
				t.Fatal("consumed challenge replayed")
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 0 {
				t.Fatal("private challenge or claim file left behind", entries, err)
			}
		})
	}
}

func TestLoginChallengeIsConsumedOnceConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "challenge.json")
	writeLoginTestState(t, path, loginTestState())
	var verify, login atomic.Int64
	transport := loginTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/auth/captcha/verify" {
			verify.Add(1)
			return loginTestResponse(200, loginTestTicket()), nil
		}
		if r.URL.Path == "/api/v1/auth/login" {
			login.Add(1)
			return loginTestResponse(200, `{"code":0,"data":{}}`), nil
		}
		return nil, errors.New("unexpected request")
	})
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := New("https://cli.example.test")
			c.http.Transport = transport
			<-start
			results <- c.LoginWithChallenge("synthetic", "synthetic-password", path, 0, 61)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 || verify.Load() != 1 || login.Load() != 1 {
		t.Fatal("concurrent processes reused a challenge", success, verify.Load(), login.Load())
	}
}

func TestIssueLoginChallengeRejectsUnsafeStateAndInvalidResponse(t *testing.T) {
	for _, failure := range []string{"permissions", "symlink", "expired", "no proof", "no image", "no ID"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "challenge.json")
			original := []byte("untouched private data")
			if failure == "permissions" || failure == "symlink" {
				if err := os.WriteFile(path, original, 0644); err != nil {
					t.Fatal(err)
				}
				if failure == "symlink" {
					if err := os.Rename(path, path+".target"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(path+".target", path); err != nil {
						t.Fatal(err)
					}
				}
			}
			c, _ := New("https://cli.example.test")
			c.http.Transport = loginTestTransport(func(r *http.Request) (*http.Response, error) {
				recorder := httptest.NewRecorder()
				if failure != "no proof" {
					http.SetCookie(recorder, &http.Cookie{Name: "wsl_human", Value: strings.Repeat("h", 43), Path: "/api/v1", Secure: true})
				}
				challenge := LoginChallenge{ChallengeID: strings.Repeat("c", 43), ExpiresAt: time.Now().Add(time.Minute),
					Image: LoginImage{Background: "synthetic-background", Tile: "synthetic-tile", Width: 300, Height: 220, TileWidth: 50, TileHeight: 50}}
				switch failure {
				case "expired":
					challenge.ExpiresAt = time.Now().Add(-time.Second)
				case "no image":
					challenge.Image.Tile = ""
				case "no ID":
					challenge.ChallengeID = ""
				}
				json.NewEncoder(recorder).Encode(map[string]any{"code": 0, "data": challenge})
				return recorder.Result(), nil
			})
			if _, err := c.IssueLoginChallenge("synthetic", path); err == nil {
				t.Fatal("invalid response or unsafe destination accepted")
			}
			if failure == "permissions" || failure == "symlink" {
				b, err := os.ReadFile(path)
				if err != nil || string(b) != string(original) {
					t.Fatal("unsafe destination overwritten", err)
				}
			} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid challenge persisted", err)
			}
		})
	}
}
