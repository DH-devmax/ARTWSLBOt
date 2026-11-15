package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/features"
)

// Registration is owned by Server.routes; these handlers never activate a model
// or submit a native action.
func (s *Server) capabilityRoutes() {
	s.handle("GET /api/v1/capabilities/catalog", false, s.capabilityCatalog)
	s.handle("GET /api/v1/tenants/{id}/capabilities", false, s.tenantCapabilities)
	s.handle("PUT /api/v1/tenants/{id}/capabilities", true, s.saveTenantCapabilities)
	s.handle("GET /api/v1/capabilities/gates/{action}/{source}", true, s.businessGate)
	s.handle("PUT /api/v1/capabilities/gates/{action}/{source}", true, s.saveBusinessGate)
	s.handle("GET /api/v1/groups/{id}/business-runtime", false, s.businessRuntime)
	s.handle("PUT /api/v1/groups/{id}/business-runtime", false, s.saveBusinessRuntime)
	s.handle("GET /api/v1/groups/{id}/capabilities", false, s.groupCapabilities)
}

func (s *Server) capabilityCatalog(w http.ResponseWriter, r *http.Request, p Principal) error {
	platform := requestWorkspace(r)
	if platform != "wsl" && platform != "lajiaoliao" {
		return fail(400, "invalid_workspace")
	}
	send(w, 200, map[string]any{"platform": platform, "permissions": control.BusinessPermissionCatalog(),
		"actions": features.BusinessActions(platform), "assistant_templates": control.AssistantPermissionTemplates(),
		"gate_states": []string{"closed", "pilot", "open"}, "member_management": "owner_only",
		"native_ready": false, "ai_ready": false})
	return nil
}

func (s *Server) tenantCapabilities(w http.ResponseWriter, r *http.Request, p Principal) error {
	tenant := r.PathValue("id")
	if !control.ValidID(tenant) {
		return fail(404, "tenant_not_found")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var exists bool
	if err = tx.QueryRow(r.Context(), `SELECT true FROM tenants WHERE id=$1 FOR SHARE`, tenant).Scan(&exists); err == pgx.ErrNoRows {
		return fail(404, "tenant_not_found")
	} else if err != nil {
		return err
	}
	if p.Role != "superadmin" {
		if tenant != p.TenantID {
			return fail(404, "tenant_not_found")
		}
		if err = control.RequireTenantOwner(r.Context(), tx, principalActor(p)); err != nil {
			return err
		}
	}
	permissions, version := []string{}, int64(0)
	err = tx.QueryRow(r.Context(), `SELECT permissions,version FROM tenant_capability_ceilings WHERE tenant_id=$1`, tenant).Scan(&permissions, &version)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	send(w, 200, map[string]any{"tenant_id": tenant, "permissions": permissions, "ceiling": permissions,
		"permission_catalog": control.BusinessPermissionCatalog(), "version": version})
	return nil
}

func (s *Server) saveTenantCapabilities(w http.ResponseWriter, r *http.Request, p Principal) error {
	if p.Role != "superadmin" {
		return fail(403, "superadmin_required")
	}
	var in struct {
		Permissions []string `json:"permissions"`
		Version     int64    `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	tenant := r.PathValue("id")
	if !control.ValidID(tenant) || in.Version < 0 || !control.ValidateGrants(in.Permissions, control.BusinessPermissionCatalog()) {
		return fail(400, "invalid_capabilities")
	}
	if in.Permissions == nil {
		in.Permissions = []string{}
	}
	slices.Sort(in.Permissions)
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var exists bool
	if err = tx.QueryRow(r.Context(), `SELECT true FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&exists); err == pgx.ErrNoRows {
		return fail(404, "tenant_not_found")
	} else if err != nil {
		return err
	}
	var version int64
	err = tx.QueryRow(r.Context(), `INSERT INTO tenant_capability_ceilings(tenant_id,permissions,updated_by)
		SELECT $1,$2,$3 WHERE $4::bigint=0
		ON CONFLICT(tenant_id) DO UPDATE SET permissions=excluded.permissions,updated_by=excluded.updated_by,
			version=tenant_capability_ceilings.version+1,updated_at=clock_timestamp()
		WHERE tenant_capability_ceilings.version=$4 RETURNING version`, tenant, in.Permissions, p.ID, in.Version).Scan(&version)
	if err == pgx.ErrNoRows && in.Version > 0 {
		err = tx.QueryRow(r.Context(), `UPDATE tenant_capability_ceilings SET permissions=$2,updated_by=$3,
			version=version+1,updated_at=clock_timestamp() WHERE tenant_id=$1 AND version=$4 RETURNING version`,
			tenant, in.Permissions, p.ID, in.Version).Scan(&version)
	}
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	// The existing member trigger revokes sessions and cancels unsubmitted work.
	if _, err = tx.Exec(r.Context(), `UPDATE users SET access_version=access_version+1 WHERE tenant_id=$1`, tenant); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "tenant_capability_ceiling_changed", tenant,
		map[string]any{"version": version, "permissions": in.Permissions}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"tenant_id": tenant, "permissions": in.Permissions, "ceiling": in.Permissions, "version": version})
	return nil
}

