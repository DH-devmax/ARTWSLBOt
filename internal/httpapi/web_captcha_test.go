package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
	"wangshangliao/internal/webcaptcha"
)

const (
	captchaTestX      = 137
	captchaTestY      = 61
	captchaTestOrigin = "http://127.0.0.1:5173"
	captchaTestTarget = "11111111-1111-4111-8111-111111111111"
	captchaTestOther  = "22222222-2222-4222-8222-222222222222"
)

// The answer is injected only in tests; clients still use both real HTTP routes.
func newCaptchaTestServer(db *database.DB, vault *security.Vault, origin string, secure bool) *Server {
	s := New(db, vault, origin, secure)
	s.captcha = webcaptcha.New(func() (webcaptcha.Puzzle, error) {
		render := func(width, height int) (string, error) {
			var out bytes.Buffer
			if err := png.Encode(&out, image.NewNRGBA(image.Rect(0, 0, width, height))); err != nil {
				return "", err
			}
			return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
		}
		background, err := render(300, 220)
		if err != nil {
			return webcaptcha.Puzzle{}, err
		}
		tile, err := render(50, 50)
		if err != nil {
			return webcaptcha.Puzzle{}, err
		}
		return webcaptcha.Puzzle{Image: webcaptcha.Image{
			Background: background, Tile: tile, Width: 300, Height: 220,
			TileWidth: 50, TileHeight: 50, InitialX: 0, InitialY: 0,
		}, X: captchaTestX, Y: captchaTestY}, nil
	})
	return s
}

func captchaTestOperation(path string) (purpose, target string) {
	path, _, _ = strings.Cut(path, "?")
	switch path {
	case "/group-binding/challenges":
		return "group_binding_create", ""
	case "/claims":
		return "group_claim", ""
	}
	for _, route := range []struct{ prefix, suffix, purpose string }{
		{"/group-binding/challenges/", "/friend-verify", "group_binding_friend"},
		{"/group-binding/challenges/", "/friend-approve", "group_binding_friend"},
		{"/group-binding/challenges/", "/friend-reject", "group_binding_friend"},
		{"/user-center/groups/", "/demo-claim", "operator_demo_claim"},
	} {
		if strings.HasPrefix(path, route.prefix) && strings.HasSuffix(path, route.suffix) {
			id := strings.TrimSuffix(strings.TrimPrefix(path, route.prefix), route.suffix)
			if id != "" && !strings.Contains(id, "/") {
				return route.purpose, id
			}
		}
	}
	return "", ""
}

func (c testClient) solveCaptcha(prefix string, scope map[string]any) (int, json.RawMessage, string) {
	status, raw, msg := c.requestRaw("POST", prefix+"/challenge", scope)
	if status != http.StatusOK {
		return status, raw, msg
	}
	var challenge webcaptcha.Challenge
	if json.Unmarshal(raw, &challenge) != nil || challenge.ChallengeID == "" {
		return 0, nil, "captcha_fixture_invalid_challenge"
	}
	verify := make(map[string]any, len(scope)+3)
	for key, value := range scope {
		verify[key] = value
	}
	verify["challenge_id"], verify["x"], verify["y"] = challenge.ChallengeID, captchaTestX, captchaTestY
	return c.requestRaw("POST", prefix+"/verify", verify)
}

