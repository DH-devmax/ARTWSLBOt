package webcaptcha

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testScope() Scope {
	return Scope{
		Purpose:  "login",
		Subject:  "alice",
		Browser:  "browser-a",
		Session:  "session-a",
		Host:     "captcha.test",
		IP:       "192.0.2.1",
		Platform: "wsl",
		Target:   "account-a",
	}
}

func numberedScope(i int) Scope {
	scope := testScope()
	scope.IP = fmt.Sprintf("test-ip-%d", i)
	scope.Browser = fmt.Sprintf("test-browser-%d", i)
	return scope
}

func changedScope(scope Scope, field string) Scope {
	value := reflect.ValueOf(&scope).Elem().FieldByName(field)
	value.SetString(value.String() + "-other")
	return scope
}

func testPuzzle() (Puzzle, error) {
	return Puzzle{Image: testImage(), X: 150, Y: 30}, nil
}

func testNow() time.Time {
	return time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
}

func newTestManager() *Manager {
	m := New(testPuzzle)
	m.now = testNow
	return m
}

func issueChallenge(t *testing.T, m *Manager, scope Scope, previous string) Challenge {
	t.Helper()
	challenge, err := m.Issue(context.Background(), scope, previous)
	if err != nil {
		t.Fatal(err)
	}
	return challenge
}

func solveChallenge(t *testing.T, m *Manager, scope Scope, challenge Challenge) Ticket {
	t.Helper()
	m.mu.Lock()
	state, exists := m.challenges[challenge.ChallengeID]
	m.mu.Unlock()
	if !exists {
		t.Fatal("challenge missing from private state")
	}
	ticket, err := m.Verify(scope, challenge.ChallengeID, state.x, state.y)
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func requireError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v; want %v", got, want)
	}
}

func TestNewNilGeneratorUnavailable(t *testing.T) {
	m := New(nil)
	if m.generator != nil {
		t.Fatal("nil generator was replaced")
	}
	for i := 0; i < 2; i++ {
		challenge, err := m.Issue(context.Background(), testScope(), "")
		requireError(t, err, ErrUnavailable)
		if challenge != (Challenge{}) {
			t.Fatal("nil generator issued a challenge")
		}
		if len(m.challenges) != 0 || len(m.tickets) != 0 || len(m.buckets) != 0 || m.generating != 0 {
			t.Fatal("nil generator allocated state")
		}
	}
}

func TestManagerLifecycleAndRandomIDs(t *testing.T) {
	m, scope := newTestManager(), testScope()
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		challenge := issueChallenge(t, m, scope, "")
		if challenge.Image != testImage() || !challenge.ExpiresAt.Equal(testNow().Add(120*time.Second)) {
			t.Fatal("challenge image or expiry differs")
		}
		ticket := solveChallenge(t, m, scope, challenge)
		if !ticket.ExpiresAt.Equal(testNow().Add(60 * time.Second)) {
			t.Fatal("ticket expiry differs")
		}
		for _, id := range []string{challenge.ChallengeID, ticket.Ticket} {
			raw, err := base64.RawURLEncoding.DecodeString(id)
			if err != nil || len(raw) != 32 || len(id) != idLength || seen[id] {
				t.Fatal("ID is not a unique 256-bit URL-safe value")
			}
			seen[id] = true
		}
		if _, exists := m.challenges[challenge.ChallengeID]; exists {
			t.Fatal("verified challenge retained")
		}
		if _, exists := m.tickets[ticket.Ticket]; !exists {
			t.Fatal("ticket missing from private state")
		}
		_, err := m.Verify(scope, challenge.ChallengeID, 150, 30)
		requireError(t, err, ErrInvalid)
		requireError(t, m.Consume(scope, ticket.Ticket), nil)
		requireError(t, m.Consume(scope, ticket.Ticket), ErrInvalid)
	}
	if len(m.challenges)+len(m.tickets) != 0 {
		t.Fatal("consumed state retained")
	}
}

