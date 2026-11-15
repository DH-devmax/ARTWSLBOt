package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for !fn() {
		if time.Now().After(until) {
			t.Fatal("condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestReadSchedulerGlobalAndAccountBounds(t *testing.T) {
	s := NewReadScheduler(context.Background())
	defer s.Close()
	var active, maximum atomic.Int32
	var mu sync.Mutex
	perAccount := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint(i % 5)
			err := s.Do(context.Background(), id, fmt.Sprint(i), ReadBackground, func(ctx context.Context) error {
				n := active.Add(1)
				for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
				}
				mu.Lock()
				perAccount[id]++
				if perAccount[id] > 1 {
					t.Error("account was concurrent")
				}
				mu.Unlock()
				time.Sleep(time.Millisecond)
				mu.Lock()
				perAccount[id]--
				mu.Unlock()
				active.Add(-1)
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if maximum.Load() != 2 {
		t.Fatalf("global concurrency %d", maximum.Load())
	}
}

func TestReadSchedulerPriorityCoalesceCancelAndQueueCap(t *testing.T) {
	s := NewReadScheduler(context.Background())
	defer s.Close()
	block, release := make(chan struct{}), make(chan struct{})
	results := make(chan error, 24)
	go func() {
		results <- s.Do(context.Background(), "a", "block", ReadBackground, func(ctx context.Context) error {
			close(block)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	<-block
	var orderMu sync.Mutex
	order := []int{}
	add := func(key string, priority int, ctx context.Context) {
		go func() {
			results <- s.Do(ctx, "a", key, priority, func(context.Context) error {
				orderMu.Lock()
				order = append(order, priority)
				orderMu.Unlock()
				return nil
			})
		}()
	}
	add("directory", ReadBackground, context.Background())
	waitFor(t, func() bool { return s.Snapshot("a").QueueDepth == 1 })
	add("directory", ReadRefresh, context.Background())
	waitFor(t, func() bool { return s.Snapshot("a").Coalesced == 1 })
	add("renew", ReadRenewal, context.Background())
	for i := 0; i < 14; i++ {
		add(fmt.Sprint(i), ReadBackground, context.Background())
	}
	waitFor(t, func() bool { return s.Snapshot("a").QueueDepth == 16 })
	if err := s.Do(context.Background(), "a", "extra", ReadBackground, func(context.Context) error { return nil }); !errors.Is(err, ErrReadBusy) {
		t.Fatal("queue bound not enforced", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Do(ctx, "a", "cancelled", ReadRenewal, func(context.Context) error { t.Error("cancelled job executed"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	for range 18 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if len(order) != 16 || order[0] != ReadRenewal {
		t.Fatalf("unexpected ordering %v", order)
	}
}

func TestReadSchedulerCooldownCancellationAndFairness(t *testing.T) {
	s := NewReadScheduler(context.Background())
	defer s.Close()
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.Do(context.Background(), "hot", "initial", ReadBackground, func(ctx context.Context) error {
			close(started)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		})
	}()
	<-started
	s.Cooldown("hot", time.Now().Add(time.Minute))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	waiter := make(chan error, 1)
	go func() {
		waiter <- s.Do(ctx, "hot", "next", ReadBackground, func(context.Context) error { t.Error("cooldown bypassed"); return nil })
	}()
	if err := s.Do(context.Background(), "quiet", "directory", ReadBackground, func(context.Context) error { return nil }); err != nil {
		t.Fatal("hot account blocked quiet account", err)
	}
	if err := <-waiter; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	s.CancelAccount("hot")
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("running task ignored account cancellation", err)
	}
	close(release)
}

func TestLatencyWindowExpiryAndReset(t *testing.T) {
	now := time.Now()
	var window latencyWindow
	window.add(now.Add(-time.Minute), 999)
	if p, n := window.read(now); p != nil || n != 0 {
		t.Fatal("expired sample survived")
	}
	window.add(now, 12)
	if p, n := window.read(now); p == nil || *p != 12 || n != 1 {
		t.Fatal("fresh sample missing")
	}
	var empty latencyWindow
	if p, n := empty.read(now); p != nil || n != 0 {
		t.Fatal("reset invented zero latency")
	}
}
