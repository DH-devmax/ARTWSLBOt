package gateway

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	ReadRenewal = iota
	ReadRefresh
	ReadBackground
	ReadQueueLimit  = 16
	ReadConcurrency = 2
)

var ErrReadBusy = errors.New("native_read_queue_full")
var ErrReadOwner = errors.New("native_read_owner_unavailable")

type ReadSnapshot struct {
	Requests60s   uint64     `json:"requests_60s"`
	PerSecond     float64    `json:"http_per_second"`
	Observed      bool       `json:"observed"`
	QueueDepth    int        `json:"queue_depth"`
	QueueCapacity int        `json:"queue_capacity"`
	Running       bool       `json:"running"`
	OldestWaiting *time.Time `json:"oldest_waiting_at"`
	CooldownUntil *time.Time `json:"cooldown_until"`
	Completed     uint64     `json:"completed"`
	Failed        uint64     `json:"failed"`
	Coalesced     uint64     `json:"coalesced"`
	WaitMS        *float64   `json:"last_wait_ms"`
	RequestMS     *float64   `json:"last_request_ms"`
	SampledAt     time.Time  `json:"sampled_at"`
}

type readJob struct {
	key      string
	priority int
	created  time.Time
	ctx      context.Context
	cancel   context.CancelFunc
	fn       func(context.Context) (any, error)
	value    any
	done     chan struct{}
	err      error
	waiters  int
}

type readAccount struct {
	queue    []*readJob
	running  *readJob
	view     ReadSnapshot
	lastUsed time.Time
	requests [60]struct {
		second int64
		count  uint64
	}
}

// ReadScheduler has one account lane and a shared bounded execution budget.
// It schedules business HTTP only; socket reception and ACK never enter it.
type ReadScheduler struct {
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	accounts    map[string]*readAccount
	active      int
	lastAccount string
	wake        chan struct{}
	done        chan struct{}
	wg          sync.WaitGroup
}

func NewReadScheduler(ctx context.Context) *ReadScheduler {
	ctx, cancel := context.WithCancel(ctx)
	s := &ReadScheduler{ctx: ctx, cancel: cancel, accounts: map[string]*readAccount{},
		wake: make(chan struct{}, 1), done: make(chan struct{})}
	go s.run()
	return s
}

func (s *ReadScheduler) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *ReadScheduler) Close() {
	s.cancel()
	<-s.done
	s.wg.Wait()
}

func (s *ReadScheduler) Do(ctx context.Context, account, key string, priority int, fn func(context.Context) error) error {
	if fn == nil {
		return errors.New("invalid_read_request")
	}
	_, err := s.Value(ctx, account, key, priority, func(work context.Context) (any, error) {
		return nil, fn(work)
	})
	return err
}

// Value shares the immutable result with all coalesced waiters. Returning data
// through a caller's closure would leave other waiters with empty results.
func (s *ReadScheduler) Value(ctx context.Context, account, key string, priority int, fn func(context.Context) (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if account == "" || len(account) > 200 || len(key) > 512 || priority < ReadRenewal || priority > ReadBackground || fn == nil {
		return nil, errors.New("invalid_read_request")
	}
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil, ErrReadOwner
	}
	a := s.accounts[account]
	if a == nil {
		for id, old := range s.accounts {
			if old.running == nil && len(old.queue) == 0 && time.Since(old.lastUsed) > time.Minute {
				delete(s.accounts, id)
			}
		}
		if len(s.accounts) >= 64 {
			s.mu.Unlock()
			return nil, ErrReadBusy
		}
		a = &readAccount{}
		s.accounts[account] = a
	}
	a.lastUsed = time.Now()
	var job *readJob
	// Only pending jobs coalesce: a fresh request must not reuse an old running
	// callback whose session/epoch may already have been invalidated.
	for _, candidate := range a.queue {
		if priority != ReadRenewal && key != "" && candidate.key == key && candidate.ctx.Err() == nil {
			job = candidate
			job.priority = min(job.priority, priority)
			a.view.Coalesced++
			break
		}
	}
	if job == nil {
		if len(a.queue) >= ReadQueueLimit {
			s.mu.Unlock()
			return nil, ErrReadBusy
		}
		deadline := time.Now().Add(15 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		work, cancel := context.WithDeadline(s.ctx, deadline)
		job = &readJob{key: key, priority: priority, created: time.Now().UTC(), ctx: work, cancel: cancel,
			fn: fn, done: make(chan struct{})}
		a.queue = append(a.queue, job)
	}
	job.waiters++
	s.mu.Unlock()
	s.notify()
	defer func() {
		s.mu.Lock()
		job.waiters--
		if job.waiters == 0 {
			job.cancel()
		}
		s.mu.Unlock()
		s.notify()
	}()
	select {
	case <-job.done:
		return job.value, job.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, ErrReadOwner
	}
}

func (s *ReadScheduler) CancelAccount(account string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.accounts[account]; a != nil {
		for _, job := range a.queue {
			job.cancel()
		}
		if a.running != nil {
			a.running.cancel()
		}
	}
	s.notify()
}

func (s *ReadScheduler) Cooldown(account string, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.accounts[account]; a != nil {
		if a.view.CooldownUntil == nil || until.After(*a.view.CooldownUntil) {
			a.view.CooldownUntil = &until
		}
	}
}

func (s *ReadScheduler) Snapshot(account string) ReadSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ReadSnapshot{Observed: s.ctx.Err() == nil, QueueCapacity: ReadQueueLimit, SampledAt: time.Now().UTC()}
	if a := s.accounts[account]; a != nil {
		out = a.view
		out.Observed = s.ctx.Err() == nil
		out.QueueCapacity = ReadQueueLimit
		out.QueueDepth = len(a.queue)
		out.Running = a.running != nil
		out.SampledAt = time.Now().UTC()
		now := time.Now().Unix()
		for _, bucket := range a.requests {
			if bucket.second <= now && now-bucket.second < 60 {
				out.Requests60s += bucket.count
			}
		}
		out.PerSecond = float64(out.Requests60s) / 60
		for _, job := range a.queue {
			if job.ctx.Err() == nil && (out.OldestWaiting == nil || job.created.Before(*out.OldestWaiting)) {
				value := job.created
				out.OldestWaiting = &value
			}
		}
	}
	return out
}

