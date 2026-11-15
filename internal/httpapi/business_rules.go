package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) businessRulesRoutes(service *control.BusinessRulesService) {
	s.handle("GET /api/v1/groups/{id}/business-operations", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		operations, err := service.Operations(r.Context(), principalActor(p), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, http.StatusOK, operations)
		return nil
	})
	s.handle("POST /api/v1/groups/{id}/business-operations/{job}/reconcile", false,
		func(w http.ResponseWriter, r *http.Request, p Principal) error {
			if err := service.Reconcile(r.Context(), principalActor(p), r.PathValue("id"), r.PathValue("job")); err != nil {
				return err
			}
			send(w, http.StatusOK, map[string]bool{"reconciled": true})
			return nil
		})
}