func (s *Server) businessGate(w http.ResponseWriter, r *http.Request, p Principal) error {
	platform, action, source := requestWorkspace(r), r.PathValue("action"), r.PathValue("source")
	if _, ok := features.FindBusinessAction(platform, action, source); !ok {
		return fail(404, "business_capability_not_found")
	}
	var raw []byte
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT jsonb_build_object('platform',c.platform,'action',c.action,'source',c.source,
		'state',c.state,'version',c.version,'evidence_refs',c.evidence_refs,
		'pilot_group_ids',coalesce((SELECT jsonb_agg(p.group_id ORDER BY p.group_id) FROM business_capability_pilots p
		WHERE p.platform=c.platform AND p.action=c.action AND p.source=c.source AND p.gate_version=c.version),'[]'))
		FROM business_capability_gates c WHERE platform=$1 AND action=$2 AND source=$3`, platform, action, source).Scan(&raw)
	if err == pgx.ErrNoRows {
		send(w, 200, map[string]any{"platform": platform, "action": action, "source": source,
			"state": "closed", "version": 0, "pilot_group_ids": []string{}, "evidence_refs": []string{}})
		return nil
	}
	if err != nil {
		return err
	}
	send(w, 200, json.RawMessage(raw))
	return nil
}

func (s *Server) saveBusinessGate(w http.ResponseWriter, r *http.Request, p Principal) error {
	if p.Role != "superadmin" {
		return fail(403, "superadmin_required")
	}
	platform, action, source := requestWorkspace(r), r.PathValue("action"), r.PathValue("source")
	d, ok := features.FindBusinessAction(platform, action, source)
	if !ok {
		return fail(404, "business_capability_not_found")
	}
	var in struct {
		State    string   `json:"state"`
		Groups   []string `json:"pilot_group_ids"`
		Evidence []string `json:"evidence_refs"`
		Version  int64    `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 0 || !slices.Contains([]string{"closed", "pilot", "open"}, in.State) || len(in.Groups) > 300 ||
		(in.State == "pilot") != (len(in.Groups) > 0) || in.State != "closed" && (!d.Implemented || len(in.Evidence) == 0) ||
		!control.ValidBusinessEvidenceRefs(in.Evidence) {
		return fail(400, "invalid_business_gate")
	}
	if in.Evidence == nil {
		in.Evidence = []string{}
	}
	seen := map[string]bool{}
	for _, group := range in.Groups {
		if !control.ValidID(group) || seen[group] {
			return fail(400, "invalid_business_gate")
		}
		seen[group] = true
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var version int64
	err = tx.QueryRow(r.Context(), `INSERT INTO business_capability_gates(platform,action,source,state,updated_by,evidence_refs)
		SELECT $1,$2,$3,$4,$5,$7 WHERE $6::bigint=0 ON CONFLICT(platform,action,source) DO UPDATE
		SET state=excluded.state,version=business_capability_gates.version+1,updated_by=excluded.updated_by,
			evidence_refs=excluded.evidence_refs,updated_at=clock_timestamp() WHERE business_capability_gates.version=$6 RETURNING version`,
		platform, action, source, in.State, p.ID, in.Version, in.Evidence).Scan(&version)
	if err == pgx.ErrNoRows && in.Version > 0 {
		err = tx.QueryRow(r.Context(), `UPDATE business_capability_gates SET state=$4,version=version+1,
			updated_by=$5,evidence_refs=$7,updated_at=clock_timestamp() WHERE platform=$1 AND action=$2 AND source=$3 AND version=$6
			RETURNING version`, platform, action, source, in.State, p.ID, in.Version, in.Evidence).Scan(&version)
	}
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM business_capability_pilots WHERE platform=$1 AND action=$2 AND source=$3`,
		platform, action, source); err != nil {
		return err
	}
	for _, group := range in.Groups {
		var valid bool
		err = tx.QueryRow(r.Context(), `SELECT true FROM native_groups WHERE id=$1 AND platform=$2 FOR SHARE`, group, platform).Scan(&valid)
		if err == pgx.ErrNoRows {
			return fail(400, "business_pilot_platform_mismatch")
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO business_capability_pilots(platform,action,source,group_id,gate_version)
			VALUES($1,$2,$3,$4,$5)`, platform, action, source, group, version); err != nil {
			return err
		}
	}
	if err = auditTx(r.Context(), tx, p, "", "business_capability_gate_changed", platform+":"+action+":"+source,
		map[string]any{"state": in.State, "version": version, "pilot_count": len(in.Groups), "evidence_refs": in.Evidence}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	if in.Groups == nil {
		in.Groups = []string{}
	}
	send(w, 200, map[string]any{"platform": platform, "action": action, "source": source,
		"state": in.State, "pilot_group_ids": in.Groups, "evidence_refs": in.Evidence, "version": version})
	return nil
}

