package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/scopecache"
)

func TestPostgresProjectionRejectsConnectionOwnerChanges(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	manager := NewReceiverManager(db, nil, nil)
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,purpose,workspace,auth_state,credential_sealed)
		VALUES('projection-owner','wsl','management','wsl','authenticated',$1) RETURNING id::text`,
		[]byte("synthetic")).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,owner)VALUES($1,'owner-a')`, account); err != nil {
		t.Fatal(err)
	}
	var calls int
	port := func(_ context.Context, key scopecache.Key) (MemberSnapshot, error) {
		calls++
		return MemberSnapshot{AccountEpoch: key.AccountEpoch}, nil
	}
	read := func() MemberSnapshot {
		view, err := manager.memberProjection(ctx, "synthetic-actor", "synthetic-tenant", account, "123", port)
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	if read().CacheHit || !read().CacheHit || calls != 1 {
		t.Fatal("cache fixture did not warm", calls)
	}
	if _, err := db.Exec(ctx, `UPDATE account_receivers SET owner='owner-b' WHERE account_id=$1`, account); err != nil {
		t.Fatal(err)
	}
	if read().CacheHit || calls != 2 {
		t.Fatal("connection handoff reused old projection", calls)
	}
	manager.ReadCache.InvalidateAll()
	_, err := manager.memberProjection(ctx, "synthetic-actor", "synthetic-tenant", account, "123",
		func(work context.Context, key scopecache.Key) (MemberSnapshot, error) {
			_, e := db.Exec(work, `UPDATE account_receivers SET owner='owner-c' WHERE account_id=$1`, account)
			return MemberSnapshot{AccountEpoch: key.AccountEpoch}, e
		})
	if !errors.Is(err, ErrMemberScope) {
		t.Fatal("late result crossed connection handoff", err)
	}
}

// Uses live wsl_test version reads and a one-millisecond synthetic native port.
// No credentials are opened and no upstream connection is created.
func TestPostgresProjectionCachePairedPerformance(t *testing.T) {
	if os.Getenv("WSL_CACHE_DB_BENCH") != "1" {
		t.Skip("explicit isolated projection benchmark")
	}
	db := receiverTestDB(t)
	ctx := context.Background()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, purpose := range []string{"management", "chat"} {
			accounts := []string{}
			for i := range 10 {
				var id string
				workspace := platform
				if purpose == "chat" {
					workspace = "maibot"
				}
				if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,purpose,workspace,
					auth_state,credential_sealed)VALUES($1,$2,$3,$4,'authenticated',$5) RETURNING id::text`,
					fmt.Sprintf("projection-bench-%d", i), platform, purpose, workspace, []byte("synthetic")).Scan(&id); err != nil {
					t.Fatal(err)
				}
				accounts = append(accounts, id)
			}
			var calls atomic.Int64
			port := func(_ context.Context, key scopecache.Key) (MemberSnapshot, error) {
				calls.Add(1)
				until := time.Now().Add(time.Millisecond)
				for time.Now().Before(until) {
				}
				return MemberSnapshot{AccountEpoch: key.AccountEpoch,
					MemberDirectory: nativeauth.MemberDirectory{Group: key.Group, Complete: true},
					SelfRole:        "admin", SampledAt: time.Now()}, nil
			}
			type observation struct {
				calls, queries, heap, goroutines, poolWait int64
				p95                                        time.Duration
			}
			for round := range 5 {
				for _, scenario := range []string{"repeat80", "miss100", "miss300", "hot300"} {
					coldManager := NewReceiverManager(db, nil, nil)
					warmManager := NewReceiverManager(db, nil, nil)
					read := func(manager *ReceiverManager, cold bool, group int, actor string) (time.Duration, observation) {
						account := accounts[group/30]
						if cold {
							manager.ReadCache.InvalidateAccount(account)
						}
						startQueries, startWait := db.Stat().AcquireCount(), db.Stat().AcquireDuration()
						before := calls.Load()
						at := time.Now()
						view, err := manager.memberProjection(ctx, actor, "synthetic-tenant",
							account, fmt.Sprint(group), port)
						latency := time.Since(at)
						if err != nil || view.SelfRole != "unknown" {
							t.Fatal("projection leaked execution role or failed", err, view.SelfRole)
						}
						return latency, observation{calls: calls.Load() - before, queries: db.Stat().AcquireCount() - startQueries,
							poolWait: (db.Stat().AcquireDuration() - startWait).Microseconds()}
					}
					var cold, warm observation
					coldTimes, warmTimes := make([]time.Duration, 1000), make([]time.Duration, 1000)
					for i := range coldTimes {
						group, actor := i/5, "projection-actor"
						switch scenario {
						case "miss100":
							group, actor = i%100, fmt.Sprint("unique-actor-", i)
						case "miss300":
							group, actor = i%300, fmt.Sprint("unique-actor-", i)
						case "hot300":
							group = 0
							if i%2 == 1 {
								group = 1 + (i/2)%299
							}
						}
						for step := range 2 {
							isCold := (i+round+step)%2 == 0
							manager := warmManager
							if isCold {
								manager = coldManager
							}
							latency, out := read(manager, isCold, group, actor)
							target := &warm
							if isCold {
								target = &cold
								coldTimes[i] = latency
							} else {
								warmTimes[i] = latency
							}
							target.calls += out.calls
							target.queries += out.queries
							target.poolWait += out.poolWait
						}
					}
					slices.Sort(coldTimes)
					slices.Sort(warmTimes)
					cold.p95, warm.p95 = coldTimes[949], warmTimes[949]
					var memory runtime.MemStats
					runtime.ReadMemStats(&memory)
					warm.heap, warm.goroutines = int64(memory.HeapAlloc), int64(runtime.NumGoroutine())
					reduction := 100 * (1 - float64(warm.calls)/float64(cold.calls))
					change := 100 * (float64(warm.p95)/float64(cold.p95) - 1)
					t.Logf("platform=%s purpose=%s round=%d scenario=%s cold_calls=%d warm_calls=%d reduction=%.2f cold_p95_us=%.2f warm_p95_us=%.2f p95_change=%.2f cold_queries=%d warm_queries=%d pool_wait_us=%d heap=%d goroutines=%d",
						platform, purpose, round+1, scenario, cold.calls, warm.calls, reduction,
						float64(cold.p95)/1000, float64(warm.p95)/1000, change, cold.queries, warm.queries,
						warm.poolWait, warm.heap, warm.goroutines)
					if scenario == "repeat80" && reduction < 70 {
						t.Fatal("TTL repetition reduction below target")
					}
					if warm.queries != 2000 || cold.queries != 2000 {
						t.Fatal("unexpected version-query amplification", cold.queries, warm.queries)
					}
				}
			}
			for _, id := range accounts {
				if _, err := db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
