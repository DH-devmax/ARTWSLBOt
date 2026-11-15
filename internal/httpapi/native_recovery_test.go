package httpapi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeRecoveryBoundedLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runNativeRecovery(ctx, time.Millisecond, func(work context.Context) error {
			if work != ctx {
				t.Error("recovery lost cancellation scope")
			}
			if calls.Add(1) == 2 {
				cancel()
			}
			return errors.New("synthetic database outage")
		})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("recovery did not stop")
	}
	if calls.Load() != 2 {
		t.Fatal("recovery failed to retry database-only work or continued after cancellation")
	}
}
