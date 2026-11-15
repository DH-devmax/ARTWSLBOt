package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

func TestPostgresChiliHistoryReadOnlyAndFinalFence(t *testing.T) {
	db := receiverTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{43}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	q, message := chiliHistoryFixture()
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,epoch,auth_state,credential_sealed)
		VALUES('history-synthetic','lajiaoliao',$1,83,'authenticated',$2) RETURNING id::text`,
		q.Sender, []byte("synthetic-not-decrypted")).Scan(&q.Account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, q.Account)
	manager := NewReceiverManager(db, vault, nil)
	manager.reads = NewReadScheduler(ctx)
	defer manager.reads.Close()
	if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
		VALUES($1,true,$2,now()+interval '1 minute')`, q.Account, manager.owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO lajiaoliao_cursors(account_id,conversation,sequence)
		VALUES($1,'sg_group',37)`, q.Account); err != nil {
		t.Fatal(err)
	}
	reader := chiliHistoryReaderFor(message)
	now := time.Now()
	connection := &ownedChiliConnection{transport: reader, ctx: ctx, uid: q.Sender}
	worker := &receiverWorker{spec: receiverSpec{ID: q.Account, UID: q.Sender, Platform: "lajiaoliao", Epoch: 83, Revision: 1},
		chiliConnection: connection, leaseUntil: now.Add(time.Minute),
		snapshot: ReceiverSnapshot{State: "connected", HeartbeatAt: &now}}
	manager.jobs[q.Account] = worker
	manager.WakeDecoder = func() { t.Error("evidence manufactured a receive event") }
	var before time.Time
	if err := db.QueryRow(ctx, `SELECT updated_at FROM lajiaoliao_cursors
		WHERE account_id=$1 AND conversation='sg_group'`, q.Account).Scan(&before); err != nil {
		t.Fatal(err)
	}

	evidence, err := manager.ChiliMediaEvidence(ctx, q)
	if err != nil || !evidence.Found || evidence.Source != "server_history" || evidence.EventID != 0 ||
		reader.sendCalls != 0 || reader.sequenceCalls != 1 || reader.pullCalls != 1 || q.Epoch != 82 {
		t.Fatal("current epoch did not read original send evidence", evidence, err)
	}
	var events, cursor int64
	var after time.Time
	if err := db.QueryRow(ctx, `SELECT count(*) FROM native_received_events WHERE account_id=$1`, q.Account).Scan(&events); err != nil || events != 0 {
		t.Fatal("history lookup manufactured durable echoes", events, err)
	}
	if err := db.QueryRow(ctx, `SELECT sequence,updated_at FROM lajiaoliao_cursors
		WHERE account_id=$1 AND conversation='sg_group'`, q.Account).Scan(&cursor, &after); err != nil ||
		cursor != 37 || !after.Equal(before) {
		t.Fatal("evidence lookup advanced the receive cursor", cursor, before, after, err)
	}
	if manager.ReadSnapshot(q.Account).Completed != 1 {
		t.Fatal("history bypassed shared read scheduler")
	}

	t.Run("database_revision_changes_during_pull", func(t *testing.T) {
		original := reader.pull
		reader.pull = func(work context.Context, conversation string, begin, end int64) ([]*sdkws.MsgData, error) {
			if _, err := db.Exec(work, `UPDATE account_receivers SET revision=revision+1 WHERE account_id=$1`, q.Account); err != nil {
				return nil, err
			}
			return original(work, conversation, begin, end)
		}
		defer func() { reader.pull = original }()
		out, err := manager.ChiliMediaEvidence(ctx, q)
		var failure *lajiaoliao.Failure
		if !errors.As(err, &failure) || failure.Code != "lajiaoliao_evidence_history_guard_owner_changed" || out.Found {
			t.Fatal("old revision delivered late evidence", out, err)
		}
		if _, err := db.Exec(ctx, `UPDATE account_receivers SET revision=1 WHERE account_id=$1`, q.Account); err != nil {
			t.Fatal(err)
		}
	})
	for _, mode := range []string{"sender", "epoch", "authentication", "credentials"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := db.Exec(ctx, `UPDATE accounts SET native_uid=$2,epoch=83,
				auth_state='authenticated',credential_sealed=$3 WHERE id=$1`,
				q.Account, q.Sender, []byte("synthetic")); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "sender":
				_, err = db.Exec(ctx, `UPDATE accounts SET native_uid='replacement' WHERE id=$1`, q.Account)
			case "epoch":
				_, err = db.Exec(ctx, `UPDATE accounts SET epoch=84 WHERE id=$1`, q.Account)
			case "authentication":
				_, err = db.Exec(ctx, `UPDATE accounts SET auth_state='expired' WHERE id=$1`, q.Account)
			case "credentials":
				_, err = db.Exec(ctx, `UPDATE accounts SET credential_sealed=NULL WHERE id=$1`, q.Account)
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := reader.sequenceCalls
			out, err := manager.ChiliMediaEvidence(ctx, q)
			var failure *lajiaoliao.Failure
			if !errors.As(err, &failure) || failure.Code != "lajiaoliao_evidence_account_owner_changed" || out.Found || reader.sequenceCalls != calls {
				t.Fatal("changed authenticated identity reached upstream", out, err)
			}
		})
	}
}

func TestPostgresChiliHistoryDurableSourceAndEpochFallback(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{47}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	q, message := chiliHistoryFixture()
	q.Epoch, q.CurrentEpoch = 83, 0
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,epoch,auth_state,credential_sealed)
		VALUES('history-echo-synthetic','lajiaoliao',$1,83,'authenticated',$2) RETURNING id::text`,
		q.Sender, []byte("synthetic-not-decrypted")).Scan(&q.Account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, q.Account)
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := vault.Seal("native-event:"+q.Account+":synthetic-echo", raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	var event int64
	if err := db.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,'synthetic-echo','group',clock_timestamp(),$2) RETURNING id`, q.Account, sealed).Scan(&event); err != nil {
		t.Fatal(err)
	}
	manager := NewReceiverManager(db, vault, nil)
	out, err := manager.ChiliMediaEvidence(ctx, q)
	if err != nil || !out.Found || out.Source != "durable_echo" || out.EventID != event {
		t.Fatal("durable evidence lost its source or epoch fallback", out, err)
	}
}
