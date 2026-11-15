package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/monitor"
	"wangshangliao/internal/scopecache"
	"wangshangliao/internal/security"
)

type poolMetrics struct {
	Total         int32   `json:"total"`
	Acquired      int32   `json:"acquired"`
	Maximum       int32   `json:"maximum"`
	EmptyAcquires int64   `json:"empty_acquires"`
	AcquireWaitMS float64 `json:"acquire_wait_total_ms"`
	Canceled      int64   `json:"canceled_acquires"`
}

func poolView(pool *pgxpool.Pool) poolMetrics {
	s := pool.Stat()
	return poolMetrics{Total: s.TotalConns(), Acquired: s.AcquiredConns(), Maximum: s.MaxConns(),
		EmptyAcquires: s.EmptyAcquireCount(), AcquireWaitMS: float64(s.AcquireDuration().Microseconds()) / 1000,
		Canceled: s.CanceledAcquireCount()}
}

type accountMonitor struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Workspace         string               `json:"workspace"`
	AuthState         string               `json:"auth_state"`
	Bound             int                  `json:"bound_groups"`
	Limit             int                  `json:"group_limit"`
	OverCapacity      bool                 `json:"over_capacity"`
	Receiver          receiverView         `json:"receiver"`
	HTTP              gateway.ReadSnapshot `json:"http"`
	DecodePending     int64                `json:"decode_pending"`
	DecodeFailed      int64                `json:"decode_failed"`
	DecodeOldest      *time.Time           `json:"decode_oldest_at"`
	DecodeP95         *float64             `json:"decode_p95_ms"`
	DecodeSamples     int                  `json:"decode_samples"`
	DecodeLatency     latencyDistribution  `json:"decode_latency"`
	decodePoints      []decodePoint
	OperationsQueued  int64      `json:"operations_queued"`
	OperationsUnknown int64      `json:"operations_unknown"`
	OperationOldest   *time.Time `json:"operation_oldest_at"`
	SampledAt         time.Time  `json:"sampled_at"`
	Reasons           []string   `json:"reasons"`
}

