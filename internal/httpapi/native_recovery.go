package httpapi

import (
	"context"
	"log/slog"
	"time"
)

// RunNativeMaintenance never sends or logs in. Recovery uses the same platform
// database but bounded transactions, outside the receiver and its ACK path.
func (s *Server) RunNativeMaintenance(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runNativeRecovery(ctx, 5*time.Second, s.nativeTextService().Expire)
	}()
	return done
}

func runNativeRecovery(ctx context.Context, interval time.Duration, recover func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := recover(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("native operation recovery failed", "code", "native_recovery_unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
