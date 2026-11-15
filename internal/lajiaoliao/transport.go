// SPDX-License-Identifier: AGPL-3.0-or-later
package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type wireRequest struct {
	ReqIdentifier                       int
	Token, SendID, OperationID, MsgIncr string
	Data                                []byte
}
type wireResponse struct {
	ReqIdentifier, ErrCode       int
	ErrMsg, MsgIncr, OperationID string
	Data                         []byte
}

// A single reader owns inbound frames. Sequenced pushes coalesce into recovery
// hints; nonpersistent pushes and RPC replies retain separate bounded queues.
type Transport struct {
	conn       *websocket.Conn
	session    Session
	write      sync.Mutex
	mu         sync.Mutex
	pending    map[string]chan wireResponse
	hints      chan struct{}
	pushes     chan []PushMessage
	done       chan struct{}
	readerDone chan struct{}
	once       sync.Once
	err        error
	verified   atomic.Bool
}

func Connect(ctx context.Context, s Session) (*Transport, error) {
	id, err := NewDeviceID()
	if err != nil || !validID(s.UserID) || s.IMToken == "" {
		return nil, &Failure{Code: "lajiaoliao_session_invalid"}
	}
	u := url.URL{Scheme: "wss", Host: "ws.lajiaoliao.com"}
	u.RawQuery = url.Values{"sendID": {s.UserID}, "token": {s.IMToken}, "platformID": {"5"},
		"operationID": {id}, "isBackground": {"false"}}.Encode()
	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second, NetDialContext: dialPublic, EnableCompression: false}
	conn, resp, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		code := "lajiaoliao_connection_failed"
		if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 403) {
			code = "lajiaoliao_reauth_required"
		}
		return nil, &Failure{Code: code}
	}
	t := newTransport(conn, s)
	go func() {
		select {
		case <-ctx.Done():
			t.Close()
		case <-t.done:
		}
	}()
	return t, nil
}

func newTransport(conn *websocket.Conn, s Session) *Transport {
	t := &Transport{conn: conn, session: s, pending: map[string]chan wireResponse{}, hints: make(chan struct{}, 1),
		pushes: make(chan []PushMessage, 8), done: make(chan struct{}), readerDone: make(chan struct{})}
	conn.SetReadLimit(2 << 20)
	go t.read()
	return t
}

func (t *Transport) shutdown(err error) {
	t.once.Do(func() {
		t.mu.Lock()
		t.err = err
		t.mu.Unlock()
		close(t.done)
		t.conn.Close()
	})
}
func (t *Transport) Stop() {
	t.shutdown(&Failure{Code: "lajiaoliao_disconnected"})
	<-t.readerDone
}
func (t *Transport) Close() {
	t.Stop()
	for {
		select {
		case batch := <-t.pushes:
			clearPushMessages(batch)
		default:
			return
		}
	}
}
func (t *Transport) Hints() <-chan struct{}       { return t.hints }
func (t *Transport) Pushes() <-chan []PushMessage { return t.pushes }
func (t *Transport) Done() <-chan struct{}        { return t.done }
func (t *Transport) Err() error                   { t.mu.Lock(); defer t.mu.Unlock(); return t.err }

func (t *Transport) read() {
	defer close(t.readerDone)
	for {
		kind, raw, err := t.conn.ReadMessage()
		if err != nil {
			t.shutdown(&Failure{Code: "lajiaoliao_disconnected"})
			return
		}
		var result wireResponse
		if kind != websocket.BinaryMessage || len(raw) > 2<<20 || gob.NewDecoder(bytes.NewReader(raw)).Decode(&result) != nil || len(result.Data) > 2<<20 {
			t.shutdown(&Failure{Code: "lajiaoliao_protocol_boundary"})
			return
		}
		if result.ReqIdentifier == 2002 {
			t.shutdown(&Failure{Code: "lajiaoliao_kicked"})
			return
		}
		if result.ReqIdentifier == 2001 {
			pushes, err := readPushMessages(result.Data)
			clear(result.Data)
			if result.ErrCode != 0 || err != nil {
				t.shutdown(&Failure{Code: "lajiaoliao_protocol_boundary"})
				return
			}
			if len(pushes) > 0 {
				select {
				case t.pushes <- pushes:
				default:
					clearPushMessages(pushes)
					t.shutdown(&Failure{Code: "lajiaoliao_receive_queue_full"})
					return
				}
			}
			select {
			case t.hints <- struct{}{}:
			default:
			}
			continue
		}
		t.mu.Lock()
		slot := t.pending[result.MsgIncr]
		t.mu.Unlock()
		if slot != nil {
			select {
			case slot <- result:
			default:
			}
		} else {
			select {
			case t.hints <- struct{}{}:
			default:
			}
		}
	}
}

func (t *Transport) rpc(ctx context.Context, code int, input proto.Message, output func([]byte) error,
	fence func(context.Context, func(context.Context) error) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := proto.Marshal(input)
	if err != nil {
		return &Failure{Code: "lajiaoliao_invalid_input"}
	}
	return t.rpcData(ctx, code, data, output, fence)
}

