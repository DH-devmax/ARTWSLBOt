package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

func principalActor(p Principal) control.Actor {
	return control.Actor{ID: p.ID, TenantID: p.TenantID, Role: p.Role}
}

func (s *Server) memberRoutes() {
	s.handle("GET /api/v1/members", false, s.members)
	s.handle("POST /api/v1/members", false, s.createMember)
	s.handle("PUT /api/v1/members/{id}", false, s.updateMember)
	s.handle("GET /api/v1/member-roles", false, s.memberRoles)
	s.handle("PUT /api/v1/member-roles/{code}", false, s.saveMemberRole)
	s.handle("POST /api/v1/tenants/{id}/owner", true, s.confirmTenantOwner)
}

func (s *Server) memberTx(r *http.Request, p Principal) (pgx.Tx, error) {
	if p.Role != "user" {
		return nil, fail(403, "tenant_owner_required")
	}
	tx, _, err := s.scopedMemberTx(r, p)
	return tx, err
}

func (s *Server) scopedMemberTx(r *http.Request, p Principal) (pgx.Tx, Principal, error) {
	tenant := r.URL.Query().Get("tenant_id")
	if p.Role == "superadmin" {
		if !control.ValidID(tenant) {
			return nil, p, fail(400, "tenant_scope_required")
		}
		p.TenantID = tenant
	} else if p.Role != "user" || !control.ValidID(p.TenantID) {
		return nil, p, fail(403, "tenant_member_required")
	} else if tenant != "" && tenant != p.TenantID {
		return nil, p, fail(403, "tenant_scope_mismatch")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return nil, p, err
	}
	ok := false
	defer func() {
		if !ok {
			tx.Rollback(r.Context())
		}
	}()
	var exists bool
	if err = tx.QueryRow(r.Context(), `SELECT true FROM tenants WHERE id=$1 FOR UPDATE`, p.TenantID).Scan(&exists); err == pgx.ErrNoRows {
		return nil, p, fail(404, "tenant_not_found")
	} else if err != nil {
		return nil, p, err
	}
	if p.Role == "superadmin" {
		_, err = control.LiveActorPermissions(r.Context(), tx, control.Actor{ID: p.ID, Role: p.Role}, tenantPermissions)
	} else {
		err = control.RequireMemberManager(r.Context(), tx, principalActor(p), tenantPermissions)
	}
	if err != nil {
		return nil, p, err
	}
	ok = true
	return tx, p, nil
}

