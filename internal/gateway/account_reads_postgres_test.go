package gateway

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

func TestPostgresReadCooldownSurvivesSchedulerReplacement(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state,credential_sealed)
        VALUES('read-cooldown-fixture','authenticated','\x01') RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	var requests atomic.Int32
	run := func() (*ReceiverManager, *ReadScheduler) {
		scheduler := NewReadScheduler(ctx)
		return &ReceiverManager{DB: db, reads: scheduler}, scheduler
	}
	m, first := run()
	err := m.read(ctx, account, "directory", ReadBackground, func(ctx context.Context) error {
		requests.Add(1)
		return &nativeauth.Error{Code: "native_rate_limited", RetryAfter: time.Minute}
	})
	first.Close()
	var native *nativeauth.Error
	if !errors.As(err, &native) || native.Code != "native_rate_limited" {
		t.Fatal(err)
	}
	replaced, second := run()
	defer second.Close()
	err = replaced.read(ctx, account, "directory", ReadBackground, func(context.Context) error { requests.Add(1); return nil })
	if !errors.As(err, &native) || native.Code != "native_rate_limited" || requests.Load() != 1 {
		t.Fatal("recreation bypassed durable cooldown", err, requests.Load())
	}
	view := second.Snapshot(account)
	if view.CooldownUntil == nil || !view.CooldownUntil.After(time.Now()) {
		t.Fatal("durable cooldown not exposed")
	}
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if err = replaced.read(short, account, "directory", ReadBackground, func(context.Context) error { t.Error("queued read bypassed cooldown"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
