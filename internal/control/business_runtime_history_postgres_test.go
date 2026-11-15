package control

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPostgresHeatHistoryRespectsBindingDataSince(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newBusinessFixture(t)
			manager := f.account(platform, 1)
			group := f.group(platform, manager, f.tenant, 1, false)
			chat := NewID()
			f.exec(`INSERT INTO accounts(id,name,platform,workspace,purpose)
				VALUES($1,'synthetic-history-chat',$2,$2,'chat')`, chat, platform)
			boundary := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Microsecond)
			f.exec(`UPDATE group_bindings SET ai_account_id=$2,data_since=$3 WHERE group_id=$1`, group.Group, chat, boundary)
			scope := HeatScope{Platform: platform, Tenant: f.tenant, Group: group.Group, Account: chat, NativeGroup: group.NativeGroup}
			if err := f.db.Platform.QueryRow(f.ctx, `SELECT epoch,version FROM group_bindings WHERE group_id=$1`, group.Group).
				Scan(&scope.BindingEpoch, &scope.BindingVersion); err != nil {
				t.Fatal(err)
			}
			var expected []int64
			for i, at := range []time.Time{boundary.Add(-time.Second), boundary, boundary.Add(time.Second)} {
				var event int64
				if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
					VALUES($1,$2,'group',$3,$4) RETURNING id`, chat, fmt.Sprintf("synthetic-history-%d", i), at, []byte("sealed-source")).Scan(&event); err != nil {
					t.Fatal(err)
				}
				f.exec(`INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,payload_sealed,
					codec_version,deployment_version,received_at) VALUES($1,$2,'decoded','group','synthetic-history',$3,7,0,$4)`,
					event, chat, []byte("sealed-projection"), at)
				if !at.Before(boundary) {
					expected = append(expected, event)
				}
			}
			service := HeatChatService{DB: f.db}
			service.Conversation = func(context.Context, HeatScope) (string, error) { return "synthetic-history", nil }
			decoded := 0
			service.Decode = func(_ HeatScope, _ string, event int64, _, _ []byte) (HeatMessage, error) {
				decoded++
				for _, allowed := range expected {
					if event == allowed {
						return HeatMessage{Text: "current tenant"}, nil
					}
				}
				t.Fatal("pre-transfer source reached decryption", event)
				return HeatMessage{}, nil
			}
			for _, history := range []bool{false, true} {
				rows, err := service.messages(f.ctx, scope, boundary.Add(-24*time.Hour), history)
				if err != nil || len(rows) != 2 {
					t.Fatal("binding cutoff omitted or exposed history", history, rows, err)
				}
			}
			rows, err := service.messages(f.ctx, scope, boundary.Add(500*time.Millisecond), true)
			if err != nil || len(rows) != 1 || rows[0].EventID != expected[1] || decoded != 5 {
				t.Fatal("caller cutoff was weakened by binding cutoff", rows, decoded, err)
			}
			nextTenant, _ := f.customer()
			f.exec(`UPDATE group_bindings SET tenant_id=$2,data_since=clock_timestamp(),
				epoch=epoch+1,version=version+1 WHERE group_id=$1`, scope.Group, nextTenant)
			rows, err = service.messages(f.ctx, scope, boundary.Add(-24*time.Hour), true)
			if err != nil || len(rows) != 0 || decoded != 5 {
				t.Fatal("old tenant scope decrypted transferred history", rows, decoded, err)
			}
		})
	}
}
