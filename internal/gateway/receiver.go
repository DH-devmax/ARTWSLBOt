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

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/scopecache"
	"wangshangliao/internal/security"
)

type ReceiverSnapshot struct {
	Platform        string     `json:"platform,omitempty"`
	ACKMode         string     `json:"ack_mode,omitempty"`
	State           string     `json:"state"`
	Error           string     `json:"error,omitempty"`
	ErrorStage      string     `json:"error_stage,omitempty"`
	UpstreamCode    int        `json:"upstream_code,omitempty"`
	ConnectedAt     *time.Time `json:"connected_at"`
	SyncedAt        *time.Time `json:"synced_at"`
	RetryAt         *time.Time `json:"retry_at"`
	HeartbeatAt     *time.Time `json:"heartbeat_at"`
	HeartbeatMS     *float64   `json:"heartbeat_ms"`
	LastReceived    *time.Time `json:"last_received_at"`
	LastCommitted   *time.Time `json:"last_committed_at"`
	LastACK         *time.Time `json:"last_ack_at"`
	Frames          int64      `json:"frames"`
	Messages        int64      `json:"messages"`
	Duplicates      int64      `json:"duplicates"`
	ACKs            int64      `json:"acks"`
	Quarantined     int64      `json:"quarantined"`
	Reconnects      int64      `json:"reconnects"`
	QueueDepth      int        `json:"queue_depth"`
	QueueCapacity   int        `json:"queue_capacity"`
	QueueBytes      int64      `json:"queue_bytes"`
	DurableP95      *float64   `json:"durable_p95_ms"`
	ACKP95          *float64   `json:"ack_p95_ms"`
	Samples         int        `json:"latency_samples"`
	ACKSamples      int        `json:"ack_samples"`
	WindowSeconds   int        `json:"latency_window_seconds"`
	SampleLimit     int        `json:"latency_sample_limit"`
	SampledAt       time.Time  `json:"sampled_at"`
	BlockedPacket   string     `json:"blocked_packet,omitempty"`
	Received60s     int64      `json:"received_60s"`
	PerSecond       float64    `json:"per_second"`
	Unique60s       int64      `json:"unique_60s"`
	UniquePerSecond float64    `json:"unique_per_second"`
}

type receiverSpec struct {
	ID, UID         string
	Platform        string
	Revision, Epoch int64
}

type receiverWorker struct {
	spec            receiverSpec
	cancel          context.CancelFunc
	done            chan struct{}
	mu              sync.Mutex
	snapshot        ReceiverSnapshot
	durable         latencyWindow
	ack             latencyWindow
	leaseUntil      time.Time
	connection      *ownedTextConnection
	chiliConnection *ownedChiliConnection
	chiliAfter      string // Owned only by this worker's connection loop.
	rate            [60]struct{ second, count, unique int64 }
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
	out.Unique60s = 0
	for _, bucket := range w.rate {
		if bucket.second <= now && now-bucket.second < 60 {
			out.Received60s += bucket.count
			out.Unique60s += bucket.unique
		}
	}
	out.PerSecond = float64(out.Received60s) / 60
	out.UniquePerSecond = float64(out.Unique60s) / 60
	out.DurableP95, out.Samples = w.durable.read(time.Now())
	out.ACKP95, out.ACKSamples = w.ack.read(time.Now())
	out.WindowSeconds, out.SampleLimit = 60, latencySampleLimit
	return out
}

const latencySampleLimit = 8192

type latencyWindow struct {
	values [latencySampleLimit]struct {
		at    time.Time
		value float64
	}
	next int
}

func (l *latencyWindow) add(at time.Time, value float64) {
	l.values[l.next].at, l.values[l.next].value = at, max(0, value)
	l.next = (l.next + 1) % latencySampleLimit
}

func (l *latencyWindow) read(now time.Time) (*float64, int) {
	values := make([]float64, 0, 256)
	for _, sample := range l.values {
		if !sample.at.IsZero() && !sample.at.After(now) && now.Sub(sample.at) < time.Minute {
			values = append(values, sample.value)
		}
	}
	return percentile(values), len(values)
}

