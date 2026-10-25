package gateway

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

var ErrBackpressure = errors.New("bounded_ingress_full")

type Message struct {
	AccountID     string
	GroupID       string
	NativeGroupID string
	NativeID      string
	SenderID      string
	ReceivedAt    time.Time
	Payload       []byte
	Resolved      bool
}

type Ledger interface {
	// Commit must return only after the entire batch is durable.
	Commit(context.Context, []Message) error
}

type Ingestor struct {
	account     string
	queue       chan Message
	ledger      Ledger
	ack         func(context.Context, Message) error
	wake        chan<- struct{}
	queuedBytes atomic.Int64
	running     atomic.Bool
	stopped     atomic.Bool
}

func NewIngestor(account string, capacity int, ledger Ledger, ack func(context.Context, Message) error, wake chan<- struct{}) *Ingestor {
	if capacity < 1 || capacity > 4096 {
		panic("invalid ingress capacity")
	}
	return &Ingestor{account: account, queue: make(chan Message, capacity), ledger: ledger, ack: ack, wake: wake}
}

// Offer never waits on a roster, business worker, AI call, or a full push queue.
func (i *Ingestor) Offer(m Message) error {
	if i.stopped.Load() {
		return errors.New("ingress_closed")
	}
	if m.AccountID != i.account || (m.GroupID == "") == (m.NativeGroupID == "") ||
		len(m.NativeGroupID) > 200 || m.NativeID == "" || len(m.NativeID) > 200 ||
		m.SenderID == "" || len(m.SenderID) > 200 || m.ReceivedAt.IsZero() || len(m.Payload) > 1024*1024 {
		return errors.New("invalid_ingress_message")
	}
	charge := int64(len(m.Payload) + 1024)
	if i.queuedBytes.Add(charge) > 16*1024*1024 {
		i.queuedBytes.Add(-charge)
		return ErrBackpressure
	}
	m.Payload = append([]byte(nil), m.Payload...)
	select {
	case i.queue <- m:
		return nil
	default:
		i.queuedBytes.Add(-charge)
		return ErrBackpressure
	}
}

func (i *Ingestor) Run(ctx context.Context) error {
	if !i.running.CompareAndSwap(false, true) {
		return errors.New("ingress_already_started")
	}
	defer i.stopped.Store(true)
	for {
		var first Message
		select {
		case <-ctx.Done():
			return ctx.Err()
		case first = <-i.queue:
		}
		i.queuedBytes.Add(-int64(len(first.Payload) + 1024))
		batch := []Message{first}
		timer := time.NewTimer(2 * time.Millisecond)
	collect:
		for len(batch) < 32 {
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case next := <-i.queue:
				i.queuedBytes.Add(-int64(len(next.Payload) + 1024))
				batch = append(batch, next)
			case <-timer.C:
				break collect
			}
		}
		timer.Stop()
		if err := i.ledger.Commit(ctx, batch); err != nil {
			return err
		}
		for _, m := range batch {
			if err := i.ack(ctx, m); err != nil {
				return err
			}
		}
		// The database inbox is the queue of record; a missed wake is harmless.
		if i.wake != nil {
			select {
			case i.wake <- struct{}{}:
			default:
			}
		}
	}
}
