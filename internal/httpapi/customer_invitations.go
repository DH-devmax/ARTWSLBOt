package httpapi

import (
	"net"
	"net/http"
	"strings"
	"time"

	"wangshangliao/internal/control"
)

func (s *Server) customerInvitationRoutes() {
	service := &control.CustomerInvitationService{DB: s.DB, Catalog: tenantPermissions}
	s.handle("POST /api/v1/customer-invitations", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.CustomerInvitationCreate
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.Create(r.Context(), ref, in)
		if err != nil {
			return err
		}
		send(w, http.StatusCreated, out)
		return nil
	})
	s.handle("POST /api/v1/customer-invitations/{id}/revoke", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		if err = service.Revoke(r.Context(), ref, r.PathValue("id")); err != nil {
			return err
		}
		send(w, http.StatusOK, map[string]string{"state": "revoked"})
		return nil
	})
	s.handle("POST /api/v1/customer-invitations/accept", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			Token string `json:"token"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.AcceptExisting(r.Context(), ref, in.Token)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.mux.HandleFunc("POST /api/v1/customer-invitations/preview", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if err := s.onboardingPublicRequest(r); err != nil {
			sendError(w, err)
			return
		}
		if err := decode(w, r, &in); err != nil {
			sendError(w, err)
			return
		}
		out, err := service.Preview(r.Context(), in.Token)
		if err != nil {
			sendError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
	})
	s.mux.HandleFunc("POST /api/v1/customer-invitations/activate", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token    string `json:"token"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := s.onboardingPublicRequest(r); err != nil {
			sendError(w, err)
			return
		}
		if err := decode(w, r, &in); err != nil {
			sendError(w, err)
			return
		}
		select {
		case s.loginSlots <- struct{}{}:
			defer func() { <-s.loginSlots }()
		default:
			sendError(w, fail(http.StatusTooManyRequests, "authentication_busy"))
			return
		}
		out, err := service.ActivateNew(r.Context(), in.Token, in.Username, in.Password)
		in.Password, in.Token = "", ""
		if err != nil {
			sendError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusCreated, out)
	})
}

func onboardingWebsiteRef(r *http.Request) (control.SessionRef, error) {
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return control.SessionRef{}, fail(http.StatusUnauthorized, "authentication_required")
	}
	ref, err := control.WebsiteSession(cookie.Value)
	if err != nil {
		return ref, fail(http.StatusUnauthorized, "authentication_required")
	}
	return ref.InPlatform(requestWorkspace(r)), nil
}

// Public activation has no existing website session or CSRF cookie. Origin and
// the shared bounded authentication limiter apply before expensive password work.
func (s *Server) onboardingPublicRequest(r *http.Request) error {
	if r.Header.Get("Origin") != s.Origin || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fail(http.StatusForbidden, "origin_denied")
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	now := time.Now()
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	for key, attempt := range s.loginAttempts {
		if now.After(attempt.until) {
			delete(s.loginAttempts, key)
		}
	}
	key := "customer-onboarding:" + ip
	if s.loginAttempts[key] == nil {
		if len(s.loginAttempts) >= 10000 {
			return fail(http.StatusTooManyRequests, "authentication_busy")
		}
		s.loginAttempts[key] = &loginAttempt{until: now.Add(10 * time.Minute)}
	}
	attempt := s.loginAttempts[key]
	attempt.count++
	if attempt.count > 20 {
		return fail(http.StatusTooManyRequests, "rate_limited")
	}
	return nil
}