// LiveSnapshot is usable only while this process still owns a fresh account
// lease. Durable database observations remain the fallback for other processes.
func (m *ReceiverManager) LiveSnapshot(account string) (ReceiverSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.jobs[account]
	if w == nil {
		return ReceiverSnapshot{}, false
	}
	w.mu.Lock()
	fresh := time.Now().Before(w.leaseUntil)
	w.mu.Unlock()
	return w.view(), fresh
}

type ReceiverManager struct {
	DB          *pgxpool.Pool
	Vault       *security.Vault
	IDs         func(int64) nativeauth.RequestIDSource
	MobileIDs   func(int64) nativeauth.RequestIDSource
	mu          sync.Mutex
	jobs        map[string]*receiverWorker
	owner       string
	reads       *ReadScheduler
	WakeDecoder func()
	Changes     *ChangeBus
	invitations map[string]invitationCache
	ReadCache   *scopecache.Cache
}

func NewReceiverManager(db *pgxpool.Pool, vault *security.Vault, ids func(int64) nativeauth.RequestIDSource) *ReceiverManager {
	owner, _ := security.Token()
	m := &ReceiverManager{DB: db, Vault: vault, IDs: ids, jobs: map[string]*receiverWorker{}, owner: owner, ReadCache: scopecache.New()}
	return m
}

func (m *ReceiverManager) Stop(account string) {
	m.ReadCache.InvalidateAccount(account)
	m.mu.Lock()
	w := m.jobs[account]
	delete(m.invitations, account)
	reads := m.reads
	if w != nil {
		w.cancel()
	}
	m.mu.Unlock()
	if reads != nil {
		reads.CancelAccount(account)
	}
	if w != nil {
		<-w.done
	}
}

