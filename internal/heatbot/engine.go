// Package heatbot is the Go chat-engine replacement. It has no native protocol,
// group-management, financial, plugin-execution or host-execution dependency.
package heatbot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	ErrScope      = errors.New("invalid_scope_or_snapshot")
	ErrCapacity   = errors.New("engine_queue_full")
	ErrSuperseded = errors.New("candidate_superseded")
	ErrExpired    = errors.New("candidate_expired")
	ErrClosed     = errors.New("engine_closed")
	ErrCancelled  = errors.New("candidate_cancelled")
)

type Scope struct {
	Platform  string `json:"platform"`
	TenantID  string `json:"tenant_id"`
	GroupID   string `json:"group_id"`
	AccountID string `json:"account_id"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	ID             string           `json:"id"`
	Scope          Scope            `json:"scope"`
	BindingVersion int64            `json:"binding_version"`
	AccountEpoch   int64            `json:"account_epoch"`
	PersonaVersion int64            `json:"persona_version"`
	Persona        string           `json:"persona"`
	Messages       []Message        `json:"messages"`
	ExpiresAt      time.Time        `json:"expires_at"`
	Model          Model            `json:"model"`
	ModelAlias     string           `json:"model_alias,omitempty"`
	ModelVersion   int64            `json:"model_version,omitempty"`
	GatewayToken   string           `json:"gateway_token,omitempty"`
	Settings       Settings         `json:"settings"`
	Context        *ContextSnapshot `json:"context,omitempty"`
}

type Proposal struct {
	ID              string    `json:"id"`
	Scope           Scope     `json:"scope"`
	BindingVersion  int64     `json:"binding_version"`
	AccountEpoch    int64     `json:"account_epoch"`
	PersonaVersion  int64     `json:"persona_version"`
	ExpiresAt       time.Time `json:"expires_at"`
	Text            string    `json:"text"`
	SettingsVersion int64     `json:"settings_version"`
	ContextVersion  int64     `json:"context_version,omitempty"`
	Usage           Usage     `json:"usage"`
}

type Result struct {
	Proposal Proposal
	Usage    Usage
	Err      error
}

type Provider interface {
	Generate(context.Context, Request) (string, error)
}

type DetailedProvider interface {
	Provider
	GenerateDetailed(context.Context, Request) (Generation, error)
}

type candidate struct {
	request Request
	ctx     context.Context
	result  chan Result
	cancel  context.CancelFunc
	stop    func() bool
}

type groupKey struct{ platform, tenant, group string }
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
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}
	var err error
	request, err = snapshot(request)
	if err != nil {
		return nil, err
	}
	deadline := request.ExpiresAt
	if request.Context != nil {
		deadline = minDeadline(deadline, request.Context.ExpiresAt)
	}
	jobCtx, cancel := context.WithDeadline(ctx, deadline)
	job := &candidate{request: request, ctx: jobCtx, result: make(chan Result, 1), cancel: cancel}
	key := groupKey{request.Scope.Platform, request.Scope.TenantID, request.Scope.GroupID}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		cancel()
		return nil, ErrClosed
	}
	state := e.groups[key]
	if state == nil {
		if len(e.groups) >= 300 {
			cancel()
			return nil, ErrCapacity
		}
		state = &groupState{}
		e.groups[key] = state
	}
	if state.pending != nil {
		state.pending.stop()
		state.pending.cancel()
		state.pending.result <- Result{Err: ErrSuperseded, Usage: Usage{Status: "not_requested"}}
	} else if !state.active {
		e.queue = append(e.queue, key)
	}
	state.pending = job
	job.stop = context.AfterFunc(jobCtx, func() { e.cancelPending(key, job) })
	e.cond.Signal()
	return job.result, nil
}

func (e *Engine) cancelPending(key groupKey, job *candidate) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.groups[key]
	if state == nil || state.pending != job {
		return
	}
	state.pending = nil
	job.cancel()
	job.result <- Result{Err: e.generationError(job.ctx), Usage: Usage{Status: "not_requested"}}
	if !state.active {
		delete(e.groups, key)
		for i, queued := range e.queue {
			if queued == key {
				e.queue = append(e.queue[:i], e.queue[i+1:]...)
				break
			}
		}
	}
}

func valid(r Request) bool {
	if r.Scope.Platform != "wsl" && r.Scope.Platform != "lajiaoliao" {
		return false
	}
	for _, field := range []string{r.ID, r.Scope.TenantID, r.Scope.GroupID, r.Scope.AccountID} {
		if strings.TrimSpace(field) == "" || strings.TrimSpace(field) != field || len(field) > 200 || !utf8.ValidString(field) {
			return false
		}
	}
	if r.BindingVersion < 1 || r.AccountEpoch < 1 || r.PersonaVersion < 1 ||
		len(r.Persona) > 16000 || !utf8.ValidString(r.Persona) || len(r.Messages) == 0 || len(r.Messages) > 100 ||
		!r.ExpiresAt.After(time.Now()) || r.ExpiresAt.After(time.Now().Add(5*time.Minute)) {
		return false
	}
	size := 0
	for _, m := range r.Messages {
		if (m.Role != "user" && m.Role != "assistant") || !utf8.ValidString(m.Content) {
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
		job.stop()
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
	defer job.cancel()
	deadline := minDeadline(job.request.ExpiresAt, time.Now().Add(90*time.Second))
	if parentDeadline, ok := job.ctx.Deadline(); ok {
		deadline = minDeadline(deadline, parentDeadline)
	}
	if job.request.Context != nil {
		deadline = minDeadline(deadline, job.request.Context.ExpiresAt)
	}
	ctx, cancel := context.WithDeadline(job.ctx, deadline)
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	defer cancel()
	if ctx.Err() != nil {
		job.result <- Result{Err: e.generationError(ctx), Usage: Usage{Status: "not_requested"}}
		return
	}
	generation := Generation{Usage: unknownUsage()}
	var err error
	if provider, ok := e.provider.(DetailedProvider); ok {
		generation, err = provider.GenerateDetailed(ctx, job.request)
		generation.Usage = normalizeUsage(generation.Usage)
	} else {
		generation.Text, err = e.provider.Generate(ctx, job.request)
	}
	if ctx.Err() != nil {
		err = e.generationError(ctx)
	}
	if err == nil && (strings.TrimSpace(generation.Text) == "" || len(generation.Text) > 16000 || !utf8.ValidString(generation.Text)) {
		err = errors.New("provider_response_invalid")
	}
	if err != nil {
		job.result <- Result{Err: err, Usage: generation.Usage}
		return
	}
	r := job.request
	var contextVersion int64
	if r.Context != nil {
		contextVersion = r.Context.Version
	}
	job.result <- Result{Usage: generation.Usage, Proposal: Proposal{ID: r.ID, Scope: r.Scope, BindingVersion: r.BindingVersion,
		AccountEpoch: r.AccountEpoch, PersonaVersion: r.PersonaVersion, ExpiresAt: deadline, Text: generation.Text,
		SettingsVersion: r.Settings.Version, ContextVersion: contextVersion, Usage: generation.Usage}}
}

func minDeadline(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func contextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrExpired
	}
	return ErrCancelled
}

func (e *Engine) generationError(ctx context.Context) error {
	if e.ctx.Err() != nil {
		return ErrClosed
	}
	return contextError(ctx)
}

func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	e.cancel()
	for _, state := range e.groups {
		if state.pending != nil {
			state.pending.stop()
			state.pending.cancel()
			state.pending.result <- Result{Err: ErrClosed, Usage: Usage{Status: "not_requested"}}
			state.pending = nil
		}
	}
	e.cond.Broadcast()
	e.mu.Unlock()
	e.workers.Wait()
}
