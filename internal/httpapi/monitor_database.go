package httpapi

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type databaseStats struct {
	Available   bool       `json:"available"`
	Stale       bool       `json:"stale"`
	SampledAt   time.Time  `json:"sampled_at"`
	CheckedAt   time.Time  `json:"checked_at"`
	ErrorCode   string     `json:"error_code,omitempty"`
	Connections int64      `json:"connections"`
	SizeBytes   int64      `json:"size_bytes"`
	Commits     int64      `json:"commits"`
	Rollbacks   int64      `json:"rollbacks"`
	Deadlocks   int64      `json:"deadlocks"`
	StatsReset  *time.Time `json:"stats_reset_at"`
	QueryMS     float64    `json:"query_ms"`
}

func sampleDatabaseStats(ctx context.Context, pool *pgxpool.Pool, old databaseStats) databaseStats {
	start := time.Now()
	out := databaseStats{}
	err := pool.QueryRow(ctx, `SELECT numbackends,pg_database_size(datid),
		xact_commit,xact_rollback,deadlocks,stats_reset
		FROM pg_stat_database WHERE datname=current_database()`).
		Scan(&out.Connections, &out.SizeBytes, &out.Commits, &out.Rollbacks, &out.Deadlocks, &out.StatsReset)
	if err != nil {
		old.Available, old.ErrorCode, old.CheckedAt = false, "database_stats_unavailable", time.Now().UTC()
		return old
	}
	out.Available, out.SampledAt, out.CheckedAt = true, time.Now().UTC(), time.Now().UTC()
	out.QueryMS = float64(time.Since(start).Microseconds()) / 1000
	return out
}
