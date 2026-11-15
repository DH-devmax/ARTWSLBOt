package aigateway

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestExactCostAndCachePartition(t *testing.T) {
	r := Rates{Input: 1_000_000, Output: 4_000_000, CacheRead: 100_000, CacheWrite: 2_000_000}
	got, err := Cost(Usage{Input: 2000, Output: 500}, r)
	if err != nil || got != 4_000_000 {
		t.Fatal(got, err)
	}
	got, err = Cost(Usage{Input: 2000, Output: 500, CacheRead: 1000, CacheWrite: 500}, r)
	if err != nil || got != 3_600_000 {
		t.Fatal(got, err)
	}
	if _, err = Cost(Usage{Input: 1, CacheRead: 2}, r); err == nil {
		t.Fatal("overlapping cache accepted")
	}
	got, err = Cost(Usage{Input: 1}, Rates{Input: 1})
	if err != nil || got != 1 {
		t.Fatal("rounding", got, err)
	}
}
func TestSchedulerLimitsCancellationAndFairness(t *testing.T) {
	s := NewScheduler()
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	var releases []func()
	for _, tenant := range []string{"a", "a", "b", "b"} {
		release, err := s.Acquire(ctx, tenant)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	ready := make(chan string, 2)
	var wg sync.WaitGroup
	for _, tenant := range []string{"a", "c"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := s.Acquire(ctx, tenant)
			if err != nil {
				return
			}
			defer release()
			ready <- tenant
		}()
	}
	releases[2]()
	select {
	case got := <-ready:
		if got != "c" {
			t.Fatal("eligible tenant starved", got)
		}
	case <-ctx.Done():
		t.Fatal("blocked")
	}
	for _, release := range releases {
		release()
	}
	wg.Wait()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if release, err := s.Acquire(cancelled, "x"); err == nil {
		release()
		t.Fatal("cancelled acquired")
	}
}
