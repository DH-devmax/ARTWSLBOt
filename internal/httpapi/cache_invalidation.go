package httpapi

import (
	"net/http"
	"strings"
)

// Called only after a successful handler/transaction. Live access/version
// checks remain mandatory when a notification is missed or the database fails.
func (s *Server) invalidateReadProjections(r *http.Request, pattern string) {
	if s.Receiver == nil || s.Receiver.ReadCache == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return
	}
	cache := s.Receiver.ReadCache
	if strings.Contains(pattern, "native-members") {
		return
	}
	if strings.Contains(pattern, "/members") || strings.Contains(pattern, "/member-roles") ||
		strings.Contains(pattern, "/users") || strings.Contains(pattern, "/subscriptions") ||
		strings.Contains(pattern, "/claims") || strings.Contains(pattern, "/bindings") {
		cache.InvalidateAll()
		return
	}
	id := r.PathValue("id")
	if strings.HasPrefix(r.URL.Path, "/api/v1/groups/") {
		var native string
		if s.DB.Platform.QueryRow(r.Context(), `SELECT native_id FROM native_groups WHERE id=$1`, id).Scan(&native) == nil {
			cache.InvalidateGroup(native)
		}
		cache.InvalidateGroup(id)
	} else if strings.HasPrefix(r.URL.Path, "/api/v1/accounts/") || strings.Contains(pattern, "/maibot/") {
		cache.InvalidateAccount(id)
	}
}
