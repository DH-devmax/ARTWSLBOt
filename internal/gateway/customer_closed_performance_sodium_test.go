//go:build sodium && cgo

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

const (
	customerClosedSamples = 10000
	customerClosedWarmup  = 1000
)

type customerClosedPorts struct {
	native atomic.Int64
	reads  atomic.Int64
}

func (p *customerClosedPorts) BindingRoles(context.Context, control.GroupBindingNativeScope) (control.GroupBindingRoleEvidence, error) {
	p.native.Add(1)
	return control.GroupBindingRoleEvidence{}, errors.New("closed_native_port_called")
}

func (p *customerClosedPorts) BindingInvitation(context.Context, control.GroupBindingInvitationRequest) (control.GroupBindingInvitationEvidence, error) {
	p.native.Add(1)
	return control.GroupBindingInvitationEvidence{}, errors.New("closed_native_port_called")
}

func (p *customerClosedPorts) AcceptBindingInvitation(context.Context, control.GroupBindingAcceptRequest, control.GroupBindingAcceptFence) error {
	p.native.Add(1)
	return errors.New("closed_native_port_called")
}

type customerClosedQueries struct {
	total, bodies, failures atomic.Int64
}

func (q *customerClosedQueries) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	q.total.Add(1)
	sql := strings.ToLower(data.SQL)
	if strings.Contains(sql, "native_received_events") || strings.Contains(sql, "native_decoded_events") {
		q.bodies.Add(1)
	}
	return ctx
}

func (q *customerClosedQueries) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil && ctx.Err() == nil {
		q.failures.Add(1)
	}
}

func customerClosedConsumers(ctx context.Context, db *pgxpool.Pool, vault *security.Vault, ports *customerClosedPorts) func() {
	work, cancel := context.WithCancel(ctx)
	store := &database.DB{Platform: db}
	bindings := &control.GroupBindingService{DB: store, Vault: vault, Native: ports,
		Decode: func(string, string, string, string, int64, []byte, []byte) (control.OnboardingMessage, error) {
			ports.reads.Add(1)
			return control.OnboardingMessage{}, errors.New("closed_source_reader_called")
		}}
	rules := &control.BusinessRulesService{DB: store, Vault: vault, Catalog: control.BusinessPermissionCatalog(),
		Conversation: func(context.Context, control.BusinessScope) (string, error) {
			ports.reads.Add(1)
			return "", errors.New("closed_conversation_reader_called")
		},
		ReadSource: func(control.BusinessScope, string, int64, []byte, []byte) (control.BusinessMessage, error) {
			ports.reads.Add(1)
			return control.BusinessMessage{}, errors.New("closed_source_reader_called")
		},
		FreshRole: func(context.Context, control.BusinessScope, string) (control.BusinessEvidence, error) {
			ports.native.Add(1)
			return control.BusinessEvidence{}, errors.New("closed_native_port_called")
		},
		Execute: func(context.Context, control.BusinessAttempt, control.BusinessFence) (bool, error) {
			ports.native.Add(1)
			return false, errors.New("closed_native_port_called")
		},
		Observe: func(context.Context, control.BusinessAttempt) (bool, error) {
			ports.native.Add(1)
			return false, errors.New("closed_native_port_called")
		}}
	bindingDone, rulesDone := bindings.Run(work), rules.Run(work)
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-bindingDone; <-rulesDone }) }
}

type customerClosedJob struct {
	job     decodeJob
	claimed time.Time
}

type customerClosedObservation struct {
	id             int64
	service, queue float64
}

type customerClosedResult struct {
	Platform             string  `json:"platform"`
	Groups               int     `json:"groups"`
	Round                int     `json:"round"`
	ConsumersRunning     bool    `json:"closed_consumers_running"`
	Accounts             int     `json:"accounts"`
	Samples              int     `json:"samples"`
	Warmup               int     `json:"warmup"`
	Codec                int     `json:"codec_version"`
	Workers              int     `json:"workers"`
	QueueCapacity        int     `json:"queue_capacity"`
	QueuePeak            int     `json:"queue_peak"`
	HotspotHalf          bool    `json:"hotspot_half"`
	Seconds              float64 `json:"seconds"`
	PerSecond            float64 `json:"per_second"`
	DecodeP95MS          float64 `json:"decode_p95_ms"`
	ServiceP95MS         float64 `json:"full_service_p95_ms"`
	QueueP95MS           float64 `json:"queue_p95_ms"`
	ClaimP95MS           float64 `json:"claim_p95_ms"`
	ClaimSamples         int     `json:"claim_samples"`
	AllocatedBytes       uint64  `json:"allocated_bytes"`
	AllocatedBytesPerMsg float64 `json:"allocated_bytes_per_event"`
	Allocs               uint64  `json:"allocs"`
	AllocsPerMsg         float64 `json:"allocs_per_event"`
	HeapBefore           uint64  `json:"heap_before_bytes"`
	HeapAfter            uint64  `json:"heap_after_bytes"`
	HeapPeak             uint64  `json:"heap_peak_bytes"`
	GoroutinesBefore     int     `json:"goroutines_before"`
	GoroutinesAfter      int     `json:"goroutines_after"`
	GoroutinesPeak       int     `json:"goroutines_peak"`
	ConsumerQueries      int64   `json:"consumer_queries"`
	ConsumerBodyReads    int64   `json:"consumer_body_reads"`
	ConsumerNativeCalls  int64   `json:"consumer_native_calls"`
	ConsumerSourceReads  int64   `json:"consumer_source_reads"`
	JIT                  string  `json:"postgres_jit"`
	FixedStatistics      bool    `json:"fixed_test_statistics"`
}