type systemMonitor struct {
	HeatChat          *control.HeatChatStats     `json:"heat_chat,omitempty"`
	ReadCaches        []scopecache.Stats         `json:"read_caches,omitempty"`
	Services          map[string]monitor.Service `json:"services"`
	DatabaseStats     databaseStats              `json:"database_stats"`
	Workspace         string                     `json:"workspace"`
	MonitorScope      string                     `json:"monitor_scope"`
	Resources         monitor.Resources          `json:"api"`
	Pools             map[string]poolMetrics     `json:"database_pools"`
	DatabaseAvailable bool                       `json:"database_available"`
	DatabaseSampledAt time.Time                  `json:"database_sampled_at"`
	Accounts          []accountMonitor           `json:"accounts"`
	DecodeLatency     latencyDistribution        `json:"decode_latency"`
	Decoder           gateway.DecoderSnapshot    `json:"decoder"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	HistoryAvailable  bool                       `json:"history_available"`
	ComponentStates   map[string]string          `json:"components"`
}

type monitoringState struct {
	mu      sync.RWMutex
	last    systemMonitor
	streams int
	actors  map[string]int
}

func (s *Server) monitorSnapshot(workspace string) systemMonitor {
	s.monitoring.mu.RLock()
	out := s.monitoring.last
	s.monitoring.mu.RUnlock()
	out.Workspace, out.MonitorScope = workspace, "platform_workspace"
	if s.HeatChat != nil {
		stats := s.HeatChat.Snapshot()
		out.HeatChat = &stats
	}
	out.Accounts = append([]accountMonitor{}, out.Accounts...)
	filtered := out.Accounts[:0]
	for _, account := range out.Accounts {
		if account.Workspace == workspace {
			filtered = append(filtered, account)
		}
	}
	out.Accounts = filtered
	out.DecodeLatency = monitorDecodeDistribution(out, workspace)
	if s.Receiver != nil && s.Receiver.ReadCache != nil {
		out.ReadCaches = s.Receiver.ReadCache.Snapshot()
	}
	return out
}

// RunMonitoring has its own bounded pool. Observation never runs in the receive
// commit/ACK path and never creates a native connection.
func (s *Server) RunMonitoring(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancelRun := context.WithCancel(ctx)
		defer cancelRun()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); s.ResourceSampler.Run(ctx) }()
		defer wg.Wait()
		config := s.DB.Platform.Config()
		config.MaxConns = 1
		config.MinConns = 0
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			cancelRun()
			return
		}
		defer pool.Close()
		services := map[string]monitor.Service{}
		serviceReader := monitor.NewServiceReader()
		lastServices, lastStats := time.Time{}, time.Time{}
		dbStats := databaseStats{}
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		lastDB, lastDBSuccess, lastMinute := time.Time{}, time.Time{}, time.Time{}
		states := map[string]string{}
		startup, stopStartup := context.WithTimeout(ctx, time.Second)
		if saved, loadErr := loadMonitorStates(startup, pool); loadErr == nil {
			states = saved
		}
		stopStartup()
		pending := []monitorTransition{}
		accounts := []accountMonitor{}
		databaseOK := false
		for ctx.Err() == nil {
			now := time.Now().UTC()
			if now.Sub(lastDB) >= 15*time.Second {
				work, cancel := context.WithTimeout(ctx, 2*time.Second)
				fresh, readErr := s.sampleMonitorAccounts(work, pool)
				cancel()
				databaseOK = readErr == nil
				if databaseOK {
					accounts = fresh
					lastDBSuccess = now
				}
				lastDB = now
			}
			if now.Sub(lastServices) >= 5*time.Second {
				services["heatbot"] = serviceReader.Heatbot(ctx, services["heatbot"])
				services["web"] = serviceReader.Web(ctx, services["web"])
				lastServices = now
			}
			if now.Sub(lastStats) >= time.Minute {
				work, cancel := context.WithTimeout(ctx, 2*time.Second)
				dbStats = sampleDatabaseStats(work, pool, dbStats)
				cancel()
				lastStats = now
			}
			currentServices := map[string]monitor.Service{
				"api":     monitor.ResourceService(s.ResourceSampler.Snapshot()),
				"heatbot": services["heatbot"], "web": services["web"],
				"postgres": {Available: databaseOK, SampledAt: lastDBSuccess, CheckedAt: now},
			}
			dbStats.Stale = dbStats.SampledAt.IsZero() || now.Sub(dbStats.SampledAt) > 120*time.Second
			current := systemMonitor{Services: currentServices, DatabaseStats: dbStats, Resources: s.ResourceSampler.Snapshot(),
				Pools:             map[string]poolMetrics{"platform": poolView(s.DB.Platform), "tenant": poolView(s.DB.Tenant), "monitor": poolView(pool)},
				DatabaseAvailable: databaseOK, DatabaseSampledAt: lastDBSuccess, UpdatedAt: now,
				Accounts: append([]accountMonitor{}, accounts...), ComponentStates: map[string]string{
					"postgres": "unavailable"}}
			for name, service := range currentServices {
				current.ComponentStates[name] = "unavailable"
				if service.Available && !service.Stale {
					current.ComponentStates[name] = "observed"
				}
			}
			if databaseOK {
				current.ComponentStates["postgres"] = "observed"
			}
			if s.Decoder != nil {
				current.Decoder = s.Decoder.Snapshot()
			}
			for i := range current.Accounts {
				a := &current.Accounts[i]
				a.DecodeLatency.Available = databaseOK && now.Sub(a.SampledAt) < 45*time.Second
				if s.Receiver != nil {
					currentRead := s.Receiver.ReadSnapshot(a.ID)
					if a.HTTP.CooldownUntil != nil && (currentRead.CooldownUntil == nil || a.HTTP.CooldownUntil.After(*currentRead.CooldownUntil)) {
						currentRead.CooldownUntil = a.HTTP.CooldownUntil
					}
					a.HTTP = currentRead
					if snapshot, fresh := s.Receiver.LiveSnapshot(a.ID); fresh {
						a.Receiver.Snapshot = &snapshot
						a.Receiver.Observed = true
					} else if a.Receiver.ObservedAt == nil || now.Sub(*a.Receiver.ObservedAt) > 5*time.Second {
						a.Receiver.Observed = false
					}
				}
				a.Reasons = monitorReasons(*a, now)
			}
			current.HistoryAvailable = databaseOK
			minute := now.Truncate(time.Minute)
			if minute != lastMinute {
				work, cancel := context.WithTimeout(ctx, 2*time.Second)
				if err := persistMonitorMinute(work, pool, current, minute); err != nil {
					current.HistoryAvailable = false
				} else {
					lastMinute = minute
				}
				cancel()
			}
			next := monitorStates(current)
			pending = append(pending, monitorChanges(states, next, now)...)
			states = next
			if len(pending) > 512 {
				pending = append([]monitorTransition{{Key: "|backlog", Reason: "monitor_event_gap", State: "opened", At: now}}, pending[len(pending)-511:]...)
			}
			work, cancel := context.WithTimeout(ctx, time.Second)
			if err := flushMonitorTransitions(work, pool, pending, current.Accounts); err == nil {
				pending = nil
			}
			cancel()
			s.monitoring.mu.Lock()
			s.monitoring.last = current
			s.monitoring.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return done
}

func (s *Server) sampleMonitorAccounts(ctx context.Context, pool *pgxpool.Pool) ([]accountMonitor, error) {
	rows, err := pool.Query(ctx, `SELECT a.id::text,a.name,a.workspace,a.auth_state,
        (SELECT count(*) FROM group_bindings b WHERE b.active AND (b.primary_account_id=a.id OR b.ai_account_id=a.id)),
        coalesce(r.enabled,false),coalesce(r.revision,0),r.observed_at,coalesce(r.snapshot,'{}'),
        coalesce(r.enabled AND r.lease_until>now() AND r.observed_at>now()-interval '5 seconds',false),
        (SELECT count(*) FROM operations o WHERE o.account_id=a.id AND o.status='queued'),
        (SELECT count(*) FROM operations o WHERE o.account_id=a.id AND o.status='unknown'),
        (SELECT min(created_at) FROM operations o WHERE o.account_id=a.id AND o.status='queued'),
        pending.count,pending.oldest,
        (SELECT count(*) FROM native_decoded_events x WHERE x.account_id=a.id AND x.state='failed'
          AND x.received_at>now()-interval '7 days'),
        (SELECT coalesce(jsonb_agg(jsonb_build_object('id',x.event_id,'at',x.decoded_at,'ms',x.decode_ms)),'[]')
          FROM (SELECT event_id,decoded_at,decode_ms FROM native_decoded_events
            WHERE account_id=a.id AND decoded_at>now()-interval '24 hours' AND decoded_at<=now()
              AND state='decoded' AND decode_ms>=0 AND decode_ms<'Infinity'::float8
            ORDER BY decoded_at DESC,event_id DESC LIMIT 1000) x),
        now(),
        s.cooldown_until
        FROM accounts a LEFT JOIN account_receivers r ON r.account_id=a.id
        LEFT JOIN account_read_state s ON s.account_id=a.id AND s.account_epoch=a.epoch
        CROSS JOIN LATERAL (
          SELECT count(*) AS count,min(e.received_at) AS oldest FROM native_received_events e
          LEFT JOIN native_decoded_events x ON x.event_id=e.id
          WHERE e.account_id=a.id AND e.kind IN('group','private') AND e.received_at>now()-interval '7 days'
          AND (x.event_id IS NULL OR x.state='processing')
        ) pending ORDER BY a.id LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []accountMonitor{}
	for rows.Next() {
		var a accountMonitor
		var raw, points []byte
		err = rows.Scan(&a.ID, &a.Name, &a.Workspace, &a.AuthState, &a.Bound, &a.Receiver.Enabled, &a.Receiver.Revision,
			&a.Receiver.ObservedAt, &raw, &a.Receiver.Observed, &a.OperationsQueued, &a.OperationsUnknown, &a.OperationOldest,
			&a.DecodePending, &a.DecodeOldest, &a.DecodeFailed, &points, &a.SampledAt, &a.HTTP.CooldownUntil)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &a.Receiver.Snapshot)
		if err = json.Unmarshal(points, &a.decodePoints); err != nil {
			return nil, err
		}
		a.DecodeLatency = decodeDistribution(a.decodePoints, a.SampledAt, true)
		a.DecodeP95, a.DecodeSamples = a.DecodeLatency.P95, a.DecodeLatency.Samples
		a.Limit = control.MaxGroupsPerAccount
		a.OverCapacity = a.Bound > a.Limit
		out = append(out, a)
	}
	return out, rows.Err()
}

