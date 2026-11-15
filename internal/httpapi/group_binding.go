package httpapi

import (
	"net/http"
	"strconv"

	"wangshangliao/internal/control"
)

// The caller owns one service and its background scanner. Registering routes
// does not start another scan loop or create upstream requests.
func (s *Server) groupBindingRoutes(service *control.GroupBindingService) {
	s.groupBindingFriendRoutes(service)
	s.groupBindingContactRoutes(service)
	s.groupBindingOutboundRoutes(service)
	s.controlledOnboardingRoutes(service)
	s.handle("GET /api/v1/group-binding/context", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		offset, limit := 0, 20
		for name, target := range map[string]*int{"offset": &offset, "limit": &limit} {
			if value := r.URL.Query().Get(name); value != "" {
				parsed, err := strconv.Atoi(value)
				if err != nil {
					return fail(http.StatusBadRequest, "group_binding_context_invalid")
				}
				*target = parsed
			}
		}
		out, err := service.Context(r.Context(), ref, r.URL.Query().Get("platform"), r.URL.Query().Get("search"), offset, limit)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("GET /api/v1/user-center/groups/{group}/demo-claim", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.OperatorDemoClaim(r.Context(), ref, r.PathValue("group"))
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/user-center/groups/{group}/demo-claim", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			BindingVersion int64 `json:"binding_version"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.CreateOperatorDemoClaim(r.Context(), ref, r.PathValue("group"), in.BindingVersion, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/user-center/groups/{group}/demo-claim/{id}/cancel", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
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
		out, err := service.CancelOperatorDemoClaim(r.Context(), ref, r.PathValue("group"), r.PathValue("id"), in.Version)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingCreate
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		if err := s.requireOnboardingVerification(r, p, service, ref, in, "group_binding_create", ""); err != nil {
			return err
		}
		out, err := service.Create(r.Context(), ref, in)
		if err != nil {
			return err
		}
		send(w, http.StatusCreated, out)
		return nil
	})
	s.handle("GET /api/v1/group-binding/challenges/{id}", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.Read(r.Context(), ref, r.PathValue("id"))
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/confirm", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingConfirmation
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.Confirm(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/cancel", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
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
		out, err := service.Cancel(r.Context(), ref, r.PathValue("id"), in.Version)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/review", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingReview
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.Review(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/transfer-review", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingTransferReview
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.ReviewTransfer(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/invitation-prepare", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingAcceptInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		out, err := service.PrepareAccept(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/invitation-accept", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.GroupBindingAcceptInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		ref, err := onboardingWebsiteRef(r)
		if err != nil {
			return err
		}
		in.Key = r.Header.Get("Idempotency-Key")
		out, err := service.ConfirmAccept(r.Context(), ref, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		send(w, http.StatusOK, out)
		return nil
	})
	s.handle("POST /api/v1/group-binding/challenges/{id}/accept-attempts/{operation}/reconcile", false,
		func(w http.ResponseWriter, r *http.Request, p Principal) error {
			ref, err := onboardingWebsiteRef(r)
			if err != nil {
				return err
			}
			out, err := service.ReconcileAccept(r.Context(), ref, r.PathValue("id"), r.PathValue("operation"))
			if err != nil {
				return err
			}
			send(w, http.StatusOK, out)
			return nil
		})
}
