package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

const DecoderWorkers = 4
const DecoderQueueCapacity = 16
const CodecVersion = 7

type DecodedMessage struct {
	Platform      string                        `json:"platform,omitempty"`
	NativeSender  string                        `json:"native_sender,omitempty"`
	NativeTarget  string                        `json:"native_target,omitempty"`
	NativeName    string                        `json:"native_name,omitempty"`
	Message       nativewire.ApplicationMessage `json:"message"`
	TransportPeer string                        `json:"transport_peer"`
	TransportTo   string                        `json:"transport_to"`
	ServerID      string                        `json:"server_id"`
	NativeFormat  string                        `json:"native_format"`
	Mapping       string                        `json:"mapping"`
	Outgoing      bool                          `json:"outgoing"`
}

type decodeJob struct {
	ID                 int64
	Account, Key, Kind string
	Received           time.Time
	DeploymentVersion  int64
	Bytes              int64
	Platform           string
}

type DecoderSnapshot struct {
	Observed      bool     `json:"observed"`
	Workers       int      `json:"workers"`
	Active        int64    `json:"active"`
	QueueDepth    int      `json:"queue_depth"`
	QueueCapacity int      `json:"queue_capacity"`
	Completed     int64    `json:"completed"`
	P95MS         *float64 `json:"p95_ms"`
	Samples       int      `json:"samples"`
	WindowSeconds int      `json:"window_seconds"`
	QueueBytes    int64    `json:"queue_bytes"`
	Error         string   `json:"error"`
}

// DecoderManager has no native sender and never participates in ACK/cursor
// advancement. Work is recoverable from PostgreSQL, not an unbounded Go queue.
type DecoderManager struct {
	DB         *pgxpool.Pool
	Vault      *security.Vault
	owner      string
	queue      chan decodeJob
	active     atomic.Int64
	done       atomic.Int64
	running    atomic.Bool
	mu         sync.Mutex
	latencies  latencyWindow
	lastErr    string
	wake       chan struct{}
	Changes    *ChangeBus
	queueBytes atomic.Int64
}

func NewDecoderManager(db *pgxpool.Pool, vault *security.Vault) *DecoderManager {
	owner, _ := security.Token()
	return &DecoderManager{DB: db, Vault: vault, owner: owner, queue: make(chan decodeJob, DecoderQueueCapacity),
		wake: make(chan struct{}, 1)}
}

// Notify coalesces arrivals. It never waits for a worker or a database query.
func (m *DecoderManager) Notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *DecoderManager) Snapshot() DecoderSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	p95, count := m.latencies.read(time.Now())
	return DecoderSnapshot{Observed: m.running.Load(), Workers: DecoderWorkers, Active: m.active.Load(),
		QueueDepth: len(m.queue), QueueCapacity: cap(m.queue), Completed: m.done.Load(),
		P95MS: p95, Samples: count, WindowSeconds: 60, QueueBytes: m.queueBytes.Load(), Error: m.lastErr}
}

