package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wangshangliao/internal/webcaptcha"
)

func outboundCaptchaRequest() (*http.Request, Principal) {
	r := httptest.NewRequest(http.MethodPost,
		"http://captcha.test/api/v1/group-binding/challenges/"+captchaTestTarget+"/friend-send", nil)
	r.RemoteAddr = "192.0.2.16:4000"
	r.SetPathValue("id", captchaTestTarget)
	r.Header.Set("X-Workspace", "wsl")
	r.AddCookie(&http.Cookie{Name: humanCookie, Value: strings.Repeat("a", 43)})
	r.AddCookie(&http.Cookie{Name: "wsl_session", Value: strings.Repeat("s", 43)})
	return r, Principal{ID: captchaTestTarget, TenantID: captchaTestOther, Role: "user"}
}

func TestOutboundFriendCaptchaHasDedicatedPurposeAndConsumesOnce(t *testing.T) {
	s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
	defer s.Native.Close()
	r, actor := outboundCaptchaRequest()
	purpose, target := humanVerificationOperation("POST /api/v1/group-binding/challenges/{id}/friend-send", r)
	if purpose != "group_binding_friend_send" || target != captchaTestTarget {
		t.Fatal("outgoing friend send lost challenge-specific human verification")
	}
	scope, err := s.operationCaptchaScope(nil, r, actor, captchaInput{Purpose: purpose,
		Platform: "wsl", Target: target}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.requireHumanVerification(r, actor, purpose, target); err == nil {
		t.Fatal("outgoing friend send admitted without human verification")
	}
	challenge, err := s.captcha.Issue(r.Context(), scope, "")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := s.captcha.Verify(scope, challenge.ChallengeID, captchaTestX, captchaTestY)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Human-Verification", ticket.Ticket)
	if err = s.requireHumanVerification(r, actor, purpose, target); err != nil {
		t.Fatal("matching outbound friend ticket rejected", err)
	}
	var failure *apiError
	if err = s.requireHumanVerification(r, actor, purpose, target); !errors.As(err, &failure) ||
		failure.status != 422 || failure.code != "captcha_invalid" {
		t.Fatal("outbound friend ticket replayed", err)
	}
}

func TestOutboundFriendCaptchaCannotReuseIncomingProofOrDifferentScope(t *testing.T) {
	for _, dimension := range []string{"purpose", "target", "platform", "actor", "tenant", "session", "browser"} {
		t.Run(dimension, func(t *testing.T) {
			s := newCaptchaTestServer(nil, nil, captchaTestOrigin, false)
			defer s.Native.Close()
			r, actor := outboundCaptchaRequest()
			in := captchaInput{Purpose: "group_binding_friend_send", Platform: "wsl", Target: captchaTestTarget}
			scope, err := s.operationCaptchaScope(nil, r, actor, in, false)
			if err != nil {
				t.Fatal(err)
			}
			challenge, err := s.captcha.Issue(r.Context(), scope, "")
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := s.captcha.Verify(scope, challenge.ChallengeID, captchaTestX, captchaTestY)
			if err != nil {
				t.Fatal(err)
			}
			switch dimension {
			case "purpose":
				in.Purpose = "group_binding_friend"
			case "target":
				in.Target = captchaTestOther
			case "platform":
				in.Platform = "lajiaoliao"
				r.Header.Set("X-Workspace", in.Platform)
			case "actor":
				actor.ID = captchaTestOther
			case "tenant":
				actor.TenantID = captchaTestTarget
			case "session", "browser":
				cookies := r.Cookies()
				r.Header.Del("Cookie")
				for _, cookie := range cookies {
					if dimension == "session" && cookie.Name == "wsl_session" ||
						dimension == "browser" && cookie.Name == humanCookie {
						cookie.Value = strings.Repeat("z", 43)
					}
					r.AddCookie(cookie)
				}
			}
			changed, err := s.operationCaptchaScope(nil, r, actor, in, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.captcha.Consume(changed, ticket.Ticket); !errors.Is(err, webcaptcha.ErrInvalid) {
				t.Fatalf("outbound ticket accepted changed %s: %v", dimension, err)
			}
		})
	}
	for _, pattern := range []string{"POST /api/v1/group-binding/challenges/{id}/friend-target",
		"POST /api/v1/group-binding/challenges/{id}/outbound-attempts/{operation}/reconcile",
		"GET /api/v1/system/group-binding/outbound-friend-gates/{account}"} {
		r, _ := outboundCaptchaRequest()
		if purpose, target := humanVerificationOperation(pattern, r); purpose != "" || target != "" {
			t.Fatal("read or preparation unexpectedly consumes outgoing send authorization", pattern)
		}
	}
}
