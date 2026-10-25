package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func (m *ReceiverManager) connection(parent context.Context, w *receiverWorker) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	d, saved, key, cursor, err := m.load(ctx, w.spec)
	if err != nil {
		return err
	}
	// Try the saved IM credential first. Business token renewal is only needed
	// when the IM server rejects it; it must not delay an otherwise valid socket.
	t, err := connectIM(ctx, d, saved.Session, key)
	if err != nil && err.Error() == "im_auth_rejected" && saved.Session.NIMAuthType != 0 {
		client, restoreErr := nativeauth.Resume(d, saved.Session, m.IDs(saved.DeploymentVersion))
		if restoreErr != nil {
			return restoreErr
		}
		fresh, renewErr := client.RenewIM(ctx, saved.Session)
		client.Close()
		if renewErr != nil {
			return renewErr
		}
		saved.Session = fresh
		plain, marshalErr := json.Marshal(saved)
		if marshalErr != nil {
			return marshalErr
		}
		sealed, sealErr := m.Vault.Seal("native-session:"+w.spec.ID, plain)
		clear(plain)
		if sealErr != nil {
			return sealErr
		}
		tag, saveErr := m.DB.Exec(ctx, `UPDATE accounts a SET credential_sealed=$1 WHERE a.id=$2 AND a.epoch=$3
            AND a.auth_state='authenticated' AND EXISTS(SELECT 1 FROM account_receivers r WHERE r.account_id=a.id
                AND r.enabled AND r.revision=$4 AND r.owner=$5 AND r.lease_until>now())`,
			sealed, w.spec.ID, w.spec.Epoch, w.spec.Revision, m.owner)
		if saveErr != nil || tag.RowsAffected() != 1 {
			return errors.New("receiver_database_unavailable")
		}
		t, err = connectIM(ctx, d, saved.Session, key)
		if err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	defer t.close(errors.New("im_disconnected"))
	now := time.Now().UTC()
	w.observe(func(s *ReceiverSnapshot) {
		s.State, s.Error, s.ConnectedAt, s.HeartbeatAt = "connected", "", &now, &now
	})
	consumer := make(chan error, 1)
	go func() { consumer <- m.consume(ctx, w, t) }()
	defer func() { cancel(); t.close(context.Canceled); <-consumer }()
	// The consumer is already running; the reader puts this reply in its FIFO.
	body, _ := nativewire.Properties([]nativewire.Property{
		{Key: 2, Value: []byte(cursor)}, {Key: 7, Value: []byte(cursor)}, {Key: 17, Value: []byte("0")},
	})
	reply, err := t.request(ctx, 5, 1, body)
	if err != nil || reply.Code != 200 || len(reply.Body) != 8 {
		return nativewire.ErrPushBoundary
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case err := <-consumer:
			// Preserve completion for the single deferred join.
			consumer <- err
			return err
		case <-t.done:
			t.mu.Lock()
			err := t.err
			t.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-heartbeat.C:
			duration, err := t.heartbeat(ctx)
			if err != nil {
				return err
			}
			now, ms := time.Now().UTC(), float64(duration.Microseconds())/1000
			w.observe(func(s *ReceiverSnapshot) { s.HeartbeatAt, s.HeartbeatMS = &now, &ms })
		}
	}
}

func (m *ReceiverManager) consume(ctx context.Context, w *receiverWorker, t *IMTransport) error {
	for {
		var first ReceivedFrame
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.done:
			return errors.New("im_disconnected")
		case first = <-t.pushes:
		}
		t.queuedBytes.Add(-int64(len(first.Frame.Body) + 256))
		batch := []ReceivedFrame{first}
		bytes := len(first.Frame.Body)
		timer := time.NewTimer(2 * time.Millisecond)
	collect:
		for len(batch) < 32 && bytes < 4*nativewire.Limit {
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case next := <-t.pushes:
				t.queuedBytes.Add(-int64(len(next.Frame.Body) + 256))
				batch = append(batch, next)
				bytes += len(next.Frame.Body)
			case <-timer.C:
				break collect
			}
		}
		timer.Stop()
		w.observe(func(s *ReceiverSnapshot) { s.QueueDepth = len(t.pushes) })
		if err := m.persistFrames(ctx, w, t, batch); err != nil {
			return err
		}
	}
}

type ackCandidate struct {
	Message nativewire.IMMessage
	At      time.Time
}

