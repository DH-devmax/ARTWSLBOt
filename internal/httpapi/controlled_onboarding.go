package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) controlledOnboardingRoutes(service *control.GroupBindingService) {
	s.handle("GET /api/v1/test-tools/onboarding/scopes", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.ControlledScopes(r.Context(), ref, r.URL.Query().Get("platform"))
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/test-tools/onboarding/scopes", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.ControlledOnboardingCreate
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.CreateControlledScope(r.Context(), ref, in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusCreated, out)
		return nil
	})
	s.handle("GET /api/v1/test-tools/onboarding/scopes/{id}", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.ReadControlledScope(r.Context(), ref, r.PathValue("id"))
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/test-tools/onboarding/scopes/{id}/close", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			Version int64 `json:"version"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.CloseControlledScope(r.Context(), ref, r.PathValue("id"), in.Version)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
}
