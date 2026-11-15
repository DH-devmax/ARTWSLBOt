package httpapi

import (
	"net/http"

	"wangshangliao/internal/control"
)

func (s *Server) previewGroupFeatures(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in control.FeaturePreviewInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := control.ValidateFeaturePreview(in); err != nil {
		return err
	}
	actor, group, platform := principalActor(p), r.PathValue("id"), requestWorkspace(r)
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	out, err := control.PreviewFeaturesTx(r.Context(), tx, actor, tenantPermissions, platform, group, in)
	if err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	if err = s.currentWebsiteSession(r, p); err != nil {
		return err
	}
	if err = control.RequireHumanGroupOwnership(r.Context(), s.DB.Platform, actor, platform, group); err != nil {
		return err
	}
	var unchanged bool
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id
		WHERE b.group_id=$1 AND b.active AND b.version=$2 AND g.platform=$3)`,
		group, in.Version, platform).Scan(&unchanged)
	if err != nil {
		return err
	}
	if !unchanged {
		return fail(409, "version_conflict")
	}
	send(w, http.StatusOK, out)
	return nil
}
