package control

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

// This optional load can run beside CPU-only frozen decoder benchmarks.
// Database integration tests still own wsl_test serially. End-to-end decoder
// regression percentages belong to the release's paired baseline/candidate run.
func TestPostgresBusinessRulesDisabledLoad(t *testing.T) {
	if os.Getenv("WSL_BUSINESS_DISABLED_LOAD") != "1" {
		t.Skip("set WSL_BUSINESS_DISABLED_LOAD=1 for the isolated disabled-worker observation")
	}
	f := newBusinessFixture(t)
	accounts := make(map[int]string)
	for _, size := range []int{100, 300} {
		start := f.count(`SELECT count(*) FROM group_bindings`)
		for i := start; i < size; i++ {
			pool := i / 30
			platform := "wsl"
			if pool%2 == 1 {
				platform = "lajiaoliao"
			}
			if accounts[pool] == "" {
				accounts[pool] = f.account(platform, pool)
			}
			f.group(platform, accounts[pool], f.tenant, i, false)
		}
		if f.count(`SELECT count(*) FROM group_business_runtime WHERE enabled`) != 0 {
			t.Fatal("disabled load has an enabled group")
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		goroutines := runtime.NumGoroutine()
		sql := f.queries.Load()
		started := time.Now()
		ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
		done := f.service.Run(ctx)
		<-done
		cancel()
		elapsed := time.Since(started)
		sql = f.queries.Load() - sql
		runtime.ReadMemStats(&after)
		// One discovery query and two dispatch statements per one-second poll,
		// independent of the group count; allow both timer boundary polls.
		maxSQL := int64(3 * (int(elapsed/time.Second) + 2))
		if sql > maxSQL || f.reads.Load() != 0 || f.roles.Load() != 0 || f.writes.Load() != 0 ||
			f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 ||
			f.count(`SELECT count(*) FROM business_rule_consumers`) != 0 || f.service.Snapshot().Active != 0 {
			t.Fatal("disabled worker exceeded bounded polling or started work", size, sql, maxSQL)
		}
		t.Logf("%d disabled groups: elapsed=%s SQL=%d source/native ports=0 jobs=0 workers=0 process_alloc_delta=%d goroutines=%d->%d",
			size, elapsed.Round(time.Millisecond), sql, after.TotalAlloc-before.TotalAlloc,
			goroutines, runtime.NumGoroutine())
	}
}