// Only the test envelope carries a queue timestamp. claim/process and their
// persisted decode_ms boundary are the unmodified v54 production methods.
func customerClosedDecode(ctx context.Context, manager *DecoderManager, count int) (result customerClosedResult, ids []int64, err error) {
	work, cancel := context.WithCancel(ctx)
	queue := make(chan customerClosedJob, DecoderQueueCapacity)
	points := make(chan customerClosedObservation, count)
	failures := make(chan error, 1)
	wake := make(chan struct{}, 1)
	var workers sync.WaitGroup
	for range DecoderWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range queue {
				if work.Err() != nil {
					return
				}
				start := time.Now()
				call, stop := context.WithTimeout(work, 10*time.Second)
				e := manager.process(call, task.job)
				stop()
				if e != nil {
					select {
					case failures <- e:
					default:
					}
					cancel()
					return
				}
				points <- customerClosedObservation{task.job.ID,
					float64(time.Since(start).Nanoseconds()) / 1e6,
					float64(start.Sub(task.claimed).Nanoseconds()) / 1e6}
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}()
	}
	start := time.Now()
	claims := make([]float64, 0, count/8+1)
	dispatched := 0
dispatch:
	for dispatched < count && work.Err() == nil {
		available := cap(queue) - len(queue)
		if available > 0 {
			call, stop := context.WithTimeout(work, 2*time.Second)
			at := time.Now()
			jobs, e := manager.claim(call, min(8, available, count-dispatched))
			claims = append(claims, float64(time.Since(at).Nanoseconds())/1e6)
			stop()
			if e != nil {
				err = fmt.Errorf("claim: %w", e)
				break
			}
			claimed := time.Now()
			for _, job := range jobs {
				select {
				case queue <- customerClosedJob{job: job, claimed: claimed}:
					dispatched++
					result.QueuePeak = max(result.QueuePeak, len(queue))
				case <-work.Done():
					break dispatch
				}
			}
			if len(jobs) > 0 && len(queue) < cap(queue) {
				continue
			}
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-work.Done():
			timer.Stop()
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
	if err != nil {
		cancel()
	}
	close(queue)
	workers.Wait()
	cancel()
	result.Seconds = time.Since(start).Seconds()
	close(points)
	select {
	case e := <-failures:
		err = fmt.Errorf("process: %w", e)
	default:
	}
	service, waiting := make([]float64, 0, count), make([]float64, 0, count)
	ids = make([]int64, 0, count)
	seen := make(map[int64]bool, count)
	for point := range points {
		if seen[point.id] {
			err = errors.New("duplicate effective decode submission")
		}
		seen[point.id] = true
		ids = append(ids, point.id)
		service, waiting = append(service, point.service), append(waiting, point.queue)
	}
	if err != nil {
		return result, ids, err
	}
	if ctx.Err() != nil {
		return result, ids, ctx.Err()
	}
	if len(ids) != count || dispatched != count {
		return result, ids, fmt.Errorf("incomplete decode: dispatched=%d completed=%d expected=%d", dispatched, len(ids), count)
	}
	result.Samples, result.PerSecond = count, float64(count)/result.Seconds
	result.ServiceP95MS, result.QueueP95MS = customerClosedQuantile(service, .95), customerClosedQuantile(waiting, .95)
	result.ClaimP95MS, result.ClaimSamples = customerClosedQuantile(claims, .95), len(claims)
	var decoded, invalid int
	err = manager.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='decoded' AND scene='group'
		AND payload_sealed IS NOT NULL AND decode_ms IS NOT NULL AND codec_version=$2),
		count(*) FILTER(WHERE state<>'decoded' OR scene<>'group' OR payload_sealed IS NULL
			OR decode_ms IS NULL OR codec_version<>$2),
		coalesce(percentile_cont(.95) WITHIN GROUP(ORDER BY decode_ms),0)
		FROM native_decoded_events WHERE event_id=ANY($1::bigint[])`, ids, CodecVersion).
		Scan(&decoded, &invalid, &result.DecodeP95MS)
	if err == nil && (decoded != count || invalid != 0) {
		err = fmt.Errorf("projection mismatch: decoded=%d invalid=%d expected=%d", decoded, invalid, count)
	}
	return result, ids, err
}

// This owns wsl_test serially. Both variants use the same saturated input,
// warm-up, pool limits, parser, durable IO and test-side measurement overhead.
// It measures the incremental closed-consumer cost, not decoder optimization.
func TestCustomerClosedDecoderPerformance(t *testing.T) {
	if os.Getenv("WSL_CUSTOMER_CLOSED_PERFORMANCE") != "1" {
		t.Skip("explicit isolated closed-business comparison required")
	}
	initial := receiverTestDB(t)
	owner := customerClosedMaintenance(t)
	customerClosedFixedStatistics(t, owner)
	customerClosedPreserveDeployment(t, owner)
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, groups := range []int{100, 300} {
			ok := t.Run(fmt.Sprintf("%s/%d", platform, groups), func(t *testing.T) {
				sources := customerClosedSources(t, platform, groups)
				var results []customerClosedResult
				for round := 1; round <= 5; round++ {
					variants := []bool{false, true}
					if round%2 == 0 {
						slices.Reverse(variants)
					}
					for _, running := range variants {
						ok := t.Run(fmt.Sprintf("round-%d/consumers-%t", round, running), func(t *testing.T) {
							results = append(results, customerClosedVariant(t, initial, owner, sources, platform, groups, round, running))
						})
						if !ok {
							return
						}
					}
				}
				customerClosedComparison(t, results)
			})
			if !ok {
				return
			}
		}
	}
}

func customerClosedVariant(t *testing.T, initial *pgxpool.Pool, owner *pgx.Conn, sources [][]byte, platform string, groups, round int, running bool) customerClosedResult {
	t.Helper()
	customerClosedMaintain(t, owner, false)
	// Registered first: stop consumers and close both pools before fixture reset.
	t.Cleanup(func() { customerClosedMaintain(t, owner, false) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	decoderConfig := initial.Config()
	decoderConfig.MaxConns = 4
	decoderConfig.MinConns = 0
	db, err := pgxpool.NewWithConfig(ctx, decoderConfig)
	if err != nil {
		t.Fatal("create isolated decoder pool", err)
	}
	defer db.Close()
	var queries customerClosedQueries
	consumerConfig := initial.Config()
	consumerConfig.MaxConns = 12
	consumerConfig.MinConns = 0
	consumerConfig.ConnConfig.Tracer = &queries
	consumerDB, err := pgxpool.NewWithConfig(ctx, consumerConfig)
	if err != nil {
		t.Fatal("create isolated consumer pool", err)
	}
	defer consumerDB.Close()
	vault, accounts := customerClosedFixture(t, db, platform, groups)
	manager := NewDecoderManager(db, vault)
	var ports customerClosedPorts
	stop := func() {}
	if running {
		stop = customerClosedConsumers(ctx, consumerDB, vault, &ports)
	}
	defer stop()
	customerClosedInsert(t, db, vault, accounts, sources, customerClosedWarmup, 0)
	customerClosedMaintain(t, owner, true)
	if _, _, err = customerClosedDecode(ctx, manager, customerClosedWarmup); err != nil {
		t.Fatal("warmup", err)
	}
	// Let both one-second pollers actually run; neither timed variant pays this
	// warm-up wait. A SQL error cannot masquerade as a cheap closed consumer.
	timer := time.NewTimer(1100 * time.Millisecond)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
		t.Fatal(ctx.Err())
	}
	customerClosedInsert(t, db, vault, accounts, sources, customerClosedSamples, customerClosedWarmup)
	customerClosedMaintain(t, owner, true)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	goroutines := runtime.NumGoroutine()
	totalQueries := queries.total.Load()
	monitorDone, monitorStop := make(chan struct{}), make(chan struct{})
	heapPeak, goroutinePeak := before.HeapAlloc, goroutines
	go func() {
		defer close(monitorDone)
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-monitorStop:
				return
			case <-tick.C:
				var memory runtime.MemStats
				runtime.ReadMemStats(&memory)
				heapPeak = max(heapPeak, memory.HeapAlloc)
				goroutinePeak = max(goroutinePeak, runtime.NumGoroutine())
			}
		}
	}()
	result, _, err := customerClosedDecode(ctx, manager, customerClosedSamples)
	runtime.ReadMemStats(&after)
	close(monitorStop)
	<-monitorDone
	result.GoroutinesAfter = runtime.NumGoroutine()
	result.ConsumerQueries = queries.total.Load() - totalQueries
	stop()
	if err != nil {
		t.Fatal("measured decode", err)
	}
	if ports.native.Load() != 0 || ports.reads.Load() != 0 || queries.bodies.Load() != 0 || queries.failures.Load() != 0 {
		t.Fatalf("closed consumers accessed source/native or failed SQL: native=%d reads=%d bodies=%d failures=%d",
			ports.native.Load(), ports.reads.Load(), queries.bodies.Load(), queries.failures.Load())
	}
	if running && queries.total.Load() == 0 {
		t.Fatal("closed consumers did not poll")
	}
	customerClosedAssertIdle(t, db, groups)
	result.Platform, result.Groups, result.Round, result.ConsumersRunning = platform, groups, round, running
	if err = db.QueryRow(ctx, `SHOW jit`).Scan(&result.JIT); err != nil {
		t.Fatal("read test JIT setting", err)
	}
	result.FixedStatistics = true
	result.Accounts, result.Warmup, result.HotspotHalf = len(accounts), customerClosedWarmup, true
	result.Codec, result.Workers, result.QueueCapacity = CodecVersion, DecoderWorkers, DecoderQueueCapacity
	result.AllocatedBytes, result.Allocs = after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs
	result.AllocatedBytesPerMsg = float64(result.AllocatedBytes) / customerClosedSamples
	result.AllocsPerMsg = float64(result.Allocs) / customerClosedSamples
	result.HeapBefore, result.HeapAfter, result.HeapPeak = before.HeapAlloc, after.HeapAlloc, max(heapPeak, after.HeapAlloc)
	result.GoroutinesBefore, result.GoroutinesPeak = goroutines, max(goroutinePeak, result.GoroutinesAfter)
	result.ConsumerBodyReads, result.ConsumerNativeCalls, result.ConsumerSourceReads = queries.bodies.Load(), ports.native.Load(), ports.reads.Load()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CUSTOMER_CLOSED_PERFORMANCE %s\n", encoded)
	return result
}

func customerClosedComparison(t *testing.T, results []customerClosedResult) {
	t.Helper()
	if len(results) != 10 {
		t.Fatalf("expected five paired rounds, got %d results", len(results))
	}
	for _, result := range results {
		if result.Platform != results[0].Platform || result.Groups != results[0].Groups {
			t.Fatalf("comparison mixed scopes: got %s/%d, expected %s/%d",
				result.Platform, result.Groups, results[0].Platform, results[0].Groups)
		}
	}
	ratios := map[string][]float64{"decode": {}, "full_service": {}, "queue": {}, "claim": {}, "throughput": {}}
	for round := 1; round <= 5; round++ {
		var off, on *customerClosedResult
		for i := range results {
			if results[i].Round == round {
				if results[i].ConsumersRunning {
					on = &results[i]
				} else {
					off = &results[i]
				}
			}
		}
		if off == nil || on == nil {
			t.Fatal("missing paired closed-consumer result")
		}
		for _, metric := range []struct {
			name    string
			off, on float64
		}{
			{"decode", off.DecodeP95MS, on.DecodeP95MS}, {"full_service", off.ServiceP95MS, on.ServiceP95MS},
			{"queue", off.QueueP95MS, on.QueueP95MS}, {"claim", off.ClaimP95MS, on.ClaimP95MS},
			{"throughput", off.PerSecond, on.PerSecond},
		} {
			if metric.off > 0 {
				ratios[metric.name] = append(ratios[metric.name], metric.on/metric.off)
			}
		}
	}
	medians := make(map[string]float64, len(ratios))
	for key, values := range ratios {
		if len(values) == 5 {
			medians[key] = customerClosedQuantile(values, .5)
		}
	}
	encoded, err := json.Marshal(map[string]any{"platform": results[0].Platform, "groups": results[0].Groups,
		"paired_rounds": 5, "median_on_off_ratios": medians})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CUSTOMER_CLOSED_COMPARISON %s\n", encoded)
	for _, metric := range []struct {
		name string
		max  float64
		min  float64
	}{
		{name: "decode", max: 1.05},
		{name: "full_service", max: 1.05},
		{name: "queue", max: 1.05},
		{name: "claim", max: 1.05},
		{name: "throughput", min: 0.95},
	} {
		ratio, ok := medians[metric.name]
		if !ok {
			t.Fatalf("missing paired metric %s", metric.name)
		}
		if metric.max > 0 && ratio > metric.max {
			t.Fatalf("closed consumers regressed %s: median on/off ratio %.4f > %.2f", metric.name, ratio, metric.max)
		}
		if metric.min > 0 && ratio < metric.min {
			t.Fatalf("closed consumers regressed %s: median on/off ratio %.4f < %.2f", metric.name, ratio, metric.min)
		}
	}
}