func (s *Server) businessRuntime(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := control.RequireGroup(r.Context(), s.DB.Platform, principalActor(p), r.PathValue("id")); err != nil {
		return err
	}
	permissions, err := s.permissions(r.Context(), p)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, "groups:read") {
		return fail(403, "permission_denied")
	}
	var enabled, management bool
	var tenant string
	var version, bindingVersion, bindingEpoch int64
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT b.tenant_id::text,coalesce(r.enabled,false),coalesce(r.version,0),
		b.management_enabled,b.version,b.epoch
		FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
		LEFT JOIN group_business_runtime r ON r.group_id=b.group_id AND r.tenant_id=b.tenant_id
		WHERE b.group_id=$1 AND b.active AND g.platform=$2`, r.PathValue("id"), requestWorkspace(r)).
		Scan(&tenant, &enabled, &version, &management, &bindingVersion, &bindingEpoch)
	if err == pgx.ErrNoRows {
		return fail(404, "group_not_found")
	} else if err != nil {
		return err
	}
	canManage := p.Role == "superadmin"
	if !canManage {
		err = control.RequireTenantOwner(r.Context(), s.DB.Platform, principalActor(p))
		var denied *control.Failure
		if err != nil && !errors.As(err, &denied) {
			return err
		}
		canManage = err == nil
	}
	canManage = canManage && slices.Contains(permissions, "groups:configure")
	if canManage {
		service, serviceErr := control.WorkspaceServiceEntitlement(r.Context(), s.DB.Platform, tenant)
		var denied *control.Failure
		if serviceErr != nil && !errors.As(serviceErr, &denied) {
			return serviceErr
		}
		canManage = serviceErr == nil
		if canManage && service.Demo {
			canManage = p.Role == "superadmin" && p.TenantID == "" && p.ID == service.OwnerID
		} else if canManage {
			err = control.RequireTenantCapability(r.Context(), s.DB.Platform, tenant, "groups:configure")
			if err != nil && !errors.As(err, &denied) {
				return err
			}
			canManage = err == nil
		}
	}
	blocked := ""
	if !enabled {
		blocked = "group_runtime_disabled"
	} else if !management {
		blocked = "group_management_disabled"
	}
	if err = control.RequireGroup(r.Context(), s.DB.Platform, principalActor(p), r.PathValue("id")); err != nil {
		return err
	}
	send(w, 200, map[string]any{"group_id": r.PathValue("id"), "tenant_id": tenant, "enabled": enabled,
		"management_enabled": management, "version": version, "binding_version": bindingVersion, "binding_epoch": bindingEpoch,
		"can_manage": canManage, "blocked_reason": blocked, "sampled_at": time.Now().UTC()})
	return nil
}

func (s *Server) groupCapabilities(w http.ResponseWriter, r *http.Request, p Principal) error {
	group, platform := r.PathValue("id"), requestWorkspace(r)
	if err := control.RequireGroup(r.Context(), s.DB.Platform, principalActor(p), group); err != nil {
		return err
	}
	permissions, err := s.permissions(r.Context(), p)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, "groups:read") {
		return fail(403, "permission_denied")
	}
	var tenant string
	var ceiling []string
	var enabled, owner, management bool
	var version, bindingVersion, bindingEpoch int64
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT b.tenant_id::text,coalesce(c.permissions,'{}'),
		coalesce(r.enabled,false),coalesce(r.version,0),
		EXISTS(SELECT 1 FROM users u WHERE u.id=$3 AND u.id=t.owner_id AND u.tenant_id=t.id
			AND u.tenant_role='owner' AND u.role='user' AND NOT u.disabled AND $4='user'),b.management_enabled,b.version,b.epoch
		FROM group_bindings b JOIN tenants t ON t.id=b.tenant_id JOIN native_groups g ON g.id=b.group_id
		LEFT JOIN tenant_capability_ceilings c ON c.tenant_id=b.tenant_id
		LEFT JOIN group_business_runtime r ON r.group_id=b.group_id AND r.tenant_id=b.tenant_id
		WHERE b.group_id=$1 AND b.active AND g.platform=$2`, group, platform, p.ID, p.Role).
		Scan(&tenant, &ceiling, &enabled, &version, &owner, &management, &bindingVersion, &bindingEpoch)
	if err != nil {
		return err
	}
	if p.Role != "superadmin" {
		permissions = slices.DeleteFunc(permissions, func(code string) bool { return !slices.Contains(ceiling, code) })
	}
	entitlement, err := control.ResolveHumanEntitlement(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, platform, group)
	if err != nil {
		return err
	}
	actions := features.AssistantActions(platform)
	manual := features.AssistantActions(platform)
	for i := range actions {
		actions[i], err = control.ScopedHumanAction(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, platform, group, actions[i].ID, "assistant")
		if err != nil {
			return err
		}
		if !slices.Contains(permissions, "assistant:action_propose") || !slices.Contains(permissions, actions[i].Permission) {
			actions[i].Production, actions[i].BlockReason = false, "permission_denied"
		}
		manual[i], err = control.ScopedHumanAction(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, platform, group, manual[i].ID, "manual")
		if err != nil {
			return err
		}
	}
	blocked := ""
	if !enabled {
		blocked = "group_runtime_disabled"
	} else if !management {
		blocked = "group_management_disabled"
	}
	if err = control.RequireGroup(r.Context(), s.DB.Platform, principalActor(p), group); err != nil {
		return err
	}
	send(w, 200, map[string]any{"group_id": group, "tenant_id": tenant, "platform": platform,
		"permissions": permissions, "ceiling": ceiling, "enabled": enabled, "version": version,
		"management_enabled": management,
		"binding_version":    bindingVersion, "binding_epoch": bindingEpoch,
		"permission_catalog": control.BusinessPermissionCatalog(),
		"can_manage":         (owner || p.Role == "superadmin") && slices.Contains(permissions, "groups:configure"),
		"blocked_reason":     blocked, "actions": actions, "manual_actions": manual,
		"execution_mode": entitlement.Mode, "entitlement": entitlementView(entitlement),
		"customer_runtime": map[string]any{"enabled": enabled, "management_enabled": management, "blocked_reason": blocked},
		"sampled_at":       time.Now().UTC()})
	return nil
}

func (s *Server) saveBusinessRuntime(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Enabled bool  `json:"enabled"`
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 0 {
		return fail(400, "invalid_business_runtime")
	}
	ref, err := onboardingWebsiteRef(r)
	if err != nil {
		return err
	}
	out, err := s.GroupBinding.SaveBusinessRuntime(r.Context(), ref, r.PathValue("id"), in.Enabled, in.Version)
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}