func TestRequiredScope(t *testing.T) {
	for _, field := range []string{"Purpose", "Subject", "Browser", "Host", "IP"} {
		t.Run("missing_"+field, func(t *testing.T) {
			scope := testScope()
			reflect.ValueOf(&scope).Elem().FieldByName(field).SetString("")
			m := newTestManager()
			called := false
			m.generator = func() (Puzzle, error) {
				called = true
				return testPuzzle()
			}
			got, err := m.Issue(context.Background(), scope, "")
			requireError(t, err, ErrInvalid)
			if called || got != (Challenge{}) || len(m.buckets) != 0 {
				t.Fatal("invalid scope reached generation or allocated state")
			}
		})
	}
	t.Run("optional_fields", func(t *testing.T) {
		scope := testScope()
		scope.Session, scope.Platform, scope.Target = "", "", ""
		scope.Purpose = "http-defined-purpose"
		m := newTestManager()
		ticket := solveChallenge(t, m, scope, issueChallenge(t, m, scope, ""))
		requireError(t, m.Consume(scope, ticket.Ticket), nil)
	})
	t.Run("login_empty_subject_consumes_verify_attempt", func(t *testing.T) {
		m, scope := newTestManager(), testScope()
		challenge := issueChallenge(t, m, scope, "")
		missing := scope
		missing.Subject = ""
		got, err := m.Verify(missing, challenge.ChallengeID, 150, 30)
		requireError(t, err, ErrInvalid)
		if got != (Ticket{}) {
			t.Fatal("empty subject received a ticket")
		}
		_, err = m.Verify(scope, challenge.ChallengeID, 150, 30)
		requireError(t, err, ErrInvalid)
	})
}

func TestEveryScopeFieldIsolated(t *testing.T) {
	for _, field := range []string{"Purpose", "Subject", "Browser", "Session", "Host", "IP", "Platform", "Target"} {
		t.Run(field, func(t *testing.T) {
			m, scope := newTestManager(), testScope()
			other := changedScope(scope, field)
			challenge := issueChallenge(t, m, scope, "")
			ticket, err := m.Verify(other, challenge.ChallengeID, 150, 30)
			requireError(t, err, ErrInvalid)
			if ticket != (Ticket{}) {
				t.Fatal("cross-scope verification issued a ticket")
			}
			_, err = m.Verify(scope, challenge.ChallengeID, 150, 30)
			requireError(t, err, ErrInvalid)
			ticket = solveChallenge(t, m, scope, issueChallenge(t, m, scope, ""))
			requireError(t, m.Consume(other, ticket.Ticket), ErrInvalid)
			requireError(t, m.Consume(scope, ticket.Ticket), ErrInvalid)
		})
	}
}

func TestRefreshOnlySameScope(t *testing.T) {
	for _, field := range []string{"Purpose", "Subject", "Browser", "Session", "Host", "IP", "Platform", "Target"} {
		t.Run(field, func(t *testing.T) {
			m, scope := newTestManager(), testScope()
			old := issueChallenge(t, m, scope, "")
			other := changedScope(scope, field)
			newChallenge := issueChallenge(t, m, other, old.ChallengeID)
			solveChallenge(t, m, scope, old)
			solveChallenge(t, m, other, newChallenge)
		})
	}
	t.Run("matching_scope", func(t *testing.T) {
		m, scope := newTestManager(), testScope()
		old := issueChallenge(t, m, scope, "")
		replacement := issueChallenge(t, m, scope, old.ChallengeID)
		_, err := m.Verify(scope, old.ChallengeID, 150, 30)
		requireError(t, err, ErrInvalid)
		if len(m.challenges) != 1 {
			t.Fatal("refresh retained old challenge")
		}
		ticket := solveChallenge(t, m, scope, replacement)
		issueChallenge(t, m, scope, ticket.Ticket)
		requireError(t, m.Consume(scope, ticket.Ticket), nil)
	})
	t.Run("limited_refresh_preserves_previous", func(t *testing.T) {
		m, scope := newTestManager(), testScope()
		old := issueChallenge(t, m, scope, "")
		for i := 1; i < 20; i++ {
			issueChallenge(t, m, scope, "")
		}
		_, err := m.Issue(context.Background(), scope, old.ChallengeID)
		requireError(t, err, ErrLimited)
		solveChallenge(t, m, scope, old)
	})
}

