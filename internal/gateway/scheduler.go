package gateway

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type RateLimited struct{ After time.Duration }

func (e *RateLimited) Error() string { return "native_rate_limited" }

func RetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 1 {
			return time.Second
		}
		if seconds > 86400 {
			return 24 * time.Hour
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		delay := date.Sub(now)
		if delay < time.Second {
			return time.Second
		}
		if delay > 24*time.Hour {
			return 24 * time.Hour
		}
		return delay
	}
	return 30 * time.Second
}

type ReadResult struct {
	Data       []byte
	ObservedAt time.Time
	Stale      bool
}
type readTask struct {
	key      string
	priority int
	ttl      time.Duration
	fn       func(context.Context) ([]byte, error)
	ready    chan struct{}
	result   ReadResult
	err      error
}
type cachedRead struct {
	data                        []byte
	observed, until, staleUntil time.Time
}
type Scheduler struct {
	ctx        context.Context
	mu         sync.Mutex
	pending    []*readTask
	flights    map[string]*readTask
	cache      map[string]cachedRead
	cacheBytes int
	wake       chan struct{}
	cooldown   time.Time
	highStreak int
}

// One scheduler owns one account's mutable signing and response state.
func NewScheduler(ctx context.Context) *Scheduler {
	s := &Scheduler{ctx: ctx, flights: map[string]*readTask{}, cache: map[string]cachedRead{}, wake: make(chan struct{}, 1)}
	go s.run()
	return s
}

func (s *Scheduler) Read(ctx context.Context, key string, ttl time.Duration, priority int, fn func(context.Context) ([]byte, error)) (ReadResult, error) {
	return s.read(ctx, key, ttl, priority, fn, true)
}

// ReadFresh never returns cached data as permission evidence, including during 429.
func (s *Scheduler) ReadFresh(ctx context.Context, key string, priority int, fn func(context.Context) ([]byte, error)) (ReadResult, error) {
	return s.read(ctx, key, 0, priority, fn, false)
}

func (s *Scheduler) read(ctx context.Context, key string, ttl time.Duration, priority int, fn func(context.Context) ([]byte, error), allowStale bool) (ReadResult, error) {
	if key == "" || len(key) > 512 || fn == nil || ttl < 0 || ttl > time.Hour || priority < 0 || priority > 2 {
		return ReadResult{}, errors.New("invalid_read_request")
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return ReadResult{}, err
	}
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return ReadResult{}, err
	}
	now := time.Now()
	if item, ok := s.cache[key]; ok && ttl > 0 && now.Before(item.until) {
		s.mu.Unlock()
		return ReadResult{Data: append([]byte(nil), item.data...), ObservedAt: item.observed}, nil
	}
	if now.Before(s.cooldown) {
		result := ReadResult{}
		if item, ok := s.cache[key]; ok && allowStale && now.Before(item.staleUntil) {
			result = ReadResult{Data: append([]byte(nil), item.data...), ObservedAt: item.observed, Stale: true}
		}
		after := s.cooldown.Sub(now)
		s.mu.Unlock()
		return result, &RateLimited{After: after}
	}
	task := s.flights[key]
	if task == nil {
		if len(s.pending) >= 64 || len(s.flights) >= 65 {
			s.mu.Unlock()
			return ReadResult{}, ErrBackpressure
		}
		task = &readTask{key: key, priority: priority, ttl: ttl, fn: fn, ready: make(chan struct{})}
		s.flights[key] = task
		s.pending = append(s.pending, task)
		select {
		case s.wake <- struct{}{}:
		default:
		}
	} else if priority < task.priority {
		task.priority = priority
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ReadResult{}, ctx.Err()
	case <-s.ctx.Done():
		return ReadResult{}, s.ctx.Err()
	case <-task.ready:
		if !allowStale && task.result.Stale {
			return ReadResult{}, task.err
		}
		return ReadResult{Data: append([]byte(nil), task.result.Data...), ObservedAt: task.result.ObservedAt, Stale: task.result.Stale}, task.err
	}
}

func (s *Scheduler) run() {
	for {
		s.mu.Lock()
		if s.ctx.Err() != nil {
			for _, task := range s.pending {
				task.err = s.ctx.Err()
				close(task.ready)
			}
			s.pending = nil
			s.mu.Unlock()
			return
		}
		if len(s.pending) == 0 {
			s.mu.Unlock()
			select {
			case <-s.ctx.Done():
			case <-s.wake:
			}
			continue
		}
		delay := time.Until(s.cooldown)
		if delay > 0 {
			s.mu.Unlock()
			timer := time.NewTimer(delay)
			select {
			case <-s.ctx.Done():
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		best := 0
		for i := 1; i < len(s.pending); i++ {
			if s.pending[i].priority < s.pending[best].priority {
				best = i
			}
		}
		if s.highStreak >= 4 {
			for i, task := range s.pending {
				if task.priority > 0 {
					best = i
					break
				}
			}
			s.highStreak = 0
		}
		task := s.pending[best]
		s.pending = append(s.pending[:best], s.pending[best+1:]...)
		if task.priority == 0 {
			s.highStreak++
		}
		s.mu.Unlock()
		call, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		data, err := invokeRead(call, task.fn)
		cancel()
		now := time.Now()
		s.mu.Lock()
		task.err = err
		if err == nil {
			task.result = ReadResult{Data: append([]byte(nil), data...), ObservedAt: now}
			if task.ttl > 0 {
				for k, item := range s.cache {
					if now.After(item.staleUntil) {
						s.cacheBytes -= len(item.data)
						delete(s.cache, k)
					}
				}
				if old, ok := s.cache[task.key]; ok {
					s.cacheBytes -= len(old.data)
					delete(s.cache, task.key)
				}
				for len(s.cache) >= 512 || s.cacheBytes+len(data) > 64*1024*1024 {
					var oldestKey string
					var oldest time.Time
					for k, item := range s.cache {
						if oldest.IsZero() || item.observed.Before(oldest) {
							oldest, oldestKey = item.observed, k
						}
					}
					s.cacheBytes -= len(s.cache[oldestKey].data)
					delete(s.cache, oldestKey)
				}
				retention := min(max(task.ttl*10, 5*time.Minute), time.Hour)
				s.cache[task.key] = cachedRead{data: append([]byte(nil), data...), observed: now,
					until: now.Add(task.ttl), staleUntil: now.Add(retention)}
				s.cacheBytes += len(data)
			}
		} else {
			var limited *RateLimited
			if errors.As(err, &limited) {
				after := limited.After
				if after < time.Second {
					after = time.Second
				}
				if after > 24*time.Hour {
					after = 24 * time.Hour
				}
				if until := now.Add(after); until.After(s.cooldown) {
					s.cooldown = until
				}
			}
			if item, ok := s.cache[task.key]; ok && now.Before(item.staleUntil) {
				task.result = ReadResult{Data: append([]byte(nil), item.data...), ObservedAt: item.observed, Stale: true}
			}
		}
		delete(s.flights, task.key)
		close(task.ready)
		s.mu.Unlock()
	}
}

func invokeRead(ctx context.Context, fn func(context.Context) ([]byte, error)) (data []byte, err error) {
	defer func() {
		if recover() != nil {
			data, err = nil, errors.New("native_read_panicked")
		}
	}()
	data, err = fn(ctx)
	if len(data) > 4*1024*1024 {
		return nil, errors.New("native_response_limit")
	}
	return data, err
}
