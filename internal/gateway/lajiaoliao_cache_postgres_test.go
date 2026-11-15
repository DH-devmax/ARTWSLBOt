package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"wangshangliao/internal/security"
)

type chiliCacheTransport struct {
	chiliObservationTransport
	pull func(int64, int64) ([]*sdkws.MsgData, error)
}

func TestPostgresChiliNewRetainedStreamDoesNotResetExistingCursor(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{63}, 32)))
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state)
		VALUES('new-retained-stream','lajiaoliao','self','authenticated') RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	m := NewReceiverManager(db, vault, nil)
	if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
		VALUES($1,true,$2,now()+interval '1 hour')`, account, m.owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO lajiaoliao_auth_state(account_id,device_id,stream_initialized)
		VALUES($1,'synthetic',true)`, account); err != nil {
		t.Fatal(err)
	}
	w := &receiverWorker{spec: receiverSpec{ID: account, UID: "self", Platform: "lajiaoliao", Epoch: 1, Revision: 1}}
	conn := &chiliCacheTransport{pull: func(first, last int64) ([]*sdkws.MsgData, error) {
		if first != 90 || last != 120 {
			t.Fatalf("new stream did not start at retained history: %d..%d", first, last)
		}
		var batch []*sdkws.MsgData
		for seq := first; seq <= last; seq++ {
			batch = append(batch, &sdkws.MsgData{Seq: seq, ClientMsgID: "synthetic-" + strconv.FormatInt(seq, 10)})
		}
		return batch, nil
	}}
	heads := &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"new": 120}, MinSeqs: map[string]int64{"new": 90}}
	cache := &chiliCursorCache{}
	if more, err := m.pullChiliCached(ctx, w, conn, heads, cache); err != nil || !more || cache.values["new"] != 120 {
		t.Fatal("newly discovered retained stream was blocked", cache.values, err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO lajiaoliao_cursors(account_id,conversation,sequence)
		VALUES($1,'existing',80)`, account); err != nil {
		t.Fatal(err)
	}
	heads.MaxSeqs["existing"], heads.MinSeqs["existing"] = 120, 90
	if _, err := m.pullChiliCached(ctx, w, conn, heads, cache); err == nil {
		t.Fatal("existing recovery gap was hidden")
	}
	var sequence int64
	if err := db.QueryRow(ctx, `SELECT sequence FROM lajiaoliao_cursors
		WHERE account_id=$1 AND conversation='existing'`, account).Scan(&sequence); err != nil || sequence != 80 {
		t.Fatal("existing cursor was reset", sequence, err)
	}
	t.Run("existing-zero-is-not-an-absent-cursor", func(t *testing.T) {
		if _, err := db.Exec(ctx, `INSERT INTO lajiaoliao_cursors(account_id,conversation,sequence)
			VALUES($1,'existing-zero',0)`, account); err != nil {
			t.Fatal(err)
		}
		current := &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"existing-zero": 120},
			MinSeqs: map[string]int64{"existing-zero": 90}}
		if _, err := m.pullChiliCached(ctx, w, conn, current, &chiliCursorCache{}); err == nil {
			t.Fatal("real zero cursor skipped a retention gap")
		}
		if err := db.QueryRow(ctx, `SELECT sequence FROM lajiaoliao_cursors
			WHERE account_id=$1 AND conversation='existing-zero'`, account).Scan(&sequence); err != nil || sequence != 0 {
			t.Fatal("zero cursor was reset", sequence, err)
		}
	})
	t.Run("empty-range-and-later-first-message", func(t *testing.T) {
		current := &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"empty-range": 120},
			MinSeqs: map[string]int64{"empty-range": 121}}
		if more, err := m.pullChiliCached(ctx, w, conn, current, &chiliCursorCache{}); err != nil || more {
			t.Fatal("empty retained range was pulled", more, err)
		}
		current = &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"empty-zero": 0}}
		zero := &chiliCursorCache{}
		if more, err := m.pullChiliCached(ctx, w, conn, current, zero); err != nil || more {
			t.Fatal("empty zero stream was pulled", more, err)
		}
		firstMessage := &chiliCacheTransport{pull: func(first, last int64) ([]*sdkws.MsgData, error) {
			if first != 1 || last != 1 {
				t.Fatal("first message was skipped", first, last)
			}
			return []*sdkws.MsgData{{Seq: 1, ClientMsgID: "first"}}, nil
		}}
		current.MaxSeqs["empty-zero"] = 1
		if more, err := m.pullChiliCached(ctx, w, firstMessage, current, zero); err != nil || !more ||
			zero.values["empty-zero"] != 1 {
			t.Fatal("zero stream did not consume its first message", zero.values, err)
		}
	})
	if _, err := db.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account); err != nil {
		t.Fatal(err)
	}
	heads.MaxSeqs["revoked-new"], heads.MinSeqs["revoked-new"] = 120, 90
	if _, err := m.pullChiliCached(ctx, w, conn, heads, &chiliCursorCache{}); err == nil {
		t.Fatal("revoked connection initialized a new stream")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM lajiaoliao_cursors WHERE account_id=$1
		AND conversation='revoked-new'`, account).Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked connection retained a bootstrap cursor", count, err)
	}
}