func (m *ReceiverManager) fence(ctx context.Context, tx pgx.Tx, w *receiverWorker) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT a.id::text FROM accounts a JOIN account_receivers r ON r.account_id=a.id
        WHERE a.id=$1 AND a.epoch=$2 AND a.auth_state='authenticated' AND r.revision=$3
            AND r.enabled AND r.owner=$4 AND r.lease_until>clock_timestamp()
        FOR SHARE OF a,r`, w.spec.ID, w.spec.Epoch, w.spec.Revision, m.owner).Scan(&id)
	if err != nil {
		return errors.New("receiver_database_unavailable")
	}
	return nil
}

func (m *ReceiverManager) persistFrames(ctx context.Context, w *receiverWorker, t *IMTransport, batch []ReceivedFrame) error {
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := m.DB.Begin(work)
	if err != nil {
		return errors.New("receiver_database_unavailable")
	}
	defer tx.Rollback(work)
	if _, err = tx.Exec(work, `SET LOCAL synchronous_commit=on`); err != nil {
		return err
	}
	if err = m.fence(work, tx, w); err != nil {
		return err
	}
	acks := []ackCandidate{}
	duplicates, total := int64(0), int64(0)
	boundaryUnknown := false
	frames := 0
	for _, item := range batch {
		frames++
		push, decodeErr := nativewire.DecodePush(item.Frame)
		kind := push.Kind
		if decodeErr != nil {
			kind, boundaryUnknown = "quarantined", true
		}
		encoded, err := json.Marshal(item.Frame)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(encoded)
		key := "frame:" + hex.EncodeToString(digest[:])
		if kind == "messages" {
			for _, message := range push.Messages {
				kind := "private"
				if message.Scene == 1 {
					kind = "group"
				}
				key := strconv.Itoa(message.Scene) + ":" + message.Target + ":" + message.ServerID
				plain, err := json.Marshal(message)
				if err != nil {
					return err
				}
				fresh, err := m.insertEvent(work, tx, w.spec.ID, key, kind, item.At, plain)
				clear(plain)
				if err != nil {
					return err
				}
				total++
				if !fresh {
					duplicates++
				}
				acks = append(acks, ackCandidate{message, item.At})
			}
		} else {
			if _, err = m.insertEvent(work, tx, w.spec.ID, key, kind, item.At, encoded); err != nil {
				return err
			}
		}
		clear(encoded)
		if push.Boundary != nil {
			if _, err = tx.Exec(work, `UPDATE account_receivers SET cursor=GREATEST(cursor,$2::numeric)
                WHERE account_id=$1`, w.spec.ID, strconv.FormatUint(*push.Boundary, 10)); err != nil {
				return err
			}
		}
		if boundaryUnknown {
			break
		} // never process a later watermark after poison
	}
	if err = tx.Commit(work); err != nil {
		return errors.New("receiver_database_unavailable")
	}
	committed := time.Now()
	committedUTC := committed.UTC()
	w.observe(func(s *ReceiverSnapshot) {
		s.Frames += int64(frames)
		s.Messages += total
		s.Duplicates += duplicates
		s.LastReceived, s.LastCommitted = &batch[frames-1].At, &committedUTC
		if boundaryUnknown {
			s.Quarantined++
			item := batch[frames-1]
			s.BlockedPacket = fmt.Sprintf("%d/%d/%d", item.Frame.Service, item.Frame.Command, len(item.Frame.Body))
		}
		for _, candidate := range acks {
			second := candidate.At.Unix()
			bucket := &w.rate[second%60]
			if bucket.second != second {
				bucket.second, bucket.count = second, 0
			}
			bucket.count++
			w.durable = appendBounded(w.durable, float64(committed.Sub(candidate.At).Microseconds())/1000)
		}
		s.DurableP95, s.Samples = percentile(w.durable), len(w.durable)
	})
	if boundaryUnknown {
		return nativewire.ErrPushBoundary
	}
	// ACK has no business side effect, but still respects stop/removal/session
	// fencing. Each bounded chunk holds the rows through the socket writes.
	for start := 0; start < len(acks); start += 32 {
		if err := m.ackChunk(ctx, w, t, acks[start:min(start+32, len(acks))]); err != nil {
			return err
		}
	}
	return nil
}

func (m *ReceiverManager) insertEvent(ctx context.Context, tx pgx.Tx, account, key, kind string, at time.Time, plain []byte) (bool, error) {
	sealed, err := m.Vault.Seal("native-event:"+account+":"+key, plain)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
        VALUES($1,$2,$3,$4,$5) ON CONFLICT(account_id,event_key) DO NOTHING`, account, key, kind, at, sealed)
	return tag.RowsAffected() == 1, err
}

func (m *ReceiverManager) ackChunk(ctx context.Context, w *receiverWorker, t *IMTransport, candidates []ackCandidate) error {
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := m.DB.Begin(work)
	if err != nil {
		return errors.New("receiver_database_unavailable")
	}
	defer tx.Rollback(work)
	if err = m.fence(work, tx, w); err != nil {
		return err
	}
	for _, candidate := range candidates {
		body, err := nativewire.ACKBody(candidate.Message.Scene, candidate.Message.ServerID)
		if err != nil {
			return err
		}
		if _, err = t.write(work, 4, 5, body, nil); err != nil {
			return err
		}
		now := time.Now()
		nowUTC := now.UTC()
		w.observe(func(s *ReceiverSnapshot) {
			s.ACKs++
			s.LastACK = &nowUTC
			w.ack = appendBounded(w.ack, float64(now.Sub(candidate.At).Microseconds())/1000)
			s.ACKP95 = percentile(w.ack)
		})
	}
	return tx.Commit(work)
}

func appendBounded(samples []float64, sample float64) []float64 {
	if len(samples) >= 1024 {
		samples = samples[1:]
	}
	return append(samples, max(0, sample))
}

func percentile(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	value := sorted[int(math.Ceil(float64(len(sorted))*0.95))-1]
	return &value
}
