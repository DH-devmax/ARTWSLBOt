package scopecache

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestCacheThirtyMinuteSoak(t *testing.T) {
	if os.Getenv("WSL_CACHE_SOAK") != "1" {
		t.Skip("explicit synthetic thirty-minute load only")
	}
	c := New()
	start := time.Now()
	deadline := start.Add(30 * time.Minute)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	count, fetches, maxG := 0, 0, 0
	var maxHeap uint64
	var maxLate time.Duration
	coverage := [3]map[int]bool{{}, {}, {}}
	reads := [3]int{}
	for at := range ticker.C {
		if !at.Before(deadline) {
			break
		}
		maxLate = max(maxLate, time.Since(at))
		phase := min(2, int(at.Sub(start)/(10*time.Minute)))
		n := count % 100
		if phase == 1 {
			n = count % 300
		}
		if phase == 2 {
			n = 0
			if count%2 == 1 {
				n = 1 + (count/2)%299
			}
		}
		coverage[phase][n] = true
		for _, platform := range []string{"wsl", "lajiaoliao"} {
			for _, purpose := range []string{"management", "chat"} {
				key := testKey()
				key.Platform, key.Purpose = platform, purpose
				key.Account, key.Group = fmt.Sprintf("account-%d", n/30), fmt.Sprintf("group-%d", n)
				if phase == 1 {
					key.Parameters = fmt.Sprint(count)
				}
				_, _, err := c.Load(context.Background(), key, func(context.Context) ([]byte, error) {
					fetches++
					return []byte("synthetic-projection"), nil
				})
				if err != nil {
					t.Fatal(err)
				}
				reads[phase]++
			}
		}
		count++
		if count%6000 == 0 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			maxHeap, maxG = max(maxHeap, m.HeapAlloc), max(maxG, runtime.NumGoroutine())
			t.Logf("elapsed=%s reads=%d fetches=%d heap=%d goroutines=%d stats=%+v",
				time.Since(start).Round(time.Second), count*4, fetches, m.HeapAlloc, runtime.NumGoroutine(), c.Snapshot())
		}
		for _, stat := range c.Snapshot() {
			if stat.Entries > MaxEntries || stat.Bytes > MaxBytes || stat.InFlight > MaxEntries {
				t.Fatal(stat)
			}
		}
	}
	for phase, expected := range []int{100, 300, 300} {
		if len(coverage[phase]) != expected || reads[phase] == 0 {
			t.Fatal("incomplete scale coverage", phase, len(coverage[phase]), reads[phase])
		}
	}
	for _, stat := range c.Snapshot() {
		if stat.InFlight != 0 {
			t.Fatal("unfinished read after stopping input", stat)
		}
	}
	t.Logf("completed=%s reads=%d fetches=%d phase_reads=%v groups=[%d %d %d] max_heap=%d max_goroutines=%d max_schedule_lateness=%s",
		time.Since(start), count*4, fetches, reads, len(coverage[0]), len(coverage[1]), len(coverage[2]), maxHeap, maxG, maxLate)
}