func (m *ReceiverManager) stopAll() {
	m.mu.Lock()
	jobs := m.jobs
	m.invitations = nil
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
	ctx, cancel = context.WithCancel(ctx)
	reads := NewReadScheduler(ctx)
	m.mu.Lock()
	m.reads = reads
	m.mu.Unlock()
	directoryDone := make(chan struct{})
	go func() { defer close(directoryDone); m.RunDirectory(ctx) }()
	defer func() {
		cancel()
		reads.Close()
		<-directoryDone
		m.mu.Lock()
		if m.reads == reads {
			m.reads = nil
		}
		m.mu.Unlock()
	}()
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
	rows, err := owner.Query(work, `SELECT a.id::text,a.native_uid,r.revision,a.epoch,a.platform
        FROM account_receivers r JOIN accounts a ON a.id=r.account_id
        WHERE r.enabled AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
            AND coalesce(r.snapshot->>'state','')<>'blocked'`)
	if err != nil {
		return err
	}
	specs := map[string]receiverSpec{}
	for rows.Next() {
		var spec receiverSpec
		if err = rows.Scan(&spec.ID, &spec.UID, &spec.Revision, &spec.Epoch, &spec.Platform); err != nil {
			break
		}
		specs[spec.ID] = spec
	}
	rows.Close()
	if err != nil || rows.Err() != nil {
		return errors.New("receiver_database_unavailable")
	}
	m.mu.Lock()
	for id, entry := range m.invitations {
		if time.Since(entry.Base.SampledAt) > 30*time.Second {
			delete(m.invitations, id)
		}
	}
	jobs := make(map[string]*receiverWorker, len(m.jobs))
	for id, w := range m.jobs {
		jobs[id] = w
		spec, active := specs[id]
		if !active || spec != w.spec {
			m.ReadCache.InvalidateAccount(id)
			w.cancel()
		}
	}
	m.mu.Unlock()
	// Submission fences may hold these rows across bounded native I/O. Skip
	// locked rows without extending the worker's last committed lease.
	for id, w := range jobs {
		spec, active := specs[id]
		select {
		case <-w.done:
			tag, releaseErr := owner.Exec(work, `WITH unlocked AS (
                SELECT r.account_id FROM account_receivers r JOIN accounts a ON a.id=r.account_id
                WHERE r.account_id=$1 AND r.owner=$2 FOR UPDATE OF a,r SKIP LOCKED),
                released AS (UPDATE account_receivers r SET owner='',lease_until=NULL FROM unlocked u
                    WHERE r.account_id=u.account_id RETURNING r.account_id)
                UPDATE accounts a SET runtime_state=CASE WHEN a.epoch=$3 THEN 'disconnected' ELSE a.runtime_state END
                FROM released r WHERE a.id=r.account_id`, id, m.owner, w.spec.Epoch)
			if releaseErr != nil {
				return releaseErr
			}
			if tag.RowsAffected() == 0 {
				var pending bool
				if err = owner.QueryRow(work, `SELECT EXISTS(SELECT 1 FROM account_receivers
                    WHERE account_id=$1 AND owner=$2)`, id, m.owner).Scan(&pending); err != nil {
					return err
				}
				if pending {
					continue
				}
			}
			m.mu.Lock()
			if m.jobs[id] == w {
				delete(m.jobs, id)
			}
			m.mu.Unlock()
		default:
			if active && spec == w.spec {
				w.mu.Lock()
				fresh := time.Now().Before(w.leaseUntil)
				w.mu.Unlock()
				if !fresh {
					w.cancel()
					continue
				}
				snapshot := w.view()
				view, _ := json.Marshal(snapshot)
				var lease time.Time
				updateErr := owner.QueryRow(work, `WITH unlocked AS (
                    SELECT r.account_id FROM account_receivers r JOIN accounts a ON a.id=r.account_id
                    WHERE r.account_id=$1 AND r.owner=$2 AND r.revision=$3 AND r.enabled
                        AND r.lease_until>clock_timestamp() AND a.epoch=$5 AND a.native_uid=$6
                        AND a.platform=$7 AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
                    FOR UPDATE OF r SKIP LOCKED)
                    UPDATE account_receivers r SET lease_until=clock_timestamp()+interval '15 seconds',
                        observed_at=clock_timestamp(),snapshot=$4 FROM unlocked u
                    WHERE r.account_id=u.account_id RETURNING r.lease_until`,
					id, m.owner, spec.Revision, view, spec.Epoch, spec.UID, spec.Platform).Scan(&lease)
				if updateErr == pgx.ErrNoRows {
					var current bool
					if err = owner.QueryRow(work, `SELECT EXISTS(
                        SELECT 1 FROM account_receivers r JOIN accounts a ON a.id=r.account_id
                        WHERE r.account_id=$1 AND r.owner=$2 AND r.revision=$3 AND r.enabled
                            AND r.lease_until>clock_timestamp() AND a.epoch=$4 AND a.native_uid=$5
                            AND a.platform=$6 AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
                            AND coalesce(r.snapshot->>'state','')<>'blocked')`,
						id, m.owner, spec.Revision, spec.Epoch, spec.UID, spec.Platform).Scan(&current); err != nil {
						return err
					}
					if !current {
						w.cancel()
					}
					continue
				}
				if updateErr != nil {
					w.cancel()
					return updateErr
				}
				w.mu.Lock()
				w.leaseUntil = lease
				w.mu.Unlock()
				if err = saveReceiverState(work, owner, spec, snapshot.State); err != nil {
					return err
				}
				m.Changes.Publish(Change{Account: id, Kind: "receiver"})
			}
		}
	}
	for id, spec := range specs {
		m.mu.Lock()
		_, exists := m.jobs[id]
		m.mu.Unlock()
		if exists {
			continue
		}
		var lease time.Time
		err := owner.QueryRow(work, `WITH unlocked AS (
            SELECT r.account_id FROM account_receivers r JOIN accounts a ON a.id=r.account_id
            WHERE r.account_id=$1 AND r.revision=$3 AND r.enabled AND a.epoch=$4
                AND a.native_uid=$5 AND a.platform=$6 AND a.auth_state='authenticated'
                AND a.credential_sealed IS NOT NULL
                AND (r.owner='' OR r.lease_until<clock_timestamp() OR r.owner=$2)
            FOR UPDATE OF r SKIP LOCKED)
            UPDATE account_receivers r SET owner=$2,lease_until=clock_timestamp()+interval '15 seconds'
            FROM unlocked u WHERE r.account_id=u.account_id RETURNING r.lease_until`,
			id, m.owner, spec.Revision, spec.Epoch, spec.UID, spec.Platform).Scan(&lease)
		if err == pgx.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		workerCtx, stop := context.WithCancel(ctx)
		w := &receiverWorker{spec: spec, cancel: stop, done: make(chan struct{}),
			leaseUntil: lease,
			snapshot:   ReceiverSnapshot{State: "connecting", QueueCapacity: 32, SampledAt: time.Now().UTC()}}
		m.mu.Lock()
		m.jobs[id] = w
		m.mu.Unlock()
		go func() {
			defer close(w.done)
			m.runAccount(workerCtx, w)
		}()
	}
	return nil
}