func TestWrongAnswerConsumesChallenge(t *testing.T) {
	for _, point := range []struct{ x, y int }{
		{0, 0}, {156, 30}, {144, 30}, {150, 36}, {150, 24}, {-1, -1},
		{int(^uint(0) >> 1), 30}, {-int(^uint(0)>>1) - 1, 30},
	} {
		t.Run(fmt.Sprintf("%d_%d", point.x, point.y), func(t *testing.T) {
			m, scope := newTestManager(), testScope()
			challenge := issueChallenge(t, m, scope, "")
			ticket, err := m.Verify(scope, challenge.ChallengeID, point.x, point.y)
			requireError(t, err, ErrInvalid)
			if ticket != (Ticket{}) {
				t.Fatal("wrong answer issued a ticket")
			}
			_, err = m.Verify(scope, challenge.ChallengeID, 150, 30)
			requireError(t, err, ErrInvalid)
		})
	}
}

func TestAnswerTolerance(t *testing.T) {
	for _, point := range []struct{ x, y int }{{145, 25}, {155, 35}, {145, 35}, {155, 25}} {
		m, scope := newTestManager(), testScope()
		challenge := issueChallenge(t, m, scope, "")
		ticket, err := m.Verify(scope, challenge.ChallengeID, point.x, point.y)
		requireError(t, err, nil)
		requireError(t, m.Consume(scope, ticket.Ticket), nil)
	}
}

func TestChallengeAndTicketIDsAreSeparate(t *testing.T) {
	m, scope := newTestManager(), testScope()
	challenge := issueChallenge(t, m, scope, "")
	requireError(t, m.Consume(scope, challenge.ChallengeID), ErrInvalid)
	ticket := solveChallenge(t, m, scope, challenge)
	_, err := m.Verify(scope, ticket.Ticket, 150, 30)
	requireError(t, err, ErrInvalid)
	requireError(t, m.Consume(scope, ticket.Ticket), nil)
	for _, id := range []string{"", "missing", "not-a-valid-issued-identifier"} {
		_, err := m.Verify(scope, id, 150, 30)
		requireError(t, err, ErrInvalid)
		requireError(t, m.Consume(scope, id), ErrInvalid)
	}
}

func TestExpiryBoundaries(t *testing.T) {
	for _, kind := range []string{"challenge", "ticket"} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_expired_%t", kind, expired), func(t *testing.T) {
				m, scope := newTestManager(), testScope()
				now := testNow()
				m.now = func() time.Time { return now }
				challenge := issueChallenge(t, m, scope, "")
				ttl := 120 * time.Second
				var ticket Ticket
				if kind == "ticket" {
					ticket = solveChallenge(t, m, scope, challenge)
					ttl = 60 * time.Second
				}
				now = now.Add(ttl)
				if !expired {
					now = now.Add(-time.Nanosecond)
				}
				var err error
				if kind == "challenge" {
					_, err = m.Verify(scope, challenge.ChallengeID, 150, 30)
				} else {
					err = m.Consume(scope, ticket.Ticket)
				}
				if expired {
					requireError(t, err, ErrInvalid)
				} else {
					requireError(t, err, nil)
				}
			})
		}
	}
}

func TestLazyCleanupAndGenerationCompletionTTL(t *testing.T) {
	m, scope := newTestManager(), testScope()
	now := testNow()
	m.now = func() time.Time { return now }
	m.generator = func() (Puzzle, error) {
		now = now.Add(45 * time.Second)
		return testPuzzle()
	}
	challenge := issueChallenge(t, m, scope, "")
	if !challenge.ExpiresAt.Equal(now.Add(120 * time.Second)) {
		t.Fatal("TTL started before generation completed")
	}
	solveChallenge(t, m, scope, challenge)
	issueChallenge(t, m, scope, "")
	now = now.Add(5 * time.Minute)
	requireError(t, m.Consume(scope, "missing"), ErrInvalid)
	if len(m.challenges) != 0 || len(m.tickets) != 0 || len(m.buckets) != 0 {
		t.Fatal("expired state was not lazily cleaned")
	}
}

