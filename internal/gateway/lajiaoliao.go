package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
)

func (m *ReceiverManager) loadChili(ctx context.Context, w *receiverWorker) (lajiaoliao.Session, error) {
	var sealed []byte
	err := m.DB.QueryRow(ctx, `SELECT a.credential_sealed FROM accounts a JOIN account_receivers r ON r.account_id=a.id
        WHERE a.id=$1 AND a.platform='lajiaoliao' AND a.native_uid=$2 AND a.epoch=$3
        AND a.auth_state='authenticated' AND r.enabled AND r.revision=$4 AND r.owner=$5 AND r.lease_until>now()`,
		w.spec.ID, w.spec.UID, w.spec.Epoch, w.spec.Revision, m.owner).Scan(&sealed)
	if err != nil {
		return lajiaoliao.Session{}, errors.New("receiver_configuration_unavailable")
	}
	plain, err := m.Vault.Open("lajiaoliao-session:"+w.spec.ID, sealed)
	if err != nil {
		return lajiaoliao.Session{}, errors.New("receiver_configuration_invalid")
	}
	defer clear(plain)
	var session lajiaoliao.Session
	if json.Unmarshal(plain, &session) != nil || session.UserID != w.spec.UID || session.IMToken == "" || session.DeviceID == "" {
		return session, errors.New("receiver_configuration_invalid")
	}
	return session, nil
}

func (m *ReceiverManager) renewChili(ctx context.Context, w *receiverWorker, s lajiaoliao.Session) error {
	return m.read(ctx, w.spec.ID, "lajiaoliao-renew", ReadRenewal, func(work context.Context) error {
		client := lajiaoliao.NewClient()
		defer client.Close()
		next, err := client.Renew(work, s)
		if err != nil {
			return err
		}
		plain, err := json.Marshal(next)
		if err != nil {
			return err
		}
		defer clear(plain)
		sealed, err := m.Vault.Seal("lajiaoliao-session:"+w.spec.ID, plain)
		if err != nil {
			return err
		}
		tag, err := m.DB.Exec(work, `UPDATE accounts a SET credential_sealed=$4,version=version+1
            WHERE id=$1 AND epoch=$2 AND auth_state='authenticated' AND platform='lajiaoliao'
            AND EXISTS(SELECT 1 FROM account_receivers r WHERE r.account_id=a.id AND r.enabled
                AND r.owner=$3 AND r.revision=$5 AND r.lease_until>now())`, w.spec.ID, w.spec.Epoch, m.owner, sealed, w.spec.Revision)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("receiver_configuration_invalid")
		}
		return nil
	})
}

type chiliReceiverTransport interface {
	Sequences(context.Context) (*sdkws.GetMaxSeqResp, error)
	Pull(context.Context, string, int64, int64) ([]*sdkws.MsgData, error)
	Stop()
	Close()
	Done() <-chan struct{}
	Hints() <-chan struct{}
	Pushes() <-chan []lajiaoliao.PushMessage
	Err() error
}

func (m *ReceiverManager) lajiaoConnection(ctx context.Context, w *receiverWorker) error {
	return m.lajiaoConnectionUsing(ctx, w, func(ctx context.Context, session lajiaoliao.Session) (chiliReceiverTransport, error) {
		return lajiaoliao.Connect(ctx, session)
	})
}

