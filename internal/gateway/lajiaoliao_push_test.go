package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/openimsdk/protocol/sdkws"
	"wangshangliao/internal/lajiaoliao"
)

type chiliPushTransient struct{}

func (chiliPushTransient) Error() string     { return "synthetic receive persistence failure" }
func (chiliPushTransient) SafeToRetry() bool { return true }

func TestChiliPushRetriesKeepContentAndRespectCancellationAndFence(t *testing.T) {
	for _, mode := range []string{"recover", "exhausted", "cancelled", "revoked", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			content := []byte("synthetic")
			batch := []lajiaoliao.PushMessage{{Conversation: "si_self_peer", Message: &sdkws.MsgData{
				ClientMsgID: "synthetic", Content: content,
			}}}
			attempts := 0
			err := retryChiliPush(ctx, batch, func(context.Context) error {
				attempts++
				if string(content) != "synthetic" {
					t.Fatal("receive body was erased before a persistence retry")
				}
				switch mode {
				case "recover":
					if attempts == 2 {
						return nil
					}
				case "cancelled":
					cancel()
				case "revoked":
					return errChiliScopeChanged
				case "invalid":
					return &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
				}
				return chiliPushTransient{}
			})
			want := map[string]int{"recover": 2, "exhausted": 3, "cancelled": 1, "revoked": 1, "invalid": 1}[mode]
			if attempts != want || (err == nil) != (mode == "recover") ||
				!bytes.Equal(content, make([]byte, len(content))) {
				t.Fatal("retry lost its bound, cancellation, fence or cleanup", attempts, err)
			}
		})
	}
}