func monitorReasons(a accountMonitor, now time.Time) []string {
	reasons := []string{}
	if a.OverCapacity {
		reasons = append(reasons, "account_over_capacity")
	}
	if a.Receiver.Enabled {
		switch {
		case !a.Receiver.Observed || a.Receiver.Snapshot == nil:
			reasons = append(reasons, "receiver_unobserved")
		case a.Receiver.Snapshot.State == "blocked":
			reasons = append(reasons, "receiver_blocked")
		case a.Receiver.Snapshot.State != "connected":
			reasons = append(reasons, "receiver_disconnected")
		case a.Receiver.Snapshot.SyncedAt == nil:
			reasons = append(reasons, "receiver_sync_pending")
		case a.Receiver.Snapshot.HeartbeatAt == nil || now.Sub(*a.Receiver.Snapshot.HeartbeatAt) > 40*time.Second:
			reasons = append(reasons, "heartbeat_stale")
		}
		if a.Receiver.Snapshot != nil {
			if a.Receiver.Snapshot.QueueDepth >= 24 {
				reasons = append(reasons, "receive_queue_pressure")
			}
			if a.Receiver.Snapshot.Quarantined > 0 {
				reasons = append(reasons, "receiver_quarantined")
			}
		}
	}
	if a.HTTP.CooldownUntil != nil && now.Before(*a.HTTP.CooldownUntil) {
		reasons = append(reasons, "http_rate_limited")
	}
	for _, at := range []*time.Time{a.HTTP.OldestWaiting, a.DecodeOldest, a.OperationOldest} {
		if at != nil && now.Sub(*at) > 30*time.Second {
			reasons = append(reasons, "backlog_oldest")
			break
		}
	}
	if a.DecodeFailed > 0 {
		reasons = append(reasons, "decode_failed")
	}
	if a.OperationsUnknown > 0 {
		reasons = append(reasons, "operation_unknown")
	}
	return reasons
}

