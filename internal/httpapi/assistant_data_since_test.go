package httpapi

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
)

func TestPostgresAssistantCandidatesRespectBindingDataSince(t *testing.T) {
	f := newCommercialFixture(t)
	owner := f.owner("candidate-owner", "PRO")
	account, group := control.NewID(), control.NewID()
	for _, row := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts(id,name,platform,workspace,purpose)
			VALUES($1,'synthetic-candidates','lajiaoliao','lajiaoliao','management')`, []any{account}},
		{`INSERT INTO native_groups(id,platform,native_id,name)
			VALUES($1,'lajiaoliao','string-group','synthetic')`, []any{group}},
		{`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) VALUES($1,$2,$3)`, []any{group, owner.tenant, account}},
	} {
		if _, err := f.db.Platform.Exec(f.ctx, row.sql, row.args...); err != nil {
			t.Fatal(err)
		}
	}
	manager := gateway.NewReceiverManager(f.db.Platform, f.s.Vault, nil)
	conversation, err := manager.ChatConversation(f.ctx, "lajiaoliao", account, "string-group")
	if err != nil {
		t.Fatal(err)
	}
	boundary := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Microsecond)
	if _, err = f.db.Platform.Exec(f.ctx, `UPDATE group_bindings SET data_since=$2 WHERE group_id=$1`, group, boundary); err != nil {
		t.Fatal(err)
	}
	for i, at := range []time.Time{boundary.Add(-time.Second), boundary, boundary.Add(time.Second)} {
		key := fmt.Sprintf("synthetic-candidate-%d", i)
		var event int64
		if err = f.db.Platform.QueryRow(f.ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at)
			VALUES($1,$2,'group',$3) RETURNING id`, account, key, at).Scan(&event); err != nil {
			t.Fatal(err)
		}
		msg := &sdkws.MsgData{ClientMsgID: key, ServerMsgID: key, GroupID: "string-group", SendID: "string-sender",
			RecvID: "string-group", SessionType: 3, Seq: int64(i + 1), SendTime: at.UnixMilli()}
		raw, _ := proto.Marshal(msg)
		source, e := f.s.Vault.Seal("native-event:"+account+":"+key, raw)
		if e != nil {
			t.Fatal(e)
		}
		projection := gateway.DecodedMessage{Platform: "lajiaoliao", NativeTarget: "string-group",
			NativeSender: "string-sender", ServerID: key}
		projection.Message.Session, projection.Message.CreatedAt, projection.Message.ClientID = 2, uint64(at.UnixMilli()), key
		projection.Message.Text = "synthetic current candidate"
		p, _ := json.Marshal(projection)
		sealed, e := f.s.Vault.Seal(gateway.DecodedAAD(account, event), p)
		clear(raw)
		clear(p)
		if e != nil {
			t.Fatal(e)
		}
		if _, err = f.db.Platform.Exec(f.ctx, `UPDATE native_received_events SET payload_sealed=$2 WHERE id=$1`, event, source); err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Platform.Exec(f.ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
			payload_sealed,codec_version,deployment_version,received_at) VALUES($1,$2,'decoded','group',$3,$4,7,0,$5)`,
			event, account, conversation, sealed, at); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := manager.MessageCandidates(f.ctx, "lajiaoliao", account, "string-group", "synthetic")
	if err != nil || len(candidates) != 2 {
		t.Fatal("assistant candidates crossed the transfer cutoff", len(candidates), err)
	}
	for _, candidate := range candidates {
		if candidate.Reference.CreatedAt < boundary.UnixMilli() {
			t.Fatal("pre-transfer plaintext reached candidate projection")
		}
	}
	if _, err = f.db.Platform.Exec(f.ctx, `UPDATE group_bindings SET active=false WHERE group_id=$1`, group); err != nil {
		t.Fatal(err)
	}
	candidates, err = manager.MessageCandidates(f.ctx, "lajiaoliao", account, "string-group", "synthetic")
	if err != nil || len(candidates) != 0 {
		t.Fatal("unbound history remained searchable", len(candidates), err)
	}
}
