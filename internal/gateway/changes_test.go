package gateway

import (
	"sync"
	"testing"
	"time"
)

func TestChangesBoundedScopedAndNonBlocking(t *testing.T) {
	bus := NewChangeBus()
	a, b := bus.Subscribe("a"), bus.Subscribe("b")
	defer a.Close()
	defer b.Close()
	for i := 0; i < 10000; i++ {
		bus.Publish(Change{Account: "a", Kind: "decoded", EventID: int64(i + 1)})
	}
	if len(a.Events) != 64 || !a.Lost() || a.Lost() || len(b.Events) != 0 {
		t.Fatal("scope, coalescing or overflow signal failed")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				sub := bus.Subscribe("a")
				bus.Publish(Change{Account: "a", Kind: "receiver"})
				if sub != nil {
					sub.Lost()
					sub.Close()
				}
			}
		}()
	}
	wg.Wait()
	subs := []*ChangeSubscription{a, b}
	for len(subs) < 16 {
		subs = append(subs, bus.Subscribe("a"))
	}
	if bus.Subscribe("a") != nil {
		t.Fatal("unbounded subscriptions")
	}
	for _, sub := range subs[2:] {
		sub.Close()
	}
}

func TestDecoderWakeIsCoalesced(t *testing.T) {
	decoder := NewDecoderManager(nil, nil)
	done := make(chan struct{})
	go func() {
		for range 100000 {
			decoder.Notify()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notification blocked ingress")
	}
	if len(decoder.wake) != 1 {
		t.Fatal("arrival signals were not coalesced")
	}
}
