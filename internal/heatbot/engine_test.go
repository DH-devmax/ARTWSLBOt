package heatbot

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type providerFunc func(context.Context, Request) (string, error)

func (f providerFunc) Generate(ctx context.Context, r Request) (string, error) { return f(ctx, r) }

func fixture(group string) Request {
	return Request{ID: group, Scope: Scope{"tenant", group, "account"}, BindingVersion: 1,
		AccountEpoch: 1, PersonaVersion: 1, Persona: "snapshot",
		Messages: []Message{{"user", "isolated"}}, ExpiresAt: time.Now().Add(time.Minute)}
}

func result(t *testing.T, channel <-chan Result) Result {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("engine did not complete")
		return Result{}
	}
}

func TestGroupQueueSnapshotsAndReplacement(t *testing.T) {
	entered := make(chan Request, 10)
	release := make(chan struct{})
	engine := New(providerFunc(func(ctx context.Context, request Request) (string, error) {
		entered <- request
		select {
		case <-release:
			return request.Messages[0].Content + request.Persona, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}))
	defer engine.Close()
	first := fixture("group-a")
	firstResult, _ := engine.Submit(context.Background(), first)
	first.Messages[0].Content = "mutated"
	<-entered
	pending := fixture("group-a")
	old, _ := engine.Submit(context.Background(), pending)
	pending.ID, pending.PersonaVersion, pending.Persona = "newest", 2, "new-persona"
	latest, _ := engine.Submit(context.Background(), pending)
	if !errors.Is(result(t, old).Err, ErrSuperseded) {
		t.Fatal("more than one pending candidate survived")
	}
	other, _ := engine.Submit(context.Background(), fixture("group-b"))
	if request := <-entered; request.Scope.GroupID != "group-b" {
		t.Fatal("same-group requests ran concurrently")
	}
	close(release)
	a, b, c := result(t, firstResult), result(t, latest), result(t, other)
	if a.Err != nil || b.Err != nil || c.Err != nil || a.Proposal.Text != "isolatedsnapshot" ||
		b.Proposal.PersonaVersion != 2 || b.Proposal.Scope.GroupID != "group-a" {
		t.Fatalf("snapshot or routing corrupted: %#v %#v %#v", a, b, c)
	}
}

func TestGlobalConcurrencyAndCancellation(t *testing.T) {
	var mu sync.Mutex
	active, highest := 0, 0
	entered := make(chan struct{}, 10)
	engine := New(providerFunc(func(ctx context.Context, request Request) (string, error) {
		mu.Lock()
		active++
		if active > highest {
			highest = active
		}
		mu.Unlock()
		entered <- struct{}{}
		<-ctx.Done()
		mu.Lock()
		active--
		mu.Unlock()
		return "", ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	channels := []<-chan Result{}
	for _, group := range []string{"a", "b", "c", "d", "e"} {
		channel, err := engine.Submit(ctx, fixture(group))
		if err != nil {
			t.Fatal(err)
		}
		channels = append(channels, channel)
	}
	for range 4 {
		<-entered
	}
	cancel()
	for _, channel := range channels {
		if result(t, channel).Err == nil {
			t.Fatal("cancelled candidate produced a proposal")
		}
	}
	engine.Close()
	if highest != 4 {
		t.Fatalf("global in-flight bound changed: %d", highest)
	}
	if _, err := engine.Submit(context.Background(), fixture("after-close")); !errors.Is(err, ErrClosed) {
		t.Fatal("closed engine accepted work")
	}
}

func TestRejectScopeAndExpiredCandidate(t *testing.T) {
	engine := New(providerFunc(func(context.Context, Request) (string, error) {
		t.Fatal("invalid candidate reached the model")
		return "", nil
	}))
	defer engine.Close()
	for _, change := range []func(*Request){
		func(r *Request) { r.Scope.TenantID = "" },
		func(r *Request) { r.Scope.AccountID = "" },
		func(r *Request) { r.Scope.GroupID = "" },
		func(r *Request) { r.PersonaVersion = 0 },
		func(r *Request) { r.ExpiresAt = time.Now().Add(-time.Second) },
		func(r *Request) { r.Messages[0].Role = "system" },
	} {
		request := fixture("group")
		change(&request)
		if _, err := engine.Submit(context.Background(), request); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}
