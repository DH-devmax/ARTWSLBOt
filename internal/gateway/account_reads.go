package gateway

import (
	"context"
	"errors"
	"time"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func (m *ReceiverManager) ReadSnapshot(account string) ReadSnapshot {
	m.mu.Lock()
	reads := m.reads
	m.mu.Unlock()
	if reads == nil {
		return ReadSnapshot{QueueCapacity: ReadQueueLimit}
	}
	return reads.Snapshot(account)
}

func (m *ReceiverManager) read(ctx context.Context, account, key string, priority int, fn func(context.Context) error) error {
	_, err := m.readValue(ctx, account, key, priority, func(work context.Context) (any, error) {
		return nil, fn(work)
	})
	return err
}

func (m *ReceiverManager) readValue(ctx context.Context, account, key string, priority int, fn func(context.Context) (any, error)) (any, error) {
	m.mu.Lock()
	reads := m.reads
	m.mu.Unlock()
	if reads == nil {
		return nil, ErrReadOwner
	}
	return reads.Value(ctx, account, key, priority, func(work context.Context) (any, error) {
		var epoch int64
		var until *time.Time
		err := m.DB.QueryRow(work, `SELECT a.epoch,s.cooldown_until FROM accounts a
            LEFT JOIN account_read_state s ON s.account_id=a.id AND s.account_epoch=a.epoch
            WHERE a.id=$1 AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL`, account).Scan(&epoch, &until)
		if err != nil {
			return nil, errors.New("native_read_state_unavailable")
		}
		if until != nil && time.Now().Before(*until) {
			reads.Cooldown(account, *until)
			return nil, &nativeauth.Error{Code: "native_rate_limited", RetryAfter: time.Until(*until)}
		}
		observe := func() { reads.ObserveRequest(account) }
		value, err := fn(lajiaoliao.WithRequestObserver(nativeauth.WithRequestObserver(work, observe), observe))
		var chili *lajiaoliao.Failure
		if errors.As(err, &chili) && chili.Code == "lajiaoliao_rate_limited" {
			err = &nativeauth.Error{Code: "native_rate_limited", RetryAfter: chili.RetryAfter}
		}
		var native *nativeauth.Error
		if errors.As(err, &native) && native.Code == "native_rate_limited" {
			until := time.Now().UTC().Add(max(time.Second, min(native.RetryAfter, 24*time.Hour)))
			reads.Cooldown(account, until)
			save, cancel := context.WithTimeout(monitorContext(work), time.Second)
			defer cancel()
			_, saveErr := m.DB.Exec(save, `INSERT INTO account_read_state(account_id,cooldown_until,account_epoch)
                SELECT id,$2,epoch FROM accounts WHERE id=$1 AND epoch=$3
                ON CONFLICT(account_id) DO UPDATE SET cooldown_until=CASE
                  WHEN account_read_state.account_epoch=excluded.account_epoch
                  THEN greatest(account_read_state.cooldown_until,excluded.cooldown_until)
                  ELSE excluded.cooldown_until END,account_epoch=excluded.account_epoch,updated_at=now()`,
				account, until, epoch)
			if saveErr != nil {
				return nil, errors.New("native_read_cooldown_not_persisted")
			}
		}
		return value, err
	})
}

// Preserve a known upstream cooldown even if the requesting page has gone away.
func monitorContext(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }
