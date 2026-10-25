package httpapi

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

type dashboardAccount struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Workspace         string `json:"workspace"`
	AuthState         string `json:"auth_state"`
	RuntimeState      string `json:"runtime_state"`
	ConnectionEnabled bool   `json:"connection_enabled"`
}

type dashboardGroups struct {
	Authorized int `json:"authorized"`
	Management int `json:"management_enabled"`
	AI         int `json:"ai_enabled"`
	AIPending  int `json:"ai_pending"`
}

type dashboardOperations struct {
	Queued       int        `json:"queued"`
	Running      int        `json:"running"`
	Unknown      int        `json:"unknown"`
	Failed15m    int        `json:"failed_15m"`
	OldestQueued *time.Time `json:"oldest_queued_at"`
}

type dashboardInbox struct {
	Pending       int        `json:"pending"`
	Unresolved    int        `json:"unresolved"`
	Quarantined   int        `json:"quarantined"`
	OldestPending *time.Time `json:"oldest_pending_at"`
}

type dashboardBucket struct {
	At    time.Time `json:"at"`
	Count int       `json:"count"`
}

type dashboardPerformance struct {
	WindowSeconds  int               `json:"window_seconds"`
	Inserted       int               `json:"inserted_60s"`
	PerSecond      float64           `json:"per_second"`
	LatencySamples int               `json:"latency_samples"`
	InvalidTiming  int               `json:"invalid_timing_samples"`
	P50            *float64          `json:"p50_ms"`
	P95            *float64          `json:"p95_ms"`
	P99            *float64          `json:"p99_ms"`
	LastInserted   *time.Time        `json:"last_inserted_at"`
	Series         []dashboardBucket `json:"series"`
}

type dashboardHealth struct {
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
}

type accountDashboard struct {
	Account          dashboardAccount     `json:"account"`
	SampledAt        time.Time            `json:"sampled_at"`
	Visibility       string               `json:"visibility"`
	Groups           dashboardGroups      `json:"groups"`
	Operations       dashboardOperations  `json:"operations"`
	Inbox            dashboardInbox       `json:"inbox"`
	Performance      dashboardPerformance `json:"performance"`
	Health           dashboardHealth      `json:"health"`
	Backlog          dashboardHealth      `json:"backlog"`
	BacklogThreshold int                  `json:"backlog_threshold_seconds"`
	RuntimeObserved  bool                 `json:"runtime_observed"`
}

func (s *Server) accountDashboard(w http.ResponseWriter, r *http.Request, p Principal) error {
	if p.Role != "superadmin" {
		permissions, err := s.permissions(r.Context(), p)
		if err != nil {
			return err
		}
		if !slices.Contains(permissions, "accounts:read") {
			return fail(403, "permission_denied")
		}
	}
	out := accountDashboard{Visibility: "account", BacklogThreshold: 30}
	out.Performance.WindowSeconds = 60
	out.Performance.Series = []dashboardBucket{}
	// Business aggregates use the restricted tenant role and its forced RLS.
	// Shared ingress remains platform-owned; only metadata for these visible,
	// still-authorized groups is aggregated below, never payloads or senders.
	groupIDs := []string{}
	read := func(tx pgx.Tx) error {
		err := tx.QueryRow(r.Context(), `SELECT id::text,name,workspace,auth_state,runtime_state,connection_enabled
            FROM accounts WHERE id=$1 AND workspace=$2`, r.PathValue("id"), requestWorkspace(r)).Scan(
			&out.Account.ID, &out.Account.Name, &out.Account.Workspace, &out.Account.AuthState,
			&out.Account.RuntimeState, &out.Account.ConnectionEnabled)
		if err == pgx.ErrNoRows {
			return fail(404, "account_not_found")
		}
		if err != nil {
			return err
		}
		if err = tx.QueryRow(r.Context(), `SELECT now()`).Scan(&out.SampledAt); err != nil {
			return err
		}
		err = tx.QueryRow(r.Context(), `SELECT count(*),
            count(*) FILTER(WHERE management_enabled),count(*) FILTER(WHERE ai_enabled),
            count(*) FILTER(WHERE ai_requested AND NOT ai_enabled),
            coalesce(array_agg(group_id::text),'{}'::text[])
            FROM group_bindings WHERE active AND
            (CASE WHEN $2='maibot' THEN ai_account_id ELSE primary_account_id END)=$1`,
			out.Account.ID, out.Account.Workspace).Scan(&out.Groups.Authorized, &out.Groups.Management,
			&out.Groups.AI, &out.Groups.AIPending, &groupIDs)
		if err != nil {
			return err
		}
		return tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE status='queued'),
            count(*) FILTER(WHERE status='running'),count(*) FILTER(WHERE status='unknown'),
            count(*) FILTER(WHERE status='failed' AND updated_at >= $2::timestamptz-interval '15 minutes'
                AND updated_at <= $2),
            min(created_at) FILTER(WHERE status='queued')
            FROM operations WHERE account_id=$1 AND
            (status IN ('queued','running','unknown') OR
                (status='failed' AND updated_at >= $2::timestamptz-interval '15 minutes'))`,
			out.Account.ID, out.SampledAt).Scan(&out.Operations.Queued, &out.Operations.Running,
			&out.Operations.Unknown, &out.Operations.Failed15m, &out.Operations.OldestQueued)
	}
	if p.Role == "superadmin" {
		tx, err := s.DB.Platform.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if err = read(tx); err != nil {
			return err
		}
		if err = s.readAccountIngress(r.Context(), tx, p, groupIDs, &out); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
	} else {
		out.Visibility = "tenant_groups"
		if err := s.DB.TenantTx(r.Context(), p.TenantID, read); err != nil {
			return err
		}
		tx, err := s.DB.Platform.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if err = s.readAccountIngress(r.Context(), tx, p, groupIDs, &out); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
	}
	out.Health, out.Backlog = classifyAccountDashboard(out)
	w.Header().Set("Cache-Control", "no-store")
	send(w, 200, out)
	return nil
}

func (s *Server) readAccountIngress(ctx context.Context, tx pgx.Tx, p Principal, groups []string, out *accountDashboard) error {
	// Tenant scope comes from authenticated RLS results, never query parameters.
	// Recheck active ownership here to fence revocations between the two reads.
	const scope = `account_id=$1 AND ($2::boolean OR
        (group_id=ANY($3::text[]::uuid[]) AND EXISTS(
            SELECT 1 FROM group_bindings b WHERE b.group_id=inbox.group_id
                AND b.tenant_id=nullif($4,'')::uuid AND b.active AND b.primary_account_id=$1)))`
	args := []any{out.Account.ID, p.Role == "superadmin", groups, p.TenantID, out.SampledAt}
	err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='pending'),
        count(*) FILTER(WHERE state='unresolved'),count(*) FILTER(WHERE state='quarantined'),
        min(persisted_at) FILTER(WHERE state IN ('pending','unresolved'))
        FROM inbox WHERE `+scope+` AND state IN ('pending','unresolved','quarantined')`, args[:4]...).Scan(
		&out.Inbox.Pending, &out.Inbox.Unresolved, &out.Inbox.Quarantined, &out.Inbox.OldestPending)
	if err != nil {
		return err
	}
	if p.Role == "superadmin" {
		err = tx.QueryRow(ctx, `SELECT persisted_at FROM inbox WHERE account_id=$1
            AND persisted_at <= $2 ORDER BY persisted_at DESC LIMIT 1`,
			out.Account.ID, out.SampledAt).Scan(&out.Performance.LastInserted)
	} else {
		err = tx.QueryRow(ctx, `SELECT max(latest.persisted_at) FROM group_bindings b
            CROSS JOIN LATERAL (
                SELECT persisted_at FROM inbox WHERE account_id=$1 AND group_id=b.group_id
                    AND persisted_at <= $4 ORDER BY persisted_at DESC LIMIT 1
            ) latest WHERE b.primary_account_id=$1 AND b.group_id=ANY($2::text[]::uuid[])
                AND b.active AND b.tenant_id=$3`, out.Account.ID, groups, p.TenantID,
			out.SampledAt).Scan(&out.Performance.LastInserted)
	}
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	// persisted_at records INSERT time, before WAL commit/ACK. Never label this
	// percentile as durable-commit or ACK latency. Negative clock samples are excluded.
	err = tx.QueryRow(ctx, `SELECT count(*),
        count(*) FILTER(WHERE received_at <= persisted_at),
        count(*) FILTER(WHERE received_at > persisted_at),
        percentile_cont(0.5) WITHIN GROUP(ORDER BY extract(epoch FROM persisted_at-received_at)*1000)
            FILTER(WHERE received_at <= persisted_at),
        percentile_cont(0.95) WITHIN GROUP(ORDER BY extract(epoch FROM persisted_at-received_at)*1000)
            FILTER(WHERE received_at <= persisted_at),
        percentile_cont(0.99) WITHIN GROUP(ORDER BY extract(epoch FROM persisted_at-received_at)*1000)
            FILTER(WHERE received_at <= persisted_at)
        FROM inbox WHERE `+scope+` AND persisted_at > $5::timestamptz-interval '60 seconds' AND persisted_at <= $5`,
		args...).Scan(&out.Performance.Inserted, &out.Performance.LatencySamples, &out.Performance.InvalidTiming,
		&out.Performance.P50, &out.Performance.P95, &out.Performance.P99)
	if err != nil {
		return err
	}
	out.Performance.PerSecond = float64(out.Performance.Inserted) / 60
	rows, err := tx.Query(ctx, `WITH counts AS (
        SELECT date_trunc('minute',persisted_at) AS at,count(*)::int AS total FROM inbox
        WHERE `+scope+` AND persisted_at >= date_trunc('minute',$5::timestamptz)-interval '14 minutes'
            AND persisted_at <= $5 GROUP BY 1
        ) SELECT at,coalesce(total,0) FROM generate_series(
            date_trunc('minute',$5::timestamptz)-interval '14 minutes',
            date_trunc('minute',$5::timestamptz),interval '1 minute') AS buckets(at)
        LEFT JOIN counts USING(at) ORDER BY at`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bucket dashboardBucket
		if err = rows.Scan(&bucket.At, &bucket.Count); err != nil {
			return err
		}
		out.Performance.Series = append(out.Performance.Series, bucket)
	}
	return rows.Err()
}

