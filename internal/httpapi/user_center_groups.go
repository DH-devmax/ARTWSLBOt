package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

func entitlementView(e control.BusinessEntitlement) map[string]any {
	out := map[string]any{"mode": e.Mode, "name": e.Name, "active": e.Active, "unlimited": e.OperatorDemo,
		"billing_source": e.BillingSource, "modules": e.Modules, "permissions": e.Permissions}
	if e.OperatorDemo {
		for _, key := range []string{"group_limit", "member_limit", "assistant_limit", "credit_limit", "expires_at"} {
			out[key] = nil
		}
		out["platforms"], out["permanent"] = []string{"wsl", "lajiaoliao"}, true
	}
	return out
}

func (s *Server) currentWebsiteSession(r *http.Request, p Principal) error {
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return fail(401, "authentication_required")
	}
	var valid bool
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM sessions se JOIN users u ON u.id=se.user_id
		WHERE se.token_hash=$1 AND se.expires_at>clock_timestamp() AND u.id=$2 AND u.role=$3 AND NOT u.disabled
		AND coalesce(u.tenant_id::text,'')=$4)`, security.Digest(cookie.Value), p.ID, p.Role, p.TenantID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fail(401, "authentication_required")
	}
	return nil
}

func (s *Server) userCenterGroupData(r *http.Request, p Principal) (map[string]any, error) {
	platform, tenant, search := r.URL.Query().Get("platform"), r.URL.Query().Get("tenant_id"), strings.TrimSpace(r.URL.Query().Get("search"))
	view := r.URL.Query().Get("workspace")
	if view != "" && view != "mine" && view != "all" {
		return nil, fail(400, "invalid_workspace_filter")
	}
	if view == "all" && p.Role != "superadmin" {
		return nil, fail(403, "permission_denied")
	}
	if platform != "" && platform != "wsl" && platform != "lajiaoliao" || tenant != "" && !control.ValidID(tenant) || len(search) > 240 {
		return nil, fail(400, "invalid_group_filter")
	}
	if p.Role != "superadmin" && tenant != "" && tenant != p.TenantID {
		return nil, fail(403, "permission_denied")
	}
	fields, err := s.centerWorkspaceFields(r, p)
	if err != nil {
		return nil, err
	}
	if p.Role == "superadmin" && tenant == "" && view != "all" {
		tenant = fields["workspace"].(control.ResourceWorkspace).ID
	}
	limit, offset := 20, 0
	for key, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if raw := r.URL.Query().Get(key); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil {
				return nil, fail(400, "invalid_pagination")
			}
			*target = v
		}
	}
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fail(400, "invalid_pagination")
		}
		limit = v
	}
	if raw := r.URL.Query().Get("page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 100000 {
			return nil, fail(400, "invalid_pagination")
		}
		offset = (v - 1) * limit
	}
	if limit < 1 || limit > 50 || offset < 0 || offset > 5000000 {
		return nil, fail(400, "invalid_pagination")
	}
	e, err := control.ResolveHumanEntitlement(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, "wsl", "")
	if err != nil {
		return nil, err
	}
	items, customers := json.RawMessage("[]"), json.RawMessage("[]")
	total := 0
	if slices.Contains(e.Permissions, "groups:read") {
		// Scope is explicit even on the platform pool; a view/header grants nothing.
		filter := ` FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
			JOIN tenants t ON t.id=b.tenant_id JOIN accounts robot ON robot.id=b.primary_account_id
			LEFT JOIN group_chat_preferences c ON c.group_id=b.group_id
			LEFT JOIN group_business_runtime runtime ON runtime.group_id=b.group_id AND runtime.tenant_id=b.tenant_id
			LEFT JOIN group_binding_ownership ownership ON ownership.group_id=b.group_id
				AND ownership.tenant_id=b.tenant_id AND ownership.platform=g.platform
			WHERE b.active AND robot.platform=g.platform AND robot.workspace=g.platform AND robot.purpose='management'
			AND (t.space_kind='tenant_business' OR workspace_is_demo(t.id,$3::uuid))
			AND ($1 OR b.tenant_id=nullif($2,'')::uuid AND member_group_visible(b.tenant_id,b.group_id,$3::uuid))
			AND ($4='' OR g.platform=$4) AND ($5='' OR b.tenant_id=nullif($5,'')::uuid)
			AND ($6='' OR strpos(lower(g.name),lower($6))>0)
			AND (coalesce(ownership.proof_kind,'authenticated_message')<>'controlled_test'
				OR ownership.state='active' AND EXISTS(SELECT 1 FROM onboarding_test_scopes cs
					WHERE cs.id=ownership.controlled_scope_id AND cs.actor_id=$3::uuid
					AND cs.group_id=b.group_id AND cs.tenant_id=b.tenant_id AND cs.platform=g.platform
					AND cs.account_id=b.primary_account_id AND onboarding_test_scope_active(cs.id)))
			AND EXISTS(SELECT 1 FROM users u WHERE u.id=$3 AND NOT u.disabled
				AND ($1 AND u.role='superadmin' OR NOT $1 AND u.role='user' AND u.tenant_id=nullif($2,'')::uuid))`
		args := []any{p.Role == "superadmin", p.TenantID, p.ID, platform, tenant, search}
		if err = s.DB.Platform.QueryRow(r.Context(), `SELECT count(*)`+filter, args...).Scan(&total); err != nil {
			return nil, err
		}
		err = s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
			SELECT b.group_id,g.name,g.platform,b.tenant_id,t.name customer_name,b.active,b.version,
				b.ai_requested,b.ai_enabled,b.ai_request_state,b.management_enabled,
				coalesce(ownership.state,'unverified') ownership_state,
				CASE WHEN $1 THEN (SELECT jsonb_build_object('id',dc.id,'group_id',dc.group_id,
					'platform',dc.platform,'version',dc.version,'expires_at',dc.expires_at,'robot_role',dc.robot_role,
					'state',CASE WHEN dc.state IN ('active','checking') AND (dc.binding_epoch<>b.epoch OR
						dc.binding_version<>b.version OR dc.account_id<>robot.id OR dc.account_epoch<>robot.epoch OR
						dc.account_version<>robot.version OR dc.actor_version<>(SELECT access_version FROM users WHERE id=$3))
						THEN 'stale' WHEN dc.state IN ('active','checking') AND dc.expires_at<=clock_timestamp()
						THEN 'expired' ELSE dc.state END,'blocked_reason',dc.blocked_reason)
					FROM operator_demo_group_claims dc WHERE dc.actor_id=$3 AND dc.group_id=b.group_id
					AND dc.platform=g.platform ORDER BY dc.created_at DESC,dc.id LIMIT 1) ELSE NULL END demo_claim,
				coalesce(runtime.enabled,false) business_enabled,coalesce(runtime.version,0) runtime_version,
				CASE WHEN NOT coalesce(runtime.enabled,false) THEN 'group_runtime_disabled'
					WHEN NOT b.management_enabled THEN 'group_management_disabled' ELSE '' END customer_blocked_reason,
				b.primary_account_id,coalesce(nullif(robot.native_nickname,''),robot.name) robot_nickname,
				coalesce(c.activity,20) activity,coalesce(c.start_minute,0) start_minute,coalesce(c.end_minute,1439) end_minute,
				coalesce(c.monthly_token_limit,0) monthly_token_limit,c.persona_id,coalesce(c.version,0) preference_version
			`+filter+` ORDER BY g.platform,g.name,b.group_id LIMIT $7 OFFSET $8)x`, append(args, limit, offset)...).Scan(&items)
		if err != nil {
			return nil, err
		}
		if p.Role == "superadmin" {
			err = s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
				SELECT DISTINCT t.id,t.name FROM tenants t JOIN group_bindings b ON b.tenant_id=t.id
				WHERE b.active AND t.space_kind='tenant_business' ORDER BY t.name,t.id LIMIT 300)x`).Scan(&customers)
			if err != nil {
				return nil, err
			}
		}
		err = s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(x.row),'[]') FROM jsonb_array_elements($1::jsonb) x(row)
			JOIN group_bindings b ON b.group_id=(x.row->>'group_id')::uuid
				AND b.primary_account_id=(x.row->>'primary_account_id')::uuid AND b.version=(x.row->>'version')::bigint
			LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id
				AND o.tenant_id=b.tenant_id AND o.platform=x.row->>'platform'
			WHERE b.active AND ($2 OR b.tenant_id=nullif($3,'')::uuid AND member_group_visible(b.tenant_id,b.group_id,$4::uuid))
			AND (coalesce(o.proof_kind,'authenticated_message')<>'controlled_test'
				OR o.state='active' AND EXISTS(SELECT 1 FROM onboarding_test_scopes cs
					WHERE cs.id=o.controlled_scope_id AND cs.actor_id=$4::uuid AND cs.group_id=b.group_id
					AND cs.tenant_id=b.tenant_id AND cs.platform=o.platform
					AND cs.account_id=b.primary_account_id AND onboarding_test_scope_active(cs.id)))`,
			items, p.Role == "superadmin", p.TenantID, p.ID).Scan(&items)
		if err != nil {
			return nil, err
		}
	}
	if _, err = control.ResolveHumanEntitlement(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, "wsl", ""); err != nil {
		return nil, err
	}
	if err = s.currentWebsiteSession(r, p); err != nil {
		return nil, err
	}
	return mergeCenterFields(map[string]any{"items": items, "groups": items, "total": total, "offset": offset, "limit": limit,
		"customers": customers, "sampled_at": time.Now().UTC()}, fields), nil
}