func (m *ReceiverManager) lajiaoConnectionUsing(ctx context.Context, w *receiverWorker,
	connect func(context.Context, lajiaoliao.Session) (chiliReceiverTransport, error),
) (result error) {
	session, err := m.loadChili(ctx, w)
	if err != nil {
		return err
	}
	conn, err := connect(ctx, session)
	if err != nil {
		return err
	}
	defer func() {
		conn.Stop()
		defer conn.Close()
		// A socket closing does not erase already accepted nonpersistent
		// events. Revoked/cancelled owners still fail the ordinary DB fence.
		for range 8 {
			if ctx.Err() != nil {
				return
			}
			select {
			case batch := <-conn.Pushes():
				if err := m.commitChiliPush(ctx, w, batch); err != nil {
					result = err
					return
				}
			default:
				return
			}
		}
	}()
	owned := &ownedChiliConnection{transport: conn, ctx: ctx, uid: session.UserID}
	w.mu.Lock()
	w.chiliConnection = owned
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		if w.chiliConnection == owned {
			w.chiliConnection = nil
		}
		w.mu.Unlock()
	}()
	connectedAt := time.Now().UTC()
	w.observe(func(v *ReceiverSnapshot) {
		v.Platform = "lajiaoliao"
		v.ACKMode = "sequence_commit_only"
		v.ConnectedAt = &connectedAt
	})
	// Sequenced pushes wake recovery; zero-sequence pushes have no recovery
	// watermark and are committed independently by this same connection owner.
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	cache := chiliCursorCache{}
	var heads *sdkws.GetMaxSeqResp
	var headsAt time.Time
	batches := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Done():
			return conn.Err()
		default:
		}
		for drained := 0; drained < 2; drained++ {
			select {
			case batch := <-conn.Pushes():
				if err := m.commitChiliPush(ctx, w, batch); err != nil {
					return err
				}
			default:
				drained = 2
			}
		}
		if heads == nil || batches >= 4 || time.Since(headsAt) >= time.Second {
			start := time.Now()
			work, cancel := context.WithTimeout(ctx, 12*time.Second)
			var e error
			heads, e = conn.Sequences(work)
			cancel()
			if e != nil {
				var failure *lajiaoliao.Failure
				if errors.As(e, &failure) && failure.Code == "lajiaoliao_token_expired" {
					if err = m.renewChili(ctx, w, session); err != nil {
						return err
					}
				}
				return e
			}
			headsAt, batches = time.Now(), 0
			heartbeatAt := headsAt.UTC()
			ms := float64(time.Since(start).Microseconds()) / 1000
			w.observe(func(v *ReceiverSnapshot) { v.State = "connected"; v.HeartbeatAt = &heartbeatAt; v.HeartbeatMS = &ms })
		}
		more, err := m.pullChiliCached(ctx, w, conn, heads, &cache)
		if err != nil {
			return err
		}
		syncedAt := time.Now().UTC()
		w.observe(func(v *ReceiverSnapshot) { v.SyncedAt = &syncedAt })
		if more {
			batches++
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-conn.Done():
				return conn.Err()
			case <-time.After(100 * time.Millisecond):
			}
			select {
			case <-conn.Hints():
				heads = nil
			default:
			}
			continue
		}
		heads = nil
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Done():
			return conn.Err()
		case batch := <-conn.Pushes():
			if err := m.commitChiliPush(ctx, w, batch); err != nil {
				return err
			}
		case <-tick.C:
		case <-conn.Hints():
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
}

type chiliCursorCache struct {
	loaded, initialized bool
	values              map[string]int64
}