func (m *DecoderManager) Run(ctx context.Context) {
	m.running.Store(true)
	defer m.running.Store(false)
	var wg sync.WaitGroup
	for range DecoderWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-m.queue:
					m.queueBytes.Add(-job.Bytes)
					m.active.Add(1)
					start := time.Now()
					work, stop := context.WithTimeout(ctx, 10*time.Second)
					err := m.process(work, job)
					stop()
					m.Notify()
					m.active.Add(-1)
					m.mu.Lock()
					if err == nil {
						m.latencies.add(time.Now(), float64(time.Since(start).Microseconds())/1000)
						m.done.Add(1)
					}
					m.mu.Unlock()
				}
			}
		}()
	}
	defer wg.Wait()
	for ctx.Err() == nil {
		delay := 5 * time.Second
		hadJobs := false
		if available := cap(m.queue) - len(m.queue); available > 0 {
			work, stop := context.WithTimeout(ctx, 2*time.Second)
			jobs, err := m.claim(work, min(available, 8))
			stop()
			m.mu.Lock()
			m.lastErr = ""
			if err != nil && ctx.Err() == nil {
				m.lastErr = "decoder_database_unavailable"
			}
			m.mu.Unlock()
			if len(jobs) > 0 {
				hadJobs = true
			}
			for _, job := range jobs {
				m.queueBytes.Add(job.Bytes)
				select {
				case <-ctx.Done():
					m.queueBytes.Add(-job.Bytes)
					return
				case m.queue <- job:
				}
			}
		}
		if hadJobs && len(m.queue) < cap(m.queue) {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (m *DecoderManager) claim(ctx context.Context, limit int) ([]decodeJob, error) {
	// At most eight candidates per account, then round-robin across accounts.
	// Decode completion may be parallel; display order is durable event_id,
	// never worker completion order. This projection dispatches no business work.
	rows, err := m.DB.Query(ctx, `WITH candidates AS (
        SELECT e.*,a.platform,row_number() OVER(PARTITION BY a.id ORDER BY e.id) AS position,coalesce(d.version,0) version
        FROM accounts a LEFT JOIN native_deployment d ON a.platform='wsl' CROSS JOIN LATERAL (
            SELECT e.id,e.account_id,e.event_key,e.kind,e.received_at,octet_length(e.payload_sealed) AS bytes
            FROM native_received_events e LEFT JOIN native_decoded_events x ON x.event_id=e.id
            WHERE e.account_id=a.id AND e.kind IN ('group','private') AND e.payload_sealed IS NOT NULL
                AND e.received_at>now()-interval '7 days'
                AND (x.event_id IS NULL OR x.codec_version<=$3 AND (
                    x.state='processing' AND x.lease_until<now()
                    OR x.state<>'processing' AND (x.codec_version<$3 AND ($3<>7 OR x.codec_version<>6 OR x.state IN ('failed','unsupported'))
                        OR x.state='failed' AND x.deployment_version<>d.version)))
            ORDER BY e.id LIMIT 8
        ) e
        WHERE a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL AND (a.platform<>'wsl' OR d.version IS NOT NULL)
        ORDER BY position,e.id LIMIT $1
    ), claimed AS (
        INSERT INTO native_decoded_events(event_id,account_id,state,owner,lease_until,deployment_version,codec_version,received_at)
        SELECT id,account_id,'processing',$2,now()+interval '60 seconds',version,$3,received_at FROM candidates
        ON CONFLICT(event_id) DO UPDATE SET state='processing',owner=$2,lease_until=now()+interval '60 seconds',
            deployment_version=excluded.deployment_version,codec_version=excluded.codec_version,attempts=native_decoded_events.attempts+1
        WHERE native_decoded_events.codec_version<=$3 AND (
            native_decoded_events.state='processing' AND native_decoded_events.lease_until<now()
            OR native_decoded_events.state<>'processing' AND (native_decoded_events.codec_version<$3
                AND ($3<>7 OR native_decoded_events.codec_version<>6 OR native_decoded_events.state IN ('failed','unsupported'))
                OR native_decoded_events.state='failed' AND native_decoded_events.deployment_version<>excluded.deployment_version))
        RETURNING event_id
    ) SELECT c.id,c.account_id::text,c.event_key,c.kind,c.received_at,c.version,c.bytes,c.platform
        FROM candidates c JOIN claimed x ON x.event_id=c.id ORDER BY c.position,c.id`, limit, m.owner, CodecVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []decodeJob{}
	for rows.Next() {
		var job decodeJob
		if err = rows.Scan(&job.ID, &job.Account, &job.Key, &job.Kind, &job.Received, &job.DeploymentVersion, &job.Bytes, &job.Platform); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (m *DecoderManager) process(ctx context.Context, job decodeJob) error {
	if job.Platform == "lajiaoliao" {
		return m.decodeChili(ctx, job)
	}
	start := time.Now()
	var raw, deployment, session []byte
	var epoch int64
	err := m.DB.QueryRow(ctx, `SELECT e.payload_sealed,d.sealed,a.credential_sealed,a.epoch
        FROM native_received_events e JOIN accounts a ON a.id=e.account_id CROSS JOIN native_deployment d
        WHERE e.id=$1 AND e.account_id=$2 AND d.version=$3 AND e.received_at>now()-interval '7 days'
            AND a.auth_state='authenticated'`, job.ID, job.Account, job.DeploymentVersion).
		Scan(&raw, &deployment, &session, &epoch)
	if err != nil {
		return err
	}
	open := func(aad string, sealed []byte, out any) error {
		plain, e := m.Vault.Open(aad, sealed)
		if e != nil {
			return e
		}
		defer clear(plain)
		return json.Unmarshal(plain, out)
	}
	var outer nativewire.IMMessage
	var d nativeauth.Deployment
	var saved savedNativeSession
	state, reason, scene, conversation := "failed", "sealed_source_invalid", "", ""
	var payload []byte
	if open("native-event:"+job.Account+":"+job.Key, raw, &outer) == nil &&
		open("native-deployment:v1", deployment, &d) == nil &&
		open("native-session:"+job.Account, session, &saved) == nil &&
		saved.DeploymentVersion == job.DeploymentVersion {
		var decoded DecodedMessage
		key := d.MessageKey
		// Pinned desktop 2.8.3 uses its project root for app-message AEAD too.
		// This is a versioned contract, not a try-every-key decryption fallback.
		if key == "" && d.ProtocolVersion == nativeauth.DesktopProtocolVersion {
			key = d.BodyKey
		}
		decoded, scene, conversation, state, reason = decodeApplication(key, saved.Session, outer)
		if state != "decoded" && (reason == "native_format" || reason == "envelope_authentication_missing") {
			if notice, noticeScene, noticeKey, ok := m.decodeTransportNotice(ctx, job.Account, outer); ok {
				decoded, scene, conversation, state, reason = notice, noticeScene, noticeKey, "decoded", "transport_notice"
			} else if outer.Fields[8] == "5" {
				state, reason = "unsupported", "notice_scope_unresolved"
			} else if _, _, noticeErr := nativewire.ReadUnencryptedGroupNotice(outer.Fields[10]); noticeErr == nil {
				state, reason = "unsupported", "notice_scope_unresolved"
			}
		}
		if conversation != "" {
			plain, e := json.Marshal(decoded)
			if e != nil {
				return e
			}
			payload, err = m.Vault.Seal(DecodedAAD(job.Account, job.ID), plain)
			clear(plain)
			if err != nil {
				return err
			}
		}
	}
	ms := float64(time.Since(start).Microseconds()) / 1000
	tag, err := m.DB.Exec(ctx, `UPDATE native_decoded_events x SET state=$1,scene=$2,conversation_key=$3,
        reason=$4,payload_sealed=$5,decoded_at=now(),decode_ms=$6,owner='',lease_until=NULL
        WHERE event_id=$7 AND account_id=$8 AND state='processing' AND owner=$9 AND lease_until>now()
            AND EXISTS(SELECT 1 FROM accounts a CROSS JOIN native_deployment d WHERE a.id=x.account_id
                AND a.epoch=$10 AND a.auth_state='authenticated' AND d.version=$11)
            AND received_at>now()-interval '7 days'`,
		state, scene, conversation, reason, payload, ms, job.ID, job.Account, m.owner, epoch, job.DeploymentVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("decoder_scope_changed")
	}
	m.Changes.Publish(Change{Account: job.Account, Kind: "decoded", EventID: job.ID, Conversation: conversation})
	return nil
}

func DecodedAAD(account string, id int64) string {
	return security.DecodedMessageAAD(account, id)
}

func conversationKey(scene string, business uint64, transport string) string {
	digest := sha256.Sum256([]byte(scene + "\x00" + strconv.FormatUint(business, 10) + "\x00" + transport))
	return hex.EncodeToString(digest[:])
}

func decodeApplication(keyText string, session nativeauth.Session, outer nativewire.IMMessage) (DecodedMessage, string, string, string, string) {
	out := DecodedMessage{TransportPeer: outer.Sender, TransportTo: outer.Target, ServerID: outer.ServerID,
		NativeFormat: outer.Fields[8], Mapping: "unverified"}
	scene := "private"
	if outer.Scene == 1 {
		scene = "group"
	}
	if outer.Scene != 0 && outer.Scene != 1 || outer.Target == "" || outer.Sender == "" {
		return out, "", "", "failed", "transport_identity"
	}
	if outer.Fields[8] != "100" {
		// Metadata only. Unsupported transport formats are not authenticated text.
		return out, scene, "", "unsupported", "native_format"
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(keyText)
	if err != nil {
		key, err = base64.StdEncoding.Strict().DecodeString(keyText)
	}
	if err != nil || len(key) != 32 {
		return out, "", "", "failed", "message_key_missing"
	}
	defer clear(key)
	inner, err := nativewire.OpenMessage(key, outer.Fields[10])
	if err != nil {
		var e *nativewire.DecodeError
		reason := "schema"
		if errors.As(err, &e) {
			reason = e.Stage
		}
		return out, "", "", "failed", reason
	}
	return bindDecodedApplication(session, outer, inner, out, scene)
}

func bindDecodedApplication(session nativeauth.Session, outer nativewire.IMMessage, inner nativewire.ApplicationMessage, out DecodedMessage, scene string) (DecodedMessage, string, string, string, string) {
	incoming := inner.Session == 1 && inner.Target.ID == session.UID && outer.Target == session.NIMID
	outgoing := inner.Session == 1 && inner.Sender.ID == session.UID && outer.Sender == session.NIMID
	if outer.Scene == 1 && inner.Session != 2 || outer.Scene == 0 && !incoming && !outgoing {
		return out, "", "", "failed", "routing"
	}
	out.Message = inner
	business, peer := inner.Target.ID, outer.Target
	if outer.Scene == 0 {
		business, peer = inner.Sender.ID, outer.Sender
		if outgoing && !incoming {
			business, peer, out.Outgoing = inner.Target.ID, outer.Target, true
		}
		// An authenticated application header is not proof that an arbitrary NIM
		// peer equals a business UID. Keep both in the private conversation key.
		out.Mapping = "authenticated_header"
	}
	state, reason := "decoded", ""
	if !inner.SupportedContent() {
		state, reason = "unsupported", "application_format"
	}
	return out, scene, conversationKey(scene, business, peer), state, reason
}
