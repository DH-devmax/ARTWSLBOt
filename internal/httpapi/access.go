package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

type PermissionSpec struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Module string `json:"module,omitempty"`
}

var tenantPermissions = []PermissionSpec{
	{"overview:read", "查看工作台", ""},
	{"accounts:read", "查看管理机器人状态", ""},
	{"groups:read", "查看已授权群", ""},
	{"groups:configure", "配置群管与自动化", "management"},
	{"groups:moderation", "编辑拦截词与名单", "moderation"},
	{"groups:replies", "编辑关键词回复", "replies"},
	{"groups:revoke", "撤销本租户群授权", ""},
	{"groups:ai", "申请 AI 入群与启停", "ai"},
	{"claims:read", "查看群授权申请", ""},
	{"claims:create", "申请认领群组", ""},
	{"operations:read", "查看操作记录", ""},
	{"operations:create", "提交群管操作", "moderation"},
	{"operations:cancel", "取消排队操作", ""},
	{"ledger:read", "查看历史台账", ""},
	{"ledger:create", "新增财务记录", "finance"},
	{"activities:read", "查看活动", ""},
	{"activities:create", "创建活动", "activities"},
	{"activities:draw", "活动开奖", "activities"},
	{"audit:read", "查看本租户审计", ""},
}

func rolePermissions(role string) []string {
	out := []string{}
	for _, spec := range tenantPermissions {
		if role == "manager" || role == "operator" && spec.Code != "groups:revoke" && spec.Module != "finance" ||
			role == "viewer" && strings.HasSuffix(spec.Code, ":read") {
			out = append(out, spec.Code)
		}
	}
	return out
}

func (s *Server) permissions(ctx context.Context, p Principal) ([]string, error) {
	if p.Role == "superadmin" {
		out := rolePermissions("manager")
		return append(out, "users:manage", "plans:manage", "accounts:manage", "claims:review",
			"personas:read", "personas:manage", "tests:manage", "access:manage", "workspaces:maibot"), nil
	}
	var allowed []string
	err := s.DB.Platform.QueryRow(ctx, `SELECT permissions FROM user_access WHERE user_id=$1`, p.ID).Scan(&allowed)
	if err == pgx.ErrNoRows {
		allowed = rolePermissions("manager")
	} else if err != nil {
		return nil, err
	}
	modules := []string{}
	var aiLimit int
	err = s.DB.Platform.QueryRow(ctx, `SELECT p.modules,p.ai_group_limit FROM subscriptions sub
        JOIN plans p ON p.id=sub.plan_id WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>now()`,
		p.TenantID).Scan(&modules, &aiLimit)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	out := []string{}
	for _, spec := range tenantPermissions {
		entitled := spec.Module == "" || slices.Contains(modules, spec.Module)
		if spec.Module == "management" {
			entitled = false
			for _, module := range []string{"moderation", "replies", "schedules", "rankings", "cards", "friends"} {
				entitled = entitled || slices.Contains(modules, module)
			}
		}
		if spec.Module == "ai" {
			entitled = entitled && aiLimit > 0
		}
		if entitled && slices.Contains(allowed, spec.Code) {
			out = append(out, spec.Code)
		}
	}
	return out, nil
}

func (s *Server) accessCatalog(w http.ResponseWriter, r *http.Request, p Principal) error {
	roles := []map[string]any{}
	for _, role := range []struct{ code, label string }{
		{"manager", "租户管理员"}, {"operator", "运营人员"}, {"viewer", "只读用户"},
	} {
		roles = append(roles, map[string]any{"code": role.code, "label": role.label, "permissions": rolePermissions(role.code)})
	}
	send(w, 200, map[string]any{"permissions": tenantPermissions, "roles": roles})
	return nil
}

