package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
)

func assistantRef(r *http.Request) control.SessionRef {
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return control.SessionRef{}
	}
	ref, _ := control.WebsiteSession(cookie.Value)
	return ref.InPlatform(requestWorkspace(r))
}

func (s *Server) StartAssistant(ctx context.Context) error {
	s.assistantContext = ctx
	s.Assistant.ActionExecutor = s.executeAssistantAction
	s.Assistant.ConfigRoles = (groupBindingAdapter{server: s}).BindingRoles
	s.Assistant.ReceiptReader = func(work context.Context, platform, account, group, client, sender string, at time.Time) (control.AssistantObservation, error) {
		if s.Receiver == nil {
			return control.AssistantObservation{}, control.Denied(503, "native_read_owner_unavailable")
		}
		r, err := s.Receiver.AssistantReceipt(work, platform, account, group, client, sender, at)
		state := "not_observed"
		if r.EventID > 0 {
			state = "receive_observed"
		}
		return control.AssistantObservation{State: state, EventID: r.EventID, NativeID: r.NativeID, ObservedAt: r.ObservedAt}, err
	}
	s.Assistant.NativeMessages = func(work context.Context, platform, account, group, search string) ([]control.AssistantMessage, error) {
		if s.Receiver == nil {
			return nil, control.Denied(503, "native_read_owner_unavailable")
		}
		rows, err := s.Receiver.MessageCandidates(work, platform, account, group, search)
		if err != nil {
			return nil, err
		}
		out := []control.AssistantMessage{}
		for _, row := range rows {
			out = append(out, control.AssistantMessage{Member: control.AssistantMember{
				ID: strconv.FormatInt(row.Reference.EventID, 10), Name: row.Preview, Role: "message"},
				Reference: row.Reference})
		}
		return out, nil
	}
	s.Assistant.NativeMembers = func(work context.Context, platform, account, group, search string) ([]control.AssistantMember, error) {
		if s.Receiver == nil {
			return nil, control.Denied(503, "native_read_owner_unavailable")
		}
		actor, ok := control.NativeReadActor(work)
		if !ok {
			return nil, control.Denied(403, "assistant_scope_changed")
		}
		snapshot, err := s.Receiver.MemberProjection(work, actor.ID, actor.TenantID, account, group)
		if err != nil {
			return nil, control.Denied(409, "assistant_member_lookup_unavailable")
		}
		if !snapshot.Complete {
			return nil, control.Denied(409, "assistant_member_lookup_incomplete")
		}
		out := []control.AssistantMember{}
		for _, m := range snapshot.Members {
			if m.ID == search || strings.Contains(strings.ToLower(m.Name+" "+m.Card), strings.ToLower(search)) {
				name := m.Name
				if m.Card != "" {
					name = m.Card
				}
				out = append(out, control.AssistantMember{ID: m.ID, Name: name, Role: m.Role})
				if len(out) == 21 {
					break
				}
			}
		}
		return out, nil
	}
	s.ManualActions.Executor = s.executeManualAction
	s.ManualActions.NativeMembers = s.Assistant.NativeMembers
	s.ManualActions.NativeMessages = s.Assistant.NativeMessages
	if err := s.ManualActions.Recover(ctx); err != nil {
		return err
	}
	return s.Assistant.Recover(ctx)
}

func (s *Server) StopAssistant() { s.assistantJobs.Stop() }