func persistMonitorMinute(ctx context.Context, pool *pgxpool.Pool, current systemMonitor, minute time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Do not persist labels, raw receiver errors, blocked packet data or user content.
	sample := map[string]any{"api": current.Resources, "services": current.Services, "database_stats": current.DatabaseStats,
		"database_pools": current.Pools, "database_available": current.DatabaseAvailable, "decoder": current.Decoder}
	raw, _ := json.Marshal(sample)
	if _, err = tx.Exec(ctx, `INSERT INTO monitor_minutes(scope,scope_id,minute,workspace,sample)
        VALUES('system','platform',$1,'platform',$2) ON CONFLICT DO NOTHING`, minute, raw); err != nil {
		return err
	}
	for _, a := range current.Accounts {
		values := map[string]any{"sampled_at": a.SampledAt, "bound_groups": a.Bound, "decode_pending": a.DecodePending,
			"decode_p95_ms": a.DecodeP95, "decode_samples": a.DecodeSamples, "decode_latency": a.DecodeLatency, "operations_queued": a.OperationsQueued,
			"operations_unknown": a.OperationsUnknown, "http": a.HTTP, "receiver": safeReceiverMetrics(a.Receiver)}
		raw, _ = json.Marshal(values)
		if _, err = tx.Exec(ctx, `INSERT INTO monitor_minutes(scope,scope_id,account_id,minute,workspace,sample)
            SELECT 'account',id::text,id,$2,$3,$4 FROM accounts WHERE id=$1
            ON CONFLICT DO NOTHING`, a.ID, minute, a.Workspace, raw); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM monitor_minutes WHERE minute<now()-interval '24 hours';
        DELETE FROM monitor_incidents WHERE created_at<now()-interval '7 days';
        DELETE FROM group_assignment_executions WHERE created_at<now()-interval '7 days'`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func safeReceiverMetrics(r receiverView) map[string]any {
	out := map[string]any{"observed": r.Observed, "enabled": r.Enabled}
	if r.Snapshot != nil {
		s := r.Snapshot
		out["state"], out["messages"], out["acks"], out["duplicates"] = s.State, s.Messages, s.ACKs, s.Duplicates
		out["queue_depth"], out["queue_bytes"] = s.QueueDepth, s.QueueBytes
		out["durable_p95_ms"], out["ack_p95_ms"], out["latency_samples"] = s.DurableP95, s.ACKP95, s.Samples
		out["ack_samples"], out["latency_window_seconds"], out["latency_sample_limit"] = s.ACKSamples, s.WindowSeconds, s.SampleLimit
		out["received_60s"], out["unique_60s"], out["reconnects"] = s.Received60s, s.Unique60s, s.Reconnects
	}
	return out
}

func monitorStates(current systemMonitor) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"api", "web", "heatbot"} {
		if service, ok := current.Services[name]; ok && (!service.Available || service.Stale) {
			component := name
			if name == "api" {
				component = "http"
			}
			out["|"+component] = name + "_monitor_unavailable"
		}
	}
	if !current.DatabaseAvailable {
		out["|database"] = "database_unavailable"
	} else if !current.DatabaseStats.CheckedAt.IsZero() && !current.DatabaseStats.Available {
		out["|database"] = "database_stats_unavailable"
	}
	if current.Decoder.Error != "" {
		out["|decoder"] = "decoder_database_unavailable"
	}
	for _, a := range current.Accounts {
		for _, reason := range a.Reasons {
			component := "backlog"
			switch reason {
			case "receiver_unobserved", "receiver_blocked", "receiver_disconnected", "receiver_sync_pending", "heartbeat_stale", "receiver_quarantined":
				component = "receiver"
			case "http_rate_limited":
				component = "http"
			case "decode_failed":
				component = "decoder"
			}
			out[a.ID+"|"+component+"|"+reason] = reason
		}
	}
	return out
}

type monitorTransition struct {
	Key, Reason, State string
	At                 time.Time
}

func monitorChanges(old, next map[string]string, now time.Time) []monitorTransition {
	out := []monitorTransition{}
	for key, reason := range next {
		if old[key] != reason {
			out = append(out, monitorTransition{key, reason, "opened", now})
		}
	}
	for key, reason := range old {
		if key == "|simon" {
			continue
		}
		if next[key] != reason {
			out = append(out, monitorTransition{key, reason, "resolved", now})
		}
	}
	return out
}

func loadMonitorStates(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	rows, err := pool.Query(ctx, `SELECT account,component,reason,state FROM (
        SELECT DISTINCT ON(coalesce(account_id::text,''),component,reason)
        coalesce(account_id::text,'') account,component,reason,state
        FROM monitor_incidents WHERE created_at>now()-interval '7 days'
        ORDER BY coalesce(account_id::text,''),component,reason,id DESC) latest WHERE state='opened'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var account, component, reason, state string
		if err = rows.Scan(&account, &component, &reason, &state); err != nil {
			return nil, err
		}
		if component == "simon" {
			continue
		}
		key := account + "|" + component
		if account != "" {
			key += "|" + reason
		}
		out[key] = reason
	}
	return out, rows.Err()
}

