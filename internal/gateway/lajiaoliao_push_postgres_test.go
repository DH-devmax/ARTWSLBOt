package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

func TestPostgresChiliPushDedupNoCursorAndRevocation(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{61}, 32)))
	session, _ := json.Marshal(lajiaoliao.Session{UserID: "self", IMToken: "synthetic", DeviceID: "synthetic"})
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state)
		VALUES('synthetic-push','lajiaoliao','self','authenticated') RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	sealed, _ := vault.Seal("lajiaoliao-session:"+account, session)
	if _, err := db.Exec(ctx, `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, account, sealed); err != nil {
		t.Fatal(err)
	}
	m := NewReceiverManager(db, vault, nil)
	if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
		VALUES($1,true,$2,now()+interval '1 hour')`, account, m.owner); err != nil {
		t.Fatal(err)
	}
	w := &receiverWorker{spec: receiverSpec{ID: account, UID: "self", Platform: "lajiaoliao", Epoch: 1, Revision: 1}}
	wakes := 0
	m.WakeDecoder = func() { wakes++ }
	batch := func() []lajiaoliao.PushMessage {
		return []lajiaoliao.PushMessage{
			{Conversation: "si_self_peer", Message: &sdkws.MsgData{ClientMsgID: "first", SendID: "peer",
				RecvID: "self", SessionType: 1, MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"synthetic"}`)}},
			{Conversation: "si_self_peer", Message: &sdkws.MsgData{ClientMsgID: "second", SendID: "peer",
				RecvID: "self", SessionType: 1, MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"another synthetic"}`)}},
		}
	}
	first := batch()
	if err := m.commitChiliPush(ctx, w, first); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first[0].Message.Content, make([]byte, len(first[0].Message.Content))) {
		t.Fatal("push body survived durable commit")
	}
	// A replay after reconnect keeps its original native identity.
	if err := m.commitChiliPush(ctx, w, batch()); err != nil {
		t.Fatal(err)
	}
	var events, cursors int
	if err := db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM native_received_events WHERE account_id=$1),
		(SELECT count(*) FROM lajiaoliao_cursors WHERE account_id=$1)`, account).Scan(&events, &cursors); err != nil ||
		events != 2 || cursors != 0 || wakes != 2 {
		t.Fatal("push was lost, repeated or advanced a cursor", events, cursors, wakes, err)
	}
	d := NewDecoderManager(db, vault)
	jobs, err := d.claim(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.Account == account {
			if err := d.process(ctx, job); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM native_decoded_events WHERE account_id=$1 AND state='decoded'`, account).Scan(&events); err != nil || events != 2 {
		t.Fatal("push did not reach the shared decoder", events, err)
	}
	for i, mode := range []string{"remote-disconnect", "stop-reader-before-drain"} {
		t.Run(mode, func(t *testing.T) {
			transport := &chiliObservationTransport{hints: make(chan struct{}, 1), done: make(chan struct{}),
				pushes: make(chan []lajiaoliao.PushMessage, 1)}
			pending := batch()[:1]
			pending[0].Message.ClientMsgID = mode
			want := error(context.Canceled)
			if mode == "remote-disconnect" {
				transport.pushes <- pending
				transport.Close()
			} else {
				want = context.DeadlineExceeded
				transport.sequenceErr = want
				transport.stopHook = func() { transport.pushes <- pending }
			}
			err := m.lajiaoConnectionUsing(ctx, w,
				func(context.Context, lajiaoliao.Session) (chiliReceiverTransport, error) { return transport, nil })
			if !errors.Is(err, want) {
				t.Fatal("receiver hid the disconnected transport result", err)
			}
			if err := db.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM native_received_events WHERE account_id=$1),
				(SELECT count(*) FROM lajiaoliao_cursors WHERE account_id=$1)`, account).Scan(&events, &cursors); err != nil ||
				events != 3+i || cursors != 0 {
				t.Fatal("exit discarded a queued push or advanced its cursor", events, cursors, err)
			}
		})
	}
	t.Run("uncertain-database-commit-is-idempotent", func(t *testing.T) {
		pending := batch()[:1]
		pending[0].Message.ClientMsgID = "retry-commit"
		attempts := 0
		err := retryChiliPush(ctx, pending, func(work context.Context) error {
			attempts++
			if err := m.commitChiliPushAttempt(work, w, pending); err != nil {
				return err
			}
			if attempts == 1 {
				return chiliPushTransient{}
			}
			return nil
		})
		if err != nil || attempts != 2 {
			t.Fatal("receive persistence did not recover once", attempts, err)
		}
		if err := db.QueryRow(ctx, `SELECT count(*) FROM native_received_events WHERE account_id=$1`, account).Scan(&events); err != nil || events != 5 {
			t.Fatal("database retry created another event", events, err)
		}
	})
	for i, mode := range []string{"push-first", "sequence-first"} {
		t.Run(mode, func(t *testing.T) {
			var tenant, group string
			nativeGroup := mode + "-" + account
			if err := db.QueryRow(ctx, `INSERT INTO tenants(name) VALUES('synthetic sequence evidence') RETURNING id::text`).
				Scan(&tenant); err != nil {
				t.Fatal(err)
			}
			defer db.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, tenant)
			if err := db.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)
				VALUES('lajiaoliao',$1,'synthetic sequence evidence') RETURNING id::text`, nativeGroup).Scan(&group); err != nil {
				t.Fatal(err)
			}
			defer db.Exec(ctx, `DELETE FROM native_groups WHERE id=$1`, group)
			if _, err := db.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
				VALUES($1,$2,'owner',clock_timestamp(),'replay')`, account, group); err != nil {
				t.Fatal(err)
			}
			defer db.Exec(ctx, `DELETE FROM account_groups WHERE group_id=$1`, group)
			if _, err := db.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)
				VALUES($1,$2,$3)`, group, tenant, account); err != nil {
				t.Fatal(err)
			}
			defer db.Exec(ctx, `DELETE FROM group_bindings WHERE group_id=$1`, group)
			at := time.Now().UTC()
			pending := batch()[:1]
			pending[0].Conversation = "sg_" + nativeGroup
			pending[0].Message = &sdkws.MsgData{ClientMsgID: "shared-" + mode, SendID: "self",
				RecvID: nativeGroup, GroupID: nativeGroup, SessionType: 3, SendTime: at.UnixMilli(),
				MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"synthetic reference"}`)}
			sequenced := proto.Clone(pending[0].Message).(*sdkws.MsgData)
			sequenced.Seq, sequenced.ServerMsgID = 1, "receipt-"+mode
			push := func() error { return m.commitChiliPush(ctx, w, pending) }
			pull := func() error {
				return m.commitChili(ctx, w, pending[0].Conversation, 0, 1, []*sdkws.MsgData{sequenced})
			}
			first, second := push, pull
			if mode == "sequence-first" {
				first, second = pull, push
			}
			if err := first(); err != nil {
				t.Fatal(err)
			}
			if err := second(); err != nil {
				t.Fatal(err)
			}
			var sequence int64
			if err := db.QueryRow(ctx, `SELECT count(*) FROM native_received_events
				WHERE account_id=$1 AND kind IN('private','group')`, account).Scan(&events); err != nil || events != 6+i {
				t.Fatal("same native identity became another chat event", events, err)
			}
			if err := db.QueryRow(ctx, `SELECT sequence FROM lajiaoliao_cursors WHERE account_id=$1 AND conversation=$2`,
				account, pending[0].Conversation).Scan(&sequence); err != nil || sequence != 1 {
				t.Fatal("native identity dedup stopped the recovery cursor", sequence, err)
			}
			jobs, err := d.claim(ctx, 16)
			if err != nil {
				t.Fatal(err)
			}
			for _, job := range jobs {
				if job.Account == account {
					if err := d.process(ctx, job); err != nil {
						t.Fatal(err)
					}
				}
			}
			candidates, err := m.MessageCandidates(ctx, "lajiaoliao", account, nativeGroup, "synthetic reference")
			if err != nil || len(candidates) != 1 || candidates[0].Reference.Sequence != 1 {
				t.Fatal("recovery lost the usable sequence or duplicated the candidate", candidates, err)
			}
			candidate := candidates[0]
			if err := m.verifyRecallSource(ctx, account, candidate.Reference); err != nil {
				t.Fatal("reference could not be verified against its original event", err)
			}
			wrong := candidate.Reference
			wrong.Sequence++
			if err := m.verifyRecallSource(ctx, account, wrong); err == nil {
				t.Fatal("a substituted native sequence was accepted")
			}
			receipt, err := m.AssistantReceipt(ctx, "lajiaoliao", account, nativeGroup, sequenced.ClientMsgID, "self", at)
			if err != nil || receipt.EventID != candidate.Reference.EventID || receipt.NativeID != sequenced.ServerMsgID {
				t.Fatal("supplemented receipt did not retain the original event", receipt, err)
			}
			digest := sha256.Sum256(sequenced.Content)
			evidence, err := m.ChiliMediaEvidence(ctx, lajiaoliao.MediaEvidenceQuery{Account: account,
				Epoch: 1, Sender: "self", Group: nativeGroup, ClientID: sequenced.ClientMsgID,
				ContentType: 101, Digest: digest[:], SubmittedAt: at})
			if err != nil || !evidence.Found || evidence.Truncated || evidence.EventID != candidate.Reference.EventID ||
				evidence.Receipt.ServerID != sequenced.ServerMsgID || evidence.Source != "durable_echo" {
				t.Fatal("read-only reconciliation lost the supplementary receipt", evidence, err)
			}
			var original []byte
			key := chiliMessageKey(pending[0].Conversation, sequenced.ClientMsgID)
			if err := db.QueryRow(ctx, `SELECT payload_sealed FROM native_received_events
				WHERE id=$1 AND event_key=$2`, candidate.Reference.EventID, key).Scan(&original); err != nil {
				t.Fatal(err)
			}
			raw, err := vault.Open("native-event:"+account+":"+key, original)
			if err != nil {
				t.Fatal(err)
			}
			var originalMessage sdkws.MsgData
			err = proto.Unmarshal(raw, &originalMessage)
			clear(raw)
			clear(originalMessage.Content)
			want := int64(0)
			if mode == "sequence-first" {
				want = 1
			}
			if err != nil || originalMessage.Seq != want {
				t.Fatal("supplement rewrote the first retained delivery", originalMessage.Seq, err)
			}
			if err := db.QueryRow(ctx, `SELECT count(*) FROM native_decoded_events
				WHERE account_id=$1 AND conversation_key=$2`, account,
				conversationKey("lajiaoliao:group", 0, nativeGroup)).Scan(&events); err != nil || events != 1 {
				t.Fatal("sequence evidence entered the business/proof projection", events, err)
			}
		})
	}
	t.Run("sequence-cannot-replace-a-different-message", func(t *testing.T) {
		pending := batch()[:1]
		pending[0].Conversation = "si_conflicting"
		pending[0].Message.ClientMsgID = "conflicting"
		conflict := proto.Clone(pending[0].Message).(*sdkws.MsgData)
		conflict.Seq, conflict.Content = 1, []byte(`{"content":"different source"}`)
		if err := m.commitChiliPush(ctx, w, pending); err != nil {
			t.Fatal(err)
		}
		if err := m.commitChili(ctx, w, pending[0].Conversation, 0, 1, []*sdkws.MsgData{conflict}); err == nil {
			t.Fatal("conflicting recovery data became sequence evidence")
		}
		var evidence int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM lajiaoliao_cursors
			WHERE account_id=$1 AND conversation=$2`, account, pending[0].Conversation).Scan(&evidence); err != nil || evidence != 0 {
			t.Fatal("conflicting recovery advanced its cursor", evidence, err)
		}
	})
	t.Run("legacy-sequence-replay-keeps-original-event", func(t *testing.T) {
		message := batch()[0].Message
		message.Seq, message.ClientMsgID = 1, "legacy"
		sum := sha256.Sum256([]byte("si_legacy\x001"))
		key := "lajiao:" + hex.EncodeToString(sum[:])
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := m.storeChiliEvent(ctx, tx, account, key, key, message, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := m.commitChili(ctx, w, "si_legacy", 0, 1, []*sdkws.MsgData{message}); err != nil {
			t.Fatal(err)
		}
		var original, canonical int
		if err := db.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM native_received_events WHERE account_id=$1 AND event_key=$2),
			(SELECT count(*) FROM native_received_events WHERE account_id=$1 AND event_key=$3)`,
			account, key, chiliMessageKey("si_legacy", "legacy")).Scan(&original, &canonical); err != nil ||
			original != 1 || canonical != 0 {
			t.Fatal("legacy replay duplicated or replaced the original event", original, canonical, err)
		}
	})
	invalid := batch()
	invalid[0].Message.Seq = 1
	if err := m.commitChiliPush(ctx, w, invalid); err == nil {
		t.Fatal("sequenced message bypassed recovery")
	}
	if _, err := db.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account); err != nil {
		t.Fatal(err)
	}
	if err := m.commitChiliPush(ctx, w, batch()); err == nil {
		t.Fatal("revoked connection committed a push")
	}
}

func TestPostgresChiliPlaceholderIsSealedAndNeverProof(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{62}, 32)))
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state)
		VALUES('synthetic-placeholder','lajiaoliao','self','authenticated') RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	m := NewReceiverManager(db, vault, nil)
	if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
		VALUES($1,true,$2,now()+interval '1 hour')`, account, m.owner); err != nil {
		t.Fatal(err)
	}
	w := &receiverWorker{spec: receiverSpec{ID: account, UID: "self", Platform: "lajiaoliao", Epoch: 1, Revision: 1}}
	batch := []*sdkws.MsgData{
		{Seq: 1, ClientMsgID: "before", SessionType: 1},
		{Seq: 2},
		{Seq: 3, ClientMsgID: "after", SessionType: 1},
	}
	if err := m.commitChili(ctx, w, "si_self_peer", 0, 3, batch); err != nil {
		t.Fatal("placeholder blocked a contiguous range", err)
	}
	var sequence int64
	var events int
	if err := db.QueryRow(ctx, `SELECT sequence FROM lajiaoliao_cursors WHERE account_id=$1`, account).Scan(&sequence); err != nil || sequence != 3 {
		t.Fatal(sequence, err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM native_received_events WHERE account_id=$1`, account).Scan(&events); err != nil || events != 3 {
		t.Fatal("raw placeholder was discarded", events, err)
	}
	var key string
	var sealed []byte
	if err := db.QueryRow(ctx, `SELECT event_key,payload_sealed FROM native_received_events
		WHERE account_id=$1 AND kind='notice'`, account).Scan(&key, &sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := vault.Open("native-event:"+account+":"+key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var placeholder sdkws.MsgData
	if proto.Unmarshal(raw, &placeholder) != nil || placeholder.Seq != 2 || placeholder.ClientMsgID != "" {
		t.Fatal("placeholder identity was manufactured")
	}
	if _, _, route, state, reason := decodeChiliMessage(raw, "self"); route != "" || state != "unsupported" || reason != "lajiaoliao_sequence_placeholder" {
		t.Fatal("placeholder acquired a usable proof route", state, reason)
	}
	if err := m.commitChili(ctx, w, "si_self_peer", 3, 6, []*sdkws.MsgData{{Seq: 4}, {Seq: 6}}); err == nil {
		t.Fatal("genuinely absent sequence was skipped")
	}
	if err := db.QueryRow(ctx, `SELECT sequence FROM lajiaoliao_cursors WHERE account_id=$1`, account).Scan(&sequence); err != nil || sequence != 3 {
		t.Fatal("failed recovery advanced the durable cursor", sequence, err)
	}
}