func (s *Server) userAccess(w http.ResponseWriter, r *http.Request, p Principal) error {
	var user Principal
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT id::text,tenant_id::text,role FROM users WHERE id=$1 AND role='user'`,
		r.PathValue("id")).Scan(&user.ID, &user.TenantID, &user.Role)
	if err == pgx.ErrNoRows {
		return fail(404, "user_not_found")
	}
	if err != nil {
		return err
	}
	role, permissions, version := "manager", rolePermissions("manager"), int64(0)
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT role_code,permissions,version FROM user_access WHERE user_id=$1`,
		user.ID).Scan(&role, &permissions, &version)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	effective, err := s.permissions(r.Context(), user)
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"role_code": role, "permissions": permissions, "effective_permissions": effective, "version": version})
	return nil
}

func (s *Server) saveUserAccess(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Role        string   `json:"role_code"`
		Permissions []string `json:"permissions"`
		Version     int64    `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !slices.Contains([]string{"manager", "operator", "viewer", "custom"}, in.Role) || in.Version < 0 ||
		len(in.Permissions) > len(tenantPermissions) {
		return fail(400, "invalid_permissions")
	}
	seen := map[string]bool{}
	for _, code := range in.Permissions {
		if seen[code] || !slices.ContainsFunc(tenantPermissions, func(spec PermissionSpec) bool { return spec.Code == code }) {
			return fail(400, "invalid_permissions")
		}
		seen[code] = true
	}
	if in.Role != "custom" {
		template := rolePermissions(in.Role)
		if len(in.Permissions) != len(template) {
			return fail(400, "invalid_permissions")
		}
		for _, code := range template {
			if !seen[code] {
				return fail(400, "invalid_permissions")
			}
		}
	}
	if in.Permissions == nil {
		in.Permissions = []string{}
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var tenant string
	if err = tx.QueryRow(r.Context(), `SELECT tenant_id::text FROM users WHERE id=$1 AND role='user' FOR UPDATE`,
		r.PathValue("id")).Scan(&tenant); err == pgx.ErrNoRows {
		return fail(404, "user_not_found")
	} else if err != nil {
		return err
	}
	var version int64
	err = tx.QueryRow(r.Context(), `INSERT INTO user_access(user_id,role_code,permissions)
        SELECT $1,$2,$3 WHERE $4::bigint=0 ON CONFLICT(user_id) DO UPDATE SET role_code=excluded.role_code,
        permissions=excluded.permissions,version=user_access.version+1,updated_at=now()
        WHERE user_access.version=$4 RETURNING version`, r.PathValue("id"), in.Role, in.Permissions, in.Version).Scan(&version)
	if err == pgx.ErrNoRows && in.Version > 0 {
		err = tx.QueryRow(r.Context(), `UPDATE user_access SET role_code=$2,permissions=$3,version=version+1,updated_at=now()
            WHERE user_id=$1 AND version=$4 RETURNING version`, r.PathValue("id"), in.Role, in.Permissions, in.Version).Scan(&version)
	}
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='user_permissions_changed',updated_at=now()
        WHERE actor_id=$1 AND status='queued'`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "user_permissions_updated", r.PathValue("id"), in); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]int64{"version": version})
	return nil
}

var routePermissions = map[string]string{
	"GET /api/v1/overview": "overview:read", "GET /api/v1/accounts": "accounts:read",
	"GET /api/v1/accounts/{id}/dashboard": "overview:read",
	"GET /api/v1/groups":                  "groups:read", "PATCH /api/v1/groups/{id}": "groups:configure",
	"POST /api/v1/groups/{id}/revoke": "groups:revoke", "PUT /api/v1/groups/{id}/ai-service": "groups:ai",
	"GET /api/v1/claims": "claims:read", "POST /api/v1/claims": "claims:create",
	"GET /api/v1/operations": "operations:read", "POST /api/v1/operations": "operations:create",
	"POST /api/v1/operations/{id}/cancel": "operations:cancel",
	"GET /api/v1/ledger":                  "ledger:read", "POST /api/v1/ledger": "ledger:create",
	"GET /api/v1/activities": "activities:read", "POST /api/v1/activities": "activities:create",
	"POST /api/v1/activities/{id}/draw": "activities:draw", "GET /api/v1/audit": "audit:read",
}