func flushMonitorTransitions(ctx context.Context, pool *pgxpool.Pool, changes []monitorTransition, accounts []accountMonitor) error {
	if len(changes) == 0 {
		return nil
	}
	workspace := map[string]string{}
	for _, a := range accounts {
		workspace[a.ID] = a.Workspace
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	insert := func(key, reason, state string, at time.Time) error {
		parts := strings.SplitN(key, "|", 3)
		ws := "platform"
		var account any
		if parts[0] != "" {
			ws = workspace[parts[0]]
			account = parts[0]
			if ws == "" {
				return nil
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO monitor_incidents(workspace,account_id,component,reason,state,created_at)
            VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, ws, account, parts[1], reason, state, at)
		return err
	}
	for _, change := range changes {
		if err = insert(change.Key, change.Reason, change.State, change.At); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Server) systemMonitor(w http.ResponseWriter, r *http.Request, p Principal) error {
	send(w, 200, s.monitorSnapshot(requestWorkspace(r)))
	return nil
}

func (s *Server) monitorHistory(w http.ResponseWriter, r *http.Request, p Principal) error {
	end := time.Now().UTC()
	start := end.Add(-time.Hour)
	var err error
	if raw := r.URL.Query().Get("start"); raw != "" {
		start, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return fail(400, "invalid_monitor_range")
		}
	}
	if raw := r.URL.Query().Get("end"); raw != "" {
		end, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return fail(400, "invalid_monitor_range")
		}
	}
	if !end.After(start) || end.Sub(start) > 24*time.Hour || end.After(time.Now().Add(time.Minute)) {
		return fail(400, "invalid_monitor_range")
	}
	account := r.URL.Query().Get("account_id")
	scope, id := "system", "platform"
	if account != "" {
		if !validUUID(account) {
			return fail(400, "invalid_resource_id")
		}
		var ws string
		if err = s.DB.Platform.QueryRow(r.Context(), `SELECT workspace FROM accounts WHERE id=$1`, account).Scan(&ws); err != nil || ws != requestWorkspace(r) {
			return fail(404, "account_not_found")
		}
		scope, id = "account", account
	}
	return s.list(w, r, p, `SELECT scope,scope_id,workspace,minute,sample FROM monitor_minutes WHERE scope=$1 AND scope_id=$2 AND minute>=$3 AND minute<=$4
        ORDER BY minute LIMIT 1441`, scope, id, start, end)
}

func (s *Server) monitorIncidents(w http.ResponseWriter, r *http.Request, p Principal) error {
	before := int64(0)
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			return fail(400, "invalid_monitor_cursor")
		}
	}
	return s.list(w, r, p, `SELECT id,workspace,account_id,component,reason,state,created_at FROM monitor_incidents
        WHERE created_at>now()-interval '7 days' AND (workspace=$1 OR workspace='platform')
        AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 100`, requestWorkspace(r), before)
}

