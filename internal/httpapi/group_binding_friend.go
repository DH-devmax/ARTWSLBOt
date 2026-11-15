package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) groupBindingFriendRoutes(service *control.GroupBindingService) {
	s.handle("GET /api/v1/group-binding/challenges/{id}/friend-verify", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.FriendState(r.Context(), ref, r.PathValue("id"))
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/friend-verify", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingFriendInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		_, err = service.VerifyFriend(r.Context(), ref, r.PathValue("id"), in)
		return err
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/friend-attempts/{operation}/reconcile", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.ReconcileFriend(r.Context(), ref, r.PathValue("id"), r.PathValue("operation"))
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("GET /api/v1/system/group-binding/friend-gates/{account}", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.FriendGate(r.Context(), ref, r.PathValue("account"))
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("PUT /api/v1/system/group-binding/friend-gates/{account}", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingFriendGateInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.SaveFriendGate(r.Context(), ref, r.PathValue("account"), in)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
}
