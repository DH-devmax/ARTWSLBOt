package httpapi

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
)

func requestWorkspace(r *http.Request) string {
	if value := r.Header.Get("X-Workspace"); value != "" {
		return value
	}
	return "wsl"
}

func (s *Server) guardWorkspace(r *http.Request, p Principal, pattern string) error {
	workspace := requestWorkspace(r)
	if workspace != "wsl" && workspace != "maibot" && workspace != "lajiaoliao" {
		return fail(400, "invalid_workspace")
	}
	if strings.Contains(pattern, "/members") || strings.Contains(pattern, "/member-roles") ||
		strings.Contains(pattern, "/billing") || strings.Contains(pattern, "/ai-gateway") || strings.Contains(pattern, "/tenants/") ||
		strings.Contains(pattern, "/customer-invitations") {
		if workspace == "maibot" && p.Role != "superadmin" {
			return fail(403, "superadmin_required")
		}
		return nil
	}
	// The onboarding control service owns preparation/package checks. Reading or
	// cancelling its durable records must remain possible after a package expires.
	if (strings.Contains(pattern, "/group-binding/") ||
		strings.Contains(pattern, "/human-verification/")) && workspace != "maibot" {
		return nil
	}
	if workspace == "lajiaoliao" {
		if strings.Contains(pattern, "/native-signup") ||
			strings.Contains(pattern, "/native-mobile-") ||
			strings.Contains(pattern, "/native-deployment") || strings.Contains(pattern, "/native-im-profile") ||
			strings.Contains(pattern, "/ledger") || strings.Contains(pattern, "/activities") {
			return fail(403, "workspace_route_forbidden")
		}
	}
	if p.Role != "superadmin" && (workspace == "wsl" || workspace == "lajiaoliao") &&
		pattern != "GET /api/v1/me" && pattern != "GET /api/v1/workspaces" &&
		!strings.HasPrefix(pattern, "GET /api/v1/user-center") && !strings.HasPrefix(pattern, "POST /api/v1/auth/") {
		var entitled bool
		if err := s.DB.Platform.QueryRow(r.Context(), `SELECT (
                $3 AND (($2='wsl' AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=$1))
                OR EXISTS(SELECT 1 FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
                    WHERE b.tenant_id=$1 AND g.platform=$2))) OR EXISTS(
                SELECT 1 FROM subscriptions s JOIN plans p ON p.id=s.plan_id
                WHERE s.tenant_id=$1 AND ($3 OR s.enabled AND s.expires_at>now())
                AND $2=ANY(p.allowed_platforms))`, p.TenantID, workspace, r.Method == http.MethodGet).Scan(&entitled); err != nil {
			return err
		}
		if !entitled {
			return fail(403, "platform_not_in_plan")
		}
	}
	if workspace == "maibot" {
		if p.Role != "superadmin" {
			return fail(403, "superadmin_required")
		}
		allowed := strings.Contains(pattern, "/accounts") || strings.Contains(pattern, "/native-") ||
			strings.Contains(pattern, "/personas") || strings.Contains(pattern, "/maibot/") ||
			strings.HasPrefix(pattern, "GET /api/v1/system/monitor") || strings.Contains(pattern, "/assignment") || pattern == "GET /api/v1/overview" || pattern == "GET /api/v1/groups" ||
			pattern == "GET /api/v1/me" || pattern == "GET /api/v1/workspaces"
		if !allowed {
			return fail(403, "workspace_route_forbidden")
		}
	}
	if strings.Contains(pattern, "/personas") && workspace != "maibot" {
		return fail(403, "workspace_route_forbidden")
	}
	if (strings.Contains(pattern, "/maibot/") || strings.Contains(pattern, "/maibot-settings/")) && workspace != "maibot" {
		return fail(403, "workspace_route_forbidden")
	}
	if strings.Contains(pattern, "/accounts/{id}") || strings.Contains(pattern, "/personas/{id}") {
		var actual, nativePlatform string
		err := s.DB.Platform.QueryRow(r.Context(), `SELECT workspace,platform FROM accounts WHERE id=$1`, r.PathValue("id")).Scan(&actual, &nativePlatform)
		if err == pgx.ErrNoRows || err == nil && actual != workspace {
			return fail(404, "account_not_found")
		}
		if err != nil {
			return err
		}
		if nativePlatform == "lajiaoliao" && strings.Contains(pattern, "/native-auth") {
			return fail(409, "lajiaoliao_connector_not_accepted")
		}
		if nativePlatform != "wsl" && strings.Contains(pattern, "/native-mobile-") {
			return fail(404, "account_not_found")
		}
		if nativePlatform != "lajiaoliao" && strings.Contains(pattern, "/lajiaoliao-") {
			return fail(404, "account_not_found")
		}
	}
	if strings.Contains(pattern, "/groups/{id}") {
		var nativePlatform string
		err := s.DB.Platform.QueryRow(r.Context(), `SELECT platform FROM native_groups WHERE id=$1`, r.PathValue("id")).Scan(&nativePlatform)
		if err == pgx.ErrNoRows || err == nil && workspace != "maibot" && nativePlatform != workspace {
			return fail(404, "group_not_found")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) workspaces(w http.ResponseWriter, r *http.Request, p Principal) error {
	items := []map[string]any{{"id": "user", "name": "用户中心", "purpose": "tenant_services"}}
	var wsl, chili bool
	if p.Role == "superadmin" {
		wsl, chili = true, true
	} else if err := s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(
        SELECT 1 FROM subscriptions s JOIN plans p ON p.id=s.plan_id WHERE s.tenant_id=$1
        AND s.enabled AND s.expires_at>now() AND 'wsl'=ANY(p.allowed_platforms)),EXISTS(
        SELECT 1 FROM subscriptions s JOIN plans p ON p.id=s.plan_id WHERE s.tenant_id=$1
        AND s.enabled AND s.expires_at>now() AND 'lajiaoliao'=ANY(p.allowed_platforms))`, p.TenantID).Scan(&wsl, &chili); err != nil {
		return err
	}
	if wsl {
		items = append(items, map[string]any{"id": "wsl", "name": "旺商聊机器人", "purpose": "group_management",
			"platform": "wsl", "limits": workspaceLimits()})
	}
	if chili {
		items = append(items, map[string]any{"id": "lajiaoliao", "name": "辣椒聊机器人", "purpose": "group_management",
			"platform": "lajiaoliao", "limits": workspaceLimits()})
	}
	if p.Role == "superadmin" {
		items = append(items, map[string]any{"id": "maibot", "name": "热群机器人", "purpose": "chat_only", "limits": workspaceLimits()})
	}
	consoles, defaultConsole := availableConsoles(p)
	send(w, 200, map[string]any{"items": items, "connection_lifecycle": "background",
		"consoles": consoles, "default_console": defaultConsole, "platforms": []string{"wsl", "lajiaoliao"},
		"native_ready": false, "ai_ready": false})
	return nil
}

func availableConsoles(p Principal) ([]map[string]any, string) {
	items := []map[string]any{{"id": "user", "name": "用户中心"}}
	if p.Role == "superadmin" {
		return append([]map[string]any{{"id": "admin", "name": "管理后台"}}, items...), "admin"
	}
	return items, "user"
}

func workspaceLimits() map[string]any {
	return map[string]any{"accounts": control.MaxAccountsPerWorkspace, "groups": control.MaxAuthorizedGroups,
		"groups_per_account": control.MaxGroupsPerAccount,
		"ai_groups":          control.MaxAuthorizedGroups, "accounts_per_pool": 10, "pools": 2,
		"native_connections": 20, "platforms_share_pool": true, "load_verified": false}
}
