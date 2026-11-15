package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type processFixture struct {
	fail  bool
	calls int
}

func (p *processFixture) Read(context.Context) (ProcessSample, error) {
	p.calls++
	if p.fail {
		return ProcessSample{}, errors.New("private error")
	}
	return ProcessSample{CPUSeconds: 1, RSSBytes: 4096, StartedAt: time.Unix(100, 0)}, nil
}

func TestSamplerFailureFreshnessAndReaders(t *testing.T) {
	p := &processFixture{}
	s := NewSampler("api_self", p)
	s.Collect(t.Context())
	first := s.Snapshot()
	if !first.Available || first.Stale || *first.ProcessRSS != 4096 {
		t.Fatal(first)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = s.Snapshot()
			}
		}()
	}
	wg.Wait()
	if p.calls != 1 {
		t.Fatal("readers caused collection")
	}
	p.fail = true
	s.Collect(t.Context())
	failed := s.Snapshot()
	if failed.Available || failed.ErrorCode != "process_sample_unavailable" || failed.SampledAt != first.SampledAt {
		t.Fatal("failed sample changed observation timestamp", failed)
	}
	s.last.SampledAt = time.Now().Add(-16 * time.Second)
	if !s.Snapshot().Stale {
		t.Fatal("old observation fresh")
	}
	p.fail = false
	s.Collect(t.Context())
	if !s.Snapshot().Available || s.Snapshot().ErrorCode != "" {
		t.Fatal("did not recover")
	}
}

func TestSamplerCancellation(t *testing.T) {
	s := NewSampler("api_self", &processFixture{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sampler leaked")
	}
}

func BenchmarkSnapshot(b *testing.B) {
	s := NewSampler("api_self", &processFixture{})
	s.Collect(b.Context())
	b.ResetTimer()
	for range b.N {
		_ = s.Snapshot()
	}
}
