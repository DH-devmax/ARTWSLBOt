package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type ReceiverSnapshot struct {
	State         string     `json:"state"`
	Error         string     `json:"error,omitempty"`
	ConnectedAt   *time.Time `json:"connected_at"`
	HeartbeatAt   *time.Time `json:"heartbeat_at"`
	HeartbeatMS   *float64   `json:"heartbeat_ms"`
	LastReceived  *time.Time `json:"last_received_at"`
	LastCommitted *time.Time `json:"last_committed_at"`
	LastACK       *time.Time `json:"last_ack_at"`
	Frames        int64      `json:"frames"`
	Messages      int64      `json:"messages"`
	Duplicates    int64      `json:"duplicates"`
	ACKs          int64      `json:"acks"`
	Quarantined   int64      `json:"quarantined"`
	Reconnects    int64      `json:"reconnects"`
	QueueDepth    int        `json:"queue_depth"`
	QueueCapacity int        `json:"queue_capacity"`
	DurableP95    *float64   `json:"durable_p95_ms"`
	ACKP95        *float64   `json:"ack_p95_ms"`
	Samples       int        `json:"latency_samples"`
	SampledAt     time.Time  `json:"sampled_at"`
	BlockedPacket string     `json:"blocked_packet,omitempty"`
	Received60s   int64      `json:"received_60s"`
	PerSecond     float64    `json:"per_second"`
}

type receiverSpec struct {
	ID, UID         string
	Revision, Epoch int64
}

type receiverWorker struct {
	spec     receiverSpec
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
	snapshot ReceiverSnapshot
	durable  []float64
	ack      []float64
	rate     [60]struct{ second, count int64 }
}

func (w *receiverWorker) observe(fn func(*ReceiverSnapshot)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	fn(&w.snapshot)
	w.snapshot.SampledAt = time.Now().UTC()
}

func (w *receiverWorker) view() ReceiverSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.snapshot
	now := time.Now().Unix()
	out.Received60s = 0
	for _, bucket := range w.rate {
		if bucket.second <= now && now-bucket.second < 60 {
			out.Received60s += bucket.count
		}
	}
	out.PerSecond = float64(out.Received60s) / 60
	return out
}

type ReceiverManager struct {
	DB    *pgxpool.Pool
	Vault *security.Vault
	IDs   func(int64) nativeauth.RequestIDSource
	mu    sync.Mutex
	jobs  map[string]*receiverWorker
	owner string
}

func NewReceiverManager(db *pgxpool.Pool, vault *security.Vault, ids func(int64) nativeauth.RequestIDSource) *ReceiverManager {
	owner, _ := security.Token()
	return &ReceiverManager{DB: db, Vault: vault, IDs: ids, jobs: map[string]*receiverWorker{}, owner: owner}
}

func (m *ReceiverManager) Stop(account string) {
	m.mu.Lock()
	w := m.jobs[account]
	if w != nil {
		w.cancel()
	}
	m.mu.Unlock()
	if w != nil {
		<-w.done
	}
}

func (m *ReceiverManager) stopAll() {
	m.mu.Lock()
	jobs := m.jobs
	for _, w := range jobs {
		w.cancel()
	}
	m.mu.Unlock()
	for _, w := range jobs {
		<-w.done
	}
}

