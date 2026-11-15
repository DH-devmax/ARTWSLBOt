package gateway

import "sync"

type Change struct {
	Account      string
	Kind         string
	EventID      int64
	Conversation string
}

// ChangeBus carries committed identifiers, never plaintext or credentials.
// Slow observers lose notifications, not durable messages; they must resync.
type ChangeBus struct {
	mu   sync.Mutex
	next uint64
	subs map[uint64]*ChangeSubscription
}

type ChangeSubscription struct {
	Account string
	Events  chan Change
	lost    bool
	bus     *ChangeBus
	id      uint64
}

func NewChangeBus() *ChangeBus { return &ChangeBus{subs: map[uint64]*ChangeSubscription{}} }

func (b *ChangeBus) Subscribe(account string) *ChangeSubscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) >= 16 {
		return nil
	}
	b.next++
	s := &ChangeSubscription{Account: account, Events: make(chan Change, 64), bus: b, id: b.next}
	b.subs[s.id] = s
	return s
}

func (b *ChangeBus) Publish(c Change) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if s.Account != c.Account {
			continue
		}
		select {
		case s.Events <- c:
		default:
			s.lost = true
		}
	}
}

func (s *ChangeSubscription) Lost() bool {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	lost := s.lost
	s.lost = false
	return lost
}

func (s *ChangeSubscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	delete(s.bus.subs, s.id)
}
