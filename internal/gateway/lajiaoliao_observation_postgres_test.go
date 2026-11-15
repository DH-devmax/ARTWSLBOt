package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

type chiliObservationTransport struct {
	hints       chan struct{}
	pushes      chan []lajiaoliao.PushMessage
	done        chan struct{}
	once        sync.Once
	stopHook    func()
	sequenceErr error
}

func (c *chiliObservationTransport) Sequences(context.Context) (*sdkws.GetMaxSeqResp, error) {
	return &sdkws.GetMaxSeqResp{}, c.sequenceErr
}
func (c *chiliObservationTransport) Pull(context.Context, string, int64, int64) ([]*sdkws.MsgData, error) {
	return nil, errors.New("unexpected synthetic pull")
}
func (c *chiliObservationTransport) Stop() {
	c.once.Do(func() {
		if c.stopHook != nil {
			c.stopHook()
		}
		close(c.done)
	})
}
func (c *chiliObservationTransport) Close()                                  { c.Stop() }
func (c *chiliObservationTransport) Done() <-chan struct{}                   { return c.done }
func (c *chiliObservationTransport) Hints() <-chan struct{}                  { return c.hints }
func (c *chiliObservationTransport) Pushes() <-chan []lajiaoliao.PushMessage { return c.pushes }
func (c *chiliObservationTransport) Err() error                              { return context.Canceled }

func TestPostgresChiliObservationTimesAndCancellation(t *testing.T) {
	db := receiverTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{31}, 32)))
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state)
        VALUES('synthetic-chili-clock','lajiaoliao','clock-self','authenticated') RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, account)
	session := lajiaoliao.Session{UserID: "clock-self", IMToken: "synthetic", DeviceID: "synthetic"}
	raw, _ := json.Marshal(session)
	sealed, _ := vault.Seal("lajiaoliao-session:"+account, raw)
	if _, err := db.Exec(ctx, `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, account, sealed); err != nil {
		t.Fatal(err)
	}
	manager := NewReceiverManager(db, vault, nil)
	if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
        VALUES($1,true,$2,now()+interval '1 minute')`, account, manager.owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO lajiaoliao_auth_state(account_id,device_id,stream_initialized)
        VALUES($1,'synthetic',true)`, account); err != nil {
		t.Fatal(err)
	}
	worker := &receiverWorker{spec: receiverSpec{ID: account, UID: session.UserID,
		Platform: "lajiaoliao", Epoch: 1, Revision: 1}}
	transport := &chiliObservationTransport{hints: make(chan struct{}, 1), done: make(chan struct{}),
		pushes: make(chan []lajiaoliao.PushMessage, 1)}
	finished := make(chan error, 1)
	go func() {
		finished <- manager.lajiaoConnectionUsing(ctx, worker,
			func(_ context.Context, current lajiaoliao.Session) (chiliReceiverTransport, error) {
				if current.UserID != session.UserID {
					return nil, errors.New("synthetic account scope changed")
				}
				return transport, nil
			})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-transport.done:
		case <-time.After(time.Second):
			t.Error("cancelled receiver did not close its transport")
		}
	})
	wait := func(after time.Time) ReceiverSnapshot {
		t.Helper()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			view := worker.view()
			if view.SyncedAt != nil && view.SyncedAt.After(after) {
				return view
			}
			select {
			case err := <-finished:
				t.Fatal("synthetic receiver ended early", err)
			case <-ctx.Done():
				t.Fatal("synthetic receiver did not synchronize")
			case <-tick.C:
			}
		}
	}
	first := wait(time.Time{})
	connected, heartbeat, synced := *first.ConnectedAt, *first.HeartbeatAt, *first.SyncedAt
	transport.hints <- struct{}{}
	second := wait(synced)
	if !second.ConnectedAt.Equal(connected) || !second.HeartbeatAt.After(heartbeat) ||
		!second.SyncedAt.After(synced) || second.Reconnects != 0 {
		t.Fatal("heartbeat changed connection time or failed to advance independent observations")
	}
	if !first.ConnectedAt.Equal(connected) || !first.HeartbeatAt.Equal(heartbeat) || !first.SyncedAt.Equal(synced) {
		t.Fatal("later receiver work mutated an earlier snapshot")
	}
	transport.pushes <- []lajiaoliao.PushMessage{{Conversation: "si_clock-self_peer", Message: &sdkws.MsgData{
		ClientMsgID: "unsaved-push", SendID: "peer", RecvID: session.UserID, SessionType: 1,
		MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"synthetic"}`),
	}}}
	third := wait(*second.SyncedAt)
	var events, cursors int
	if err := db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM native_received_events WHERE account_id=$1),
		(SELECT count(*) FROM lajiaoliao_cursors WHERE account_id=$1)`, account).Scan(&events, &cursors); err != nil ||
		events != 1 || cursors != 0 || third.Messages != 1 || !third.ConnectedAt.Equal(connected) {
		t.Fatal("unchanged sequence heads hid a zero-sequence event", events, cursors, err)
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("unexpected cancellation result", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled receiver did not finish")
	}
}
