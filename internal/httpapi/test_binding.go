package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) authorizeTestBinding(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Account          string `json:"account_id"`
		Tenant           string `json:"tenant_id"`
		Version          int64  `json:"account_version"`
		ConfirmIsolated  bool   `json:"confirm_isolated"`
		IdentityVerified bool   `json:"identity_verified"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := control.AuthorizeTestBinding(r.Context(), s.DB.Platform, assistantRef(r), control.TestBindingRequest{
		Account: in.Account, Group: r.PathValue("id"), Tenant: in.Tenant, Version: in.Version,
		Platform: requestWorkspace(r), ConfirmIsolated: in.ConfirmIsolated, IdentityVerified: in.IdentityVerified,
	}); err != nil {
		return err
	}
	send(w, 201, map[string]any{"group_id": r.PathValue("id"), "admission_mode": "native_debug",
		"management_enabled": false, "ai_enabled": false, "native_ready": false})
	return nil
}
