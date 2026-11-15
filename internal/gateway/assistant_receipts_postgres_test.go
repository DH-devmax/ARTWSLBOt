package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"wangshangliao/internal/security"
)

func TestPostgresAssistantReceiptRetainsEarlyObservationUnderLaterTraffic(t *testing.T) {
	db := receiverTestDB(t)
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{37}, 32)))
			if err != nil {
				t.Fatal(err)
			}
			manager := NewReceiverManager(db, vault, nil)
			var account, group string
			if err = db.QueryRow(ctx, `INSERT INTO accounts(name,platform,auth_state)
				VALUES('receipt-synthetic',$1,'authenticated') RETURNING id::text`, platform).Scan(&account); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, account) })
			nativeGroup := strconv.FormatInt(time.Now().UnixNano(), 10)
			groupID, _ := strconv.ParseUint(nativeGroup, 10, 64)
			if err = db.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)
				VALUES($1,$2,'receipt-synthetic') RETURNING id::text`, platform, nativeGroup).Scan(&group); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				db.Exec(context.Background(), `DELETE FROM account_groups WHERE account_id=$1`, account)
				db.Exec(context.Background(), `DELETE FROM native_groups WHERE id=$1`, group)
			})
			if _, err = db.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
				VALUES($1,$2,'member',now(),'replay')`, account, group); err != nil {
				t.Fatal(err)
			}
			sent := time.Now().UTC().Truncate(time.Microsecond)
			var matchingEvent int64
			// More than the bounded result size arrives after the real receipt.
			// The real receipt must not disappear behind newer unrelated traffic.
			for i := range 132 {
				at := sent.Add(time.Duration(i+1) * time.Second)
				var event int64
				if err = db.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
					VALUES($1,$2,'group',$3,'\x01') RETURNING id`, account, strconv.Itoa(i), at).Scan(&event); err != nil {
					t.Fatal(err)
				}
				d := DecodedMessage{Platform: platform, NativeTarget: nativeGroup, NativeSender: "456", ServerID: "native-receipt"}
				d.Message.Session = 2
				d.Message.Target.ID = groupID
				d.Message.Sender.ID = 456
				d.Message.ClientID = "stable-client"
				if i > 0 {
					d.Message.ClientID = "unrelated-client"
				}
				raw, e := json.Marshal(d)
				if e != nil {
					t.Fatal(e)
				}
				sealed, e := vault.Seal(DecodedAAD(account, event), raw)
				if e != nil {
					t.Fatal(e)
				}
				if _, err = db.Exec(ctx, `INSERT INTO native_decoded_events
					(event_id,account_id,state,scene,conversation_key,deployment_version,payload_sealed,received_at)
					VALUES($1,$2,'decoded','group',$3,988,$4,$5)`, event, account, nativeGroup, sealed, at); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					matchingEvent = event
				}
			}
			receipt, err := manager.AssistantReceipt(ctx, platform, account, nativeGroup, "stable-client", "456", sent)
			if err != nil || receipt.EventID != matchingEvent || receipt.NativeID != "native-receipt" {
				t.Fatal("early native observation lost", receipt, err)
			}
			for _, target := range [][3]string{
				{nativeGroup, "another-client", "456"},
				{nativeGroup, "stable-client", "other-sender"},
				{"other-group", "stable-client", "456"},
			} {
				receipt, err := manager.AssistantReceipt(ctx, platform, account, target[0], target[1], target[2], sent)
				if err != nil || receipt.EventID != 0 {
					t.Fatal("unrelated receipt accepted", receipt, err)
				}
			}
		})
	}
}
