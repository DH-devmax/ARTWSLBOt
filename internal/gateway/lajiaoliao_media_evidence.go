package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

const (
	chiliEvidenceLimit   = 200
	chiliEvidenceBytes   = 16 << 20
	chiliEvidenceTimeout = 5 * time.Second
)

var (
	errChiliEvidenceUnavailable = errors.New("native_text_evidence_unavailable")
	errChiliEvidenceGap         = errors.New("native_text_evidence_gap")
	errChiliEvidenceConflict    = errors.New("native_text_evidence_conflict")
)

func chiliEvidenceDiagnosticCode(code string) bool {
	_, _, ok := lajiaoliao.MediaEvidenceDiagnostic(code)
	return ok
}

// Only fixed stage/reason codes cross the port; raw errors and RPC data do not.
func chiliEvidenceFailure(ctx context.Context, stage string, err error) error {
	if err == nil {
		return nil
	}
	var chili *lajiaoliao.Failure
	if errors.As(err, &chili) && chiliEvidenceDiagnosticCode(chili.Code) {
		return &lajiaoliao.Failure{Code: chili.Code}
	}
	reason := "unavailable"
	var retryAfter time.Duration
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		reason = "timeout"
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		reason = "cancelled"
	case errors.Is(err, ErrTextOwner), errors.Is(err, ErrReadOwner):
		reason = "owner_changed"
	case errors.Is(err, ErrTextRoute):
		reason = "route_changed"
	case errors.Is(err, ErrReadBusy):
		reason = "busy"
	case errors.Is(err, errChiliEvidenceGap):
		reason = "gap"
	case errors.Is(err, errChiliEvidenceConflict):
		reason = "conflict"
	default:
		var native *nativeauth.Error
		if errors.As(err, &native) && native.Code == "native_rate_limited" {
			reason = "rate_limited"
			retryAfter = native.RetryAfter
		} else if errors.As(err, &chili) {
			switch chili.Code {
			case "lajiaoliao_rpc_busy":
				reason = "busy"
			case "lajiaoliao_rate_limited":
				reason = "rate_limited"
				retryAfter = chili.RetryAfter
			case "lajiaoliao_token_expired", "lajiaoliao_reauth_required", "lajiaoliao_kicked":
				reason = "reauth_required"
			case "lajiaoliao_disconnected", "lajiaoliao_result_unknown":
				reason = "disconnected"
			case "lajiaoliao_request_rejected":
				reason = "rejected"
			case "lajiaoliao_protocol_boundary", "lajiaoliao_sequence_limit", "lajiaoliao_invalid_range",
				"lajiaoliao_invalid_input":
				reason = "protocol"
			case "lajiaoliao_sequence_gap":
				reason = "gap"
			case "lajiaoliao_history_empty", "lajiaoliao_history_partial", "lajiaoliao_history_scope",
				"lajiaoliao_history_record", "lajiaoliao_history_sequence":
				stage, reason = chili.Code[len("lajiaoliao_"):], "gap"
			}
		}
	}
	// Preserve the scheduler's existing durable cooldown handling.
	if reason == "rate_limited" && stage != "schedule" {
		return &lajiaoliao.Failure{Code: "lajiaoliao_rate_limited", RetryAfter: retryAfter}
	}
	code := "lajiaoliao_evidence_" + stage + "_" + reason
	if !chiliEvidenceDiagnosticCode(code) {
		code = "lajiaoliao_evidence_input_unavailable"
	}
	return &lajiaoliao.Failure{Code: code}
}

func matchChiliMediaEcho(q lajiaoliao.MediaEvidenceQuery, raw []byte) (lajiaoliao.Receipt, bool) {
	var m sdkws.MsgData
	if len(raw) > 4<<20 || proto.Unmarshal(raw, &m) != nil || m.SendID != q.Sender ||
		m.ClientMsgID != q.ClientID || m.MsgFrom != 100 ||
		m.ContentType != 101 && m.ContentType != 102 && m.ContentType != 103 && m.ContentType != 104 &&
			m.ContentType != 106 && m.ContentType != 114 ||
		!lajiaoliao.ValidID(m.ServerMsgID) || m.SendTime <= 0 {
		return lajiaoliao.Receipt{}, false
	}
	if q.ContentType != 0 && m.ContentType != q.ContentType ||
		q.ContentType == 0 && m.ContentType != 103 && m.ContentType != 104 {
		return lajiaoliao.Receipt{}, false
	}
	if q.Peer == "" && (m.SessionType != 3 || m.GroupID != q.Group || m.RecvID != q.Group) ||
		q.Peer != "" && (m.SessionType != 1 || m.GroupID != "" || m.RecvID != q.Peer) {
		return lajiaoliao.Receipt{}, false
	}
	hash := sha256.Sum256(m.Content)
	if !bytes.Equal(hash[:], q.Digest) {
		return lajiaoliao.Receipt{}, false
	}
	return lajiaoliao.Receipt{ClientID: m.ClientMsgID, ServerID: m.ServerMsgID, Timestamp: m.SendTime}, true
}

