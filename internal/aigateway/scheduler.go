package aigateway

import (
	"context"
	"errors"
	"sync"
)

var ErrBusy = errors.New("ai_gateway_queue_full")

type waiter struct {
	ctx     context.Context
	tenant  string
	ready   chan struct{}
	granted bool
}
type Scheduler struct {
	mu       sync.Mutex
	active   int
	byTenant map[string]int
	queue    []*waiter
	last     string
}

func NewScheduler() *Scheduler { return &Scheduler{byTenant: map[string]int{}} }
func (s *Scheduler) Acquire(ctx context.Context, tenant string) (func(), error) {
	if tenant == "" {
		return nil, ErrBusy
	}
	s.mu.Lock()
	waiting := 0
	for _, w := range s.queue {
		if w.tenant == tenant {
			waiting++
		}
	}
	if len(s.queue) >= 64 || waiting >= 8 {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	w := &waiter{ctx: ctx, tenant: tenant, ready: make(chan struct{})}
	s.queue = append(s.queue, w)
	s.dispatch()
	s.mu.Unlock()
	select {
	case <-w.ready:
		var once sync.Once
		release := func() { once.Do(func() { s.mu.Lock(); s.active--; s.byTenant[tenant]--; s.dispatch(); s.mu.Unlock() }) }
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	case <-ctx.Done():
		s.mu.Lock()
		if w.granted {
			s.active--
			s.byTenant[tenant]--
		} else {
			for i, q := range s.queue {
				if q == w {
					s.queue = append(s.queue[:i], s.queue[i+1:]...)
					break
				}
			}
		}
		s.dispatch()
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}
func (s *Scheduler) dispatch() {
	for s.active < 4 {
		pick := -1
		for i, w := range s.queue {
			if w.ctx.Err() != nil || s.byTenant[w.tenant] >= 2 {
				continue
			}
			if pick < 0 {
				pick = i
			}
			if w.tenant != s.last {
				pick = i
				break
			}
		}
		if pick < 0 {
			return
		}
		w := s.queue[pick]
		s.queue = append(s.queue[:pick], s.queue[pick+1:]...)
		s.active++
		s.byTenant[w.tenant]++
		s.last = w.tenant
		w.granted = true
		close(w.ready)
	}
}