func TestConcurrentConsumeUnique(t *testing.T) {
	m, scope := newTestManager(), testScope()
	ticket := solveChallenge(t, m, scope, issueChallenge(t, m, scope, ""))
	start := make(chan struct{})
	results := make(chan error, 32)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			results <- m.Consume(scope, ticket.Ticket)
		}()
	}
	close(start)
	successes := 0
	for i := 0; i < cap(results); i++ {
		if err := <-results; err == nil {
			successes++
		} else {
			requireError(t, err, ErrInvalid)
		}
	}
	if successes != 1 || len(m.tickets) != 0 {
		t.Fatalf("successful consumers = %d; want 1", successes)
	}
}

func TestConcurrentVerifyUnique(t *testing.T) {
	m, scope := newTestManager(), testScope()
	challenge := issueChallenge(t, m, scope, "")
	type result struct {
		ticket Ticket
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 32)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			ticket, err := m.Verify(scope, challenge.ChallengeID, 150, 30)
			results <- result{ticket, err}
		}()
	}
	close(start)
	successes := 0
	for i := 0; i < cap(results); i++ {
		got := <-results
		if got.err == nil {
			successes++
			requireError(t, m.Consume(scope, got.ticket.Ticket), nil)
		} else {
			requireError(t, got.err, ErrInvalid)
		}
	}
	if successes != 1 || len(m.challenges)+len(m.tickets) != 0 {
		t.Fatalf("successful verifications = %d; want 1", successes)
	}
}

func TestGenerationSlotsDoNotQueueOrHoldStateLock(t *testing.T) {
	m, scope := newTestManager(), testScope()
	ticket := solveChallenge(t, m, scope, issueChallenge(t, m, scope, ""))
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var calls atomic.Int32
	m.generator = func() (Puzzle, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return testPuzzle()
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := m.Issue(context.Background(), numberedScope(i), "")
			results <- err
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("two generators did not start")
		}
	}
	busy := make(chan error, 1)
	go func() {
		_, err := m.Issue(context.Background(), numberedScope(3), "")
		busy <- err
	}()
	select {
	case err := <-busy:
		requireError(t, err, ErrBusy)
	case <-time.After(3 * time.Second):
		t.Fatal("third generation queued")
	}
	consumed := make(chan error, 1)
	go func() { consumed <- m.Consume(scope, ticket.Ticket) }()
	select {
	case err := <-consumed:
		requireError(t, err, nil)
	case <-time.After(3 * time.Second):
		t.Fatal("generation held the state lock")
	}
	if calls.Load() != 2 {
		t.Fatal("busy request reached generator")
	}
	unblock()
	for i := 0; i < 2; i++ {
		requireError(t, <-results, nil)
	}
	if m.generating != 0 || len(m.challenges) != 2 {
		t.Fatal("generation slots were not released")
	}
}

func TestSharedCapacity(t *testing.T) {
	m := newTestManager()
	challenges := make([]Challenge, 1024)
	for i := range challenges {
		challenges[i] = issueChallenge(t, m, numberedScope(i), "")
	}
	ticket := solveChallenge(t, m, numberedScope(0), challenges[0])
	if len(m.challenges) != 1023 || len(m.tickets) != 1 {
		t.Fatal("verification at capacity did not replace one entry")
	}
	_, err := m.Issue(context.Background(), numberedScope(1024), "")
	requireError(t, err, ErrBusy)
	replacement := issueChallenge(t, m, numberedScope(1), challenges[1].ChallengeID)
	if len(m.challenges)+len(m.tickets) != 1024 {
		t.Fatal("same-scope refresh changed total capacity")
	}
	_, err = m.Issue(context.Background(), numberedScope(1025), replacement.ChallengeID)
	requireError(t, err, ErrBusy)
	if _, exists := m.challenges[replacement.ChallengeID]; !exists {
		t.Fatal("busy cross-scope refresh removed another challenge")
	}
	requireError(t, m.Consume(numberedScope(0), ticket.Ticket), nil)
	issueChallenge(t, m, numberedScope(1024), "")
	if len(m.challenges)+len(m.tickets) != 1024 {
		t.Fatal("consumed ticket did not free exactly one entry")
	}
}

