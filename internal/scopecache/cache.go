// Package scopecache stores bounded, immutable read projections. Admission and
// result delivery remain the caller's responsibility; cached data is not proof.
package scopecache

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

const (
	MaxEntries = 256
	MaxBytes   = 4 << 20
	MaxAccount = 32
	MaxValue   = 64 << 10
)

var ErrScope = errors.New("read_cache_invalid_scope")
var ErrBusy = errors.New("read_cache_busy")
var ErrInvalidated = errors.New("read_cache_scope_changed")

type Key struct {
	Platform, Purpose, Account, Group, Tenant, Actor, Kind, Parameters, Owner string
	AccountEpoch, AccountVersion, BindingVersion, ConfigVersion               int64
	ReceiverVersion, DeploymentVersion                                        int64
}

func (k Key) valid() bool {
	if k.Platform != "wsl" && k.Platform != "lajiaoliao" ||
		k.Purpose != "management" && k.Purpose != "chat" ||
		k.AccountEpoch < 0 || k.AccountVersion < 0 || k.BindingVersion < 0 || k.ConfigVersion < 0 {
		return false
	}
	for _, s := range []string{k.Account, k.Group, k.Tenant, k.Actor, k.Kind, k.Owner} {
		if s == "" || len(s) > 200 {
			return false
		}
	}
	return (k.Kind == "member_projection" || k.Kind == "group_projection" || k.Kind == "sealed_config") &&
		len(k.Parameters) <= 512
}

type Stats struct {
	Platform  string `json:"platform"`
	Purpose   string `json:"purpose"`
	Hits      uint64 `json:"hits"`
	Misses    uint64 `json:"misses"`
	Coalesced uint64 `json:"coalesced"`
	Evictions uint64 `json:"evictions"`
	Expired   uint64 `json:"expired"`
	Entries   int    `json:"entries"`
	Bytes     int    `json:"bytes"`
	InFlight  int    `json:"in_flight"`
}

type entry struct {
	key     Key
	value   []byte
	expires time.Time
}

type flight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	value   []byte
	err     error
}

type partition struct {
	items   map[Key]*list.Element
	lru     list.List
	flights map[Key]*flight
	stats   Stats
}

type Cache struct {
	mu    sync.Mutex
	parts [4]partition
	now   func() time.Time
}

func New() *Cache {
	c := &Cache{now: time.Now}
	for i := range c.parts {
		p := &c.parts[i]
		p.items = map[Key]*list.Element{}
		p.flights = map[Key]*flight{}
		p.stats.Platform = []string{"wsl", "wsl", "lajiaoliao", "lajiaoliao"}[i]
		p.stats.Purpose = []string{"management", "chat", "management", "chat"}[i]
	}
	return c
}

func (c *Cache) part(k Key) *partition {
	i := 0
	if k.Platform == "lajiaoliao" {
		i += 2
	}
	if k.Purpose == "chat" {
		i++
	}
	return &c.parts[i]
}

func (p *partition) remove(e *list.Element) {
	item := e.Value.(entry)
	delete(p.items, item.key)
	p.stats.Bytes -= len(item.value)
	p.lru.Remove(e)
}

func (p *partition) expire(now time.Time) {
	for _, e := range p.items {
		if !now.Before(e.Value.(entry).expires) {
			p.remove(e)
			p.stats.Expired++
		}
	}
}

func (p *partition) put(k Key, value []byte, expires time.Time) {
	if len(value) == 0 || len(value) > MaxValue {
		return
	}
	if old := p.items[k]; old != nil {
		p.remove(old)
	}
	count := 0
	var oldest *list.Element
	for e := p.lru.Back(); e != nil; e = e.Prev() {
		if e.Value.(entry).key.Account == k.Account {
			count++
			if oldest == nil {
				oldest = e
			}
		}
	}
	if count >= MaxAccount {
		p.remove(oldest)
		p.stats.Evictions++
	}
	for len(p.items) >= MaxEntries || p.stats.Bytes+len(value) > MaxBytes {
		p.remove(p.lru.Back())
		p.stats.Evictions++
	}
	e := p.lru.PushFront(entry{key: k, value: append([]byte(nil), value...), expires: expires})
	p.items[k] = e
	p.stats.Bytes += len(value)
}

// Load coalesces only explicitly scoped immutable reads. Each waiter owns its
// cancellation and receives a copy; the last departing waiter cancels the read.
func (c *Cache) Load(ctx context.Context, k Key, fetch func(context.Context) ([]byte, error)) ([]byte, bool, error) {
	if !k.valid() || fetch == nil {
		return nil, false, ErrScope
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	ttl := 5 * time.Second
	if k.Kind == "sealed_config" {
		ttl = 30 * time.Second
	}
	c.mu.Lock()
	p := c.part(k)
	p.expire(c.now())
	if e := p.items[k]; e != nil {
		p.lru.MoveToFront(e)
		value := append([]byte(nil), e.Value.(entry).value...)
		p.stats.Hits++
		c.mu.Unlock()
		return value, true, nil
	}
	f := p.flights[k]
	if f != nil {
		p.stats.Coalesced++
	} else {
		if len(p.flights) >= MaxEntries {
			c.mu.Unlock()
			return nil, false, ErrBusy
		}
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		f = &flight{done: make(chan struct{}), cancel: cancel}
		p.flights[k] = f
		p.stats.Misses++
		go func() {
			value, err := fetch(work)
			c.mu.Lock()
			defer c.mu.Unlock()
			if work.Err() != nil {
				err = work.Err()
			}
			if p.flights[k] != f {
				err = ErrInvalidated
			}
			if err == nil {
				f.value = append([]byte(nil), value...)
				p.expire(c.now())
				p.put(k, value, c.now().Add(ttl))
			}
			f.err = err
			if p.flights[k] == f {
				delete(p.flights, k)
			}
			close(f.done)
			cancel()
		}()
	}
	f.waiters++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		f.waiters--
		if f.waiters == 0 {
			f.cancel()
			if p.flights[k] == f {
				delete(p.flights, k)
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-f.done:
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		return append([]byte(nil), f.value...), false, f.err
	}
}

func (c *Cache) InvalidateAccount(account string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.parts {
		p := &c.parts[i]
		for k, e := range p.items {
			if k.Account == account {
				p.remove(e)
			}
		}
		for k, f := range p.flights {
			if k.Account == account {
				delete(p.flights, k)
				f.cancel()
			}
		}
	}
}

func (c *Cache) InvalidateGroup(group string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.parts {
		p := &c.parts[i]
		for k, e := range p.items {
			if k.Group == group {
				p.remove(e)
			}
		}
		for k, f := range p.flights {
			if k.Group == group {
				delete(p.flights, k)
				f.cancel()
			}
		}
	}
}

func (c *Cache) InvalidateAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.parts {
		p := &c.parts[i]
		for _, e := range p.items {
			p.remove(e)
		}
		for k, f := range p.flights {
			delete(p.flights, k)
			f.cancel()
		}
	}
}

func (c *Cache) Snapshot() []Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Stats, 4)
	for i := range c.parts {
		p := &c.parts[i]
		p.expire(c.now())
		out[i] = p.stats
		out[i].Entries, out[i].InFlight = len(p.items), len(p.flights)
	}
	return out
}
