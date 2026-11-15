package gateway

import (
	"context"
	"testing"
	"time"
)

func TestReadValueCoalescedWaitersShareImmutableResult(t *testing.T) {
	s := NewReadScheduler(context.Background())
	defer s.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	blocked := make(chan error, 1)
	go func() {
		blocked <- s.Do(context.Background(), "account", "block", ReadRefresh, func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	<-entered
	type result struct {
		value any
		err   error
	}
	results := make(chan result, 2)
	firstQueued := make(chan struct{})
	go func() {
		close(firstQueued)
		value, err := s.Value(context.Background(), "account", "members:123", ReadRefresh, func(context.Context) (any, error) { return "synthetic-roster", nil })
		results <- result{value, err}
	}()
	<-firstQueued
	deadline := time.Now().Add(time.Second)
	for s.Snapshot("account").QueueDepth != 1 {
		if time.Now().After(deadline) {
			t.Fatal("read did not queue")
		}
		time.Sleep(time.Millisecond)
	}
	go func() {
		value, err := s.Value(context.Background(), "account", "members:123", ReadRefresh, func(context.Context) (any, error) {
			t.Error("coalesced read ran another callback")
			return "wrong-result", nil
		})
		results <- result{value, err}
	}()
	for s.Snapshot("account").Coalesced != 1 {
		if time.Now().After(deadline) {
			t.Fatal("read did not coalesce")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-blocked; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got := <-results
		if got.err != nil || got.value != "synthetic-roster" {
			t.Fatal("waiter lost shared result", got)
		}
	}
}