func (s *Server) monitorEvents(w http.ResponseWriter, r *http.Request, p Principal) error {
	s.monitoring.mu.Lock()
	if s.monitoring.streams >= 8 || s.monitoring.actors[p.ID] >= 2 {
		s.monitoring.mu.Unlock()
		return fail(429, "monitor_stream_busy")
	}
	s.monitoring.streams++
	s.monitoring.actors[p.ID]++
	s.monitoring.mu.Unlock()
	defer func() {
		s.monitoring.mu.Lock()
		s.monitoring.streams--
		s.monitoring.actors[p.ID]--
		if s.monitoring.actors[p.ID] == 0 {
			delete(s.monitoring.actors, p.ID)
		}
		s.monitoring.mu.Unlock()
	}()
	cookie, _ := r.Cookie("wsl_session")
	controller := http.NewResponseController(w)
	defer controller.SetWriteDeadline(time.Time{})
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	seq := 0
	for {
		work, cancel := context.WithTimeout(r.Context(), time.Second)
		var allowed bool
		err := s.DB.Platform.QueryRow(work, `SELECT EXISTS(SELECT 1 FROM sessions se JOIN users u ON u.id=se.user_id
            WHERE se.token_hash=$1 AND se.user_id=$2 AND se.expires_at>clock_timestamp() AND NOT u.disabled AND u.role='superadmin')`,
			security.Digest(cookie.Value), p.ID).Scan(&allowed)
		cancel()
		if err != nil || !allowed {
			return nil
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		seq++
		raw, _ := json.Marshal(s.monitorSnapshot(requestWorkspace(r)))
		if err = controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return nil
		}
		if _, err = fmt.Fprintf(w, "id: %d\nevent: snapshot\ndata: %s\n\n", seq, raw); err != nil {
			return nil
		}
		if err = controller.Flush(); err != nil {
			return nil
		}
		select {
		case <-r.Context().Done():
			return nil
		case <-tick.C:
		}
	}
}
