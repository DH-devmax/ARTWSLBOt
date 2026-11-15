package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) groupBindingContactRoutes(service *control.GroupBindingService) {
	s.handle("POST /api/v1/group-binding/challenges/{id}/friend-candidate", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingContactInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.PrepareContact(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/friend-approve", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingContactApproval
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		if err := s.requireOnboardingVerification(r, p, service, ref, control.GroupBindingCreate{}, "group_binding_friend", r.PathValue("id")); err != nil {
			return err
		}
		out, err := service.ApproveContact(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/friend-reject", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingContactApproval
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		if err := s.requireOnboardingVerification(r, p, service, ref, control.GroupBindingCreate{}, "group_binding_friend", r.PathValue("id")); err != nil {
			return err
		}
		out, err := service.RejectExpiredContact(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
}