// Run holds one process coordinator lock, and leases each account separately.
// A replacement process waits for the old account lease to expire; a lost
// coordinator connection cancels all sockets before that lease can be renewed.
func (m *ReceiverManager) Run(ctx context.Context) {
	defer m.stopAll()
	for ctx.Err() == nil {
		if err := m.coordinate(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("native receiver coordinator unavailable", "code", "receiver_owner_unavailable")
		}
		m.stopAll()
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

func (m *ReceiverManager) coordinate(ctx context.Context) error {
	open, cancel := context.WithTimeout(ctx, 3*time.Second)
	conn, err := pgx.ConnectConfig(open, m.DB.Config().ConnConfig.Copy())
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	var owns bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(798428)`).Scan(&owns); err != nil || !owns {
		return errors.New("receiver_owner_busy")
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if err = m.reconcile(ctx, conn); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (m *ReceiverManager) reconcile(ctx context.Context, owner *pgx.Conn) error {
	work, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := owner.Ping(work); err != nil {
		return err
	}
	rows, err := owner.Query(work, `SELECT a.id::text,a.native_uid,r.revision,a.epoch
        FROM account_receivers r JOIN accounts a ON a.id=r.account_id
        WHERE r.enabled AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
            AND coalesce(r.snapshot->>'state','')<>'blocked'`)
	if err != nil {
		return err
	}
	specs := map[string]receiverSpec{}
	for rows.Next() {
		var spec receiverSpec
		if err = rows.Scan(&spec.ID, &spec.UID, &spec.Revision, &spec.Epoch); err != nil {
			break
		}
		specs[spec.ID] = spec
	}
	rows.Close()
	if err != nil || rows.Err() != nil {
		return errors.New("receiver_database_unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, w := range m.jobs {
		spec, active := specs[id]
		if !active || spec != w.spec {
			w.cancel()
		}
		select {
		case <-w.done:
			delete(m.jobs, id)
			_, err = owner.Exec(work, `UPDATE account_receivers SET owner='',lease_until=NULL
                WHERE account_id=$1 AND owner=$2`, id, m.owner)
			if err != nil {
				return err
			}
			if _, err = owner.Exec(work, `UPDATE accounts SET runtime_state='disconnected' WHERE id=$1`, id); err != nil {
				return err
			}
		default:
			if active && spec == w.spec {
				view, _ := json.Marshal(w.view())
				tag, updateErr := owner.Exec(work, `UPDATE account_receivers SET lease_until=now()+interval '15 seconds',
                    observed_at=now(),snapshot=$4 WHERE account_id=$1 AND owner=$2 AND revision=$3 AND enabled`,
					id, m.owner, spec.Revision, view)
				if updateErr != nil || tag.RowsAffected() != 1 {
					w.cancel()
					if updateErr != nil {
						return updateErr
					}
				}
				if _, err = owner.Exec(work, `UPDATE accounts SET runtime_state=$2 WHERE id=$1`, id, w.view().State); err != nil {
					return err
				}
			}
		}
	}
	for id, spec := range specs {
		if _, exists := m.jobs[id]; exists {
			continue
		}
		tag, err := owner.Exec(work, `UPDATE account_receivers SET owner=$2,lease_until=now()+interval '15 seconds'
            WHERE account_id=$1 AND revision=$3 AND enabled AND
            (owner='' OR lease_until<now() OR owner=$2)`, id, m.owner, spec.Revision)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		workerCtx, stop := context.WithCancel(ctx)
		w := &receiverWorker{spec: spec, cancel: stop, done: make(chan struct{}),
			snapshot: ReceiverSnapshot{State: "connecting", QueueCapacity: 32, SampledAt: time.Now().UTC()}}
		m.jobs[id] = w
		go func() {
			defer close(w.done)
			m.runAccount(workerCtx, w)
		}()
	}
	return nil
}

type savedNativeSession struct {
	DeploymentVersion int64              `json:"deployment_version"`
	Session           nativeauth.Session `json:"session"`
}

func (m *ReceiverManager) load(ctx context.Context, spec receiverSpec) (nativeauth.Deployment, savedNativeSession, string, string, error) {
	var session, deployment, profile []byte
	var version int64
	var cursor string
	err := m.DB.QueryRow(ctx, `SELECT a.credential_sealed,d.sealed,d.version,p.sealed,r.cursor::text
        FROM accounts a JOIN account_receivers r ON r.account_id=a.id
        CROSS JOIN native_deployment d CROSS JOIN native_im_profile p
        WHERE a.id=$1 AND a.epoch=$2 AND a.auth_state='authenticated'
            AND r.enabled AND r.revision=$3 AND r.owner=$4 AND r.lease_until>now()`,
		spec.ID, spec.Epoch, spec.Revision, m.owner).Scan(&session, &deployment, &version, &profile, &cursor)
	var d nativeauth.Deployment
	var saved savedNativeSession
	if err != nil {
		return d, saved, "", "", errors.New("receiver_configuration_unavailable")
	}
	open := func(aad string, sealed []byte, out any) error {
		plain, err := m.Vault.Open(aad, sealed)
		if err != nil {
			return err
		}
		defer clear(plain)
		return json.Unmarshal(plain, out)
	}
	var im struct {
		AppKey string `json:"app_key"`
	}
	if open("native-deployment:v1", deployment, &d) != nil ||
		open("native-session:"+spec.ID, session, &saved) != nil ||
		open("native-im-profile:v1", profile, &im) != nil ||
		im.AppKey == "" || saved.DeploymentVersion != version ||
		strconv.FormatUint(saved.Session.UID, 10) != spec.UID {
		return d, saved, "", "", errors.New("receiver_configuration_invalid")
	}
	return d, saved, im.AppKey, cursor, nil
}

func (m *ReceiverManager) runAccount(ctx context.Context, w *receiverWorker) {
	defer w.observe(func(s *ReceiverSnapshot) { s.State = "stopped" })
	delay := time.Second
	for ctx.Err() == nil {
		w.observe(func(s *ReceiverSnapshot) { s.State = "connecting" })
		err := m.connection(ctx, w)
		if ctx.Err() != nil {
			return
		}
		code := receiverError(err)
		block := code == "im_kicked" || code == "im_auth_rejected" ||
			code == "native_push_boundary_unknown" || code == "receiver_configuration_invalid" ||
			code == "native_reauth_required" || code == "native_unknown_result"
		w.observe(func(s *ReceiverSnapshot) {
			s.Error, s.State = code, "reconnecting"
			s.Reconnects++
			if block {
				s.State = "blocked"
			}
		})
		if block {
			persist, stop := context.WithTimeout(ctx, 2*time.Second)
			raw, _ := json.Marshal(w.view())
			_, _ = m.DB.Exec(persist, `UPDATE account_receivers SET snapshot=$4,observed_at=now()
                WHERE account_id=$1 AND revision=$2 AND owner=$3`, w.spec.ID, w.spec.Revision, m.owner, raw)
			stop()
			// Keep the visible blocked state; only an explicit stop/start retries.
			<-ctx.Done()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(receiverRetry(err, delay)):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func receiverError(err error) string {
	if err == nil {
		return "im_disconnected"
	}
	var native *nativeauth.Error
	if errors.As(err, &native) {
		switch native.Code {
		case "native_reauth_required", "native_security_required":
			return "native_reauth_required"
		case "native_transport_unknown", "native_unknown_result":
			return "native_unknown_result"
		case "native_rate_limited":
			return "native_rate_limited"
		default:
			return "native_session_renewal_failed"
		}
	}
	switch err.Error() {
	case "im_kicked", "im_auth_rejected", "im_disconnected", "im_connection_failed", "im_write_failed",
		"im_reply_timeout", "im_heartbeat_invalid", "im_discovery_failed", "im_discovery_endpoint",
		"native_push_boundary_unknown", "bounded_ingress_full", "receiver_configuration_invalid",
		"receiver_configuration_unavailable", "receiver_database_unavailable", "native_connection_rotation_required":
		return err.Error()
	default:
		return "receiver_failed"
	}
}

func receiverRetry(err error, delay time.Duration) time.Duration {
	var native *nativeauth.Error
	if errors.As(err, &native) && native.Code == "native_rate_limited" {
		return max(delay, min(native.RetryAfter, 24*time.Hour))
	}
	return delay
}
