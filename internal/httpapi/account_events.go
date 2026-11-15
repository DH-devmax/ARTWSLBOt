package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

// Recheck both the website identity and the original account/workspace before
// each delivery. An observer cannot retain access through a long-lived cookie.
func (s *Server) accountEventAccess(r *http.Request, original Principal, view string) error {
	r = r.WithContext(database.WithActor(r.Context(), original.ID))
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return fail(401, "authentication_required")
	}
	var p Principal
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT u.id::text,coalesce(u.tenant_id::text,''),u.role
        FROM sessions se JOIN users u ON u.id=se.user_id
        WHERE se.token_hash=$1 AND se.expires_at>clock_timestamp() AND NOT u.disabled`,
		security.Digest(cookie.Value)).Scan(&p.ID, &p.TenantID, &p.Role)
	if err != nil {
		return fail(403, "live_access_unavailable")
	}
	if p.ID != original.ID || p.TenantID != original.TenantID || p.Role != original.Role {
		return fail(403, "live_access_unavailable")
	}
	if err = s.guardWorkspace(r, p, "GET /api/v1/accounts/{id}/events"); err != nil {
		return err
	}
	if p.Role == "superadmin" {
		return nil
	}
	if view != "dashboard" {
		return fail(403, "superadmin_required")
	}
	permissions, err := s.permissions(r.Context(), p)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, "overview:read") || !slices.Contains(permissions, "accounts:read") {
		return fail(403, "permission_denied")
	}
	// RLS proves an active binding; global account membership is insufficient.
	return s.DB.TenantTx(r.Context(), p.TenantID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM group_bindings b
            JOIN subscriptions sub ON sub.tenant_id=b.tenant_id
            WHERE b.active AND sub.enabled AND sub.expires_at>clock_timestamp() AND
            (CASE WHEN $2='maibot' THEN b.ai_account_id ELSE b.primary_account_id END)=$1)`,
			r.PathValue("id"), requestWorkspace(r)).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fail(404, "account_not_found")
		}
		return nil
	})
}

func (s *Server) accountEvents(w http.ResponseWriter, r *http.Request, p Principal) error {
	view := r.URL.Query().Get("view")
	if view != "messages" && view != "dashboard" {
		return fail(400, "invalid_live_view")
	}
	check := func() error {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		return s.accountEventAccess(r.WithContext(ctx), p, view)
	}
	if err := check(); err != nil {
		return err
	}
	s.liveMu.Lock()
	if s.liveActors[p.ID] >= 2 {
		s.liveMu.Unlock()
		return fail(429, "live_stream_busy")
	}
	sub := s.Changes.Subscribe(r.PathValue("id"))
	if sub == nil {
		s.liveMu.Unlock()
		return fail(429, "live_stream_busy")
	}
	s.liveActors[p.ID]++
	s.liveMu.Unlock()
	defer func() {
		sub.Close()
		s.liveMu.Lock()
		s.liveActors[p.ID]--
		if s.liveActors[p.ID] == 0 {
			delete(s.liveActors, p.ID)
		}
		s.liveMu.Unlock()
	}()
	controller := http.NewResponseController(w)
	write := func(kind string, seq int64, payload any) error {
		if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, kind, body); err != nil {
			return err
		}
		return controller.Flush()
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-store")
	defer controller.SetWriteDeadline(time.Time{})
	seq := int64(0)
	ids := map[int64]bool{}
	dirty, directory, reset, receiver := true, false, true, true
	lastMetrics, lastReconcile := time.Time{}, time.Now()
	var dashboard accountDashboard
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case change := <-sub.Events:
			if p.Role != "superadmin" {
				continue
			}
			switch change.Kind {
			case "decoded":
				dirty = true
				if view != "messages" {
					continue
				}
				if len(ids) < 64 {
					ids[change.EventID] = true
				} else {
					reset = true
				}
			case "directory":
				directory = true
			case "receiver":
				receiver = true
			}
		case <-tick.C:
			lost := sub.Lost()
			reset = reset || p.Role == "superadmin" && lost
			now := time.Now()
			reconcile := now.Sub(lastReconcile) >= 30*time.Second
			if reconcile {
				reset, dirty, receiver, lastReconcile = true, true, true, now
			}
			metrics := now.Sub(lastMetrics) >= 5*time.Second && dirty
			if view == "dashboard" {
				metrics = now.Sub(lastMetrics) >= 15*time.Second || lastMetrics.IsZero()
			}
			if !reset && !metrics && !receiver && len(ids) == 0 && !directory {
				continue
			}
			if err := check(); err != nil {
				_ = write("error", seq+1, map[string]string{"code": "live_access_unavailable"})
				return nil
			}
			payload := map[string]any{"account_id": r.PathValue("id"), "workspace": requestWorkspace(r),
				"reset": reset, "directory_changed": directory, "updated_at": now.UTC()}
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			request := r.WithContext(ctx)
			var err error
			if metrics {
				if view == "messages" {
					payload["health"], err = s.readDecoderHealth(request)
				} else {
					dashboard, err = s.readAccountDashboard(request, p)
					payload["dashboard"] = dashboard
				}
				dirty, lastMetrics = false, now
			}
			if err == nil && receiver && p.Role == "superadmin" {
				var current receiverView
				current, err = s.readReceiver(request, r.PathValue("id"))
				payload["receiver"] = current
				if view == "dashboard" && dashboard.Account.ID != "" {
					dashboard.Receiver, dashboard.RuntimeObserved = &current, current.Observed
					observed := dashboard
					observed.SampledAt = now
					dashboard.Health, dashboard.Backlog = classifyAccountDashboard(observed)
					payload["health"], payload["backlog"] = dashboard.Health, dashboard.Backlog
				}
			}
			if err == nil {
				err = s.accountEventAccess(request, p, view)
			}
			cancel()
			if err != nil {
				_ = write("error", seq+1, map[string]string{"code": "live_access_unavailable"})
				return nil
			}
			if view == "messages" {
				eventIDs := make([]string, 0, len(ids))
				for id := range ids {
					eventIDs = append(eventIDs, strconv.FormatInt(id, 10))
				}
				payload["decoded_ids"] = eventIDs
				payload["conversations_changed"] = len(ids) > 0
			}
			seq++
			if err = write("update", seq, payload); err != nil {
				return nil
			}
			ids = map[int64]bool{}
			directory, reset, receiver = false, false, false
		}
	}
}
