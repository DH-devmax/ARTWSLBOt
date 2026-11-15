package httpapi

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
)

func cloneCaptchaTestClient(t *testing.T, c testClient) testClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(c.base + "/api/v1")
	if err != nil {
		t.Fatal(err)
	}
	cookies := c.client.Jar.Cookies(u)
	for _, cookie := range cookies {
		cookie.Path = "/"
		if cookie.Name == "wsl_human" {
			cookie.Path = "/api/v1"
		}
	}
	jar.SetCookies(u, cookies)
	client := *c.client
	client.Jar = jar
	c.client = &client
	return c
}

func copyCaptchaTestBrowser(t *testing.T, from, to testClient) {
	t.Helper()
	source, _ := url.Parse(from.base + "/api/v1")
	dest, _ := url.Parse(to.base + "/api/v1")
	for _, cookie := range from.client.Jar.Cookies(source) {
		if cookie.Name == "wsl_human" {
			cookie.Path = "/api/v1"
			to.client.Jar.SetCookies(dest, []*http.Cookie{cookie})
			return
		}
	}
	t.Fatal("source browser proof missing")
}

func TestWebCaptchaPostgresProtectedMutationsRequireTicketWithoutNativeCalls(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			n := &bindingFriendNativeFixture{onboardingNativeFixture: f.native}
			f.app.GroupBinding.Native = n
			challenge := f.createChallenge(t, f.alice, platform, "captcha-admission-"+platform)
			group := operatorClaimGroup(t, f, platform)
			c := cloneCaptchaTestClient(t, f.admin)
			c.workspace = platform
			c.client.Transport = captchaTestTransport{handler: f.app.Handler(), remote: "192.0.2.20:4000"}
			state := func() [6]int {
				t.Helper()
				var counts [6]int
				err := f.db.Platform.QueryRow(context.Background(), `SELECT
					(SELECT count(*) FROM group_binding_challenges),
					(SELECT count(*) FROM group_claims),
					(SELECT count(*) FROM group_bindings),
					(SELECT count(*) FROM operator_demo_group_claims),
					(SELECT count(*) FROM group_binding_friend_attempts),
					(SELECT count(*) FROM test_windows)`).
					Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5])
				if err != nil {
					t.Fatal(err)
				}
				return counts
			}
			before := state()
			for _, operation := range []struct {
				purpose, target, path string
				body                  any
			}{
				{"group_binding_create", "", "/group-binding/challenges", map[string]any{"platform": platform}},
				{"group_binding_friend", challenge.ID, "/group-binding/challenges/" + challenge.ID + "/friend-verify",
					map[string]any{"version": challenge.Version}},
				{"group_claim", "", "/claims", map[string]any{
					"code": strings.Repeat("c", 43), "native_identity": "synthetic-identity"}},
				{"operator_demo_claim", group, "/user-center/groups/" + group + "/demo-claim",
					map[string]any{"binding_version": 1}},
			} {
				captchaTestExpect(t, c, operation.path, operation.body, 400, "captcha_required")
				captchaTestExpect(t, c, operation.path, operation.body, 422, "captcha_invalid",
					http.Header{"X-Human-Verification": {"not-a-verified-ticket"}})
				scope := map[string]any{"purpose": operation.purpose, "platform": platform}
				if operation.target != "" {
					scope["target"] = operation.target
				}
				captchaTestTicket(t, c, "/human-verification", scope)
				if after := state(); after != before {
					t.Fatalf("%s captcha issued authorization or submitted work: %v -> %v", operation.purpose, before, after)
				}
			}
			if n.roles.Load() != 0 || n.invites.Load() != 0 || n.sends.Load() != 0 ||
				n.lookups.Load() != 0 || n.approvals.Load() != 0 || n.submits.Load() != 0 || n.reconciles.Load() != 0 {
				t.Fatal("captcha or unverified HTTP mutation reached a native port")
			}
			// Human verification supplies no tenant/group permission by itself.
			ticket := captchaTestTicket(t, c, "/human-verification", map[string]any{"purpose": "group_claim", "platform": platform})
			headers := http.Header{"X-Human-Verification": {ticket}}
			captchaTestExpect(t, c, "/claims", map[string]any{}, 400, "tenant_user_required", headers)
			captchaTestExpect(t, c, "/claims", map[string]any{}, 422, "captcha_invalid", headers)
			if after := state(); after != before {
				t.Fatal("rejected business request changed authorization", before, after)
			}
		})
	}
}