func TestPendingGenerationReservesCapacity(t *testing.T) {
	m := newTestManager()
	for i := 0; i < 1023; i++ {
		m.challenges[fmt.Sprintf("fixture-%d", i)] = challengeState{
			scope: testScope(), x: 150, y: 30, expires: testNow().Add(challengeTTL),
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	m.generator = func() (Puzzle, error) {
		close(started)
		<-release
		return testPuzzle()
	}
	result := make(chan error, 1)
	go func() {
		_, err := m.Issue(context.Background(), numberedScope(0), "")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("generation did not start")
	}
	_, err := m.Issue(context.Background(), numberedScope(1), "")
	requireError(t, err, ErrBusy)
	unblock()
	requireError(t, <-result, nil)
	if len(m.challenges)+len(m.tickets) != 1024 || m.generating != 0 {
		t.Fatal("pending generation exceeded capacity or leaked reservation")
	}
}

func TestIssueIPRateLimit(t *testing.T) {
	m, scope := newTestManager(), testScope()
	now := testNow()
	m.now = func() time.Time { return now }
	for i := 0; i < 60; i++ {
		scope.Browser = fmt.Sprintf("browser-%d", i)
		issueChallenge(t, m, scope, "")
	}
	scope.Browser = "new-browser"
	_, err := m.Issue(context.Background(), scope, "")
	requireError(t, err, ErrLimited)
	now = now.Add(5*time.Minute - time.Nanosecond)
	_, err = m.Issue(context.Background(), scope, "")
	requireError(t, err, ErrLimited)
	now = now.Add(time.Nanosecond)
	issueChallenge(t, m, scope, "")
}

func TestIssueBrowserSessionRateLimit(t *testing.T) {
	m, scope := newTestManager(), testScope()
	for i := 0; i < 20; i++ {
		scope.IP = fmt.Sprintf("ip-%d", i)
		issueChallenge(t, m, scope, "")
	}
	scope.IP, scope.Host, scope.Subject, scope.Purpose = "new-ip", "other.test", "bob", "another-purpose"
	_, err := m.Issue(context.Background(), scope, "")
	requireError(t, err, ErrLimited)
	other := scope
	other.Session = "session-b"
	issueChallenge(t, m, other, "")
	other = scope
	other.Browser = "browser-b"
	issueChallenge(t, m, other, "")
}

func TestBrowserSessionKeyHasNoDelimiterCollision(t *testing.T) {
	m, scope := newTestManager(), testScope()
	scope.Browser, scope.Session = "a:b", "c"
	for i := 0; i < 20; i++ {
		issueChallenge(t, m, scope, "")
	}
	scope.Browser, scope.Session = "a", "b:c"
	issueChallenge(t, m, scope, "")
}

func TestVerifyIPRateLimitConsumesLimitedAttempt(t *testing.T) {
	m, scope := newTestManager(), testScope()
	now := testNow()
	m.now = func() time.Time { return now }
	challenge := issueChallenge(t, m, scope, "")
	for i := 0; i < 120; i++ {
		other := scope
		other.Browser = fmt.Sprintf("verify-browser-%d", i)
		_, err := m.Verify(other, "missing", 150, 30)
		requireError(t, err, ErrInvalid)
	}
	ticket, err := m.Verify(scope, challenge.ChallengeID, 150, 30)
	requireError(t, err, ErrLimited)
	if ticket != (Ticket{}) {
		t.Fatal("rate-limited verification issued a ticket")
	}
	if _, exists := m.challenges[challenge.ChallengeID]; exists {
		t.Fatal("rate-limited attempt retained the challenge")
	}
	now = now.Add(5 * time.Minute)
	challenge = issueChallenge(t, m, scope, "")
	solveChallenge(t, m, scope, challenge)
}

func seedRateBuckets(m *Manager, count int) {
	for i := 0; i < count; i++ {
		key := rateKey{kind: rateVerifyIP, first: fmt.Sprintf("bucket-ip-%d", i)}
		m.buckets[key] = rateBucket{count: 1, expires: testNow().Add(rateWindow)}
	}
}

func TestRateBucketCapacity(t *testing.T) {
	m, scope := newTestManager(), testScope()
	seedRateBuckets(m, 4095)
	_, err := m.Verify(scope, "missing", 150, 30)
	requireError(t, err, ErrInvalid)
	if len(m.buckets) != 4096 {
		t.Fatal("final available bucket was not admitted")
	}
	other := scope
	other.IP = "different-ip"
	_, err = m.Verify(other, "missing", 150, 30)
	requireError(t, err, ErrBusy)
	_, err = m.Issue(context.Background(), other, "")
	requireError(t, err, ErrBusy)
	_, err = m.Verify(scope, "missing", 150, 30)
	requireError(t, err, ErrInvalid)
	if len(m.buckets) != 4096 || len(m.challenges) != 0 || m.generating != 0 {
		t.Fatal("bucket capacity was exceeded or allocated generation state")
	}
	m.now = func() time.Time { return testNow().Add(5 * time.Minute) }
	issueChallenge(t, m, other, "")
	if len(m.buckets) != 2 {
		t.Fatal("expired full buckets were not reclaimed")
	}
}

func TestIssueRateAdmissionIsAtomic(t *testing.T) {
	m, scope := newTestManager(), testScope()
	seedRateBuckets(m, 4095)
	_, err := m.Issue(context.Background(), scope, "")
	requireError(t, err, ErrBusy)
	if len(m.buckets) != 4095 {
		t.Fatal("partially allocated two issue buckets")
	}
	key := rateKey{kind: rateIssueIP, first: scope.IP}
	m.buckets[key] = rateBucket{count: 1, expires: testNow().Add(rateWindow)}
	_, err = m.Issue(context.Background(), scope, "")
	requireError(t, err, ErrBusy)
	if m.buckets[key].count != 1 || len(m.buckets) != 4096 {
		t.Fatal("failed admission incremented an existing rate bucket")
	}
}

func TestCanceledIssue(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			m, scope := newTestManager(), testScope()
			var called bool
			m.generator = func() (Puzzle, error) {
				called = true
				return testPuzzle()
			}
			ctx, cancel := context.WithCancel(context.Background())
			if want == context.DeadlineExceeded {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			defer cancel()
			if want == context.Canceled {
				cancel()
			}
			challenge, err := m.Issue(ctx, scope, "")
			requireError(t, err, want)
			if called || challenge != (Challenge{}) || len(m.buckets) != 0 || m.generating != 0 {
				t.Fatal("canceled request allocated generation state")
			}
		})
	}
}