func TestChiliPushRetryClassification(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(live)
	cancel()
	expired, expire := context.WithDeadline(live, time.Unix(0, 0))
	defer expire()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "no_error", ctx: live},
		{name: "internal_deadline", ctx: live, err: context.DeadlineExceeded, want: true},
		{name: "wrapped_internal_deadline", ctx: live, err: fmt.Errorf("synthetic database timeout: %w", context.DeadlineExceeded), want: true},
		{name: "safe_to_retry", ctx: live, err: chiliPushTransient{}, want: true},
		{name: "serialization_failure", ctx: live, err: &pgconn.PgError{Code: "40001"}, want: true},
		{name: "internal_cancellation", ctx: live, err: fmt.Errorf("synthetic database cancellation: %w", context.Canceled)},
		{name: "scope_changed", ctx: live, err: errChiliScopeChanged},
		{name: "protocol_boundary", ctx: live, err: &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}},
		{name: "permanent", ctx: live, err: errors.New("synthetic permanent error")},
		{name: "permission_denied", ctx: live, err: &pgconn.PgError{Code: "42501"}},
		{name: "cancelled_parent", ctx: cancelled, err: chiliPushTransient{}},
		{name: "expired_parent", ctx: expired, err: fmt.Errorf("synthetic database timeout: %w", context.DeadlineExceeded)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableChiliPush(tc.ctx, tc.err); got != tc.want {
				t.Fatalf("retryableChiliPush() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChiliPushErrorNormalization(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(live)
	cancel()
	expired, expire := context.WithDeadline(live, time.Unix(0, 0))
	defer expire()
	deadline := fmt.Errorf("synthetic database timeout detail: %w", context.DeadlineExceeded)
	internalCancel := fmt.Errorf("synthetic database cancellation detail: %w", context.Canceled)
	scope := fmt.Errorf("synthetic scope failure: %w", errChiliScopeChanged)
	boundary := fmt.Errorf("synthetic boundary failure: %w", &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"})
	for _, tc := range []struct {
		name            string
		ctx             context.Context
		err             error
		want            error
		wantPersistence bool
	}{
		{name: "success", ctx: live},
		{name: "committed_before_cancellation", ctx: cancelled},
		{name: "internal_deadline", ctx: live, err: deadline, wantPersistence: true},
		{name: "internal_cancellation", ctx: live, err: internalCancel, wantPersistence: true},
		{name: "database_error", ctx: live, err: &pgconn.PgError{Code: "42501", Detail: "synthetic database detail"}, wantPersistence: true},
		{name: "scope_changed", ctx: live, err: scope, want: scope},
		{name: "protocol_boundary", ctx: live, err: boundary, want: boundary},
		{name: "cancelled_parent", ctx: cancelled, err: deadline, want: context.Canceled},
		{name: "expired_parent", ctx: expired, err: internalCancel, want: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := normalizeChiliPushError(tc.ctx, tc.err)
			if !tc.wantPersistence {
				if err != tc.want {
					t.Fatalf("normalizeChiliPushError() = %v, want %v", err, tc.want)
				}
				return
			}
			var failure *lajiaoliao.Failure
			if !errors.As(err, &failure) || failure.Code != "lajiaoliao_receive_persistence_failed" ||
				err.Error() != failure.Code || errors.Unwrap(err) != nil || !lajiaoliao.IsTerminal(err) {
				t.Fatalf("database failure was not normalized to a fixed terminal failure: %v", err)
			}
		})
	}
}

func TestChiliPushInternalDeadlineRetries(t *testing.T) {
	for _, recover := range []bool{true, false} {
		name := "exhausted"
		if recover {
			name = "recovered"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			content := []byte("synthetic")
			batch := []lajiaoliao.PushMessage{{Conversation: "si_self_peer", Message: &sdkws.MsgData{
				ClientMsgID: "synthetic", Content: content,
			}}}
			attempts := 0
			err := retryChiliPush(ctx, batch, func(work context.Context) error {
				attempts++
				if work.Err() != nil || string(content) != "synthetic" {
					t.Fatal("internal deadline canceled the parent or erased content before retry")
				}
				if recover && attempts == 3 {
					return nil
				}
				return fmt.Errorf("synthetic database timeout: %w", context.DeadlineExceeded)
			})
			if attempts != 3 || !bytes.Equal(content, make([]byte, len(content))) {
				t.Fatalf("internal deadline lost retry bound or cleanup: attempts=%d", attempts)
			}
			normalized := normalizeChiliPushError(ctx, err)
			if recover {
				if normalized != nil {
					t.Fatalf("recovered receive persistence failed: %v", normalized)
				}
				return
			}
			if !errors.Is(err, context.DeadlineExceeded) || normalized == nil ||
				normalized.Error() != "lajiaoliao_receive_persistence_failed" ||
				errors.Is(normalized, context.DeadlineExceeded) || !lajiaoliao.IsTerminal(normalized) {
				t.Fatalf("exhausted internal deadline escaped terminal normalization: %v", normalized)
			}
		})
	}
}

func TestChiliPushParentCancellationStopsAttempts(t *testing.T) {
	for _, mode := range []string{"cancelled_before", "deadline_before", "cancelled_during"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			want := error(context.Canceled)
			if mode == "deadline_before" {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
				want = context.DeadlineExceeded
			} else if mode == "cancelled_before" {
				cancel()
			}
			defer cancel()
			content := []byte("synthetic")
			batch := []lajiaoliao.PushMessage{{Conversation: "si_self_peer", Message: &sdkws.MsgData{
				ClientMsgID: "synthetic", Content: content,
			}}}
			attempts := 0
			err := retryChiliPush(ctx, batch, func(context.Context) error {
				attempts++
				cancel()
				return fmt.Errorf("synthetic database timeout: %w", context.DeadlineExceeded)
			})
			wantAttempts := 0
			if mode == "cancelled_during" {
				wantAttempts = 1
			}
			if err != want || normalizeChiliPushError(ctx, err) != want || attempts != wantAttempts ||
				!bytes.Equal(content, make([]byte, len(content))) {
				t.Fatalf("parent termination did not stop persistence: attempts=%d, err=%v", attempts, err)
			}
		})
	}
}