func (t *Transport) rpcData(ctx context.Context, code int, data []byte, output func([]byte) error,
	fence func(context.Context, func(context.Context) error) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) > 65536 {
		return &Failure{Code: "lajiaoliao_invalid_input"}
	}
	id, err := NewDeviceID()
	if err != nil {
		return err
	}
	slot := make(chan wireResponse, 1)
	t.mu.Lock()
	if len(t.pending) >= 2 {
		t.mu.Unlock()
		return &Failure{Code: "lajiaoliao_rpc_busy"}
	}
	t.pending[id] = slot
	t.mu.Unlock()
	defer func() { t.mu.Lock(); delete(t.pending, id); t.mu.Unlock() }()
	var buf bytes.Buffer
	if err = gob.NewEncoder(&buf).Encode(wireRequest{ReqIdentifier: code, SendID: t.session.UserID, OperationID: id, MsgIncr: id, Data: data}); err != nil {
		return err
	}
	var attempted atomic.Bool
	write := func(work context.Context) error {
		if err := work.Err(); err != nil {
			return err
		}
		select {
		case <-t.done:
			return t.Err()
		default:
		}
		deadline := time.Now().Add(10 * time.Second)
		if at, ok := work.Deadline(); ok && at.Before(deadline) {
			deadline = at
		}
		t.write.Lock()
		defer t.write.Unlock()
		if err := work.Err(); err != nil {
			return err
		}
		t.conn.SetWriteDeadline(deadline)
		attempted.Store(true)
		if err := t.conn.WriteMessage(websocket.BinaryMessage, buf.Bytes()); err != nil {
			t.shutdown(&Failure{Code: "lajiaoliao_disconnected"})
			return &Failure{Code: "lajiaoliao_result_unknown", Submitted: code == 1003}
		}
		return nil
	}
	if fence != nil {
		err = fence(ctx, write)
	} else {
		err = write(ctx)
	}
	if err != nil {
		if code == 1003 {
			if !attempted.Load() {
				return &Failure{Code: "lajiaoliao_not_submitted", cause: err}
			}
			return &Failure{Code: "lajiaoliao_result_unknown", Submitted: true, cause: err}
		}
		return err
	}
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	select {
	case result := <-slot:
		if result.ReqIdentifier != code {
			return &Failure{Code: "lajiaoliao_protocol_boundary", Submitted: code == 1003}
		}
		if result.ErrCode != 0 {
			reason := "lajiaoliao_request_rejected"
			if result.ErrCode == 1501 {
				reason = "lajiaoliao_token_expired"
			}
			if result.ErrCode >= 1502 && result.ErrCode <= 1507 {
				reason = "lajiaoliao_reauth_required"
			}
			if result.ErrCode == 1506 {
				reason = "lajiaoliao_kicked"
			}
			return &Failure{Code: reason, Submitted: code == 1003}
		}
		if output(result.Data) != nil {
			return &Failure{Code: "lajiaoliao_protocol_boundary", Submitted: code == 1003}
		}
		return nil
	case <-ctx.Done():
		return &Failure{Code: "lajiaoliao_result_unknown", Submitted: code == 1003}
	case <-timer.C:
		return &Failure{Code: "lajiaoliao_result_unknown", Submitted: code == 1003}
	case <-t.done:
		if code != 1003 {
			return t.Err()
		}
		return &Failure{Code: "lajiaoliao_result_unknown", Submitted: code == 1003}
	}
}

func (t *Transport) Sequences(ctx context.Context) (*sdkws.GetMaxSeqResp, error) {
	result := &sdkws.GetMaxSeqResp{}
	err := t.rpc(ctx, 1001, &sdkws.GetMaxSeqReq{UserID: t.session.UserID}, func(b []byte) error { return proto.Unmarshal(b, result) }, nil)
	if err == nil && (len(result.MaxSeqs) > 10000 || len(result.MinSeqs) > 10000) {
		err = &Failure{Code: "lajiaoliao_sequence_limit"}
	}
	for id, seq := range result.MaxSeqs {
		if !validID(id) || seq < 0 {
			err = &Failure{Code: "lajiaoliao_protocol_boundary"}
			break
		}
	}
	for id, seq := range result.MinSeqs {
		if !validID(id) || seq < 0 {
			err = &Failure{Code: "lajiaoliao_protocol_boundary"}
			break
		}
	}
	if err == nil {
		t.verified.Store(true)
	}
	return result, err
}