func TestWebCaptchaPostgresAuthenticatedRoutesRequireSessionCSRFAndRejectCrossOrigin(t *testing.T) {
	f := newOnboardingFixture(t)
	c := cloneCaptchaTestClient(t, f.admin)
	c.workspace = "wsl"
	c.client.Transport = captchaTestTransport{handler: f.app.Handler(), remote: "192.0.2.21:4000"}
	scope := map[string]any{"purpose": "group_claim", "platform": "wsl"}
	anonymous := newCaptchaHTTPClient(t, f.app, "192.0.2.21:4000")
	rejectedHeaders := []http.Header{
		{"X-CSRF-Token": {""}},
		{"Origin": {"https://untrusted.example"}},
	}
	for _, action := range []string{"challenge", "verify"} {
		captchaTestExpect(t, anonymous, "/human-verification/"+action, scope, 401, "authentication_required")
		for _, headers := range rejectedHeaders {
			id := captchaTestChallenge(t, c, "/human-verification", scope)
			body := map[string]any{"purpose": "group_claim", "platform": "wsl"}
			if action == "verify" {
				body["challenge_id"], body["x"], body["y"] = id, captchaTestX, captchaTestY
			}
			captchaTestExpect(t, c, "/human-verification/"+action, body,
				403, "csrf_rejected", headers)
			// Rejected CSRF/cross-origin requests never reach captcha verification.
			captchaTestExpect(t, c, "/human-verification/verify", map[string]any{
				"purpose": "group_claim", "platform": "wsl", "challenge_id": id, "x": captchaTestX, "y": captchaTestY,
			}, 200, "ok")
		}
	}
	for _, rejected := range rejectedHeaders {
		ticket := captchaTestTicket(t, c, "/human-verification", scope)
		headers := rejected.Clone()
		headers.Set("X-Human-Verification", ticket)
		captchaTestExpect(t, c, "/claims", map[string]any{}, 403, "csrf_rejected", headers)
		// A valid retry reaches the business rejection once, then loses its ticket.
		headers = http.Header{"X-Human-Verification": {ticket}}
		captchaTestExpect(t, c, "/claims", map[string]any{}, 400, "tenant_user_required", headers)
		captchaTestExpect(t, c, "/claims", map[string]any{}, 422, "captcha_invalid", headers)
	}
}

