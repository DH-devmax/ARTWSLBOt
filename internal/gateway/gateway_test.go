package gateway

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testLedger struct {
	committed atomic.Bool
	err       error
}

func (l *testLedger) Commit(_ context.Context, _ []Message) error {
	if l.err == nil {
		l.committed.Store(true)
	}
	return l.err
}
func message() Message {
	return Message{AccountID: "account", GroupID: "group", NativeID: "message", SenderID: "sender", ReceivedAt: time.Now(), Payload: []byte("message")}
}

func TestDurableBeforeACK(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ledger := &testLedger{}
	acked := make(chan struct{}, 1)
	ingest := NewIngestor("account", 4, ledger, func(context.Context, Message) error {
		if !ledger.committed.Load() {
			t.Error("ACK before commit")
		}
		acked <- struct{}{}
		return nil
	}, nil)
	if err := ingest.Offer(message()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- ingest.Run(ctx) }()
	select {
	case <-acked:
	case <-time.After(time.Second):
		t.Fatal("ACK timeout")
	}
	cancel()
	<-done
}
func TestFailedCommitNeverACKs(t *testing.T) {
	ledger := &testLedger{err: errors.New("database unavailable")}
	ingest := NewIngestor("account", 1, ledger, func(context.Context, Message) error { t.Error("ACK after failed commit"); return nil }, nil)
	if err := ingest.Offer(message()); err != nil {
		t.Fatal(err)
	}
	if err := ingest.Offer(message()); !errors.Is(err, ErrBackpressure) {
		t.Fatal("queue is not bounded")
	}
	if err := ingest.Run(context.Background()); err == nil {
		t.Fatal("database failure hidden")
	}
}

func TestIngressIsBoundedByBytesAsWellAsCount(t *testing.T) {
	ingest := NewIngestor("account", 128, &testLedger{}, func(context.Context, Message) error { return nil }, nil)
	m := message()
	m.Payload = make([]byte, 1024*1024)
	accepted := 0
	for range 128 {
		err := ingest.Offer(m)
		if errors.Is(err, ErrBackpressure) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		accepted++
	}
	if accepted == 0 || accepted > 16 {
		t.Fatalf("ingress byte budget not enforced: %d MiB accepted", accepted)
	}
}
func TestSingleflightAndSerialization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewScheduler(ctx)
	var calls, inflight, max atomic.Int32
	start := make(chan struct{})
	release := make(chan struct{})
	fn := func(context.Context) ([]byte, error) {
		calls.Add(1)
		n := inflight.Add(1)
		for {
			old := max.Load()
			if n <= old || max.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case start <- struct{}{}:
		default:
		}
		<-release
		inflight.Add(-1)
		return []byte("roster"), nil
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Read(ctx, "roster/group", time.Minute, 1, fn); err != nil {
				t.Error(err)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 || max.Load() != 1 {
		t.Fatalf("calls=%d inflight=%d", calls.Load(), max.Load())
	}
	for _, key := range []string{"a", "b", "c"} {
		if _, err := s.Read(ctx, key, time.Minute, 1, fn); err != nil {
			t.Fatal(err)
		}
	}
	if max.Load() != 1 {
		t.Fatal("account HTTP state was used concurrently")
	}
}
func TestRetryAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	if got := RetryAfter("31", now); got != 31*time.Second {
		t.Fatal(got)
	}
	if got := RetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now); got != time.Minute {
		t.Fatal(got)
	}
	if got := RetryAfter("99999999999", now); got != 24*time.Hour {
		t.Fatal(got)
	}
}

func TestRateLimitSnapshotDoesNotBecomePermissionEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewScheduler(ctx)
	var calls int
	fn := func(context.Context) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("old-roster"), nil
		}
		return nil, &RateLimited{After: time.Minute}
	}
	first, err := s.Read(ctx, "members/group", time.Millisecond, 1, fn)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Millisecond)
	stale, err := s.Read(ctx, "members/group", time.Millisecond, 1, fn)
	if err == nil || !stale.Stale || stale.ObservedAt != first.ObservedAt {
		t.Fatal("429 did not preserve timestamped snapshot")
	}
	start := time.Now()
	again, err := s.Read(ctx, "members/group", time.Millisecond, 1, fn)
	if err == nil || !again.Stale || time.Since(start) > 100*time.Millisecond {
		t.Fatal("a page read waited through the native cooldown")
	}
	fresh, err := s.ReadFresh(ctx, "members/group", 0, fn)
	if err == nil || fresh.Data != nil || fresh.Stale {
		t.Fatal("stale roster was returned as fresh permission evidence")
	}
	if calls != 2 {
		t.Fatalf("cooldown did not suppress redundant native reads: %d", calls)
	}
}

func TestFreshBypassesCacheAndPanicIsContained(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewScheduler(ctx)
	var n int
	fn := func(context.Context) ([]byte, error) {
		n++
		return []byte{byte(n)}, nil
	}
	if _, err := s.Read(ctx, "role", time.Hour, 1, fn); err != nil {
		t.Fatal(err)
	}
	result, err := s.ReadFresh(ctx, "role", 0, fn)
	if err != nil || !bytes.Equal(result.Data, []byte{2}) {
		t.Fatal("fresh read used cached role")
	}
	if _, err := s.ReadFresh(ctx, "panic", 0, func(context.Context) ([]byte, error) {
		panic("untrusted response")
	}); err == nil {
		t.Fatal("native HTTP panic not contained")
	}
	if _, err := s.ReadFresh(ctx, "still-alive", 0, fn); err != nil {
		t.Fatal("scheduler stopped after a poisoned response")
	}
}