func (t *Transport) Pull(ctx context.Context, conversation string, begin, end int64) ([]*sdkws.MsgData, error) {
	if !validID(conversation) || begin < 1 || end < begin || end-begin >= 50 {
		return nil, &Failure{Code: "lajiaoliao_invalid_range"}
	}
	result := &sdkws.PullMessageBySeqsResp{}
	if err := t.rpc(ctx, 1002, &sdkws.PullMessageBySeqsReq{UserID: t.session.UserID,
		SeqRanges: []*sdkws.SeqRange{{ConversationID: conversation, Begin: begin, End: end, Num: 50}}}, func(b []byte) error { return proto.Unmarshal(b, result) }, nil); err != nil {
		return nil, err
	}
	messages := result.Msgs[conversation]
	if messages == nil {
		messages = result.NotificationMsgs[conversation]
	}
	if messages == nil || len(messages.Msgs) != int(end-begin+1) {
		return nil, &Failure{Code: "lajiaoliao_sequence_gap"}
	}
	for _, m := range messages.Msgs {
		if m == nil {
			return nil, &Failure{Code: "lajiaoliao_sequence_gap"}
		}
	}
	sort.Slice(messages.Msgs, func(i, j int) bool { return messages.Msgs[i].Seq < messages.Msgs[j].Seq })
	for i, m := range messages.Msgs {
		if m.Seq != begin+int64(i) || len(m.Content) > 65536 ||
			proto.Size(m) > 128<<10 || m.ClientMsgID != "" && !validID(m.ClientMsgID) {
			return nil, &Failure{Code: "lajiaoliao_sequence_gap"}
		}
	}
	return messages.Msgs, nil
}

type Receipt struct {
	ClientID, ServerID string
	Timestamp          int64
}

// Pinned msg.SendMsgResp fields 1/2/3 only. Importing the upstream msg package
// would also link its unrelated gRPC server stubs into this lightweight client.
func decodeReceipt(data []byte) (Receipt, error) {
	r := Receipt{}
	seen := map[protowire.Number]bool{}
	if len(data) > 65536 {
		return r, errors.New("receipt_limit")
	}
	for len(data) > 0 {
		n, kind, count := protowire.ConsumeTag(data)
		if count < 0 || n < 1 {
			return r, errors.New("receipt_shape")
		}
		data = data[count:]
		if n <= 3 {
			if seen[n] {
				return r, errors.New("receipt_shape")
			}
			seen[n] = true
			if n == 3 {
				if kind != protowire.VarintType {
					return r, errors.New("receipt_shape")
				}
				v, c := protowire.ConsumeVarint(data)
				if c < 0 || v > 1<<63-1 {
					return r, errors.New("receipt_shape")
				}
				r.Timestamp = int64(v)
				data = data[c:]
				continue
			}
			if kind != protowire.BytesType {
				return r, errors.New("receipt_shape")
			}
			value, c := protowire.ConsumeString(data)
			if c < 0 || !validID(value) {
				return r, errors.New("receipt_shape")
			}
			if n == 1 {
				r.ServerID = value
			} else {
				r.ClientID = value
			}
			data = data[c:]
		} else {
			c := protowire.ConsumeFieldValue(n, kind, data)
			if c < 0 {
				return r, errors.New("receipt_shape")
			}
			data = data[c:]
		}
	}
	if r.ClientID == "" || r.ServerID == "" || r.Timestamp <= 0 {
		return r, errors.New("receipt_shape")
	}
	return r, nil
}
func (t *Transport) SendText(ctx context.Context, group, clientID, text string,
	fence func(context.Context, func(context.Context) error) error) (Receipt, error) {
	if !t.verified.Load() {
		return Receipt{}, &Failure{Code: "lajiaoliao_connection_unverified"}
	}
	if !validID(group) || !validID(clientID) || text == "" || len(text) > 16384 || !utf8.ValidString(text) || fence == nil {
		return Receipt{}, &Failure{Code: "lajiaoliao_invalid_input"}
	}
	content, _ := json.Marshal(map[string]string{"content": text})
	var result Receipt
	err := t.rpc(ctx, 1003, &sdkws.MsgData{SendID: t.session.UserID, RecvID: group, GroupID: group,
		ClientMsgID: clientID, SenderPlatformID: 5, SessionType: 3, MsgFrom: 100, ContentType: 101,
		Content: content, CreateTime: time.Now().UnixMilli(), Options: map[string]bool{"history": true, "persistent": true, "senderSync": true}},
		func(b []byte) error { var err error; result, err = decodeReceipt(b); return err }, fence)
	if err != nil {
		return Receipt{}, err
	}
	if result.ClientID != clientID || result.ServerID == "" {
		return Receipt{}, &Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
	}
	return result, nil
}

func IsTerminal(err error) bool {
	var e *Failure
	return errors.As(err, &e) && (e.Code == "lajiaoliao_kicked" || e.Code == "lajiaoliao_reauth_required" ||
		e.Code == "lajiaoliao_sequence_gap" || e.Code == "lajiaoliao_protocol_boundary" ||
		e.Code == "lajiaoliao_receive_queue_full" ||
		e.Code == "lajiaoliao_receive_persistence_failed" ||
		e.Code == "lajiaoliao_sequence_limit" || e.Code == "lajiaoliao_session_invalid")
}