type chiliEvidenceBudget struct{ count, bytes int }

func (b *chiliEvidenceBudget) take(size int) bool {
	if size < 0 || b.count >= chiliEvidenceLimit || size > chiliEvidenceBytes-b.bytes {
		return false
	}
	b.count++
	b.bytes += size
	return true
}

func chiliEvidenceEpoch(q lajiaoliao.MediaEvidenceQuery) int64 {
	if q.CurrentEpoch != 0 {
		return q.CurrentEpoch
	}
	return q.Epoch
}

func (m *ReceiverManager) chiliEvidenceAccount(ctx context.Context, q lajiaoliao.MediaEvidenceQuery) error {
	if m.DB == nil {
		return errChiliEvidenceUnavailable
	}
	var current bool
	if err := m.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts
		WHERE id=$1 AND epoch=$2 AND native_uid=$3 AND platform='lajiaoliao'
		AND auth_state='authenticated' AND credential_sealed IS NOT NULL)`,
		q.Account, chiliEvidenceEpoch(q), q.Sender).Scan(&current); err != nil {
		return err
	}
	if !current {
		return ErrTextOwner
	}
	return nil
}

func (m *ReceiverManager) chiliEvidenceConnection(q lajiaoliao.MediaEvidenceQuery) (*receiverWorker, *ownedChiliConnection, error) {
	m.mu.Lock()
	w, owner := m.jobs[q.Account], m.owner
	var revision int64
	if w != nil {
		revision = w.spec.Revision
	}
	m.mu.Unlock()
	if w == nil || owner == "" {
		return nil, nil, ErrTextOwner
	}
	return m.assistantChiliOwner(AssistantMessageIntent{Account: q.Account, Owner: owner, Sender: q.Sender,
		Epoch: chiliEvidenceEpoch(q), Revision: revision})
}

func (m *ReceiverManager) chiliEvidenceFence(ctx context.Context, q lajiaoliao.MediaEvidenceQuery,
	w *receiverWorker, connection *ownedChiliConnection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	currentWorker, current, err := m.chiliEvidenceConnection(q)
	if err != nil || currentWorker != w || current != connection {
		return ErrTextOwner
	}
	var valid bool
	if err := m.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a JOIN account_receivers r ON r.account_id=a.id
		WHERE a.id=$1 AND a.epoch=$2 AND a.native_uid=$3 AND a.platform='lajiaoliao'
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND r.enabled AND r.owner=$4 AND r.revision=$5 AND r.lease_until>clock_timestamp()+interval '2 seconds')`,
		q.Account, chiliEvidenceEpoch(q), q.Sender, m.owner, w.spec.Revision).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrTextOwner
	}
	currentWorker, current, err = m.chiliEvidenceConnection(q)
	if err != nil || currentWorker != w || current != connection {
		return ErrTextOwner
	}
	return nil
}

