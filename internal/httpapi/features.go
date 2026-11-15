package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/features"
)

func (s *Server) featureCatalog(w http.ResponseWriter, r *http.Request, p Principal) error {
	platform := requestWorkspace(r)
	send(w, 200, map[string]any{"items": features.CatalogForPlatform(platform), "native_capabilities": features.NativeCatalogForPlatform(platform),
		"native_details":        features.NativeDetailsForPlatform(platform),
		"native_usage":          features.NativeUsageForPlatform(platform),
		"native_demonstrations": features.NativeDemonstrationsForPlatform(platform),
		"native_acceptance":     features.NativeAcceptanceForPlatform(platform), "platform": platform, "native_ready": false})
	return nil
}

func (s *Server) groupFeatures(w http.ResponseWriter, r *http.Request, p Principal) error {
	var tenant, account, platform string
	var active, detached bool
	var raw []byte
	var version int64
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT b.tenant_id::text,coalesce(b.primary_account_id::text,''),
        b.active,b.primary_account_id IS NULL,b.config,b.version,g.platform FROM group_bindings b
        JOIN native_groups g ON g.id=b.group_id WHERE b.group_id=$1`,
		r.PathValue("id")).Scan(&tenant, &account, &active, &detached, &raw, &version, &platform)
	if err == pgx.ErrNoRows || err == nil && p.Role != "superadmin" && tenant != p.TenantID {
		return fail(404, "group_not_found")
	}
	if err != nil {
		return err
	}
	var config features.Config
	if err = json.Unmarshal(raw, &config); err != nil {
		return err
	}
	entitlement, err := control.ResolveHumanEntitlement(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, platform, r.PathValue("id"))
	if err != nil {
		return err
	}
	grants, err := s.permissions(r.Context(), p)
	if err != nil {
		return err
	}
	groupAccess, err := control.ResolveHumanGroupAccess(r.Context(), s.DB.Platform, principalActor(p), platform, r.PathValue("id"))
	if err != nil {
		return err
	}
	if p.Role != "superadmin" && groupAccess.AccessMode != "manage" {
		return fail(409, groupAccess.BlockedReason)
	}
	rows := []map[string]any{}
	for _, definition := range features.CatalogForPlatform(platform) {
		editable := []string{}
		entitled := slices.Contains(entitlement.Modules, definition.Module)
		for _, field := range definition.Fields {
			if groupAccess.AccessMode == "manage" && active && !detached && entitled && (p.Role == "superadmin" || features.CanEdit(grants, definition, field.Key)) {
				editable = append(editable, field.Key)
			}
		}
		rows = append(rows, map[string]any{"definition": definition, "values": features.Values(config, definition.ID),
			"editable_fields": editable, "entitled": entitled, "effective_enabled": false,
			"native_blockers": features.NativeBlockersForPlatform(platform, definition)})
	}
	if err = control.RequireGroup(r.Context(), s.DB.Platform, principalActor(p), r.PathValue("id")); err != nil {
		return err
	}
	var unchanged bool
	if err = s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM group_bindings WHERE group_id=$1
		AND active AND primary_account_id=nullif($2,'')::uuid AND version=$3)`, r.PathValue("id"), account, version).Scan(&unchanged); err != nil {
		return err
	}
	if !unchanged {
		return fail(409, "version_conflict")
	}
	if err = s.currentWebsiteSession(r, p); err != nil {
		return err
	}
	currentAccess, err := control.ResolveHumanGroupAccess(r.Context(), s.DB.Platform, principalActor(p), platform, r.PathValue("id"))
	if err != nil {
		return err
	}
	if currentAccess.AccessMode != groupAccess.AccessMode || currentAccess.BlockedReason != groupAccess.BlockedReason {
		return fail(409, "group_access_changed")
	}
	send(w, 200, map[string]any{"group_id": r.PathValue("id"), "account_id": account,
		"version": version, "active": active, "platform": platform, "group_access": groupAccess, "items": rows, "native_capabilities": features.NativeCatalogForPlatform(platform),
		"native_details":        features.NativeDetailsForPlatform(platform),
		"native_usage":          features.NativeUsageForPlatform(platform),
		"native_demonstrations": features.NativeDemonstrationsForPlatform(platform),
		"native_acceptance":     features.NativeAcceptanceForPlatform(platform), "native_ready": false})
	return nil
}

func (s *Server) saveGroupFeatures(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64                                 `json:"version"`
		Changes map[string]map[string]json.RawMessage `json:"changes"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 1 || len(in.Changes) == 0 || len(in.Changes) > len(features.Catalog()) {
		return fail(400, "invalid_feature_settings")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	result, err := control.SaveFeaturesTx(r.Context(), tx, control.Actor{ID: p.ID, TenantID: p.TenantID, Role: p.Role},
		tenantPermissions, r.PathValue("id"), in.Version, in.Changes, false)
	if err != nil {
		if e, ok := err.(*control.Failure); ok {
			return fail(e.Status, e.Code)
		}
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, result)
	return nil
}
