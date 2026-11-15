package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

func TestPostgresChiliCursorCommitAndDecode(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{29}, 32)))
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state)VALUES(
        'synthetic-chili','lajiaoliao','self','authenticated')RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	session, _ := json.Marshal(lajiaoliao.Session{UserID: "self", IMToken: "synthetic", ChatToken: "synthetic", DeviceID: "synthetic"})
	sealed, _ := vault.Seal("lajiaoliao-session:"+account, session)
	if _, err := db.Exec(ctx, `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, account, sealed); err != nil {
		t.Fatal(err)
	}
	m := NewReceiverManager(db, vault, nil)
	if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
        VALUES($1,true,$2,now()+interval '1 hour')`, account, m.owner); err != nil {
		t.Fatal(err)
	}
	worker := &receiverWorker{spec: receiverSpec{ID: account, UID: "self", Platform: "lajiaoliao", Epoch: 1, Revision: 1}}
	batch := []*sdkws.MsgData{
		{Seq: 1, ClientMsgID: "first", ServerMsgID: "server-first", SendID: "peer", RecvID: "group", GroupID: "group", SessionType: 3, MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"synthetic"}`)},
		{Seq: 2, ClientMsgID: "second", ServerMsgID: "server-second", SendID: "peer", RecvID: "group", GroupID: "group", SessionType: 3, MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"synthetic second"}`)},
	}
	wakes := 0
	m.WakeDecoder = func() { wakes++ }
	if err := m.commitChili(ctx, worker, "sg_group", 0, 2, batch[:1]); err == nil {
		t.Fatal("incomplete range advanced cursor")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM lajiaoliao_cursors WHERE account_id=$1`, account).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed batch left cursor", err)
	}
	if err := m.commitChili(ctx, worker, "sg_group", 0, 2, batch); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 {
		t.Fatal("decoder wake missing")
	}
	var sequence int64
	if err := db.QueryRow(ctx, `SELECT sequence FROM lajiaoliao_cursors WHERE account_id=$1 AND conversation='sg_group'`, account).Scan(&sequence); err != nil || sequence != 2 {
		t.Fatal("durable cursor", sequence, err)
	}
	if err := m.commitChili(ctx, worker, "sg_group", 0, 2, batch); err == nil {
		t.Fatal("stale range accepted")
	}
	// The same native message ID at a different sequence is retained as raw
	// evidence, rather than silently erasing a stream boundary.
	if _, err := db.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account); err != nil {
		t.Fatal(err)
	}
	next := []*sdkws.MsgData{{Seq: 3, ClientMsgID: "third"}}
	if err := m.commitChili(ctx, worker, "sg_group", 2, 3, next); err == nil {
		t.Fatal("old epoch advanced cursor")
	}
	if err := db.QueryRow(ctx, `SELECT sequence FROM lajiaoliao_cursors WHERE account_id=$1 AND conversation='sg_group'`, account).Scan(&sequence); err != nil || sequence != 2 {
		t.Fatal("fence did not preserve cursor", err)
	}
	d := NewDecoderManager(db, vault)
	jobs, err := d.claim(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.Account == account {
			if err = d.process(ctx, job); err != nil {
				t.Fatal("shared decoder", err)
			}
		}
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM native_decoded_events WHERE account_id=$1 AND state='decoded'`, account).Scan(&count); err != nil || count != 2 {
		t.Fatal("LajiaoLiao never entered central decoder", count, err)
	}
	var decoderScope string
	if err = db.QueryRow(ctx, `SELECT conversation_key FROM native_decoded_events WHERE account_id=$1 LIMIT 1`, account).Scan(&decoderScope); err != nil || decoderScope != conversationKey("lajiaoliao:group", 0, "group") {
		t.Fatal("projection scope", err)
	}
}
