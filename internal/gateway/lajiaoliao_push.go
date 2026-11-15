package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"wangshangliao/internal/lajiaoliao"
)

func (m *ReceiverManager) commitChiliPush(ctx context.Context, w *receiverWorker, batch []lajiaoliao.PushMessage) error {
	err := retryChiliPush(ctx, batch, func(work context.Context) error {
		return m.commitChiliPushAttempt(work, w, batch)
	})
	return normalizeChiliPushError(ctx, err)
}

func normalizeChiliPushError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if parentErr := ctx.Err(); parentErr != nil {
		return parentErr
	}
	var failure *lajiaoliao.Failure
	if errors.Is(err, errChiliScopeChanged) || errors.As(err, &failure) {
		return err
	}
	return &lajiaoliao.Failure{Code: "lajiaoliao_receive_persistence_failed"}
}

func retryChiliPush(ctx context.Context, batch []lajiaoliao.PushMessage, commit func(context.Context) error) error {
	defer func() {
		for _, item := range batch {
			if item.Message != nil {
				clear(item.Message.Content)
			}
		}
	}()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := commit(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt == 2 || !retryableChiliPush(ctx, err) {
			return err
		}
		// Only receive persistence is retried. Its stable event key makes an
		// uncertain database commit idempotent; every attempt repeats the fence.
		timer := time.NewTimer(time.Duration(attempt+1) * 25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func retryableChiliPush(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	// pgx can wrap its own connection deadline while the parent remains live.
	if errors.Is(err, context.DeadlineExceeded) || pgconn.SafeToRetry(err) || pgconn.Timeout(err) {
		return true
	}
	var failure *pgconn.PgError
	if errors.As(err, &failure) {
		switch failure.Code {
		case "40001", "40P01", "55P03", "53300", "57P01", "57P02", "57P03":
			return true
		}
	}
	return false
}

func (m *ReceiverManager) commitChiliPushAttempt(ctx context.Context, w *receiverWorker, batch []lajiaoliao.PushMessage) error {
	if len(batch) == 0 || len(batch) > 50 {
		return &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
	}
	for _, item := range batch {
		if !lajiaoliao.ValidID(item.Conversation) || item.Message == nil || item.Message.Seq != 0 ||
			!lajiaoliao.ValidID(item.Message.ClientMsgID) {
			return &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
		}
	}
	start := time.Now()
	now := start.UTC()
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = m.chiliFence(ctx, tx, w); err != nil {
		return err
	}
	var unique int64
	for _, item := range batch {
		key := chiliMessageKey(item.Conversation, item.Message.ClientMsgID)
		count, err := m.storeChiliEvent(ctx, tx, w.spec.ID, key, key, item.Message, now)
		if err != nil {
			return err
		}
		unique += count
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	m.observeChiliCommit(w, start, now, len(batch), unique)
	return nil
}

func chiliMessageKey(conversation, clientID string) string {
	sum := sha256.Sum256([]byte(conversation + "\x00" + clientID))
	return "lajiao:message:" + hex.EncodeToString(sum[:])
}