func classifyAccountDashboard(out accountDashboard) (dashboardHealth, dashboardHealth) {
	backlog := dashboardHealth{Status: "empty", Reasons: []string{}}
	if out.Inbox.Pending+out.Inbox.Unresolved+out.Operations.Queued+out.Operations.Running > 0 {
		backlog.Status = "pending"
	}
	for _, oldest := range []*time.Time{out.Inbox.OldestPending, out.Operations.OldestQueued} {
		if oldest != nil && out.SampledAt.Sub(*oldest) >= time.Duration(out.BacklogThreshold)*time.Second {
			backlog.Status = "backlogged"
			backlog.Reasons = append(backlog.Reasons, "old_pending")
			break
		}
	}
	if out.Inbox.Quarantined > 0 {
		backlog.Reasons = append(backlog.Reasons, "quarantined")
	}
	if out.Operations.Unknown > 0 {
		backlog.Reasons = append(backlog.Reasons, "unknown_operations")
	}
	if out.Inbox.Quarantined+out.Operations.Unknown > 0 {
		backlog.Status = "attention"
	}
	health := dashboardHealth{Status: "unobserved", Reasons: slices.Clone(backlog.Reasons)}
	switch {
	case backlog.Status == "attention" || backlog.Status == "backlogged":
		health.Status = backlog.Status
	case out.Account.AuthState != "authenticated":
		health.Status = "not_authenticated"
	case !out.Account.ConnectionEnabled:
		health.Status = "disabled"
	case out.Account.RuntimeState != "connected":
		health.Status = "disconnected"
	}
	// No heartbeat/ACK/HTTP scheduler observer is wired yet. A stored connected
	// flag or empty database queue cannot prove a healthy live receiver.
	health.Reasons = append(health.Reasons, "runtime_unobserved")
	if out.Performance.InvalidTiming > 0 {
		health.Reasons = append(health.Reasons, "invalid_timing")
	}
	return health, backlog
}
