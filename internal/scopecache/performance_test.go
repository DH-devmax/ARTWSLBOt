package scopecache

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"
)

// Simulates a bounded read port, not a native load test or decoder benchmark.
// The one-millisecond port cost is identical in cold and warm runs.
func TestCachePairedReadPerformance(t *testing.T) {
	if os.Getenv("WSL_CACHE_BENCH") != "1" {
		t.Skip("opt-in synthetic read-port benchmark")
	}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for round := 0; round < 5; round++ {
			for _, scenario := range []string{"repeat80", "miss100", "miss300", "hot300"} {
				type sample struct {
					calls int
					p95   time.Duration
				}
				run := func(cached bool) sample {
					cache := New()
					out := sample{}
					latencies := make([]time.Duration, 1000)
					groups := map[int]bool{}
					for i := range latencies {
						group := i
						switch scenario {
						case "repeat80":
							group = i / 5
						case "miss100":
							group = i % 100
						case "miss300":
							group = i % 300
						case "hot300":
							group = 1 + (i/2)%299
							if i%2 == 0 {
								group = 0
							}
						}
						groups[group] = true
						key := Key{Platform: platform, Purpose: "management", Account: fmt.Sprint(group / 30),
							Group: fmt.Sprint(group), Tenant: "synthetic", Actor: "synthetic", Owner: "test",
							Kind: "member_projection", AccountEpoch: 1}
						if scenario == "miss100" || scenario == "miss300" {
							key.Parameters = fmt.Sprint(i)
						}
						fetch := func(context.Context) ([]byte, error) {
							out.calls++
							until := time.Now().Add(time.Millisecond)
							for time.Now().Before(until) {
							}
							return []byte("immutable synthetic projection"), nil
						}
						start := time.Now()
						var err error
						if cached {
							_, _, err = cache.Load(context.Background(), key, fetch)
						} else {
							_, err = fetch(context.Background())
						}
						if err != nil {
							t.Fatal(err)
						}
						latencies[i] = time.Since(start)
					}
					slices.Sort(latencies)
					out.p95 = latencies[len(latencies)*95/100-1]
					if scenario == "hot300" && len(groups) != 300 {
						t.Fatal("hot scenario did not cover 300 groups", len(groups))
					}
					return out
				}
				var baseline, candidate sample
				if round%2 == 0 {
					baseline = run(false)
					candidate = run(true)
				} else {
					candidate = run(true)
					baseline = run(false)
				}
				reduction := 100 * (1 - float64(candidate.calls)/float64(baseline.calls))
				regression := 100 * (float64(candidate.p95)/float64(baseline.p95) - 1)
				t.Logf("platform=%s round=%d scenario=%s baseline_calls=%d candidate_calls=%d reduction=%.2f baseline_p95_us=%.2f candidate_p95_us=%.2f p95_change=%.2f",
					platform, round+1, scenario, baseline.calls, candidate.calls, reduction, float64(baseline.p95)/1000, float64(candidate.p95)/1000, regression)
				if scenario == "repeat80" && reduction < 70 {
					t.Fatal("read reduction below target")
				}
			}
		}
	}
}