func TestWebCaptchaPostgresAuthenticatedHTTPScopes(t *testing.T) {
	for _, phase := range []string{"verify", "consume"} {
		t.Run(phase, func(t *testing.T) {
			f := newOnboardingFixture(t)
			for _, dimension := range []string{"actor", "session", "browser", "host", "ip", "platform", "purpose", "target"} {
				t.Run(dimension, func(t *testing.T) {
					c := cloneCaptchaTestClient(t, f.admin)
					c.workspace = "wsl"
					c.client.Transport = captchaTestTransport{handler: f.app.Handler(), remote: "192.0.2.22:4000"}
					scope := map[string]any{"purpose": "group_binding_friend", "platform": "wsl", "target": captchaTestTarget}
					id := captchaTestChallenge(t, c, "/human-verification", scope)
					ticket := ""
					if phase == "consume" {
						raw := captchaTestExpect(t, c, "/human-verification/verify", map[string]any{
							"purpose": scope["purpose"], "platform": scope["platform"], "target": scope["target"],
							"challenge_id": id, "x": captchaTestX, "y": captchaTestY,
						}, 200, "ok")
						ticket = field(t, raw, "ticket")
					}
					changed := cloneCaptchaTestClient(t, c)
					extra := make(http.Header)
					path := "/group-binding/challenges/" + captchaTestTarget + "/friend-verify"
					switch dimension {
					case "actor", "session":
						if dimension == "actor" {
							changed = cloneCaptchaTestClient(t, f.alice)
						} else {
							changed = f.client(t, c.base, f.adminID)
						}
						copyCaptchaTestBrowser(t, c, changed)
						changed.workspace = "wsl"
						changed.client.Transport = c.client.Transport
					case "browser":
						u, _ := url.Parse(changed.base + "/api/v1")
						changed.client.Jar.SetCookies(u, []*http.Cookie{{
							Name: "wsl_human", Value: strings.Repeat("b", 43), Path: "/api/v1",
						}})
					case "host":
						// Mutate the server-visible Host after the client's cookie
						// policy so this exercises captcha binding with a session.
						changed.client.Transport = captchaTestTransport{handler: f.app.Handler(),
							remote: "192.0.2.22:4000", host: "other-captcha.test"}
					case "ip":
						changed.client.Transport = captchaTestTransport{handler: f.app.Handler(), remote: "192.0.2.23:4000"}
						extra.Set("X-Forwarded-For", "192.0.2.22")
					case "platform":
						changed.workspace, scope["platform"] = "lajiaoliao", "lajiaoliao"
					case "purpose":
						scope["purpose"] = "group_claim"
						delete(scope, "target")
						path = "/claims"
					case "target":
						scope["target"] = captchaTestOther
						path = "/group-binding/challenges/" + captchaTestOther + "/friend-verify"
					}
					if phase == "verify" {
						scope["challenge_id"], scope["x"], scope["y"] = id, captchaTestX, captchaTestY
						captchaTestExpect(t, changed, "/human-verification/verify", scope, 422, "captcha_invalid", extra)
					} else {
						extra.Set("X-Human-Verification", ticket)
						captchaTestExpect(t, changed, path, map[string]any{"version": 1}, 422, "captcha_invalid", extra)
					}
				})
			}
			if f.native.roles.Load() != 0 || f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("scope mismatch reached native execution")
			}
		})
	}
}

func TestWebCaptchaPostgresFailedCredentialsConsumeTicket(t *testing.T) {
	f := newOnboardingFixture(t)
	c := newCaptchaHTTPClient(t, f.app, "192.0.2.24:4000")
	ticket := captchaTestTicket(t, c, "/auth/captcha", map[string]any{"username": "customer-a"})
	body := map[string]any{"username": "customer-a", "password": "synthetic-wrong-password", "captcha_ticket": ticket}
	captchaTestExpect(t, c, "/auth/login", body, 401, "invalid_credentials")
	body["password"] = "synthetic-onboarding-password"
	captchaTestExpect(t, c, "/auth/login", body, 422, "captcha_invalid")
	u, _ := url.Parse(c.base + "/api/v1")
	for _, cookie := range c.client.Jar.Cookies(u) {
		if cookie.Name == "wsl_session" {
			t.Fatal("failed login created a website session")
		}
	}
	body["captcha_ticket"] = captchaTestTicket(t, c, "/auth/captcha", map[string]any{"username": "customer-a"})
	captchaTestExpect(t, c, "/auth/login", body, 200, "ok")
}

func TestWebCaptchaPostgresLoginAndOperationTicketsCannotMix(t *testing.T) {
	f := newOnboardingFixture(t)
	c := cloneCaptchaTestClient(t, f.admin)
	c.client.Transport = captchaTestTransport{handler: f.app.Handler(), remote: "192.0.2.25:4000"}
	login := captchaTestTicket(t, c, "/auth/captcha", map[string]any{"username": "onboarding-admin"})
	captchaTestExpect(t, c, "/claims", map[string]any{}, 422, "captcha_invalid",
		http.Header{"X-Human-Verification": {login}})
	operation := captchaTestTicket(t, c, "/human-verification", map[string]any{"purpose": "group_claim", "platform": "wsl"})
	captchaTestExpect(t, c, "/auth/login", map[string]any{
		"username": "onboarding-admin", "password": "synthetic-onboarding-password", "captcha_ticket": operation,
	}, 422, "captcha_invalid")
}
