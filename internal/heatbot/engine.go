// Package heatbot is the Go chat-engine replacement. It has no native protocol,
// group-management, financial, plugin-execution or host-execution dependency.
package heatbot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrScope      = errors.New("invalid_scope_or_snapshot")
	ErrCapacity   = errors.New("engine_queue_full")
	ErrSuperseded = errors.New("candidate_superseded")
	ErrExpired    = errors.New("candidate_expired")
	ErrClosed     = errors.New("engine_closed")
)

type Scope struct {
	TenantID  string `json:"tenant_id"`
	GroupID   string `json:"group_id"`
	AccountID string `json:"account_id"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	ID             string    `json:"id"`
	Scope          Scope     `json:"scope"`
	BindingVersion int64     `json:"binding_version"`
	AccountEpoch   int64     `json:"account_epoch"`
	PersonaVersion int64     `json:"persona_version"`
	Persona        string    `json:"persona"`
	Messages       []Message `json:"messages"`
	ExpiresAt      time.Time `json:"expires_at"`
	Model          Model     `json:"model"`
}

type Proposal struct {
	ID             string    `json:"id"`
	Scope          Scope     `json:"scope"`
	BindingVersion int64     `json:"binding_version"`
	AccountEpoch   int64     `json:"account_epoch"`
	PersonaVersion int64     `json:"persona_version"`
	ExpiresAt      time.Time `json:"expires_at"`
	Text           string    `json:"text"`
}

type Result struct {
	Proposal Proposal
	Err      error
}

type Provider interface {
	Generate(context.Context, Request) (string, error)
}

type candidate struct {
	request Request
	ctx     context.Context
	result  chan Result
}

type groupKey struct{ tenant, group string }
type groupState struct {
	active  bool
	pending *candidate
}

type Engine struct {
	mu       sync.Mutex
	cond     *sync.Cond
	groups   map[groupKey]*groupState
	queue    []groupKey
	provider Provider
	ctx      context.Context
	cancel   context.CancelFunc
	workers  sync.WaitGroup
	closed   bool
}

func New(provider Provider) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{provider: provider, groups: make(map[groupKey]*groupState), ctx: ctx, cancel: cancel}
	e.cond = sync.NewCond(&e.mu)
	for range 4 {
		e.workers.Add(1)
		go e.worker()
	}
	return e
}

func (e *Engine) Submit(ctx context.Context, request Request) (<-chan Result, error) {
	if !valid(request) {
		return nil, ErrScope
	}
	// A caller cannot mutate the scope/configuration of an in-flight generation.
	request.Messages = append([]Message(nil), request.Messages...)
	job := &candidate{request: request, ctx: ctx, result: make(chan Result, 1)}
	key := groupKey{request.Scope.TenantID, request.Scope.GroupID}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrClosed
	}
	state := e.groups[key]
	if state == nil {
		if len(e.groups) >= 100 {
			return nil, ErrCapacity
		}
		state = &groupState{}
		e.groups[key] = state
	}
	if state.pending != nil {
		state.pending.result <- Result{Err: ErrSuperseded}
	} else if !state.active {
		e.queue = append(e.queue, key)
	}
	state.pending = job
	e.cond.Signal()
	return job.result, nil
}

func valid(r Request) bool {
	for _, field := range []string{r.ID, r.Scope.TenantID, r.Scope.GroupID, r.Scope.AccountID} {
		if strings.TrimSpace(field) == "" || len(field) > 200 {
			return false
		}
	}
	if r.BindingVersion < 1 || r.AccountEpoch < 1 || r.PersonaVersion < 1 ||
		len(r.Persona) > 16000 || len(r.Messages) == 0 || len(r.Messages) > 100 ||
		!r.ExpiresAt.After(time.Now()) || r.ExpiresAt.After(time.Now().Add(5*time.Minute)) {
		return false
	}
	size := 0
	for _, m := range r.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return false
		}
		size += len(m.Content)
	}
	return size <= 32768
}

func (e *Engine) worker() {
	defer e.workers.Done()
	for {
		e.mu.Lock()
		for len(e.queue) == 0 && !e.closed {
			e.cond.Wait()
		}
		if e.closed {
			e.mu.Unlock()
			return
		}
		key := e.queue[0]
		e.queue = e.queue[1:]
		state := e.groups[key]
		job := state.pending
		state.pending, state.active = nil, true
		e.mu.Unlock()
		e.generate(job)
		e.mu.Lock()
		state.active = false
		if state.pending != nil && !e.closed {
			e.queue = append(e.queue, key)
			e.cond.Signal()
		} else {
			delete(e.groups, key)
		}
		e.mu.Unlock()
	}
}

func (e *Engine) generate(job *candidate) {
	ctx, cancel := context.WithDeadline(job.ctx, job.request.ExpiresAt)
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	defer cancel()
	if ctx.Err() != nil {
		job.result <- Result{Err: ErrExpired}
		return
	}
	text, err := e.provider.Generate(ctx, job.request)
	if ctx.Err() != nil {
		err = ErrExpired
	}
	if err != nil {
		job.result <- Result{Err: err}
		return
	}
	r := job.request
	job.result <- Result{Proposal: Proposal{ID: r.ID, Scope: r.Scope, BindingVersion: r.BindingVersion,
		AccountEpoch: r.AccountEpoch, PersonaVersion: r.PersonaVersion, ExpiresAt: r.ExpiresAt, Text: text}}
}

func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	e.cancel()
	for _, state := range e.groups {
		if state.pending != nil {
			state.pending.result <- Result{Err: ErrClosed}
			state.pending = nil
		}
	}
	e.cond.Broadcast()
	e.mu.Unlock()
	e.workers.Wait()
}