func (s *Server) userCenterGroups(w http.ResponseWriter, r *http.Request, p Principal) error {
	out, err := s.userCenterGroupData(r, p)
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}

func (s *Server) userCenterUsage(w http.ResponseWriter, r *http.Request, p Principal) error {
	out, err := s.Gateway.UsageForCenter(r.Context(), assistantRef(r))
	if err != nil {
		return err
	}
	out["sampled_at"] = time.Now().UTC()
	fields, err := s.centerWorkspaceFields(r, p)
	if err != nil {
		return err
	}
	send(w, 200, mergeCenterFields(out, fields))
	return nil
}

func (s *Server) operatorUserCenter(w http.ResponseWriter, r *http.Request, p Principal) error {
	e, err := control.ResolveHumanEntitlement(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, "wsl", "")
	if err != nil {
		return err
	}
	groups, err := s.userCenterGroupData(r, p)
	if err != nil {
		return err
	}
	usage, err := s.Gateway.UsageForCenter(r.Context(), assistantRef(r))
	if err != nil {
		return err
	}
	var personas json.RawMessage
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
		SELECT p.account_id id,p.nickname name,a.platform,p.version FROM personas p JOIN accounts a ON a.id=p.account_id
		WHERE p.published AND a.purpose='chat' ORDER BY p.account_id LIMIT 10)x`).Scan(&personas)
	if err != nil {
		return err
	}
	operations := []map[string]any{}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		raw, readErr := control.SafeBusinessOperations(r.Context(), s.DB.Platform, principalActor(p), platform, "", 50)
		if readErr != nil {
			return readErr
		}
		var rows []map[string]any
		if err = json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		for _, row := range rows {
			source, _ := row["source"].(string)
			if source == "assistant" || source == "assistant_configuration" || source == "manual" || source == "controlled_test" {
				row["platform"] = platform
				operations = append(operations, row)
			}
		}
	}
	if err = s.currentWebsiteSession(r, p); err != nil {
		return err
	}
	fields, err := s.centerWorkspaceFields(r, p)
	if err != nil {
		return err
	}
	send(w, 200, mergeCenterFields(map[string]any{"platform_admin": true, "subscription": nil, "entitlement": entitlementView(e),
		"billing_source": e.BillingSource, "permissions": e.Permissions, "groups": groups["items"], "group_count": groups["total"],
		"active_group_count": groups["total"], "personas": personas, "claims": []any{}, "operations": operations, "usage": usage,
		"credit_wallet": nil, "native_ready": false, "ai_ready": false, "sampled_at": time.Now().UTC()}, fields))
	return nil
}
