package httpapi

import (
	"errors"
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) issueClaimCode(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) != "wsl" && requestWorkspace(r) != "lajiaoliao" {
		return fail(403, "workspace_forbidden")
	}
	var in struct {
		AccountID string `json:"account_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !validUUID(in.AccountID) {
		return fail(400, "invalid_account")
	}
	code, expires, err := control.IssueClaimCodeForSession(r.Context(), s.DB.Platform, assistantRef(r), in.AccountID, r.PathValue("id"))
	if errors.Is(err, control.ErrFreshRole) || errors.Is(err, control.ErrLiveCode) || errors.Is(err, control.ErrOwned) {
		return fail(409, err.Error())
	}
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	send(w, 201, map[string]any{"code": code, "expires_at": expires, "grants_ownership": false})
	return nil
}