func (s *ReadScheduler) ObserveRequest(account string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.accounts[account]; a != nil {
		second := time.Now().Unix()
		bucket := &a.requests[second%60]
		if bucket.second != second {
			bucket.second, bucket.count = second, 0
		}
		bucket.count++
	}
}

func (s *ReadScheduler) run() {
	defer close(s.done)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		s.dispatch()
		select {
		case <-s.ctx.Done():
			s.mu.Lock()
			for _, a := range s.accounts {
				for _, job := range a.queue {
					job.cancel()
					job.err = ErrReadOwner
					close(job.done)
				}
				a.queue = nil
				if a.running != nil {
					a.running.cancel()
				}
			}
			s.mu.Unlock()
			return
		case <-s.wake:
		case <-tick.C:
		}
	}
}

func (s *ReadScheduler) dispatch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.accounts))
	for id, a := range s.accounts {
		kept := a.queue[:0]
		for _, job := range a.queue {
			if err := job.ctx.Err(); err != nil {
				job.err = err
				job.cancel()
				close(job.done)
			} else {
				kept = append(kept, job)
			}
		}
		a.queue = kept
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// Rotate account order after each dispatch; priorities apply inside a lane.
	start := sort.SearchStrings(ids, s.lastAccount)
	if start < len(ids) && ids[start] == s.lastAccount {
		start++
	}
	for n := 0; n < len(ids) && s.active < ReadConcurrency; n++ {
		id := ids[(start+n)%len(ids)]
		a := s.accounts[id]
		if a.running != nil || len(a.queue) == 0 {
			continue
		}
		if a.view.CooldownUntil != nil && time.Now().Before(*a.view.CooldownUntil) {
			continue
		}
		index := 0
		for i, job := range a.queue {
			if job.priority < a.queue[index].priority {
				index = i
			}
		}
		job := a.queue[index]
		a.queue = append(a.queue[:index], a.queue[index+1:]...)
		a.running = job
		s.active++
		s.lastAccount = id
		wait := float64(time.Since(job.created).Microseconds()) / 1000
		a.view.WaitMS = &wait
		s.wg.Add(1)
		go func(a *readAccount, job *readJob) {
			defer s.wg.Done()
			start := time.Now()
			value, err := job.fn(job.ctx)
			job.cancel()
			elapsed := float64(time.Since(start).Microseconds()) / 1000
			s.mu.Lock()
			a.running = nil
			a.lastUsed = time.Now()
			a.view.RequestMS = &elapsed
			a.view.Completed++
			if err != nil {
				a.view.Failed++
			}
			s.active--
			job.err = err
			if err == nil {
				job.value = value
			}
			close(job.done)
			s.mu.Unlock()
			s.notify()
		}(a, job)
	}
}