func (c testClient) requestWithCaptcha(method, path string, body any, headers http.Header) (int, json.RawMessage, string) {
	if method != http.MethodPost {
		return c.requestRaw(method, path, body, headers)
	}
	purpose, target := captchaTestOperation(path)
	if path != "/auth/login" && purpose == "" {
		return c.requestRaw(method, path, body, headers)
	}
	extra := make(http.Header)
	for key, values := range headers {
		extra[key] = append([]string(nil), values...)
	}
	prefix := "/human-verification"
	platform := c.workspace
	if platform == "" {
		platform = "wsl"
	}
	scope := map[string]any{"purpose": purpose, "platform": platform}
	if target != "" {
		scope["target"] = target
	}
	var login map[string]any
	if path == "/auth/login" {
		raw, err := json.Marshal(body)
		if err != nil || json.Unmarshal(raw, &login) != nil || login == nil {
			return c.requestRaw(method, path, body, headers)
		}
		if _, supplied := login["captcha_ticket"]; supplied {
			return c.requestRaw(method, path, body, headers)
		}
		prefix, scope = "/auth/captcha", map[string]any{"username": login["username"]}
	} else if extra.Get("X-Human-Verification") != "" {
		return c.requestRaw(method, path, body, headers)
	}
	status, raw, msg := c.solveCaptcha(prefix, scope)
	if status != http.StatusOK {
		// Keep existing endpoint authentication/role assertions on that endpoint.
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return c.requestRaw(method, path, body, headers)
		}
		return status, raw, msg
	}
	var ticket webcaptcha.Ticket
	if json.Unmarshal(raw, &ticket) != nil || ticket.Ticket == "" {
		return 0, nil, "captcha_fixture_invalid_ticket"
	}
	if login != nil {
		login["captcha_ticket"], body = ticket.Ticket, login
	} else {
		extra.Set("X-Human-Verification", ticket.Ticket)
	}
	return c.requestRaw(method, path, body, extra)
}

// Requests stay inside the synthetic HTTP handler. RemoteAddr models the peer,
// rather than relying on a forwarded header that production intentionally ignores.
type captchaTestTransport struct {
	handler http.Handler
	remote  string
	host    string
}

func (c captchaTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	request := r.Clone(r.Context())
	if request.Body != nil {
		defer request.Body.Close()
	}
	request.RemoteAddr = c.remote
	if c.host != "" {
		request.Host = c.host
	}
	response := httptest.NewRecorder()
	c.handler.ServeHTTP(response, request)
	return response.Result(), nil
}

func newCaptchaHTTPClient(t *testing.T, s *Server, remote string) testClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return testClient{base: "http://captcha.test", client: &http.Client{
		Jar: jar, Transport: captchaTestTransport{handler: s.Handler(), remote: remote},
	}}
}

func captchaTestExpect(t *testing.T, c testClient, path string, body any, want int, code string, headers ...http.Header) json.RawMessage {
	t.Helper()
	status, data, msg := c.requestRaw("POST", path, body, headers...)
	if status != want || code != "" && msg != code {
		t.Fatalf("POST %s: got %d (%s), want %d (%s)", path, status, msg, want, code)
	}
	return data
}

func captchaTestChallenge(t *testing.T, c testClient, path string, body any) string {
	t.Helper()
	raw := captchaTestExpect(t, c, path+"/challenge", body, 200, "ok")
	return field(t, raw, "challenge_id")
}

func captchaTestTicket(t *testing.T, c testClient, prefix string, scope map[string]any) string {
	t.Helper()
	status, raw, msg := c.solveCaptcha(prefix, scope)
	if status != 200 {
		t.Fatalf("captcha ticket: got %d (%s)", status, msg)
	}
	return field(t, raw, "ticket")
}

