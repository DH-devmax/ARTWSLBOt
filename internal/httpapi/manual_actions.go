package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) manualActionRoutes() {
	s.handle("GET /api/v1/groups/{id}/action-targets", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		items, err := s.ManualActions.Targets(r.Context(), assistantRef(r), r.PathValue("id"),
			r.URL.Query().Get("action"), r.URL.Query().Get("search"))
		if err != nil {
			return err
		}
		send(w, 200, map[string]any{"items": items})
		return nil
	})
	s.handle("POST /api/v1/groups/{id}/action-plans", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			Action      string `json:"action"`
			TargetToken string `json:"target_token"`
			Text        string `json:"text"`
			Minutes     int    `json:"minutes"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := s.ManualActions.Create(r.Context(), assistantRef(r), r.PathValue("id"), in.Action, in.TargetToken, in.Text, in.Minutes)
		if err != nil {
			return err
		}
		send(w, 201, out)
		return nil
	})
	s.handle("GET /api/v1/groups/{id}/action-plans/{plan}", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.ManualActions.Plan(r.Context(), assistantRef(r), r.PathValue("id"), r.PathValue("plan"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/groups/{id}/action-plans/{plan}/cancel", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.ManualActions.Cancel(r.Context(), assistantRef(r), r.PathValue("id"), r.PathValue("plan"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/groups/{id}/action-plans/{plan}/approve", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			Digest  string `json:"digest"`
			Version int64  `json:"version"`
			Key     string `json:"key"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		if in.Key == "" {
			in.Key = r.Header.Get("Idempotency-Key")
		}
		if !control.ValidID(r.PathValue("plan")) {
			return fail(404, "assistant_plan_not_found")
		}
		out, err := s.ManualActions.Approve(r.Context(), assistantRef(r), r.PathValue("id"), r.PathValue("plan"), in.Digest, in.Version, in.Key)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
}
