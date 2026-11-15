package httpapi

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
	"wangshangliao/internal/webcaptcha"
)

const humanCookie = "wsl_human"

type captchaInput struct {
	Purpose     string `json:"purpose"`
	Username    string `json:"username"`
	Platform    string `json:"platform"`
	Target      string `json:"target"`
	PreviousID  string `json:"previous_id"`
	ChallengeID string `json:"challenge_id"`
	X           *int   `json:"x"`
	Y           *int   `json:"y"`
}

func captchaFailure(err error) error {
	switch {
	case errors.Is(err, webcaptcha.ErrInvalid):
		return fail(http.StatusUnprocessableEntity, "captcha_invalid")
	case errors.Is(err, webcaptcha.ErrLimited):
		return fail(http.StatusTooManyRequests, "captcha_rate_limited")
	case errors.Is(err, webcaptcha.ErrBusy):
		return fail(http.StatusTooManyRequests, "captcha_busy")
	default:
		return fail(http.StatusServiceUnavailable, "captcha_unavailable")
	}
}

func (s *Server) browserProof(w http.ResponseWriter, r *http.Request, create bool) (string, error) {
	cookie, err := r.Cookie(humanCookie)
	if err != nil || len(cookie.Value) != 43 {
		if !create {
			return "", fail(http.StatusUnprocessableEntity, "captcha_invalid")
		}
		token, err := security.Token()
		if err != nil {
			return "", fail(http.StatusServiceUnavailable, "captcha_unavailable")
		}
		cookie = &http.Cookie{Name: humanCookie, Value: token, Path: "/api/v1",
			HttpOnly: true, Secure: s.SecureCookie, SameSite: http.SameSiteStrictMode,
			MaxAge: 43200, Expires: time.Now().Add(12 * time.Hour)}
		http.SetCookie(w, cookie)
	}
	return hex.EncodeToString(security.Digest(cookie.Value)), nil
}

func (s *Server) loginCaptchaScope(w http.ResponseWriter, r *http.Request, username string, create bool) (webcaptcha.Scope, error) {
	if strings.TrimSpace(username) == "" || len(username) > 80 {
		return webcaptcha.Scope{}, fail(http.StatusBadRequest, "invalid_request")
	}
	browser, err := s.browserProof(w, r, create)
	if err != nil {
		return webcaptcha.Scope{}, err
	}
	return webcaptcha.Scope{Purpose: "login", Subject: username, Browser: browser,
		Host: strings.ToLower(r.Host), IP: remoteIP(r)}, nil
}

func (s *Server) operationCaptchaScope(w http.ResponseWriter, r *http.Request, p Principal, in captchaInput, create bool) (webcaptcha.Scope, error) {
	if in.Username != "" || in.Platform != requestWorkspace(r) ||
		(in.Platform != "wsl" && in.Platform != "lajiaoliao") {
		return webcaptcha.Scope{}, fail(http.StatusBadRequest, "invalid_request")
	}
	switch in.Purpose {
	case "group_binding_create", "group_claim":
		if in.Target != "" {
			return webcaptcha.Scope{}, fail(http.StatusBadRequest, "invalid_request")
		}
	case "group_binding_friend", "group_binding_friend_send":
		if !validUUID(in.Target) {
			return webcaptcha.Scope{}, fail(http.StatusBadRequest, "invalid_resource_id")
		}
	case "operator_demo_claim":
		if p.Role != "superadmin" {
			return webcaptcha.Scope{}, fail(http.StatusForbidden, "superadmin_required")
		}
		if !validUUID(in.Target) {
			return webcaptcha.Scope{}, fail(http.StatusBadRequest, "invalid_resource_id")
		}
	default:
		return webcaptcha.Scope{}, fail(http.StatusBadRequest, "invalid_request")
	}
	browser, err := s.browserProof(w, r, create)
	if err != nil {
		return webcaptcha.Scope{}, err
	}
	session, err := r.Cookie("wsl_session")
	if err != nil || p.ID == "" {
		return webcaptcha.Scope{}, fail(http.StatusUnauthorized, "authentication_required")
	}
	return webcaptcha.Scope{Purpose: in.Purpose, Subject: p.ID + ":" + p.TenantID,
		Browser: browser, Session: hex.EncodeToString(security.Digest(session.Value)),
		Host: strings.ToLower(r.Host), IP: remoteIP(r), Platform: in.Platform, Target: in.Target}, nil
}

