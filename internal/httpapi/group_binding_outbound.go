package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) groupBindingOutboundRoutes(service *control.GroupBindingService) {
	s.onboardingControlledRoutes(service)
	// Retired routes have no body or captcha contract. The distinct wildcard
	// keeps the old submission captcha middleware from masking the terminal 410.
	for _, action := range []string{"friend-target", "friend-send"} {
		s.handle("POST /api/v1/group-binding/challenges/{retired}/"+action, false, retiredOutboundFriend)
	}
	s.handle("POST /api/v1/group-binding/challenges/{id}/outbound-attempts/{operation}/reconcile", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingConfirmation
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.ReconcileOutboundFriend(r.Context(), ref, r.PathValue("id"), r.PathValue("operation"), in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("GET /api/v1/system/group-binding/outbound-friend-gates/{account}", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.OutboundFriendGate(r.Context(), ref, r.PathValue("account"))
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("PUT /api/v1/system/group-binding/outbound-friend-gates/{account}", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingFriendGateInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.SaveOutboundFriendGate(r.Context(), ref, r.PathValue("account"), in)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
}

func retiredOutboundFriend(w http.ResponseWriter, r *http.Request, p Principal) error {
	w.Header().Set("Cache-Control", "no-store")
	return control.Denied(http.StatusGone, "group_binding_outbound_controlled_only")
}
