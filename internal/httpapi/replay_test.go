package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/security"
)

// This is an opt-in PostgreSQL ingress replay, not a real-platform capacity claim.
func TestReplayLoad(t *testing.T) {
	if os.Getenv("WSL_REPLAY") != "1" {
		t.Skip("set WSL_REPLAY=1 and WSL_INTEGRATION=1 for the isolated inbox replay")
	}
	duration := func(key string, fallback, max int) int {
		value := os.Getenv(key)
		if value == "" {
			return fallback
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > max {
			t.Fatalf("invalid %s", key)
		}
		return n
	}
	seconds := duration("WSL_REPLAY_SECONDS", 60, 86400)
	burstSeconds := duration("WSL_BURST_SECONDS", 10, 60)
	db := integrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("r", 32))))
	ledger := &gateway.PostgresLedger{DB: db.Platform, Vault: vault}
	payload := []byte(`{"type":"synthetic_replay","text":"` + strings.Repeat("x", 480) + `"}`)
	accounts := make([]string, 10)
	for i := range accounts {
		if err := db.Platform.QueryRow(ctx, `INSERT INTO accounts(name) VALUES($1) RETURNING id::text`, fmt.Sprintf("replay-%d", i)).Scan(&accounts[i]); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	latencies := make([]float64, 0, (100*seconds + 500*burstSeconds))
	var acked atomic.Int64
	done := make(chan error, 10)
	ingress := make([]*gateway.Ingestor, 10)
	for i, account := range accounts {
		ingress[i] = gateway.NewIngestor(account, 1024, ledger, func(_ context.Context, m gateway.Message) error {
			mu.Lock()
			latencies = append(latencies, float64(time.Since(m.ReceivedAt).Microseconds())/1000)
			mu.Unlock()
			acked.Add(1)
			return nil
		}, nil)
		go func(in *gateway.Ingestor) { done <- in.Run(ctx) }(ingress[i])
	}
	var offered int64
	phase := func(rate, seconds int) time.Duration {
		t.Helper()
		start := time.Now()
		interval := time.Second / time.Duration(rate)
		for n := 0; n < rate*seconds; n++ {
			if delay := time.Until(start.Add(time.Duration(n) * interval)); delay > 0 {
				time.Sleep(delay)
			}
			select {
			case err := <-done:
				t.Fatalf("ingress exited during replay: %v", err)
			default:
			}
			accountIndex := int(offered % 10)
			groupIndex := accountIndex*10 + int((offered/10)%10)
			m := gateway.Message{AccountID: accounts[accountIndex], NativeGroupID: fmt.Sprintf("replay-group-%d", groupIndex),
				NativeID: strconv.FormatInt(offered, 10), SenderID: "replay-member", ReceivedAt: time.Now(),
				Payload: payload}
			if err := ingress[accountIndex].Offer(m); err != nil {
				t.Fatalf("replay offer failed: %v", err)
			}
			offered++
		}
		if delay := time.Until(start.Add(time.Duration(seconds) * time.Second)); delay > 0 {
			time.Sleep(delay)
		}
		return time.Since(start)
	}
	steady := phase(100, seconds)
	burst := phase(500, burstSeconds)
	drainDeadline := time.Now().Add(30 * time.Second)
	for acked.Load() < offered {
		select {
		case err := <-done:
			t.Fatalf("ingress exited before ACK drain: %v", err)
		default:
		}
		if time.Now().After(drainDeadline) {
			t.Fatal("ACK drain timed out")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	for range 10 {
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	var rows int64
	if err := db.Platform.QueryRow(context.Background(), `SELECT count(*) FROM inbox`).Scan(&rows); err != nil || rows != offered {
		t.Fatalf("offered=%d acked=%d persisted=%d err=%v", offered, acked.Load(), rows, err)
	}
	slices.Sort(latencies)
	percentile := func(p float64) float64 {
		index := min(len(latencies)-1, int(float64(len(latencies))*p))
		return latencies[index]
	}
	report := map[string]any{
		"generated_at": time.Now().UTC(), "test": "synthetic PostgreSQL inbox replay",
		"native_connected": false, "ai_connected": false, "capacity_accepted": false,
		"accounts": 10, "native_groups": 100, "payload_bytes": len(payload),
		"steady_requested_seconds": seconds, "steady_actual_seconds": steady.Seconds(), "steady_rate": 100,
		"burst_requested_seconds": burstSeconds, "burst_actual_seconds": burst.Seconds(), "burst_rate": 500,
		"offered": offered, "acked": acked.Load(), "persisted": rows,
		"receive_to_durable_ack_ms": map[string]float64{"p50": percentile(.50), "p95": percentile(.95), "p99": percentile(.99), "max": latencies[len(latencies)-1]},
		"note":                      "ACK timing is a local upper bound including batch delay, encryption and commit. Not a native protocol, full business, AI or target-machine acceptance.",
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("..", "..", "var")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "replay-report.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("offered=%d acked=%d persisted=%d p95=%.3fms p99=%.3fms; report: var/replay-report.json; capacity NOT accepted",
		offered, acked.Load(), rows, percentile(.95), percentile(.99))
}
