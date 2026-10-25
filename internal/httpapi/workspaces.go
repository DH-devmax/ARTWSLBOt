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
	if workspace != "wsl" && workspace != "maibot" {
		return fail(400, "invalid_workspace")
	}
	if workspace == "maibot" {
		if p.Role != "superadmin" {
			return fail(403, "superadmin_required")
		}
		allowed := strings.Contains(pattern, "/accounts") || strings.Contains(pattern, "/native-") ||
			strings.Contains(pattern, "/personas") || strings.Contains(pattern, "/maibot/") ||
			pattern == "GET /api/v1/system/monitor" || pattern == "GET /api/v1/overview" || pattern == "GET /api/v1/groups" ||
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
		var actual string
		err := s.DB.Platform.QueryRow(r.Context(), `SELECT workspace FROM accounts WHERE id=$1`, r.PathValue("id")).Scan(&actual)
		if err == pgx.ErrNoRows || err == nil && actual != workspace {
			return fail(404, "account_not_found")
		}
		return err
	}
	return nil
}

func (s *Server) workspaces(w http.ResponseWriter, r *http.Request, p Principal) error {
	items := []map[string]any{{"id": "wsl", "name": "旺商聊机器人", "purpose": "group_management", "limits": workspaceLimits()}}
	if p.Role == "superadmin" {
		items = append(items, map[string]any{"id": "maibot", "name": "热群机器人", "purpose": "chat_only", "limits": workspaceLimits()})
	}
	send(w, 200, map[string]any{"items": items, "connection_lifecycle": "background",
		"native_ready": false, "ai_ready": false})
	return nil
}

func workspaceLimits() map[string]any {
	return map[string]any{"accounts": control.MaxAccountsPerWorkspace, "groups": control.MaxAuthorizedGroups,
		"ai_groups": control.MaxAuthorizedGroups, "load_verified": false}
}
