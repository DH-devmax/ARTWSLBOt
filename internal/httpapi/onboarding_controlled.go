package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

// Separate administrative endpoints; these are never registered as model Tools.
func (s *Server) onboardingControlledRoutes(service *control.GroupBindingService) {
	s.onboardingControlledProofRoutes(service)
	s.handle("POST /api/v1/test-tools/onboarding-friend/windows", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.OnboardingControlledWindowInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.CreateOnboardingControlledWindow(r.Context(), ref, in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusCreated, out)
		return nil
	})
	s.handle("POST /api/v1/test-tools/onboarding-friend/challenges/{id}/friend-target", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.OnboardingControlledTargetInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.PrepareControlledOutboundFriend(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/test-tools/onboarding-friend/challenges/{id}/friend-send", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.OnboardingControlledConfirmation
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.SendControlledOutboundFriend(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
}
