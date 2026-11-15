package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

// Preparing a proof window does not send a message or establish identity.
func (s *Server) onboardingControlledProofRoutes(service *control.GroupBindingService) {
	s.handle("POST /api/v1/test-tools/onboarding-friend/challenges/{id}/proof-windows", true,
		s.onboardingControlledProofWindow(service))
}

func (s *Server) onboardingControlledProofWindow(service *control.GroupBindingService) endpoint {
	return func(w http.ResponseWriter, r *http.Request, p Principal) error {
		w.Header().Set("Cache-Control", "no-store")
		var in control.OnboardingControlledProofInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.CreateControlledProofWindow(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		send(w, http.StatusCreated, out)
		return nil
	}
}