func TestWebCaptchaLoginChallengeContract(t *testing.T) {
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, true)
	defer s.Native.Close()
	request := httptest.NewRequest("POST", "https://captcha.test/api/v1/auth/captcha/challenge",
		strings.NewReader(`{"username":"synthetic-captcha-user"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", captchaTestOrigin)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("challenge unavailable or cacheable", response.Code, response.Body.String())
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 3 || envelope.Data["challenge_id"] == nil ||
		envelope.Data["expires_at"] == nil || envelope.Data["image"] == nil {
		t.Fatal("unexpected challenge fields, including a possible answer", response.Body.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data["image"], &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"background", "tile", "width", "height", "tile_width", "tile_height", "initial_x", "initial_y"} {
		if fields[key] == nil {
			t.Fatal("missing snake_case image field", key)
		}
	}
	if len(fields) != 8 {
		t.Fatal("unexpected image fields, including a possible answer", string(envelope.Data["image"]))
	}
	var bitmap webcaptcha.Image
	if err := json.Unmarshal(envelope.Data["image"], &bitmap); err != nil {
		t.Fatal(err)
	}
	if bitmap.Width != 300 || bitmap.Height != 220 || bitmap.TileWidth != 50 || bitmap.TileHeight != 50 ||
		bitmap.InitialX == captchaTestX || bitmap.InitialY == captchaTestY ||
		bitmap.Background == "" || bitmap.Tile == "" {
		t.Fatal("image dimensions or initial position exposed the answer", bitmap)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "wsl_human" || len(cookies[0].Value) != 43 ||
		cookies[0].Path != "/api/v1" || !cookies[0].HttpOnly || !cookies[0].Secure ||
		cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("browser proof cookie attributes changed", cookies)
	}
}

func TestWebCaptchaLoginRejectsBeforePasswordLookup(t *testing.T) {
	// A password lookup would touch the nil DB and become an HTTP 500.
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
	defer s.Native.Close()
	c := newCaptchaHTTPClient(t, s, "192.0.2.1:4000")
	body := map[string]any{"username": "synthetic-captcha-user", "password": "synthetic-private-password"}
	captchaTestExpect(t, c, "/auth/login", body, 400, "captcha_required")
	id := captchaTestChallenge(t, c, "/auth/captcha", map[string]any{"username": body["username"]})
	for _, invalid := range []string{id, "not-a-verified-ticket"} {
		body["captcha_ticket"] = invalid
		captchaTestExpect(t, c, "/auth/login", body, 422, "captcha_invalid")
	}
	if len(s.loginAttempts) != 0 || len(s.loginSlots) != 0 {
		t.Fatal("unverified login reached the password limiter")
	}
}

func TestWebCaptchaLoginCoordinatesRefreshAndReplay(t *testing.T) {
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
	defer s.Native.Close()
	c := newCaptchaHTTPClient(t, s, "192.0.2.2:4000")
	scope := map[string]any{"username": "synthetic-captcha-user"}
	for _, coordinates := range [][2]int{{captchaTestX + 50, captchaTestY}, {captchaTestX, captchaTestY + 50}} {
		id := captchaTestChallenge(t, c, "/auth/captcha", scope)
		captchaTestExpect(t, c, "/auth/captcha/verify", map[string]any{
			"username": scope["username"], "challenge_id": id, "x": coordinates[0], "y": coordinates[1],
		}, 422, "captcha_invalid")
		captchaTestExpect(t, c, "/auth/captcha/verify", map[string]any{
			"username": scope["username"], "challenge_id": id, "x": captchaTestX, "y": captchaTestY,
		}, 422, "captcha_invalid")
	}
	first := captchaTestChallenge(t, c, "/auth/captcha", scope)
	next := captchaTestChallenge(t, c, "/auth/captcha", map[string]any{"username": scope["username"], "previous_id": first})
	verify := map[string]any{"username": scope["username"], "challenge_id": first, "x": captchaTestX, "y": captchaTestY}
	captchaTestExpect(t, c, "/auth/captcha/verify", verify, 422, "captcha_invalid")
	verify["challenge_id"] = next
	raw := captchaTestExpect(t, c, "/auth/captcha/verify", verify, 200, "ok")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 2 ||
		fields["ticket"] == nil || fields["expires_at"] == nil {
		t.Fatal("verification exposed fields beyond the ticket and expiry", string(raw), err)
	}
	captchaTestExpect(t, c, "/auth/captcha/verify", verify, 422, "captcha_invalid")
	// A busy password worker is a failed login too; its ticket is already spent.
	for range cap(s.loginSlots) {
		s.loginSlots <- struct{}{}
	}
	body := map[string]any{"username": scope["username"], "password": "synthetic-password", "captcha_ticket": field(t, raw, "ticket")}
	captchaTestExpect(t, c, "/auth/login", body, 429, "login_busy")
	captchaTestExpect(t, c, "/auth/login", body, 422, "captcha_invalid")
}

func TestWebCaptchaLoginChallengeAndTicketScope(t *testing.T) {
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
	defer s.Native.Close()
	for _, phase := range []string{"verify", "consume"} {
		for _, dimension := range []string{"username", "ip", "host", "browser", "missing_browser"} {
			t.Run(phase+"/"+dimension, func(t *testing.T) {
				c := newCaptchaHTTPClient(t, s, "192.0.2.3:4000")
				username := "synthetic-captcha-user"
				scope := map[string]any{"username": username}
				id := captchaTestChallenge(t, c, "/auth/captcha", scope)
				var ticket string
				if phase == "consume" {
					raw := captchaTestExpect(t, c, "/auth/captcha/verify", map[string]any{
						"username": username, "challenge_id": id, "x": captchaTestX, "y": captchaTestY,
					}, 200, "ok")
					ticket = field(t, raw, "ticket")
				}
				extra := make(http.Header)
				switch dimension {
				case "username":
					username = "another-captcha-user"
				case "ip":
					c.client.Transport = captchaTestTransport{handler: s.Handler(), remote: "192.0.2.4:4000"}
					extra.Set("X-Forwarded-For", "192.0.2.3")
				case "host":
					c.client.Transport = captchaTestTransport{handler: s.Handler(),
						remote: "192.0.2.3:4000", host: "other-captcha.test"}
				case "browser", "missing_browser":
					u, _ := url.Parse(c.base + "/api/v1")
					cookie := &http.Cookie{Name: "wsl_human", Value: strings.Repeat("b", 43), Path: "/api/v1"}
					if dimension == "missing_browser" {
						cookie.MaxAge = -1
					}
					c.client.Jar.SetCookies(u, []*http.Cookie{cookie})
				}
				if phase == "verify" {
					captchaTestExpect(t, c, "/auth/captcha/verify", map[string]any{
						"username": username, "challenge_id": id, "x": captchaTestX, "y": captchaTestY,
					}, 422, "captcha_invalid", extra)
				} else {
					captchaTestExpect(t, c, "/auth/login", map[string]any{
						"username": username, "password": "synthetic-password", "captcha_ticket": ticket,
					}, 422, "captcha_invalid", extra)
				}
			})
		}
	}
	if len(s.loginAttempts) != 0 {
		t.Fatal("a scope-mismatched ticket reached password admission")
	}
}

func TestWebCaptchaPublicRoutesRejectCrossOrigin(t *testing.T) {
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
	defer s.Native.Close()
	c := newCaptchaHTTPClient(t, s, "192.0.2.5:4000")
	for _, path := range []string{"/auth/captcha/challenge", "/auth/captcha/verify", "/auth/login"} {
		captchaTestExpect(t, c, path, map[string]any{"username": "synthetic-captcha-user"},
			403, "origin_rejected", http.Header{"Origin": {"https://untrusted.example"}})
	}
}

func TestWebCaptchaOperationScopeIsolation(t *testing.T) {
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
	defer s.Native.Close()
	for _, phase := range []string{"verify", "consume"} {
		for _, dimension := range []string{"actor", "tenant", "session", "browser", "host", "ip", "platform", "purpose", "target"} {
			t.Run(phase+"/"+dimension, func(t *testing.T) {
				request := httptest.NewRequest("POST", "http://captcha.test/api/v1/group-binding/challenges/"+captchaTestTarget+"/friend-verify", nil)
				request.RemoteAddr = "192.0.2.6:4000"
				request.Header.Set("X-Workspace", "wsl")
				request.AddCookie(&http.Cookie{Name: "wsl_human", Value: strings.Repeat("a", 43)})
				request.AddCookie(&http.Cookie{Name: "wsl_session", Value: strings.Repeat("s", 43)})
				p := Principal{ID: captchaTestTarget, TenantID: captchaTestOther, Role: "superadmin"}
				in := captchaInput{Purpose: "group_binding_friend", Platform: "wsl", Target: captchaTestTarget}
				scope, err := s.operationCaptchaScope(nil, request, p, in, false)
				if err != nil {
					t.Fatal(err)
				}
				challenge, err := s.captcha.Issue(context.Background(), scope, "")
				if err != nil {
					t.Fatal(err)
				}
				var ticket webcaptcha.Ticket
				if phase == "consume" {
					ticket, err = s.captcha.Verify(scope, challenge.ChallengeID, captchaTestX, captchaTestY)
					if err != nil {
						t.Fatal(err)
					}
				}
				switch dimension {
				case "actor":
					p.ID = captchaTestOther
				case "tenant":
					p.TenantID = captchaTestTarget
				case "session", "browser":
					name := "wsl_session"
					if dimension == "browser" {
						name = "wsl_human"
					}
					cookies := request.Cookies()
					request.Header.Del("Cookie")
					for _, cookie := range cookies {
						if cookie.Name == name {
							cookie.Value = strings.Repeat("z", 43)
						}
						request.AddCookie(cookie)
					}
				case "host":
					request.Host = "other-captcha.test"
				case "ip":
					request.RemoteAddr = "192.0.2.7:4000"
				case "platform":
					in.Platform = "lajiaoliao"
					request.Header.Set("X-Workspace", in.Platform)
				case "purpose":
					in.Purpose = "operator_demo_claim"
				case "target":
					in.Target = captchaTestOther
				}
				if phase == "verify" {
					changed, err := s.operationCaptchaScope(nil, request, p, in, false)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = s.captcha.Verify(changed, challenge.ChallengeID, captchaTestX, captchaTestY); !errors.Is(err, webcaptcha.ErrInvalid) {
						t.Fatalf("challenge accepted changed %s: %v", dimension, err)
					}
				} else {
					request.Header.Set("X-Human-Verification", ticket.Ticket)
					var failure *apiError
					err = s.requireHumanVerification(request, p, in.Purpose, in.Target)
					if !errors.As(err, &failure) || failure.status != 422 || failure.code != "captcha_invalid" {
						t.Fatalf("ticket accepted changed %s: %v", dimension, err)
					}
				}
			})
		}
	}
}

func TestWebCaptchaOperationTicketIsSingleUse(t *testing.T) {
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
	defer s.Native.Close()
	request := httptest.NewRequest("POST", "http://captcha.test/api/v1/claims", nil)
	request.RemoteAddr = "192.0.2.8:4000"
	request.AddCookie(&http.Cookie{Name: "wsl_human", Value: strings.Repeat("a", 43)})
	request.AddCookie(&http.Cookie{Name: "wsl_session", Value: strings.Repeat("s", 43)})
	p := Principal{ID: captchaTestTarget, TenantID: captchaTestOther, Role: "user"}
	in := captchaInput{Purpose: "group_claim", Platform: "wsl"}
	scope, err := s.operationCaptchaScope(nil, request, p, in, false)
	if err != nil {
		t.Fatal(err)
	}
	var failure *apiError
	if err = s.requireHumanVerification(request, p, in.Purpose, ""); !errors.As(err, &failure) || failure.code != "captcha_required" {
		t.Fatal("operation admitted without a ticket", err)
	}
	challenge, err := s.captcha.Issue(request.Context(), scope, "")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := s.captcha.Verify(scope, challenge.ChallengeID, captchaTestX, captchaTestY)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Human-Verification", ticket.Ticket)
	if err = s.requireHumanVerification(request, p, in.Purpose, ""); err != nil {
		t.Fatal("matching operation ticket rejected", err)
	}
	if err = s.requireHumanVerification(request, p, in.Purpose, ""); !errors.As(err, &failure) || failure.code != "captcha_invalid" {
		t.Fatal("operation ticket admitted twice", err)
	}
}

func TestWebCaptchaTestClientSolvesLoginWithoutMutatingBody(t *testing.T) {
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
	defer s.Native.Close()
	for range cap(s.loginSlots) {
		s.loginSlots <- struct{}{}
	}
	c := newCaptchaHTTPClient(t, s, "192.0.2.9:4000")
	body := map[string]string{"username": "synthetic-captcha-user", "password": "synthetic-password"}
	status, _, msg := c.request("POST", "/auth/login", body)
	if status != 429 || msg != "login_busy" {
		t.Fatalf("helper did not solve the real captcha routes: %d (%s)", status, msg)
	}
	if _, mutated := body["captcha_ticket"]; mutated {
		t.Fatal("automatic captcha changed the caller's login fixture")
	}
	captchaTestExpect(t, c, "/auth/login", body, 400, "captcha_required")
}