func (m *ReceiverManager) pullChiliCached(ctx context.Context, w *receiverWorker, conn chiliReceiverTransport, heads *sdkws.GetMaxSeqResp, cache *chiliCursorCache) (bool, error) {
	if heads == nil || len(heads.MaxSeqs) > 10000 {
		return false, &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
	}
	for conversation, seq := range heads.MaxSeqs {
		if len(conversation) == 0 || len(conversation) > 128 || seq < 0 {
			return false, &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
		}
	}
	if !cache.loaded {
		if err := m.DB.QueryRow(ctx, `SELECT stream_initialized FROM lajiaoliao_auth_state WHERE account_id=$1`, w.spec.ID).Scan(&cache.initialized); err != nil {
			return false, err
		}
		cache.loaded = true
		cache.values = map[string]int64{}
	}
	if !cache.initialized {
		tx, err := m.DB.Begin(ctx)
		if err != nil {
			return false, err
		}
		defer tx.Rollback(ctx)
		if err = m.chiliFence(ctx, tx, w); err != nil {
			return false, err
		}
		for conversation, seq := range heads.MaxSeqs {
			if len(conversation) == 0 || len(conversation) > 128 || seq < 0 {
				return false, &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
			}
			if _, err = tx.Exec(ctx, `INSERT INTO lajiaoliao_cursors(account_id,conversation,sequence)VALUES($1,$2,$3)
                ON CONFLICT DO NOTHING`, w.spec.ID, conversation, seq); err != nil {
				return false, err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE lajiaoliao_auth_state SET stream_initialized=true WHERE account_id=$1`, w.spec.ID); err != nil {
			return false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		cache.initialized = true
		return false, nil
	}
	missing := []string{}
	for key := range cache.values {
		if _, ok := heads.MaxSeqs[key]; !ok {
			delete(cache.values, key)
		}
	}
	for key := range heads.MaxSeqs {
		if _, ok := cache.values[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		if err := m.loadChiliCursors(ctx, w, heads, missing, cache.values); err != nil {
			return false, err
		}
	}
	key, previous, end, err := nextChiliRange(heads, cache.values, w.chiliAfter)
	if err != nil || key == "" {
		return false, err
	}
	work, cancel := context.WithTimeout(ctx, 12*time.Second)
	batch, err := conn.Pull(work, key, previous+1, end)
	cancel()
	if err != nil {
		return false, err
	}
	if err = m.commitChili(ctx, w, key, previous, end, batch); err != nil {
		return false, err
	}
	cache.values[key] = end
	w.chiliAfter = key
	return true, nil
}

func (m *ReceiverManager) loadChiliCursors(ctx context.Context, w *receiverWorker, heads *sdkws.GetMaxSeqResp, keys []string, cursor map[string]int64) error {
	initial := make([]int64, len(keys))
	for i, key := range keys {
		first, last := heads.MinSeqs[key], heads.MaxSeqs[key]
		if first < 0 || first > last && first-last > 1 {
			return &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
		}
		initial[i] = max(first-1, 0)
	}
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = m.chiliFence(ctx, tx, w); err != nil {
		return err
	}
	// Only a newly attached stream starts at its earliest retained sequence.
	// An existing durable cursor is never reset to conceal a recovery gap.
	if _, err = tx.Exec(ctx, `INSERT INTO lajiaoliao_cursors(account_id,conversation,sequence)
		SELECT $1,conversation,sequence FROM unnest($2::text[],$3::bigint[]) AS seed(conversation,sequence)
		ON CONFLICT DO NOTHING`, w.spec.ID, keys, initial); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT conversation,sequence FROM lajiaoliao_cursors WHERE account_id=$1 AND conversation=ANY($2::text[])`, w.spec.ID, keys)
	if err != nil {
		return err
	}
	defer rows.Close()
	loaded := map[string]int64{}
	for rows.Next() {
		var key string
		var seq int64
		if err = rows.Scan(&key, &seq); err != nil {
			break
		}
		loaded[key] = seq
	}
	rows.Close()
	if err != nil {
		return err
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(loaded) != len(keys) {
		return errors.New("receiver_cursor_changed")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, key := range keys {
		cursor[key] = loaded[key]
	}
	return nil
}

func nextChiliRange(heads *sdkws.GetMaxSeqResp, cursor map[string]int64, after string) (string, int64, int64, error) {
	if heads == nil || len(heads.MaxSeqs) > 10000 {
		return "", 0, 0, &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
	}
	keys := make([]string, 0, len(heads.MaxSeqs))
	for key, seq := range heads.MaxSeqs {
		if len(key) == 0 || len(key) > 128 || seq < 0 {
			return "", 0, 0, &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
		}
		last := cursor[key]
		if last < 0 || seq < last {
			return "", 0, 0, &lajiaoliao.Failure{Code: "lajiaoliao_sequence_gap"}
		}
		if seq == last {
			continue
		}
		if minSeq := heads.MinSeqs[key]; minSeq > last+1 {
			return "", 0, 0, &lajiaoliao.Failure{Code: "lajiaoliao_sequence_gap"}
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return "", 0, 0, nil
	}
	sort.Strings(keys)
	index := sort.Search(len(keys), func(i int) bool { return keys[i] > after })
	key := keys[index%len(keys)]
	previous := cursor[key]
	return key, previous, previous + min(heads.MaxSeqs[key]-previous, 50), nil
}

var errChiliScopeChanged = errors.New("receiver_scope_changed")

func (m *ReceiverManager) chiliFence(ctx context.Context, tx pgx.Tx, w *receiverWorker) error {
	var id string
	if err := tx.QueryRow(ctx, `SELECT a.id::text FROM accounts a JOIN account_receivers r ON r.account_id=a.id
        WHERE a.id=$1 AND a.epoch=$2 AND a.platform='lajiaoliao' AND a.auth_state='authenticated'
        AND r.enabled AND r.revision=$3 AND r.owner=$4 AND r.lease_until>now() FOR SHARE OF a,r`,
		w.spec.ID, w.spec.Epoch, w.spec.Revision, m.owner).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errChiliScopeChanged
		}
		return err
	}
	return nil
}

func (m *ReceiverManager) commitChili(ctx context.Context, w *receiverWorker, conversation string, previous, end int64, batch []*sdkws.MsgData) error {
	if previous < 0 || end <= previous || end-previous > 50 || len(batch) != int(end-previous) {
		return &lajiaoliao.Failure{Code: "lajiaoliao_sequence_gap"}
	}
	for i, message := range batch {
		if message == nil || message.Seq != previous+int64(i)+1 {
			return &lajiaoliao.Failure{Code: "lajiaoliao_sequence_gap"}
		}
	}
	start := time.Now()
	now := start.UTC()
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = m.chiliFence(ctx, tx, w); err != nil {
		return err
	}
	var current int64
	if err = tx.QueryRow(ctx, `INSERT INTO lajiaoliao_cursors(account_id,conversation,sequence)
        VALUES($1,$2,0) ON CONFLICT(account_id,conversation) DO UPDATE SET sequence=lajiaoliao_cursors.sequence
        RETURNING sequence`, w.spec.ID, conversation).Scan(&current); err != nil {
		return err
	}
	if current != previous {
		return errors.New("receiver_cursor_changed")
	}
	var unique int64
	for _, message := range batch {
		if message == nil || message.Seq <= previous || message.Seq > end {
			return &lajiaoliao.Failure{Code: "lajiaoliao_sequence_gap"}
		}
		sum := sha256.Sum256([]byte(conversation + "\x00" + strconv.FormatInt(message.Seq, 10)))
		legacyKey := "lajiao:" + hex.EncodeToString(sum[:])
		key := legacyKey
		if message.ClientMsgID != "" {
			key = chiliMessageKey(conversation, message.ClientMsgID)
		}
		count, e := m.storeChiliEvent(ctx, tx, w.spec.ID, key, legacyKey, message, now)
		if e != nil {
			return e
		}
		unique += count
	}
	if _, err = tx.Exec(ctx, `UPDATE lajiaoliao_cursors SET sequence=$3,updated_at=now()
        WHERE account_id=$1 AND conversation=$2`, w.spec.ID, conversation, end); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	m.observeChiliCommit(w, start, now, len(batch), unique)
	return nil
}

func (m *ReceiverManager) storeChiliEvent(ctx context.Context, tx pgx.Tx, account, key, legacyKey string, message *sdkws.MsgData, now time.Time) (int64, error) {
	raw, err := proto.Marshal(message)
	if err != nil || len(raw) > 128<<10 || len(message.Content) > 65536 {
		return 0, &lajiaoliao.Failure{Code: "lajiaoliao_protocol_boundary"}
	}
	sealed, err := m.Vault.Seal("native-event:"+account+":"+key, raw)
	clear(raw)
	if err != nil {
		return 0, err
	}
	kind := "notice"
	if message.ClientMsgID != "" {
		if message.SessionType == 1 {
			kind = "private"
		} else if message.SessionType == 2 || message.SessionType == 3 {
			kind = "group"
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		SELECT $1,$2,$3,$4,$5 WHERE NOT EXISTS
			(SELECT 1 FROM native_received_events WHERE account_id=$1 AND event_key=$6)
		ON CONFLICT(account_id,event_key) DO NOTHING`, account, key, kind, now, sealed, legacyKey)
	if err != nil || tag.RowsAffected() != 0 {
		return tag.RowsAffected(), err
	}
	return 0, m.storeChiliSequence(ctx, tx, account, key, message, now)
}

func (m *ReceiverManager) observeChiliCommit(w *receiverWorker, start, now time.Time, count int, unique int64) {
	w.mu.Lock()
	w.snapshot.Frames++
	w.snapshot.Messages += int64(count)
	w.snapshot.Duplicates += int64(count) - unique
	w.snapshot.LastReceived = &now
	w.snapshot.LastCommitted = &now
	sec := time.Now().Unix()
	bucket := &w.rate[sec%60]
	if bucket.second != sec {
		bucket.second = sec
		bucket.count = 0
		bucket.unique = 0
	}
	bucket.count += int64(count)
	bucket.unique += unique
	w.durable.add(time.Now(), float64(time.Since(start).Microseconds())/1000)
	w.mu.Unlock()
	if m.WakeDecoder != nil {
		m.WakeDecoder()
	}
}