func (c *chiliCacheTransport) Pull(_ context.Context, _ string, first, last int64) ([]*sdkws.MsgData, error) {
	return c.pull(first, last)
}

func TestPostgresChiliCacheCommitReconnectAndRevocation(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{29}, 32)))
	var account string
	if err := db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state,credential_sealed)
		VALUES('cache-synthetic','lajiaoliao','self','authenticated',$1) RETURNING id::text`, []byte("synthetic")).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	m := NewReceiverManager(db, vault, nil)
	for _, query := range []string{
		`INSERT INTO account_receivers(account_id,enabled,owner,lease_until) VALUES($1,true,$2,now()+interval '1 hour')`,
		`INSERT INTO lajiaoliao_auth_state(account_id,device_id,stream_initialized) VALUES($1,'synthetic',true)`,
	} {
		args := []any{account}
		if bytes.Contains([]byte(query), []byte("$2")) {
			args = append(args, m.owner)
		}
		if _, err := db.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	w := &receiverWorker{spec: receiverSpec{ID: account, UID: "self", Platform: "lajiaoliao", Epoch: 1, Revision: 1}}
	cache := &chiliCursorCache{}
	reject := false
	conn := &chiliCacheTransport{pull: func(first, last int64) ([]*sdkws.MsgData, error) {
		if reject {
			return nil, errors.New("synthetic uncertain pull")
		}
		if last-first+1 > 50 {
			t.Fatal("pull exceeded existing bound")
		}
		batch := []*sdkws.MsgData{}
		for seq := first; seq <= last; seq++ {
			batch = append(batch, &sdkws.MsgData{Seq: seq, ClientMsgID: "synthetic-" + strconv.FormatInt(seq, 10)})
		}
		return batch, nil
	}}
	heads := &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"sg_group": 101}}
	for _, want := range []int64{50, 100} {
		if more, err := m.pullChiliCached(ctx, w, conn, heads, cache); err != nil || !more || cache.values["sg_group"] != want {
			t.Fatal("confirmed cursor not reused", cache.values, more, err)
		}
	}
	reject = true
	if _, err := m.pullChiliCached(ctx, w, conn, heads, cache); err == nil || cache.values["sg_group"] != 100 {
		t.Fatal("failed pull advanced cache", err)
	}
	reject = false
	reconnected := &chiliCursorCache{}
	if _, err := m.pullChiliCached(ctx, w, conn, heads, reconnected); err != nil || reconnected.values["sg_group"] != 101 {
		t.Fatal("reconnection did not reload durable cursor", err)
	}
	heads.MaxSeqs["sg_new"] = 1
	if _, err := m.pullChiliCached(ctx, w, conn, heads, reconnected); err != nil || reconnected.values["sg_new"] != 1 {
		t.Fatal("new conversation was not loaded on demand", err)
	}
	if _, err := db.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, account); err != nil {
		t.Fatal(err)
	}
	heads.MaxSeqs["sg_new"] = 2
	if _, err := m.pullChiliCached(ctx, w, conn, heads, reconnected); err == nil || reconnected.values["sg_new"] != 1 {
		t.Fatal("revoked connection advanced cached cursor", err)
	}
	var seq int64
	if err := db.QueryRow(ctx, `SELECT sequence FROM lajiaoliao_cursors WHERE account_id=$1 AND conversation='sg_new'`, account).Scan(&seq); err != nil || seq != 1 {
		t.Fatal("revoked connection advanced durable cursor", seq, err)
	}
}
