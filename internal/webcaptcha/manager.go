package webcaptcha

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const (
	challengeTTL      = 120 * time.Second
	ticketTTL         = 60 * time.Second
	rateWindow        = 5 * time.Minute
	maxGenerating     = 2
	maxEntries        = 1024
	maxRateBuckets    = 4096
	issueIPLimit      = 60
	issueBrowserLimit = 20
	verifyIPLimit     = 120
	answerTolerance   = 5
	idBytes           = 32
	idLength          = 43
)

const (
	rateIssueIP byte = iota
	rateIssueBrowser
	rateVerifyIP
)

type challengeState struct {
	scope   Scope
	x, y    int
	expires time.Time
}

type ticketState struct {
	scope   Scope
	expires time.Time
}

type rateKey struct {
	kind          byte
	first, second string
}

type rateBucket struct {
	count   int
	expires time.Time
}

type rateLimit struct {
	key   rateKey
	limit int
}

// Manager holds bounded, process-local state. It starts no background workers.
type Manager struct {
	mu         sync.Mutex
	generator  Generator
	now        func() time.Time
	generating int
	challenges map[string]challengeState
	tickets    map[string]ticketState
	buckets    map[rateKey]rateBucket
}

// New uses the supplied generator. A nil generator leaves issuance unavailable.
func New(generator Generator) *Manager {
	return &Manager{
		generator:  generator,
		now:        time.Now,
		challenges: make(map[string]challengeState),
		tickets:    make(map[string]ticketState),
		buckets:    make(map[rateKey]rateBucket),
	}
}

// Issue runs generation synchronously outside the state lock. Cancellation
// discards its result without interrupting the generator.
func (m *Manager) Issue(ctx context.Context, scope Scope, previous string) (Challenge, error) {
	if err := ctx.Err(); err != nil {
		return Challenge{}, err
	}
	if !validScope(scope) {
		return Challenge{}, ErrInvalid
	}
	if m.generator == nil {
		return Challenge{}, ErrUnavailable
	}

	m.mu.Lock()
	now := m.now()
	m.prune(now)
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return Challenge{}, err
	}
	old, exists := m.challenges[previous]
	replace := exists && old.scope == scope
	size := len(m.challenges) + len(m.tickets) + m.generating
	if replace {
		size--
	}
	if m.generating >= maxGenerating || size >= maxEntries {
		m.mu.Unlock()
		return Challenge{}, ErrBusy
	}
	if err := m.takeRates(now,
		rateLimit{rateKey{kind: rateIssueIP, first: scope.IP}, issueIPLimit},
		rateLimit{rateKey{kind: rateIssueBrowser, first: scope.Browser, second: scope.Session}, issueBrowserLimit},
	); err != nil {
		m.mu.Unlock()
		return Challenge{}, err
	}
	if replace {
		delete(m.challenges, previous)
	}
	// Pending generation reserves both a generation slot and a state entry.
	m.generating++
	m.mu.Unlock()

	puzzle, err := runGenerator(m.generator)
	var id string
	if err == nil && ctx.Err() == nil {
		id, err = randomID()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.generating--
	if canceled := ctx.Err(); canceled != nil {
		return Challenge{}, canceled
	}
	if err != nil {
		return Challenge{}, ErrUnavailable
	}
	now = m.now()
	m.prune(now)
	if m.hasID(id) {
		return Challenge{}, ErrUnavailable
	}
	expires := now.Add(challengeTTL).UTC()
	m.challenges[id] = challengeState{scope: scope, x: puzzle.X, y: puzzle.Y, expires: expires}
	return Challenge{ChallengeID: id, Image: puzzle.Image, ExpiresAt: expires}, nil
}

// Verify consumes a found challenge before checking scope, rate or position.
// A rejected attempt cannot retry that challenge.
func (m *Manager) Verify(scope Scope, id string, x, y int) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.prune(now)
	state, exists := m.challenges[id]
	delete(m.challenges, id)
	if !validScope(scope) {
		return Ticket{}, ErrInvalid
	}
	if err := m.takeRates(now, rateLimit{rateKey{kind: rateVerifyIP, first: scope.IP}, verifyIPLimit}); err != nil {
		return Ticket{}, err
	}
	if !exists || state.scope != scope ||
		x < state.x-answerTolerance || x > state.x+answerTolerance ||
		y < state.y-answerTolerance || y > state.y+answerTolerance {
		return Ticket{}, ErrInvalid
	}
	ticket, err := randomID()
	if err != nil {
		return Ticket{}, ErrUnavailable
	}
	if m.hasID(ticket) {
		return Ticket{}, ErrUnavailable
	}
	expires := now.Add(ticketTTL).UTC()
	m.tickets[ticket] = ticketState{scope: scope, expires: expires}
	return Ticket{Ticket: ticket, ExpiresAt: expires}, nil
}

// Consume takes a ticket once, including attempts with a mismatched scope.
func (m *Manager) Consume(scope Scope, ticket string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(m.now())
	state, exists := m.tickets[ticket]
	delete(m.tickets, ticket)
	if !exists || state.scope != scope {
		return ErrInvalid
	}
	return nil
}

// Optional operation-specific fields remain exact-match fields, including empty
// strings. The HTTP caller owns purpose enumeration and scope normalization.
func validScope(scope Scope) bool {
	return scope.Purpose != "" && scope.Subject != "" && scope.Browser != "" && scope.Host != "" && scope.IP != ""
}

// prune, takeRates and hasID are called only with mu held.
func (m *Manager) prune(now time.Time) {
	for id, state := range m.challenges {
		if !now.Before(state.expires) {
			delete(m.challenges, id)
		}
	}
	for id, state := range m.tickets {
		if !now.Before(state.expires) {
			delete(m.tickets, id)
		}
	}
	for key, bucket := range m.buckets {
		if !now.Before(bucket.expires) {
			delete(m.buckets, key)
		}
	}
}

func (m *Manager) takeRates(now time.Time, limits ...rateLimit) error {
	missing := 0
	for _, limit := range limits {
		bucket, exists := m.buckets[limit.key]
		if exists && bucket.count >= limit.limit {
			return ErrLimited
		}
		if !exists {
			missing++
		}
	}
	if len(m.buckets)+missing > maxRateBuckets {
		return ErrBusy
	}
	for _, limit := range limits {
		bucket, exists := m.buckets[limit.key]
		if !exists {
			bucket.expires = now.Add(rateWindow)
		}
		bucket.count++
		m.buckets[limit.key] = bucket
	}
	return nil
}

func (m *Manager) hasID(id string) bool {
	_, challenge := m.challenges[id]
	_, ticket := m.tickets[id]
	return challenge || ticket
}

func randomID() (string, error) {
	var raw [idBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func runGenerator(generator Generator) (out Puzzle, err error) {
	defer func() {
		if recover() != nil {
			out, err = Puzzle{}, ErrUnavailable
		}
	}()
	return generator()
}