func saveReceiverState(ctx context.Context, owner *pgx.Conn, spec receiverSpec, state string) error {
	_, err := owner.Exec(ctx, `WITH unlocked AS (
        SELECT id FROM accounts WHERE id=$1 AND epoch=$3 AND runtime_state IS DISTINCT FROM $2
        FOR UPDATE SKIP LOCKED)
        UPDATE accounts a SET runtime_state=$2 FROM unlocked u WHERE a.id=u.id`,
		spec.ID, state, spec.Epoch)
	return err
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
	if d.ProtocolVersion != nativeauth.DesktopProtocolVersion ||
		saved.Session.ProtocolVersion != nativeauth.DesktopProtocolVersion {
		return d, saved, "", "", errors.New("receiver_configuration_invalid")
	}
	return d, saved, im.AppKey, cursor, nil
}

func (m *ReceiverManager) runAccount(ctx context.Context, w *receiverWorker) {
	defer w.observe(func(s *ReceiverSnapshot) { s.State = "stopped" })
	delay := time.Second
	for ctx.Err() == nil {
		w.observe(func(s *ReceiverSnapshot) {
			s.State, s.RetryAt, s.SyncedAt, s.HeartbeatAt, s.HeartbeatMS = "connecting", nil, nil, nil, nil
			s.Error, s.ErrorStage, s.UpstreamCode = "", "", 0
		})
		var err error
		if w.spec.Platform == "lajiaoliao" {
			err = m.lajiaoConnection(ctx, w)
		} else {
			err = m.connection(ctx, w)
		}
		if ctx.Err() != nil {
			return
		}
		code := receiverError(err)
		block := receiverBlocked(code)
		if w.spec.Platform == "lajiaoliao" {
			block = block || lajiaoliao.IsTerminal(err)
		}
		if synced := w.view().SyncedAt; synced != nil && time.Since(*synced) >= 30*time.Second {
			delay = time.Second
		}
		wait := receiverRetry(err, delay)
		next := time.Now().UTC().Add(wait)
		w.observe(func(s *ReceiverSnapshot) {
			s.Error, s.State = code, "reconnecting"
			s.ErrorStage, s.UpstreamCode = receiverDiagnostic(err)
			s.Reconnects++
			s.RetryAt = &next
			if block {
				s.State, s.RetryAt = "blocked", nil
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
		case <-time.After(wait):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func receiverDiagnostic(err error) (string, int) {
	var login *IMLoginError
	if errors.As(err, &login) {
		return "im_login", int(login.Code)
	}
	var native *nativeauth.Error
	if errors.As(err, &native) {
		return "im_token", max(0, min(native.Number, 65535))
	}
	return "", 0
}

func receiverBlocked(code string) bool {
	switch code {
	case "im_kicked", "im_auth_rejected", "native_push_boundary_unknown", "receiver_configuration_invalid",
		"native_reauth_required", "native_unknown_result", "native_session_renewal_failed":
		return true
	default:
		return false
	}
}

func receiverError(err error) string {
	if err == nil {
		return "im_disconnected"
	}
	var limited *RateLimited
	var chili *lajiaoliao.Failure
	if errors.As(err, &chili) {
		return chili.Code
	}
	if errors.As(err, &limited) {
		return "native_rate_limited"
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
		"im_reply_timeout", "im_sync_timeout", "im_heartbeat_invalid", "im_discovery_failed", "im_discovery_endpoint",
		"native_push_boundary_unknown", "bounded_ingress_full", "receiver_configuration_invalid",
		"receiver_configuration_unavailable", "receiver_database_unavailable", "native_connection_rotation_required":
		return err.Error()
	default:
		return "receiver_failed"
	}
}

func receiverRetry(err error, delay time.Duration) time.Duration {
	var limited *RateLimited
	if errors.As(err, &limited) {
		return max(delay, min(max(time.Second, limited.After), 24*time.Hour))
	}
	var native *nativeauth.Error
	if errors.As(err, &native) && native.Code == "native_rate_limited" {
		return max(delay, min(native.RetryAfter, 24*time.Hour))
	}
	return delay
}
