package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) platformAssistantRoutes() {
	service := &control.PlatformAssistant{Gateway: s.Gateway}
	s.handle("GET /api/v1/platform-assistant", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		settings, err := service.Settings(r.Context(), assistantRef(r))
		if err != nil {
			return err
		}
		facts, err := service.Snapshot(r.Context(), assistantRef(r))
		if err != nil {
			return err
		}
		send(w, 200, map[string]any{"settings": settings, "snapshot": facts, "consumer": "platform_assistant",
			"read_only": true, "tools": []string{"platform_health"}})
		return nil
	})
	s.handle("PUT /api/v1/platform-assistant/settings", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.PlatformAssistantSettings
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := service.SaveSettings(r.Context(), assistantRef(r), in)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/platform-assistant/sessions", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		// Empty body contract prevents client-selected tenant, toolset or management scope.
		var in struct{}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := service.CreateSession(r.Context(), assistantRef(r))
		if err != nil {
			return err
		}
		send(w, 201, out)
		return nil
	})
	s.handle("GET /api/v1/platform-assistant/sessions/{id}/turns", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := service.Turns(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/platform-assistant/sessions/{id}/turns", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.PlatformTurnInput
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := service.Generate(r.Context(), assistantRef(r), r.PathValue("id"), in)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/platform-assistant/sessions/{id}/turns/{key}/cancel", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		if err := service.Cancel(r.Context(), assistantRef(r), r.PathValue("id"), r.PathValue("key")); err != nil {
			return err
		}
		send(w, 200, map[string]bool{"cancel_requested": true})
		return nil
	})
}