func (s *Server) assistantRoutes() {
	s.handle("GET /api/v1/assistant/module-exposure", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.ModuleExposure(r.Context(), assistantRef(r))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("PUT /api/v1/assistant/module-exposure", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			Version      int64    `json:"version"`
			Capabilities []string `json:"capabilities"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := s.Assistant.SaveModuleExposure(r.Context(), assistantRef(r), in.Version, in.Capabilities)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("GET /api/v1/assistant/action-plans/{id}", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.ActionPlan(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/assistant/action-plans/{id}/cancel", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.CancelAction(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/assistant/action-plans/{id}/approve", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			Digest  string `json:"digest"`
			Version int64  `json:"version"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := s.Assistant.ApproveAction(r.Context(), assistantRef(r), r.PathValue("id"), in.Digest, in.Version, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/assistant/sessions/{id}/target", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			Token string `json:"token"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := s.Assistant.ChooseTarget(r.Context(), assistantRef(r), r.PathValue("id"), in.Token)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("GET /api/v1/assistant/context", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		offset, limit := 0, 30
		if raw := r.URL.Query().Get("offset"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return fail(400, "assistant_invalid_cursor")
			}
			offset = value
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return fail(400, "assistant_invalid_cursor")
			}
			limit = value
		}
		out, err := s.Assistant.Context(r.Context(), assistantRef(r), r.URL.Query().Get("group_id"), r.URL.Query().Get("search"), offset, limit)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("GET /api/v1/assistant/health", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		settings, err := s.Assistant.Settings(r.Context(), assistantRef(r))
		if err != nil {
			return err
		}
		var pending, unknown, active int64
		err = s.DB.Platform.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE status IN ('reserved','submitted')),
            count(*) FILTER(WHERE status='unknown') FROM ai_calls
            WHERE purpose='assistant' AND created_at>clock_timestamp()-interval '90 days'`).Scan(&pending, &unknown)
		if err != nil {
			return err
		}
		if err = s.DB.Platform.QueryRow(r.Context(), `SELECT count(*) FROM assistant_turns WHERE status IN ('queued','running')`).Scan(&active); err != nil {
			return err
		}
		send(w, 200, map[string]any{"enabled": settings.Enabled, "active_turns": s.assistantJobs.Count(), "concurrency_limit": 2,
			"persisted_active_turns": active, "pending_usage": pending, "unverified_usage": unknown, "sampled_at": time.Now().UTC(), "native_ready": false, "ai_ready": false})
		return nil
	})
	s.handle("GET /api/v1/assistant/settings", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.Settings(r.Context(), assistantRef(r))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("PUT /api/v1/assistant/settings", true, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in control.AssistantSettings
		if err := decode(w, r, &in); err != nil {
			return err
		}
		out, err := s.Assistant.SaveSettings(r.Context(), assistantRef(r), in)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("GET /api/v1/assistant/usage", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.Usage(r.Context(), assistantRef(r))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("GET /api/v1/assistant/sessions", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		scope := assistant.Scope{Platform: requestWorkspace(r), Kind: r.URL.Query().Get("kind"), AccountID: r.URL.Query().Get("account_id"), GroupID: r.URL.Query().Get("group_id")}
		out, err := s.Assistant.Sessions(r.Context(), assistantRef(r), scope)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/assistant/sessions", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var scope assistant.Scope
		if err := decode(w, r, &scope); err != nil {
			return err
		}
		out, err := s.Assistant.CreateSession(r.Context(), assistantRef(r), scope)
		if err != nil {
			return err
		}
		send(w, 201, out)
		return nil
	})
	s.handle("GET /api/v1/assistant/sessions/{id}", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.Session(r.Context(), assistantRef(r), r.PathValue("id"), false)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("DELETE /api/v1/assistant/sessions/{id}", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.DeleteSession(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("GET /api/v1/assistant/sessions/{id}/turns", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.Turns(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("GET /api/v1/assistant/sessions/{id}/transcript", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.Transcript(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/assistant/sessions/{id}/turns", false, s.createAssistantTurn)
	s.handle("GET /api/v1/assistant/turns/{id}", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, _, err := s.Assistant.Turn(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/assistant/turns/{id}/cancel", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.CancelTurn(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		s.assistantJobs.Cancel(out.ID, p.ID)
		send(w, 200, map[string]any{"cancel_requested": true, "turn_id": out.ID})
		return nil
	})
	s.handle("GET /api/v1/assistant/turns/{id}/events", false, s.assistantEvents)
	s.handle("GET /api/v1/assistant/plans/{id}", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.Plan(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/assistant/plans/{id}/cancel", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		out, err := s.Assistant.CancelPlan(r.Context(), assistantRef(r), r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
	s.handle("POST /api/v1/assistant/plans/{id}/approve", false, func(w http.ResponseWriter, r *http.Request, p Principal) error {
		var in struct {
			Digest  string `json:"digest"`
			Version int64  `json:"version"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		if key := r.Header.Get("Idempotency-Key"); len(key) < 8 || len(key) > 100 {
			return fail(400, "idempotency_key_required")
		}
		out, err := s.Assistant.Approve(r.Context(), assistantRef(r), r.PathValue("id"), in.Digest, in.Version)
		if err != nil {
			return err
		}
		send(w, 200, out)
		return nil
	})
}

