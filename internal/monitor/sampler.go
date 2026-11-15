package monitor

import (
	"context"
	"sync"
	"time"
)

// ProcessReader is injected by the executable; the monitoring core has no
// dependency on platform-specific process libraries.
type ProcessReader interface {
	Read(context.Context) (ProcessSample, error)
}

type ProcessSample struct {
	CPUSeconds float64
	RSSBytes   uint64
	StartedAt  time.Time
}

type Sampler struct {
	mu      sync.RWMutex
	reader  *ResourceReader
	process ProcessReader
	source  string
	last    Resources
	count   uint64
}

func NewSampler(source string, process ProcessReader) *Sampler {
	return &Sampler{reader: NewResourceReader(), process: process, source: source}
}

func (s *Sampler) Collect(ctx context.Context) {
	start := time.Now()
	out := s.reader.Read()
	out.Source = s.source
	var process ProcessSample
	var err error
	if s.process != nil {
		process, err = s.process.Read(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	if s.process == nil || err != nil || ctx.Err() != nil {
		s.last.Available = false
		s.last.ErrorCode = "process_sample_unavailable"
		s.last.CollectionCount = s.count
		return
	}
	out.ProcessRSS = &process.RSSBytes
	out.ProcessCPUSeconds = &process.CPUSeconds
	out.ProcessStartedAt = &process.StartedAt
	out.Available = true
	out.CollectionCount = s.count
	out.CollectionMS = float64(time.Since(start).Microseconds()) / 1000
	s.last = out
}

func (s *Sampler) Snapshot() Resources {
	s.mu.RLock()
	out := s.last
	s.mu.RUnlock()
	out.Stale = out.SampledAt.IsZero() || time.Since(out.SampledAt) > 15*time.Second
	return out
}

func (s *Sampler) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, time.Second)
		s.Collect(work)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