func TestCancellationDiscardsLateResult(t *testing.T) {
	m, scope := newTestManager(), testScope()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	m.generator = func() (Puzzle, error) {
		close(started)
		<-release
		return testPuzzle()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		challenge Challenge
		err       error
	}
	done := make(chan result, 1)
	go func() {
		challenge, err := m.Issue(ctx, scope, "")
		done <- result{challenge, err}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("generator did not start")
	}
	cancel()
	unblock()
	got := <-done
	requireError(t, got.err, context.Canceled)
	if got.challenge != (Challenge{}) || len(m.challenges)+len(m.tickets) != 0 || m.generating != 0 {
		t.Fatal("canceled late result retained state")
	}
	m.generator = testPuzzle
	issueChallenge(t, m, scope, "")
}

func TestGeneratorFailureAndPanicAreUnavailable(t *testing.T) {
	internal := errors.New("generator-private-details")
	for _, tc := range []struct {
		name      string
		generator Generator
	}{
		{"error", func() (Puzzle, error) { return Puzzle{}, internal }},
		{"panic", func() (Puzzle, error) { panic("generator-private-details") }},
		{"nil_panic", func() (Puzzle, error) { panic(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager()
			m.generator = tc.generator
			for i := 0; i < 2; i++ {
				challenge, err := m.Issue(context.Background(), testScope(), "")
				if err != ErrUnavailable || errors.Is(err, internal) || challenge != (Challenge{}) {
					t.Fatal("generation failure exposed internal data")
				}
				if m.generating != 0 || len(m.challenges)+len(m.tickets) != 0 {
					t.Fatal("generation failure leaked capacity")
				}
			}
			m.generator = testPuzzle
			issueChallenge(t, m, testScope(), "")
		})
	}
}
