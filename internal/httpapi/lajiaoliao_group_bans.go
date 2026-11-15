package httpapi

import (
	"net/http"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
)

func (s *Server) nativeGroupBans(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) != "lajiaoliao" {
		return fail(400, "lajiaoliao_action_unavailable")
	}
	group := r.URL.Query().Get("group_id")
	if !lajiaoliao.ValidID(group) {
		return fail(400, "lajiaoliao_invalid_identity")
	}
	var allowed bool
	if err := s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1
		AND platform='lajiaoliao' AND workspace='lajiaoliao')`, r.PathValue("id")).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return fail(404, "account_not_found")
	}
	if s.Receiver == nil {
		return fail(503, "native_read_owner_unavailable")
	}
	view, err := s.Receiver.GroupBans(r.Context(), r.PathValue("id"), group)
	if err != nil {
		return nativeSettingsError(w, err)
	}
	if err = s.memberResultAccess(r, p, gateway.MemberSnapshot{
		AccountEpoch: view.AccountEpoch, DeploymentVersion: view.DeploymentVersion}); err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	send(w, 200, map[string]any{"group_id": view.Group, "items": view.GroupBans,
		"complete": view.BansKnown, "sampled_at": view.SampledAt, "read_only": true})
	return nil
}