// Unknown submissions are resolved from read-only evidence, never replayed.
func (m *ReceiverManager) ChiliMediaEvidence(ctx context.Context, q lajiaoliao.MediaEvidenceQuery) (lajiaoliao.MediaEvidence, error) {
	var out lajiaoliao.MediaEvidence
	if q.Account == "" || q.Epoch < 1 || q.CurrentEpoch < 0 || chiliEvidenceEpoch(q) < q.Epoch ||
		!lajiaoliao.ValidID(q.ClientID) || !lajiaoliao.ValidID(q.Sender) ||
		!lajiaoliao.ValidID(q.Group) || q.Peer != "" && !lajiaoliao.ValidID(q.Peer) ||
		len(q.Digest) != 32 || q.SubmittedAt.IsZero() || q.SubmittedAt.After(time.Now()) {
		return out, chiliEvidenceFailure(ctx, "input", ErrTextRoute)
	}
	ctx, cancel := context.WithTimeout(ctx, chiliEvidenceTimeout)
	defer cancel()
	if err := m.chiliEvidenceAccount(ctx, q); err != nil {
		return out, chiliEvidenceFailure(ctx, "account", err)
	}
	rows, err := m.DB.Query(ctx, `SELECT e.id,coalesce(sx.event_key,e.event_key),coalesce(sx.payload_sealed,e.payload_sealed)
		FROM native_received_events e LEFT JOIN native_received_events sx
			ON e.event_key LIKE 'lajiao:message:%' AND sx.account_id=e.account_id AND sx.kind='notice'
			AND sx.event_key=replace(e.event_key,'lajiao:message:','lajiao:sequence:')
			AND sx.received_at>clock_timestamp()-interval '7 days' AND sx.payload_sealed IS NOT NULL
		WHERE e.account_id=$1 AND e.kind=$3 AND e.received_at>=$2 AND e.received_at<=clock_timestamp()
		AND e.received_at>clock_timestamp()-interval '7 days' ORDER BY e.id DESC LIMIT 201`, q.Account, q.SubmittedAt,
		map[bool]string{true: "private", false: "group"}[q.Peer != ""])
	if err != nil {
		return out, chiliEvidenceFailure(ctx, "durable_read", err)
	}
	defer rows.Close()
	budget := chiliEvidenceBudget{}
	for rows.Next() {
		var id int64
		var key string
		var sealed []byte
		if err = rows.Scan(&id, &key, &sealed); err != nil {
			return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "durable_read", err)
		}
		if !budget.take(len(sealed)) {
			out.Truncated = true
			break
		}
		raw, err := m.Vault.Open("native-event:"+q.Account+":"+key, sealed)
		if err != nil {
			return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "durable_open", err)
		}
		receipt, matched := matchChiliMediaEcho(q, raw)
		clear(raw)
		if !matched {
			continue
		}
		if out.Found && out.Receipt != receipt {
			return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "durable_match", errChiliEvidenceConflict)
		}
		out.Found, out.EventID, out.Receipt, out.Source = true, id, receipt, "durable_echo"
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "durable_read", err)
	}
	if out.Truncated {
		return lajiaoliao.MediaEvidence{Truncated: true, Check: &lajiaoliao.EvidenceCheck{
			State: "scan_truncated", DurableRecords: budget.count, SampledAt: time.Now().UTC(),
		}}, nil
	}
	if err := m.chiliEvidenceAccount(ctx, q); err != nil {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "account", err)
	}
	if out.Found {
		out.Check = &lajiaoliao.EvidenceCheck{
			State: "matched", DurableRecords: budget.count, SampledAt: time.Now().UTC(),
		}
		return out, nil
	}
	w, connection, err := m.chiliEvidenceConnection(q)
	if err != nil {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "connection", err)
	}
	reader, ok := connection.transport.(chiliHistoryReader)
	if !ok {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "connection", errChiliEvidenceUnavailable)
	}
	stop := context.AfterFunc(connection.ctx, cancel)
	defer stop()
	value, err := m.readValue(ctx, q.Account, "", ReadRefresh, func(work context.Context) (any, error) {
		work, cancel := context.WithCancel(work)
		stop := context.AfterFunc(connection.ctx, cancel)
		defer func() { stop(); cancel() }()
		return readChiliHistory(work, q, reader, budget, func() error {
			return m.chiliEvidenceFence(work, q, w, connection)
		})
	})
	if err != nil {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "schedule", err)
	}
	history, ok := value.(lajiaoliao.MediaEvidence)
	if !ok {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "schedule", ErrTextRoute)
	}
	if err := m.chiliEvidenceFence(ctx, q, w, connection); err != nil {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "final_fence", err)
	}
	return history, nil
}

func chiliHistoryConversations(q lajiaoliao.MediaEvidenceQuery, heads *sdkws.GetMaxSeqResp) ([]string, error) {
	if heads == nil || len(heads.MaxSeqs) > 10000 || len(heads.MinSeqs) > 10000 {
		return nil, errChiliEvidenceUnavailable
	}
	candidates := []string{"sg_" + q.Group}
	if q.Peer != "" {
		if !lajiaoliao.ValidID(q.Sender) || !lajiaoliao.ValidID(q.Peer) || q.Sender == q.Peer {
			return nil, ErrTextRoute
		}
		// Pinned desktop 1.0.3 package.json uses OpenIM 3.8.3:
		// pkg/utils/utils.go:GetConversationIDByMsg sorts the two string IDs.
		// App-24552bef.js:lJ also forms si_<recvID>_<selfUserID>.
		first, last := q.Sender, q.Peer
		if first > last {
			first, last = last, first
		}
		candidates = []string{"si_" + first + "_" + last}
		ordered := "si_" + q.Peer + "_" + q.Sender
		if ordered != candidates[0] {
			candidates = append(candidates, ordered)
		}
	}
	conversations := []string{}
	for _, id := range candidates {
		if !lajiaoliao.ValidID(id) {
			return nil, ErrTextRoute
		}
		if _, exists := heads.MaxSeqs[id]; exists {
			conversations = append(conversations, id)
		}
	}
	return conversations, nil
}