func (s *Server) members(w http.ResponseWriter, r *http.Request, p Principal) error {
	tx, p, err := s.scopedMemberTx(r, p)
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var data []byte
	err = tx.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
        SELECT u.id,u.username,u.tenant_role,u.disabled,u.access_version,
        coalesce((SELECT jsonb_agg(m.group_id) FROM member_groups m WHERE m.user_id=u.id),'[]') groups,
        coalesce((SELECT jsonb_agg(jsonb_build_object('permission',o.permission,'effect',o.effect,'expires_at',o.expires_at))
            FROM member_overrides o WHERE o.user_id=u.id),'[]') overrides
        FROM users u WHERE tenant_id=$1 ORDER BY created_at) x`, p.TenantID).Scan(&data)
	if err != nil {
		return err
	}
	send(w, 200, json.RawMessage(data))
	return nil
}

type memberChange struct {
	Username  string   `json:"username"`
	Password  string   `json:"password"`
	Role      string   `json:"role"`
	Disabled  bool     `json:"disabled"`
	Version   int64    `json:"version"`
	Groups    []string `json:"groups"`
	Overrides []struct {
		Permission string     `json:"permission"`
		Effect     string     `json:"effect"`
		ExpiresAt  *time.Time `json:"expires_at"`
	} `json:"overrides"`
}

func (s *Server) createMember(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.writeMember(w, r, p, true)
}
func (s *Server) updateMember(w http.ResponseWriter, r *http.Request, p Principal) error {
	return s.writeMember(w, r, p, false)
}
func (s *Server) writeMember(w http.ResponseWriter, r *http.Request, p Principal, create bool) error {
	var in memberChange
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !control.ValidRoleCode(in.Role) || in.Role == "owner" || in.Role == "legacy" || len(in.Groups) > 300 || len(in.Overrides) > len(tenantPermissions) {
		return fail(400, "invalid_member")
	}
	if create && (len(strings.TrimSpace(in.Username)) < 3 || len(in.Username) > 80 || in.Disabled) {
		return fail(400, "invalid_member")
	}
	hash := ""
	if create || in.Password != "" {
		var err error
		hash, err = security.Password(in.Password)
		if err != nil {
			return fail(400, "password_too_short")
		}
	}
	tx, p, err := s.scopedMemberTx(r, p)
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var exists bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenant_roles WHERE tenant_id=$1 AND code=$2)`,
		p.TenantID, in.Role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fail(400, "invalid_role")
	}
	id := r.PathValue("id")
	version := int64(1)
	disabled := false
	if !create {
		var role string
		err = tx.QueryRow(r.Context(), `SELECT tenant_role,disabled,access_version FROM users WHERE id=$1 AND tenant_id=$2 FOR UPDATE`,
			id, p.TenantID).Scan(&role, &disabled, &version)
		if err == pgx.ErrNoRows {
			return fail(404, "member_not_found")
		}
		if err != nil {
			return err
		}
		if role == "owner" || id == p.ID {
			return fail(409, "owner_or_self_protected")
		}
		if version != in.Version {
			return fail(409, "version_conflict")
		}
	}
	if create || disabled && !in.Disabled {
		if err = control.CheckMemberSeats(r.Context(), tx, p.TenantID); err != nil {
			return err
		}
	}
	for _, group := range in.Groups {
		if !validUUID(group) {
			return fail(400, "invalid_resource_id")
		}
		if err = control.RequireGroup(r.Context(), tx, principalActor(p), group); err != nil {
			return err
		}
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM group_bindings WHERE group_id=$1 AND tenant_id=$2 AND active)`, group, p.TenantID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fail(404, "group_not_found")
		}
	}
	seen := map[string]bool{}
	for _, o := range in.Overrides {
		if seen[o.Permission] || !control.ValidateGrants([]string{o.Permission}, tenantPermissions) || (o.Effect != "allow" && o.Effect != "deny") ||
			o.ExpiresAt != nil && !o.ExpiresAt.After(time.Now()) {
			return fail(400, "invalid_permissions")
		}
		seen[o.Permission] = true
		if o.Effect == "allow" {
			if err = control.RequireTenantCapability(r.Context(), tx, p.TenantID, o.Permission); err != nil {
				return err
			}
		}
	}
	if create {
		id = control.NewID()
		tag, err := tx.Exec(r.Context(), `INSERT INTO users(id,tenant_id,username,password_hash,role,tenant_role)
            VALUES($1,$2,$3,$4,'user',$5) ON CONFLICT(username) DO NOTHING`, id, p.TenantID, strings.TrimSpace(in.Username), hash, in.Role)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(409, "username_exists")
		}
	} else {
		if _, err = tx.Exec(r.Context(), `DELETE FROM user_access WHERE user_id=$1`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `UPDATE users SET disabled=$2,tenant_role=$3,
            password_hash=CASE WHEN $4='' THEN password_hash ELSE $4 END,access_version=access_version+1 WHERE id=$1`,
			id, in.Disabled, in.Role, hash); err != nil {
			return err
		}
		version++
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM member_groups WHERE user_id=$1`, id); err != nil {
		return err
	}
	for _, group := range in.Groups {
		if _, err = tx.Exec(r.Context(), `INSERT INTO member_groups(user_id,tenant_id,group_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, p.TenantID, group); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM member_overrides WHERE user_id=$1`, id); err != nil {
		return err
	}
	for _, o := range in.Overrides {
		if _, err = tx.Exec(r.Context(), `INSERT INTO member_overrides(user_id,permission,effect,expires_at) VALUES($1,$2,$3,$4)`,
			id, o.Permission, o.Effect, o.ExpiresAt); err != nil {
			return err
		}
	}
	if err = auditTx(r.Context(), tx, p, p.TenantID, "tenant_member_changed", id, map[string]any{"created": create, "version": version, "disabled": in.Disabled, "role": in.Role}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	status := 200
	if create {
		status = 201
	}
	send(w, status, map[string]any{"id": id, "version": version})
	return nil
}

func (s *Server) memberRoles(w http.ResponseWriter, r *http.Request, p Principal) error {
	tx, p, err := s.scopedMemberTx(r, p)
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var data []byte
	if err = tx.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
        SELECT code,name,permissions,version FROM tenant_roles WHERE tenant_id=$1 ORDER BY code) x`, p.TenantID).Scan(&data); err != nil {
		return err
	}
	var ceiling []string
	if err = tx.QueryRow(r.Context(), `SELECT permissions FROM tenant_capability_ceilings WHERE tenant_id=$1`, p.TenantID).Scan(&ceiling); err != nil && err != pgx.ErrNoRows {
		return err
	}
	if ceiling == nil {
		ceiling = []string{}
	}
	var groups json.RawMessage
	err = tx.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY x.platform,x.group_id),'[]') FROM (
		SELECT b.group_id,g.name,g.platform,b.primary_account_id,
			coalesce(nullif(a.native_nickname,''),a.name) robot_nickname,b.active
		FROM group_bindings b JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		WHERE b.tenant_id=$1 AND b.active AND a.platform=g.platform AND a.workspace=g.platform AND a.purpose='management'
		ORDER BY g.platform,b.group_id LIMIT 300) x`, p.TenantID).Scan(&groups)
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"tenant_id": p.TenantID, "roles": json.RawMessage(data), "permissions": tenantPermissions,
		"capability_ceiling": ceiling, "member_management": "owner_only", "groups": groups,
		"assistant_templates": control.AssistantPermissionTemplates()})
	return nil
}

