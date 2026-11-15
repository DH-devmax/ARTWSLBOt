package scopecache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testKey() Key {
	return Key{Platform: "wsl", Purpose: "management", Account: "account", Group: "group", Tenant: "tenant", Actor: "actor",
		Kind: "member_projection", Owner: "owner", AccountEpoch: 1, AccountVersion: 1}
}

func TestPartitionsVersionsExpiryAndOwnership(t *testing.T) {
	c := New()
	now := time.Now()
	c.now = func() time.Time { return now }
	key := testKey()
	calls := 0
	fetch := func(context.Context) ([]byte, error) { calls++; return []byte("safe"), nil }
	v, _, err := c.Load(context.Background(), key, fetch)
	if err != nil {
		t.Fatal(err)
	}
	v[0] = 'X'
	got, hit, err := c.Load(context.Background(), key, fetch)
	if err != nil || !hit || string(got) != "safe" || calls != 1 {
		t.Fatal(string(got), hit, err, calls)
	}
	for _, change := range []func(*Key){
		func(k *Key) { k.Platform = "lajiaoliao" }, func(k *Key) { k.Purpose = "chat" },
		func(k *Key) { k.Tenant = "other" }, func(k *Key) { k.Actor = "other" },
		func(k *Key) { k.AccountEpoch++ }, func(k *Key) { k.BindingVersion++ },
		func(k *Key) { k.Owner = "replacement" },
	} {
		k := key
		change(&k)
		if _, hit, err := c.Load(context.Background(), k, fetch); hit || err != nil {
			t.Fatal(k, hit, err)
		}
	}
	now = now.Add(6 * time.Second)
	if _, hit, err := c.Load(context.Background(), key, fetch); hit || err != nil {
		t.Fatal(hit, err)
	}
	key.Actor = ""
	if _, _, err := c.Load(context.Background(), key, fetch); !errors.Is(err, ErrScope) {
		t.Fatal("empty scope accepted", err)
	}
}

func TestCoalescingCancellationAndInvalidation(t *testing.T) {
	c := New()
	key := testKey()
	first, cancel := context.WithCancel(context.Background())
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	fetch := func(ctx context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return []byte("one"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	done := make(chan error, 2)
	go func() { _, _, err := c.Load(first, key, fetch); done <- err }()
	<-started
	go func() { _, _, err := c.Load(context.Background(), key, fetch); done <- err }()
	deadline := time.Now().Add(time.Second)
	for c.Snapshot()[0].Coalesced == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("running read was duplicated")
	}
	c.InvalidateAccount(key.Account)
	block := make(chan struct{})
	go func() {
		_, _, err := c.Load(context.Background(), key, func(ctx context.Context) ([]byte, error) {
			close(block)
			<-ctx.Done()
			return []byte("late"), nil
		})
		done <- err
	}()
	<-block
	c.InvalidateGroup(key.Group)
	if err := <-done; !errors.Is(err, ErrInvalidated) {
		t.Fatal("invalidated late result delivered", err)
	}
}

func TestCapacityErrorsAndLastWaiter(t *testing.T) {
	c := New()
	key := testKey()
	value := bytes.Repeat([]byte{'x'}, MaxValue)
	for i := 0; i < 400; i++ {
		k := key
		k.Group = fmt.Sprint(i)
		k.Account = fmt.Sprint(i / 20)
		if _, _, err := c.Load(context.Background(), k, func(context.Context) ([]byte, error) { return value, nil }); err != nil {
			t.Fatal(err)
		}
	}
	stat := c.Snapshot()[0]
	if stat.Bytes > MaxBytes || stat.Entries > MaxEntries || stat.Evictions == 0 {
		t.Fatal(stat)
	}
	calls := 0
	for i := 0; i < 2; i++ {
		_, _, err := c.Load(context.Background(), key, func(context.Context) ([]byte, error) { calls++; return nil, errors.New("failure") })
		if err == nil {
			t.Fatal("error swallowed")
		}
	}
	if calls != 2 {
		t.Fatal("error cached")
	}
	ctx, cancel := context.WithCancel(context.Background())
	start, stopped := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _ = c.Load(ctx, key, func(ctx context.Context) ([]byte, error) {
			close(start)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		})
	}()
	<-start
	cancel()
	wg.Wait()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("orphan read retained")
	}
}

func TestRepeatedWorkload(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, purpose := range []string{"management", "chat"} {
			c := New()
			key := testKey()
			key.Platform = platform
			key.Purpose = purpose
			calls := 0
			for i := 0; i < 1000; i++ {
				k := key
				k.Account = fmt.Sprint(i / 150)
				k.Group = fmt.Sprint((i % 150) / 5)
				if _, _, err := c.Load(context.Background(), k, func(context.Context) ([]byte, error) { calls++; return []byte("projection"), nil }); err != nil {
					t.Fatal(err)
				}
			}
			if calls > 300 {
				t.Fatal(platform, purpose, "less than 70% reduction", calls)
			}
			t.Logf("%s/%s: 1000 reads, %d upstream reads, %.1f%% reduction", platform, purpose, calls, 100-float64(calls)/10)
		}
	}
}

func BenchmarkCacheHit(b *testing.B) {
	c := New()
	key := testKey()
	fetch := func(context.Context) ([]byte, error) { return []byte("projection"), nil }
	_, _, _ = c.Load(context.Background(), key, fetch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := c.Load(context.Background(), key, fetch); err != nil {
			b.Fatal(err)
		}
	}
}