func (s *Server) createAssistantTurn(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Input       string `json:"input"`
		TargetToken string `json:"target_token"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	ref := assistantRef(r)
	turn, created, err := s.Assistant.CreateTurnWithTarget(r.Context(), ref, r.PathValue("id"), r.Header.Get("Idempotency-Key"), in.Input, in.TargetToken)
	if err != nil {
		return err
	}
	if created {
		err = s.assistantJobs.Start(s.assistantContext, turn.ID, p.ID, p.TenantID, func(ctx context.Context) {
			var runErr error
			defer func() {
				if recover() != nil {
					runErr = assistant.ErrModel
				}
				if ctx.Err() != nil {
					runErr = ctx.Err()
				}
				finish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				// The state remains queryable if the process dies before this final write.
				if err := s.Assistant.FinishTurn(finish, turn.ID, runErr); err != nil {
					slog.Warn("assistant finalization pending", "turn_id", turn.ID)
				}
			}()
			host, model, history, propose, e := s.Assistant.Host(ctx, ref, turn.ID)
			if e != nil {
				runErr = e
				return
			}
			_ = model
			provider := s.Gateway.AssistantProvider(host)
			if e = host.Emit(ctx, assistant.Event{Kind: "turn_started", Data: map[string]string{"status": "running"}}); e != nil {
				runErr = e
				return
			}
			runErr = s.AssistantRuntime.Run(ctx, provider, host, history, propose)
		})
		if err != nil {
			finish, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = s.Assistant.FinishTurn(finish, turn.ID, err)
			cancel()
			return control.Denied(429, "assistant_busy")
		}
	}
	send(w, http.StatusAccepted, turn)
	return nil
}

func (s *Server) assistantEvents(w http.ResponseWriter, r *http.Request, p Principal) error {
	select {
	case s.assistantStreams <- struct{}{}:
		defer func() { <-s.assistantStreams }()
	default:
		return fail(429, "assistant_stream_busy")
	}
	after := int64(0)
	cursor := r.URL.Query().Get("after")
	if cursor == "" {
		cursor = r.Header.Get("Last-Event-ID")
	}
	if cursor != "" {
		value, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return fail(400, "assistant_invalid_cursor")
		}
		after = value
	}
	id, ref := r.PathValue("id"), assistantRef(r)
	readBatch := func() ([]control.AssistantEvent, control.AssistantTurn, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		return s.Assistant.Events(ctx, ref, id, after)
	}
	events, turn, err := readBatch()
	if err != nil {
		return err
	}
	if r.URL.Query().Get("format") == "json" {
		send(w, 200, map[string]any{"turn": turn, "events": events})
		return nil
	}
	if !s.validOrigin(r) {
		return fail(403, "csrf_rejected")
	}
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-store")
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		for _, event := range events {
			if event.Seq != after+1 {
				return nil
			}
			raw, _ := json.Marshal(event)
			if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Seq, event.Kind, raw); err != nil {
				return nil
			}
			after = event.Seq
		}
		if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
			return nil
		}
		if err = controller.Flush(); err != nil {
			return nil
		}
		if len(events) < 50 && turn.Status != "queued" && turn.Status != "running" {
			return nil
		}
		select {
		case <-r.Context().Done():
			return nil
		case <-tick.C:
		}
		events, turn, err = readBatch()
		if err != nil {
			// No resource payload after authorization loss or database failure.
			fmt.Fprint(w, "event: access_revoked\ndata: {}\n\n")
			_ = controller.Flush()
			return nil
		}
	}
}