func (s *Server) webCaptchaRoutes() {
	for _, action := range []string{"challenge", "verify"} {
		s.mux.HandleFunc("POST /api/v1/auth/captcha/"+action, func(w http.ResponseWriter, r *http.Request) {
			if !s.validOrigin(r) {
				sendError(w, fail(http.StatusForbidden, "origin_rejected"))
				return
			}
			var in captchaInput
			if err := decode(w, r, &in); err != nil {
				sendError(w, err)
				return
			}
			if in.Purpose != "" || in.Platform != "" || in.Target != "" {
				sendError(w, fail(http.StatusBadRequest, "invalid_request"))
				return
			}
			scope, err := s.loginCaptchaScope(w, r, in.Username, action == "challenge")
			if err == nil {
				err = s.serveCaptcha(w, r, scope, in, action)
			}
			if err != nil {
				sendError(w, err)
			}
		})
		s.handle("POST /api/v1/human-verification/"+action, false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
			var in captchaInput
			if err := decode(w, r, &in); err != nil {
				return err
			}
			scope, err := s.operationCaptchaScope(w, r, p, in, action == "challenge")
			if err != nil {
				return err
			}
			return s.serveCaptcha(w, r, scope, in, action)
		})
	}
}

func (s *Server) serveCaptcha(w http.ResponseWriter, r *http.Request, scope webcaptcha.Scope, in captchaInput, action string) error {
	if action == "challenge" {
		if in.ChallengeID != "" || in.X != nil || in.Y != nil {
			return fail(http.StatusBadRequest, "invalid_request")
		}
		challenge, err := s.captcha.Issue(r.Context(), scope, in.PreviousID)
		if err != nil {
			return captchaFailure(err)
		}
		send(w, http.StatusOK, challenge)
		return nil
	}
	if in.X == nil || in.Y == nil || in.PreviousID != "" {
		return fail(http.StatusBadRequest, "invalid_request")
	}
	ticket, err := s.captcha.Verify(scope, in.ChallengeID, *in.X, *in.Y)
	if err != nil {
		return captchaFailure(err)
	}
	send(w, http.StatusOK, ticket)
	return nil
}

func humanVerificationOperation(pattern string, r *http.Request) (purpose, target string) {
	switch pattern {
	case "POST /api/v1/group-binding/challenges":
		return "group_binding_create", ""
	case "POST /api/v1/group-binding/challenges/{id}/friend-verify":
		return "group_binding_friend", r.PathValue("id")
	case "POST /api/v1/group-binding/challenges/{id}/friend-approve",
		"POST /api/v1/group-binding/challenges/{id}/friend-reject":
		return "group_binding_friend", r.PathValue("id")
	case "POST /api/v1/group-binding/challenges/{id}/friend-send":
		return "group_binding_friend_send", r.PathValue("id")
	case "POST /api/v1/claims":
		return "group_claim", ""
	case "POST /api/v1/user-center/groups/{group}/demo-claim":
		return "operator_demo_claim", r.PathValue("group")
	}
	return "", ""
}

func (s *Server) requireHumanVerification(r *http.Request, p Principal, purpose, target string) error {
	ticket := r.Header.Get("X-Human-Verification")
	if ticket == "" {
		return fail(http.StatusBadRequest, "captcha_required")
	}
	scope, err := s.operationCaptchaScope(nil, r, p, captchaInput{
		Purpose: purpose, Platform: requestWorkspace(r), Target: target,
	}, false)
	if err != nil {
		return err
	}
	if err := s.captcha.Consume(scope, ticket); err != nil {
		return captchaFailure(err)
	}
	return nil
}

func onboardingVerificationInHandler(pattern string) bool {
	switch pattern {
	case "POST /api/v1/group-binding/challenges",
		"POST /api/v1/group-binding/challenges/{id}/friend-approve",
		"POST /api/v1/group-binding/challenges/{id}/friend-reject":
		return true
	}
	return false
}

func (s *Server) requireOnboardingVerification(r *http.Request, p Principal, service *control.GroupBindingService, ref control.SessionRef, in control.GroupBindingCreate, purpose, target string) error {
	// Verified legacy callers retain the existing service and final fences,
	// including read-only responses for an already submitted operation.
	if r.Header.Get("X-Human-Verification") != "" {
		return s.requireHumanVerification(r, p, purpose, target)
	}
	if p.Role == "superadmin" {
		exempt, err := service.ControlledVerificationExempt(r.Context(), ref, in, target)
		if err != nil {
			return err
		}
		if exempt {
			return nil
		}
	}
	return s.requireHumanVerification(r, p, purpose, target)
}
