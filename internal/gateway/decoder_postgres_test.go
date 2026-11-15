package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func TestPostgresDecoderClaimRecoveryAndFairness(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{23}, 32)))
	var original []byte
	var version int64
	oldErr := db.QueryRow(ctx, `SELECT sealed,version FROM native_deployment`).Scan(&original, &version)
	if oldErr != nil && oldErr != pgx.ErrNoRows {
		t.Fatal(oldErr)
	}
	defer func() {
		if oldErr == pgx.ErrNoRows {
			db.Exec(ctx, `DELETE FROM native_deployment`)
		} else {
			db.Exec(ctx, `UPDATE native_deployment SET sealed=$1,version=$2`, original, version)
		}
	}()
	d, _ := json.Marshal(nativeauth.Deployment{ProtocolVersion: nativeauth.DesktopProtocolVersion,
		BodyKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))})
	deployment, _ := vault.Seal("native-deployment:v1", d)
	if _, err := db.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed) VALUES(true,$1)
        ON CONFLICT(singleton) DO UPDATE SET sealed=$1,version=1`, deployment); err != nil {
		t.Fatal(err)
	}
	accounts := []string{}
	for i := 0; i < 2; i++ {
		var id string
		if err := db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state) VALUES($1,'authenticated') RETURNING id::text`,
			fmt.Sprintf("decoder-fixture-%d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, id)
		defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id)
		session, _ := json.Marshal(savedNativeSession{DeploymentVersion: 1, Session: nativeauth.Session{UID: 123, NIMID: "self"}})
		sealed, _ := vault.Seal("native-session:"+id, session)
		db.Exec(ctx, `UPDATE accounts SET credential_sealed=$1 WHERE id=$2`, sealed, id)
		for j := 0; j < 24; j++ {
			key := fmt.Sprintf("fixture-%d", j)
			event, _ := json.Marshal(nativewire.IMMessage{Scene: 1, Target: "team", Sender: "peer", Fields: map[uint32]string{8: "5"}})
			sealed, _ := vault.Seal("native-event:"+id+":"+key, event)
			if _, err := db.Exec(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
                VALUES($1,$2,'group',now(),$3)`, id, key, sealed); err != nil {
				t.Fatal(err)
			}
		}
	}
	m := NewDecoderManager(db, vault)
	jobs, err := m.claim(ctx, 4)
	if err != nil || len(jobs) != 4 {
		t.Fatal("claim", len(jobs), err)
	}
	fair := map[string]bool{}
	for _, job := range jobs {
		fair[job.Account] = true
	}
	if len(fair) != 2 {
		t.Fatal("hot account starved second account")
	}
	second := NewDecoderManager(db, vault)
	other, err := second.claim(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range jobs {
		for _, b := range other {
			if a.ID == b.ID {
				t.Fatal("duplicate live claim")
			}
		}
	}
	for _, job := range jobs {
		if err = m.process(ctx, job); err != nil {
			t.Fatal("decode projection", err)
		}
	}
	if _, err = db.Exec(ctx, `UPDATE native_decoded_events SET lease_until=now()-interval '1 second'
        WHERE owner=$1 AND state='processing'`, second.owner); err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); m.Run(workerCtx) }()
	deadline := time.Now().Add(10 * time.Second)
	var n int
	for time.Now().Before(deadline) {
		err = db.QueryRow(ctx, `SELECT count(*) FROM native_decoded_events WHERE account_id=ANY($1::uuid[]) AND state='unsupported'`, accounts).Scan(&n)
		if err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		if n == 48 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n == 48 {
		// Once idle, a committed arrival must not wait for the five-second
		// recovery scan. No native connection is opened by this fixture.
		time.Sleep(50 * time.Millisecond)
		account, key := accounts[0], "wake-after-idle"
		event, _ := json.Marshal(nativewire.IMMessage{Scene: 1, Target: "team", Sender: "peer", Fields: map[uint32]string{8: "5"}})
		sealed, _ := vault.Seal("native-event:"+account+":"+key, event)
		if _, err := db.Exec(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
            VALUES($1,$2,'group',now(),$3)`, account, key, sealed); err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		m.Notify()
		until := time.Now().Add(2 * time.Second)
		for time.Now().Before(until) {
			err = db.QueryRow(ctx, `SELECT count(*) FROM native_decoded_events
                WHERE account_id=ANY($1::uuid[]) AND state='unsupported'`, accounts).Scan(&n)
			if err != nil || n == 49 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers ignored cancellation")
	}
	if n != 49 {
		t.Fatalf("durable jobs/woken arrival not recovered: %d/49", n)
	}
	var previous int64
	err = db.QueryRow(ctx, `UPDATE native_decoded_events SET codec_version=$2,state='processing',
        lease_until=now()+interval '60 seconds' WHERE event_id=(
            SELECT min(event_id) FROM native_decoded_events WHERE account_id=$1)
        RETURNING event_id`, accounts[0], CodecVersion-1).Scan(&previous)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err = m.claim(ctx, 8)
	if err != nil || len(jobs) != 0 {
		t.Fatal("codec upgrade stole a live prior-version lease", err)
	}
	if _, err = db.Exec(ctx, `UPDATE native_decoded_events SET state='unsupported',lease_until=NULL
        WHERE event_id=$1`, previous); err != nil {
		t.Fatal(err)
	}
	jobs, err = m.claim(ctx, 8)
	if err != nil || len(jobs) != 1 || jobs[0].ID != previous {
		t.Fatal("prior codec projection was not scheduled for retained replay", err)
	}
	if err = m.process(ctx, jobs[0]); err != nil {
		t.Fatal("prior codec replay failed", err)
	}
	jobs, err = m.claim(ctx, 8)
	if err != nil || len(jobs) != 0 {
		t.Fatal("completed work replayed", err)
	}
	var exposed bool
	err = db.QueryRow(ctx, `SELECT coalesce(bool_or(position('transport_to'::bytea IN payload_sealed)>0),false)
        FROM native_decoded_events WHERE account_id=ANY($1::uuid[])`, accounts).Scan(&exposed)
	if err != nil || exposed {
		t.Fatal("projection stored plaintext", err)
	}
	// The v7 compatibility repair must not replay successfully decoded v6
	// history. Only failed/unsupported v6 records enter its existing leases.
	var preserved int64
	if err = db.QueryRow(ctx, `UPDATE native_decoded_events SET codec_version=6,state='decoded',reason=''
		WHERE event_id=(SELECT min(event_id) FROM native_decoded_events WHERE account_id=$1)
		RETURNING event_id`, accounts[0]).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	jobs, err = m.claim(ctx, 8)
	if err != nil || len(jobs) != 0 {
		t.Fatal("compatibility repair replayed successful history", err)
	}
	if _, err = db.Exec(ctx, `UPDATE native_decoded_events SET state='failed',reason='packet_signature' WHERE event_id=$1`, preserved); err != nil {
		t.Fatal(err)
	}
	jobs, err = m.claim(ctx, 8)
	if err != nil || len(jobs) != 1 || jobs[0].ID != preserved {
		t.Fatal("compatibility repair missed failed history", err)
	}
	if err = m.process(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	directory, _ := json.Marshal([]nativeauth.DirectoryGroup{{ID: "456", Team: "team", Name: "synthetic"}})
	dirSealed, _ := vault.Seal("native-directory:"+accounts[0], directory)
	if _, err = db.Exec(ctx, `INSERT INTO native_directory_snapshots(account_id,payload_sealed,sampled_at)
		VALUES($1,$2,now())`, accounts[0], dirSealed); err != nil {
		t.Fatal(err)
	}
	source := nativewire.IMMessage{Scene: 1, Sender: "peer", Target: "team", ServerID: "123",
		Fields: map[uint32]string{0: "1", 1: "team", 2: "peer", 8: "5", 12: "123"}}
	out, scene, conversation, ok := m.decodeTransportNotice(ctx, accounts[0], source)
	if !ok || scene != "group" || conversation != conversationKey("group", 456, "team") ||
		out.Message.Notice.Verification != "transport" || out.Message.Sender.ID != 0 {
		t.Fatal("transport notice lost display scope")
	}
	if _, _, _, ok = m.decodeTransportNotice(ctx, accounts[1], source); ok {
		t.Fatal("directory leaked across accounts")
	}
}
