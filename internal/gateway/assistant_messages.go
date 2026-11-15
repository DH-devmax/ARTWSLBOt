package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type ownedChiliConnection struct {
	transport chiliReceiverTransport
	ctx       context.Context
	uid       string
}

type MessageCandidate struct {
	Reference nativeauth.MessageReference
	Preview   string
}

// MessageCandidates reads at most 100 recent sealed inputs, capped at 2 MiB.
// Authorization and the human target choice belong to the caller in control.
func (m *ReceiverManager) MessageCandidates(ctx context.Context, platform, account, group, search string) ([]MessageCandidate, error) {
	var conversation string
	if platform == "lajiaoliao" {
		conversation = conversationKey("lajiaoliao:group", 0, group)
	} else if platform == "wsl" {
		snapshot, err := m.Moderation(ctx, account, group)
		if err != nil || snapshot.NativeTeam == "" {
			return nil, ErrTextRoute
		}
		id, err := strconv.ParseUint(group, 10, 30)
		if err != nil {
			return nil, ErrTextRoute
		}
		conversation = conversationKey("group", id, snapshot.NativeTeam)
	} else {
		return nil, ErrTextRoute
	}
	rows, err := m.DB.Query(ctx, `SELECT d.event_id,d.payload_sealed,
		coalesce(sx.payload_sealed,e.payload_sealed),coalesce(sx.event_key,e.event_key)
		FROM native_decoded_events d JOIN native_received_events e ON e.id=d.event_id AND e.account_id=d.account_id
		JOIN accounts a ON a.id=d.account_id
		JOIN native_groups g ON g.platform=a.platform AND g.native_id=$4
		JOIN group_bindings b ON b.group_id=g.id AND b.primary_account_id=a.id AND b.active
		LEFT JOIN native_received_events sx ON a.platform='lajiaoliao' AND e.event_key LIKE 'lajiao:message:%'
			AND sx.account_id=e.account_id AND sx.kind='notice'
			AND sx.event_key=replace(e.event_key,'lajiao:message:','lajiao:sequence:')
			AND sx.received_at>clock_timestamp()-interval '7 days' AND sx.payload_sealed IS NOT NULL
		WHERE d.account_id=$1 AND a.platform=$2 AND d.scene='group' AND d.state='decoded' AND d.conversation_key=$3
		AND d.received_at>clock_timestamp()-interval '2 minutes' AND e.received_at>clock_timestamp()-interval '2 minutes'
		AND d.received_at>=b.data_since AND e.received_at>=b.data_since
		AND d.payload_sealed IS NOT NULL AND e.payload_sealed IS NOT NULL
		ORDER BY d.event_id DESC LIMIT 100`, account, platform, conversation, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, total := []MessageCandidate{}, 0
	for rows.Next() {
		var event int64
		var projection, source []byte
		var key string
		if err = rows.Scan(&event, &projection, &source, &key); err != nil {
			return nil, err
		}
		total += len(projection) + len(source)
		if total > 2<<20 {
			break
		}
		candidate, err := m.readMessageCandidate(platform, account, group, key, event, projection, source)
		if err != nil || !strings.Contains(strings.ToLower(candidate.Preview), strings.ToLower(search)) {
			continue
		}
		out = append(out, candidate)
		if len(out) == 20 {
			break
		}
	}
	return out, rows.Err()
}

func (m *ReceiverManager) readMessageCandidate(platform, account, group, key string, event int64, projection, source []byte) (MessageCandidate, error) {
	var out MessageCandidate
	if len(source) > 256<<10 || len(projection) > 256<<10 {
		return out, ErrTextRoute
	}
	p, err := m.Vault.Open(DecodedAAD(account, event), projection)
	if err != nil {
		return out, ErrTextRoute
	}
	var decoded DecodedMessage
	err = json.Unmarshal(p, &decoded)
	clear(p)
	if err != nil || decoded.Message.Session != 2 || decoded.Message.CreatedAt > 1<<63-1 {
		return out, ErrTextRoute
	}
	raw, err := m.Vault.Open("native-event:"+account+":"+key, source)
	if err != nil {
		return out, ErrTextRoute
	}
	defer clear(raw)
	hash := sha256.Sum256(raw)
	ref := nativeauth.MessageReference{Platform: platform, EventID: event, Group: group,
		SourceDigest: hex.EncodeToString(hash[:]), CreatedAt: int64(decoded.Message.CreatedAt)}
	if platform == "wsl" {
		if strconv.FormatUint(decoded.Message.Target.ID, 10) != group {
			return out, ErrTextRoute
		}
		var message nativewire.IMMessage
		if json.Unmarshal(raw, &message) != nil || message.Scene != 1 ||
			message.Fields[11] != decoded.Message.ClientID || message.ServerID != decoded.ServerID ||
			message.Target != decoded.TransportTo || message.Sender != decoded.TransportPeer {
			return out, ErrTextRoute
		}
		ref.Sender = strconv.FormatUint(decoded.Message.Sender.ID, 10)
		ref.Route = nativewire.ReplyRoute{Target: message.Target, Sender: message.Sender,
			ClientID: message.Fields[11], ServerID: message.ServerID, TimeMS: message.Fields[7]}
	} else if platform == "lajiaoliao" {
		var message sdkws.MsgData
		if proto.Unmarshal(raw, &message) != nil || message.GroupID != group || decoded.NativeTarget != group ||
			message.ClientMsgID != decoded.Message.ClientID || message.SendID != decoded.NativeSender ||
			message.Seq < 1 || message.SessionType != 3 || message.SendTime != ref.CreatedAt {
			return out, ErrTextRoute
		}
		ref.Sender, ref.Sequence, ref.Conversation = message.SendID, message.Seq, "sg_"+group
	} else {
		return out, ErrTextRoute
	}
	text, _ := json.Marshal(ref)
	if !nativeauth.ValidMessageReference(string(text), platform, group) || !ref.Fresh(time.Now()) {
		return out, ErrTextRoute
	}
	preview := []rune(decoded.Message.Text)
	if len(preview) > 160 {
		preview = preview[:160]
	}
	out.Reference, out.Preview = ref, string(preview)
	return out, nil
}

func (m *ReceiverManager) verifyRecallSource(ctx context.Context, account string, expected nativeauth.MessageReference) error {
	var projection, source []byte
	var key string
	err := m.DB.QueryRow(ctx, `SELECT d.payload_sealed,
		coalesce(sx.payload_sealed,e.payload_sealed),coalesce(sx.event_key,e.event_key)
		FROM native_decoded_events d JOIN native_received_events e ON e.id=d.event_id AND e.account_id=d.account_id
		JOIN accounts a ON a.id=d.account_id
		LEFT JOIN native_received_events sx ON a.platform='lajiaoliao' AND e.event_key LIKE 'lajiao:message:%'
			AND sx.account_id=e.account_id AND sx.kind='notice'
			AND sx.event_key=replace(e.event_key,'lajiao:message:','lajiao:sequence:')
			AND sx.received_at>clock_timestamp()-interval '7 days' AND sx.payload_sealed IS NOT NULL
		WHERE d.account_id=$1 AND d.event_id=$2
		AND a.platform=$3 AND d.scene='group' AND d.state='decoded'
		AND e.received_at>clock_timestamp()-interval '2 minutes'
		AND d.received_at>clock_timestamp()-interval '2 minutes'`, account, expected.EventID, expected.Platform).
		Scan(&projection, &source, &key)
	if err != nil {
		return ErrTextRoute
	}
	got, err := m.readMessageCandidate(expected.Platform, account, expected.Group, key, expected.EventID, projection, source)
	if err != nil || got.Reference != expected {
		return ErrTextRoute
	}
	return nil
}

type AssistantMessageIntent struct {
	Account, Owner, Sender, Group, ClientID, Text string
	Epoch, Revision                               int64
	Reference                                     *nativeauth.MessageReference
}

func (m *ReceiverManager) assistantChiliOwner(i AssistantMessageIntent) (*receiverWorker, *ownedChiliConnection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.jobs[i.Account]
	if w == nil || m.owner != i.Owner || w.spec.Platform != "lajiaoliao" ||
		w.spec.Epoch != i.Epoch || w.spec.Revision != i.Revision || w.spec.UID != i.Sender {
		return nil, nil, ErrTextOwner
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	c, now := w.chiliConnection, time.Now()
	if c == nil || c.ctx.Err() != nil || c.uid != i.Sender || w.snapshot.State != "connected" ||
		w.snapshot.HeartbeatAt == nil || w.snapshot.HeartbeatAt.After(now) ||
		now.Sub(*w.snapshot.HeartbeatAt) > 37*time.Second || !w.leaseUntil.After(now.Add(2*time.Second)) {
		return nil, nil, ErrTextOwner
	}
	select {
	case <-c.transport.Done():
		return nil, nil, ErrTextOwner
	default:
		return w, c, nil
	}
}

func (m *ReceiverManager) SubmitAssistantChiliMessage(ctx context.Context, i AssistantMessageIntent, fence nativewire.SubmissionFence) error {
	_, err := m.submitChiliMessage(ctx, i, fence)
	return err
}

// SubmitChiliGroupText shares only the owned transport adapter. Its caller must
// provide its own production admission and durable submission fence.
func (m *ReceiverManager) SubmitChiliGroupText(ctx context.Context, i AssistantMessageIntent, fence nativewire.SubmissionFence) (lajiaoliao.Receipt, error) {
	if i.Reference != nil {
		return lajiaoliao.Receipt{}, ErrTextRoute
	}
	return m.submitChiliMessage(ctx, i, fence)
}

func (m *ReceiverManager) submitChiliMessage(ctx context.Context, i AssistantMessageIntent, fence nativewire.SubmissionFence) (lajiaoliao.Receipt, error) {
	var receipt lajiaoliao.Receipt
	if fence == nil || !lajiaoliao.ValidID(i.Group) || !lajiaoliao.ValidID(i.ClientID) {
		return receipt, ErrTextRoute
	}
	w, connection, err := m.assistantChiliOwner(i)
	if err != nil {
		return receipt, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(connection.ctx, cancel)
	defer func() { stop(); cancel() }()
	// The refresh lane is released before the submission fence or reply wait.
	value, err := m.readValue(ctx, i.Account, "assistant-message:"+i.ClientID, ReadRefresh, func(work context.Context) (any, error) {
		session, err := m.loadChili(work, w)
		if err != nil || session.UserID != i.Sender {
			return nil, ErrTextOwner
		}
		client := lajiaoliao.NewClient()
		defer client.Close()
		info, err := client.GroupInfo(work, session, i.Group)
		if err != nil {
			return nil, err
		}
		self, err := client.Member(work, session, i.Group, i.Sender)
		if err != nil || info.Status == nil ||
			i.Reference == nil && (*info.Status != 0 || self.MuteEnd > time.Now().Unix()) ||
			self.Role != 20 && self.Role != 60 && self.Role != 100 {
			return nil, ErrTextRoute
		}
		if i.Reference != nil {
			if self.Role != 60 && self.Role != 100 {
				return nil, ErrTextRoute
			}
			if err := m.verifyRecallSource(work, i.Account, *i.Reference); err != nil {
				return nil, err
			}
		}
		return session, nil
	})
	if err != nil {
		return receipt, err
	}
	session, ok := value.(lajiaoliao.Session)
	if !ok {
		return receipt, ErrTextOwner
	}
	at := time.Now()
	guard := func(work context.Context, write func(context.Context) error) error {
		return fence(work, func(final context.Context) error {
			_, current, err := m.assistantChiliOwner(i)
			if err != nil || current != connection || time.Since(at) > 5*time.Second ||
				i.Reference != nil && !i.Reference.Fresh(time.Now()) {
				return ErrTextOwner
			}
			return write(final)
		})
	}
	if i.Reference != nil {
		client := lajiaoliao.NewClient()
		defer client.Close()
		return receipt, client.Recall(ctx, session, *i.Reference, guard)
	}
	sender, ok := connection.transport.(interface {
		SendText(context.Context, string, string, string, func(context.Context, func(context.Context) error) error) (lajiaoliao.Receipt, error)
	})
	if !ok {
		return receipt, ErrTextOwner
	}
	return sender.SendText(ctx, i.Group, i.ClientID, i.Text, guard)
}

func (m *ReceiverManager) RecallAssistantWSL(ctx context.Context, i AssistantMessageIntent, fence nativewire.SubmissionFence) error {
	if i.Reference == nil || i.Reference.Platform != "wsl" || i.Reference.Group != i.Group || fence == nil {
		return ErrTextRoute
	}
	uid, err := strconv.ParseUint(i.Sender, 10, 30)
	if err != nil {
		return ErrTextRoute
	}
	intent := TextIntent{AccountID: i.Account, ReceiverOwner: i.Owner, AccountEpoch: i.Epoch,
		Revision: i.Revision, Message: nativewire.GroupText{SenderID: uid}}
	_, connection, err := m.textOwner(intent)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(connection.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err = m.verifyRecallSource(ctx, i.Account, *i.Reference); err != nil {
		return err
	}
	// The ordinary moderation snapshot/final fence supplies fresh role proof.
	// Independently bind the retained native transport group to this account.
	snapshot, err := m.Moderation(ctx, i.Account, i.Group)
	if err != nil || snapshot.SelfRole != "admin" && snapshot.SelfRole != "owner" ||
		snapshot.NativeTeam != i.Reference.Route.Target {
		return ErrTextRoute
	}
	var senderFound bool
	for _, member := range snapshot.Members {
		if member.ID == i.Reference.Sender && member.Transport == i.Reference.Route.Sender {
			senderFound = true
		}
	}
	if !senderFound {
		return ErrTextRoute
	}
	body, err := nativewire.GroupRecallRequest(i.Reference.Route.Target, i.Reference.Route)
	if err != nil {
		return err
	}
	defer clear(body)
	wait := make(chan nativewire.Frame, 1)
	var key [3]uint16
	called := false
	err = fence(ctx, func(final context.Context) error {
		if called {
			return errors.New("native_submission_repeated")
		}
		called = true
		_, current, err := m.textOwner(intent)
		if err != nil || current != connection || !i.Reference.Fresh(time.Now()) {
			return ErrTextOwner
		}
		key, _, err = connection.transport.writeTracked(final, 7, 13, body, wait)
		return err
	})
	defer func() {
		connection.transport.mu.Lock()
		delete(connection.transport.pending, key)
		connection.transport.mu.Unlock()
	}()
	if err != nil {
		return err
	}
	if !called {
		return ErrTextRoute
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case response := <-wait:
		if response.Code != 200 {
			return &nativeauth.Error{Code: "native_business_rejected", Submitted: true}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.transport.done:
		return ErrTextOwner
	case <-timer.C:
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	}
}