func chiliHistoryRoute(q lajiaoliao.MediaEvidenceQuery, message *sdkws.MsgData) bool {
	if q.Peer != "" {
		return message.SessionType == 1 && message.GroupID == "" &&
			(message.SendID == q.Sender && message.RecvID == q.Peer ||
				message.SendID == q.Peer && message.RecvID == q.Sender)
	}
	return message.SessionType == 3 && message.GroupID == q.Group && message.RecvID == q.Group
}

// The evidence reader has no send, connect or persistence methods.
type chiliHistoryReader interface {
	Sequences(context.Context) (*sdkws.GetMaxSeqResp, error)
	HistoryWindow(context.Context, string, int64, int64) ([]*sdkws.MsgData, error)
	History(context.Context, string, int64, int64) ([]*sdkws.MsgData, error)
}

type chiliHistoryCandidate struct {
	conversation string
	seq          int64
	receipt      lajiaoliao.Receipt
}

func readChiliHistory(ctx context.Context, q lajiaoliao.MediaEvidenceQuery, reader chiliHistoryReader,
	budget chiliEvidenceBudget, guard func() error) (out lajiaoliao.MediaEvidence, err error) {
	diagnostic := lajiaoliao.EvidenceCheck{DurableRecords: budget.count}
	defer func() {
		if err != nil {
			return
		}
		switch {
		case out.Truncated:
			diagnostic.State = "scan_truncated"
		case out.Found:
			diagnostic.State = "matched"
		case diagnostic.Conversations == 0:
			diagnostic.State = "conversation_not_returned"
		case diagnostic.Returned == 0:
			diagnostic.State = "history_empty"
		default:
			diagnostic.State = "message_not_found"
		}
		diagnostic.SampledAt = time.Now().UTC()
		out.Check = &diagnostic
	}()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if guard == nil {
			return ErrTextOwner
		}
		return guard()
	}
	if err := check(); err != nil {
		return out, chiliEvidenceFailure(ctx, "history_guard", err)
	}
	heads, err := reader.Sequences(ctx)
	if err != nil {
		return out, chiliEvidenceFailure(ctx, "sequences", err)
	}
	if err := check(); err != nil {
		return out, chiliEvidenceFailure(ctx, "history_guard", err)
	}
	conversations, err := chiliHistoryConversations(q, heads)
	if err != nil {
		return out, chiliEvidenceFailure(ctx, "conversation", err)
	}
	if size := proto.Size(heads); size > chiliEvidenceBytes-budget.bytes {
		return lajiaoliao.MediaEvidence{Truncated: true}, nil
	} else {
		budget.bytes += size
	}
	diagnostic.SequenceHeads, diagnostic.Conversations = len(heads.MaxSeqs), len(conversations)
	requested := budget.count
	sparse := false
	candidates := []chiliHistoryCandidate{}
	for _, conversation := range conversations {
		first, last := max(int64(1), heads.MinSeqs[conversation]), heads.MaxSeqs[conversation]
		if last < 0 || heads.MinSeqs[conversation] < 0 || last > 0 && first > last {
			return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "conversation", errChiliEvidenceUnavailable)
		}
		for last >= first {
			remaining := min(chiliEvidenceLimit-budget.count, chiliEvidenceLimit-requested)
			if remaining <= 0 {
				return lajiaoliao.MediaEvidence{Truncated: true}, nil
			}
			begin := max(first, last-int64(min(50, remaining))+1)
			if err := check(); err != nil {
				return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "history_guard", err)
			}
			requested += int(last - begin + 1)
			diagnostic.Requested += int(last - begin + 1)
			batch, err := reader.HistoryWindow(ctx, conversation, begin, last)
			if err != nil {
				return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "pull", err)
			}
			diagnostic.Returned += len(batch)
			sparse = sparse || len(batch) != int(last-begin+1)
			withinBudget, seq, err := inspectChiliHistoryWindow(q, begin, last, batch, &budget, &out)
			if err != nil || !withinBudget {
				return lajiaoliao.MediaEvidence{Truncated: !withinBudget && err == nil}, chiliEvidenceFailure(ctx, "history_validate", err)
			}
			if seq != 0 {
				// At most one immutable message per canonical conversation.
				for _, candidate := range candidates {
					if candidate.conversation == conversation {
						return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "history_validate", errChiliEvidenceConflict)
					}
				}
				candidates = append(candidates, chiliHistoryCandidate{conversation, seq, out.Receipt})
			}
			if err := check(); err != nil {
				return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "history_guard", err)
			}
			last = begin - 1
		}
	}
	if sparse && out.Found {
		// Missing neighboring messages are not evidence. Re-read each exact
		// candidate separately, inside the original request and byte budgets.
		for _, candidate := range candidates {
			if requested >= chiliEvidenceLimit || budget.count >= chiliEvidenceLimit {
				return lajiaoliao.MediaEvidence{Truncated: true}, nil
			}
			if err := check(); err != nil {
				return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "history_guard", err)
			}
			requested++
			diagnostic.Requested++
			batch, err := reader.History(ctx, candidate.conversation, candidate.seq, candidate.seq)
			if err != nil {
				return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "pull", err)
			}
			diagnostic.Returned += len(batch)
			var verified lajiaoliao.MediaEvidence
			withinBudget, err := inspectChiliHistoryBatch(q, candidate.seq, candidate.seq, batch, &budget, &verified)
			if err != nil || !withinBudget {
				return lajiaoliao.MediaEvidence{Truncated: !withinBudget && err == nil}, chiliEvidenceFailure(ctx, "history_validate", err)
			}
			if !verified.Found || verified.Receipt != candidate.receipt {
				return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "history_validate", errChiliEvidenceConflict)
			}
			if err := check(); err != nil {
				return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "history_guard", err)
			}
		}
	} else if sparse {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "pull",
			&lajiaoliao.Failure{Code: "lajiaoliao_history_partial"})
	}
	if err := check(); err != nil {
		return lajiaoliao.MediaEvidence{}, chiliEvidenceFailure(ctx, "history_guard", err)
	}
	return out, nil
}