func (s *Server) saveMemberRole(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
		Version     int64    `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	code := r.PathValue("code")
	if !control.ValidRoleCode(code) || code == "owner" || code == "legacy" || in.Name == "" || len(in.Name) > 80 ||
		!control.ValidateGrants(in.Permissions, tenantPermissions) {
		return fail(400, "invalid_permissions")
	}
	tx, p, err := s.scopedMemberTx(r, p)
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if p.Role != "superadmin" {
		if err = control.RequireTenantOwner(r.Context(), tx, principalActor(p)); err != nil {
			return err
		}
	}
	for _, permission := range in.Permissions {
		if err = control.RequireTenantCapability(r.Context(), tx, p.TenantID, permission); err != nil {
			return err
		}
	}
	var version int64
	err = tx.QueryRow(r.Context(), `INSERT INTO tenant_roles(tenant_id,code,name,permissions) SELECT $1,$2,$3,$4 WHERE $5::bigint=0
        ON CONFLICT(tenant_id,code) DO UPDATE SET name=excluded.name,permissions=excluded.permissions,version=tenant_roles.version+1
        WHERE tenant_roles.version=$5 RETURNING version`, p.TenantID, code, in.Name, in.Permissions, in.Version).Scan(&version)
	if err == pgx.ErrNoRows && in.Version > 0 {
		err = tx.QueryRow(r.Context(), `UPDATE tenant_roles SET name=$3,permissions=$4,version=version+1
			WHERE tenant_id=$1 AND code=$2 AND version=$5 RETURNING version`,
			p.TenantID, code, in.Name, in.Permissions, in.Version).Scan(&version)
	}
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET access_version=access_version+1 WHERE tenant_id=$1 AND tenant_role=$2`, p.TenantID, code); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, p.TenantID, "tenant_role_changed", code, map[string]any{"version": version}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]int64{"version": version})
	return nil
}

func (s *Server) confirmTenantOwner(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		UserID        string `json:"user_id"`
		PreviousOwner string `json:"previous_owner_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !validUUID(in.UserID) {
		return fail(400, "invalid_resource_id")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var old string
	if err = tx.QueryRow(r.Context(), `SELECT coalesce(owner_id::text,'') FROM tenants WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&old); err != nil {
		return err
	}
	if old != in.PreviousOwner {
		return fail(409, "version_conflict")
	}
	var ok bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND tenant_id=$2 AND NOT disabled AND role='user')`, in.UserID, r.PathValue("id")).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fail(404, "member_not_found")
	}
	if err = control.SeedTenantRoles(r.Context(), tx, r.PathValue("id"), tenantPermissions); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE tenants SET owner_id=$2 WHERE id=$1`, r.PathValue("id"), in.UserID); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM user_access WHERE user_id=$1`, in.UserID); err != nil {
		return err
	}
	if old != "" && old != in.UserID {
		if _, err = tx.Exec(r.Context(), `UPDATE users SET tenant_role='manager',access_version=access_version+1 WHERE id=$1`, old); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET tenant_role='owner',access_version=access_version+1 WHERE id=$1`, in.UserID); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, r.PathValue("id"), "tenant_owner_confirmed", in.UserID, map[string]string{"previous_owner_id": old}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]string{"owner_id": in.UserID})
	return nil
}