func inspectChiliHistoryBatch(q lajiaoliao.MediaEvidenceQuery, begin, end int64, batch []*sdkws.MsgData,
	budget *chiliEvidenceBudget, out *lajiaoliao.MediaEvidence) (bool, error) {
	if len(batch) != int(end-begin+1) {
		clearChiliHistoryBatch(batch)
		return false, errChiliEvidenceGap
	}
	for i, message := range batch {
		if message == nil || message.Seq != begin+int64(i) {
			clearChiliHistoryBatch(batch)
			return false, errChiliEvidenceGap
		}
	}
	complete, _, err := inspectChiliHistoryWindow(q, begin, end, batch, budget, out)
	return complete, err
}

func clearChiliHistoryBatch(batch []*sdkws.MsgData) {
	for _, message := range batch {
		if message != nil {
			clear(message.Content)
		}
	}
}

func inspectChiliHistoryWindow(q lajiaoliao.MediaEvidenceQuery, begin, end int64, batch []*sdkws.MsgData,
	budget *chiliEvidenceBudget, out *lajiaoliao.MediaEvidence) (bool, int64, error) {
	defer func() {
		clearChiliHistoryBatch(batch)
	}()
	if len(batch) > int(end-begin+1) {
		return false, 0, errChiliEvidenceGap
	}
	var matchedSeq int64
	for i, message := range batch {
		if message == nil || message.Seq < begin || message.Seq > end ||
			i > 0 && message.Seq <= batch[i-1].Seq || len(message.Content) > 65536 ||
			message.ClientMsgID != "" && !lajiaoliao.ValidID(message.ClientMsgID) {
			return false, 0, errChiliEvidenceGap
		}
		size := proto.Size(message)
		if size > 128<<10 || !budget.take(size) {
			return false, 0, nil
		}
		// OpenIM's history checker consumes empty-client sequence placeholders,
		// but they cannot supply routing, content or positive send evidence.
		if message.ClientMsgID == "" {
			continue
		}
		if message.MsgFrom == 100 && !chiliHistoryRoute(q, message) {
			return false, 0, ErrTextRoute
		}
		raw, err := proto.Marshal(message)
		if err != nil {
			return false, 0, errChiliEvidenceUnavailable
		}
		receipt, matched := matchChiliMediaEcho(q, raw)
		clear(raw)
		if matched && (message.SendTime < q.SubmittedAt.UnixMilli() || message.SendTime > time.Now().UnixMilli()) {
			matched = false
		}
		if message.ClientMsgID == q.ClientID && !matched ||
			matched && (matchedSeq != 0 || out.Found && out.Receipt != receipt) {
			return false, 0, errChiliEvidenceConflict
		}
		if matched {
			out.Found, out.Receipt, out.Source = true, receipt, "server_history"
			matchedSeq = message.Seq
		}
	}
	return true, matchedSeq, nil
}
