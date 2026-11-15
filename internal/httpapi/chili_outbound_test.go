package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

func TestChiliTextDurableIsolationAndOneAttempt(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{53}, 32)))
	service := &control.NativeTextService{DB: db, Vault: vault}
	for _, mode := range []string{"recipient_grant", "unowned_recipient_grant", "text", "quote", "mention", "private", "private_image_source",
		"private_unknown_source", "private_bad_projection_reason", "quote_cross_group",
		"mention_ungranted", "private_unowned", "source_changed", "cancelled", "unknown", "repeat", "idempotency_conflict"} {
		t.Run(mode, func(t *testing.T) {
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			for _, table := range []string{"operations", "test_windows", "inbox", "group_bindings", "account_groups", "native_groups", "accounts"} {
				exec("DELETE FROM " + table)
			}
			actor, tenant, account, peer, group, window := control.NewID(), control.NewID(), control.NewID(), control.NewID(), control.NewID(), control.NewID()
			token, _ := security.Token()
			ref, _ := control.WebsiteSession(token)
			exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,$2,'synthetic','superadmin')`, actor, actor)
			exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
				security.Digest(token), security.Digest("synthetic-csrf"), actor)
			exec(`INSERT INTO tenants(id,name) VALUES($1,'synthetic')`, tenant)
			for _, item := range []struct{ id, uid string }{{account, "self-string"}, {peer, "peer-string"}} {
				exec(`INSERT INTO accounts(id,name,platform,workspace,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
					VALUES($1,'synthetic','lajiaoliao','lajiaoliao',$2,'authenticated','\x01',true,true)`, item.id, item.uid)
			}
			snapshot, _ := json.Marshal(map[string]any{"state": "connected", "heartbeat_at": time.Now().UTC()})
			exec(`INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
				VALUES($1,true,'synthetic-owner',now()+interval '1 hour',now(),$2)`, account, snapshot)
			exec(`INSERT INTO native_groups(id,platform,native_id,name,isolated_test)
				VALUES($1,'lajiaoliao','group-string','synthetic',true)`, group)
			exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled)
				VALUES($1,$2,$3,false)`, group, tenant, account)
			exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
				VALUES($1,$2,'owner',now(),'native')`, account, group)
			if mode == "recipient_grant" || mode == "unowned_recipient_grant" {
				exec(`UPDATE accounts SET connection_enabled=false,testing_enabled=false WHERE id=$1`, peer)
				target := "peer-string"
				if mode == "unowned_recipient_grant" {
					target = "unowned-string"
				}
				body, _ := json.Marshal(map[string]any{"group_id": group, "account_ids": []string{account},
					"target_ids": []string{target}, "actions": []string{"send_text", "send_private_text"},
					"seconds": 300, "budget": 4, "confirm_isolated": true, "admission_mode": "native_debug"})
				request := httptest.NewRequest("POST", "/", bytes.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-Workspace", "lajiaoliao")
				server := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
				defer server.Native.Close()
				defer server.StopAssistant()
				err := server.createTestWindow(httptest.NewRecorder(), request, Principal{ID: actor, Role: "superadmin"})
				if mode == "unowned_recipient_grant" {
					var failure *apiError
					if !errors.As(err, &failure) || failure.code != "native_text_test_target_invalid" {
						t.Fatal("unowned recipient grant not rejected", err)
					}
					return
				}
				if err != nil {
					t.Fatal("ordinary owned recipient treated as executor", err)
				}
				var isolated bool
				if err := db.Platform.QueryRow(ctx, `SELECT w.account_ids=ARRAY[$1::uuid]
					AND w.account_epochs->>$2::text=a.epoch::text AND NOT a.testing_enabled AND NOT a.connection_enabled
					AND NOT b.management_enabled AND NOT b.ai_enabled FROM test_windows w
					JOIN accounts a ON a.id=$2 JOIN group_bindings b ON b.group_id=w.group_id
					WHERE w.actor_id=$3`, account, peer, actor).Scan(&isolated); err != nil || !isolated {
					t.Fatal("recipient acquired executor or production admission", isolated, err)
				}
				return
			}
			epochs, _ := json.Marshal(map[string]int64{account: 1, peer: 1})
			exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget,admission_mode)
				VALUES($1,$2,$3,$4::uuid[],ARRAY['self-string','peer-string'],ARRAY['send_text','send_private_text'],
				$5,now()+interval '5 minutes',4,'native_debug')`, window, actor, group, []string{account}, epochs)
			req := control.TextRequest{AccountID: account, GroupID: group, TestWindowID: window,
				AccountEpoch: 1, BindingEpoch: 1, IdempotencyKey: control.NewID(), Text: "synthetic outbound"}
			source := &sdkws.MsgData{ClientMsgID: "source-client", ServerMsgID: "source-server", SendID: "peer-string",
				RecvID: "group-string", GroupID: "group-string", SessionType: 3, MsgFrom: 100, ContentType: 101,
				Seq: 20, SendTime: time.Now().UnixMilli(), Content: []byte(`{"content":"quoted sample"}`)}
			scene := "group"
			conversation := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			if strings.HasPrefix(mode, "private") {
				source.GroupID, source.RecvID, source.SessionType = "", "self-string", 1
				scene = "private"
				req.PrivateConversation = conversation
			}
			if mode == "private_image_source" || mode == "private_bad_projection_reason" {
				source.ContentType, source.Content = 102, []byte(`{"sourcePicture":{"url":"https://lj-im-chat-01.lajiaoliao.com/synthetic.png"}}`)
			}
			if mode == "private_unknown_source" {
				source.ContentType, source.Content = 999, []byte(`{}`)
			}
			if mode == "quote_cross_group" {
				source.GroupID, source.RecvID = "another-group", "another-group"
			}
			if mode == "mention" || mode == "mention_ungranted" {
				req.ChiliMentionID = "peer-string"
				if mode == "mention_ungranted" {
					req.ChiliMentionID = "another-member"
				}
			}
			raw, _ := proto.Marshal(source)
			sealed, _ := vault.Seal("native-event:"+account+":synthetic-source", raw)
			var event int64
			if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
				VALUES($1,'synthetic-source',$2,now(),$3) RETURNING id`, account, scene, sealed).Scan(&event); err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,payload_sealed,
				deployment_version,codec_version,received_at,decoded_at,decode_ms)
				VALUES($1,$2,'decoded',$3,$4,'\x01',0,6,now(),now(),0)`, event, account, scene, conversation)
			if mode == "private_image_source" || mode == "private_unknown_source" || mode == "private_bad_projection_reason" {
				reason := "lajiaoliao_content_type"
				if mode == "private_bad_projection_reason" {
					reason = "lajiaoliao_session_type"
				}
				exec(`UPDATE native_decoded_events SET state='unsupported',reason=$2 WHERE event_id=$1`, event, reason)
				server := &Server{DB: db, Vault: vault}
				request := httptest.NewRequest("GET", "/", nil)
				request.SetPathValue("id", account)
				request.SetPathValue("conversation", conversation)
				request.Header.Set("X-Workspace", "lajiaoliao")
				request.Header.Set("Cookie", "wsl_session="+token)
				scope, err := server.conversationSendScope(request)
				if mode == "private_image_source" {
					if err != nil || scope.ChiliPeer != "peer-string" || scope.PrivateEventID != event {
						t.Fatal("image-only private conversation unavailable", scope, err)
					}
				} else if err == nil {
					t.Fatal("unrecognized private source accepted by HTTP scope", mode)
				}
			}
			if mode == "quote" || mode == "quote_cross_group" || mode == "source_changed" {
				req.QuoteEventID = event
			}
			if req.PrivateConversation != "" {
				req.PrivateEventID = event
			}
			if mode == "private_unowned" {
				exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, peer)
			}
			out, err := service.StageChiliMessage(ctx, ref, req)
			if mode == "quote_cross_group" || mode == "mention_ungranted" || mode == "private_unowned" ||
				mode == "private_unknown_source" || mode == "private_bad_projection_reason" {
				if err == nil {
					t.Fatal("invalid target reached durable send", out)
				}
				return
			}
			if err != nil || out.Status != "queued" {
				t.Fatal("stage", out, err)
			}
			if mode == "idempotency_conflict" {
				req.ChiliMentionID = "peer-string"
				if _, err = service.StageChiliMessage(ctx, ref, req); err == nil {
					t.Fatal("idempotency key accepted a different mention")
				}
				return
			}
			if mode == "source_changed" {
				source.Content = []byte(`{"content":"changed after approval"}`)
				raw, _ = proto.Marshal(source)
				sealed, _ = vault.Seal("native-event:"+account+":synthetic-source", raw)
				exec(`UPDATE native_received_events SET payload_sealed=$2 WHERE id=$1`, event, sealed)
			}
			port := &syntheticChiliMediaPort{unknownSend: mode == "unknown"}
			if mode == "cancelled" {
				port.beforeSend = func() { exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, window) }
			}
			result, err := service.DispatchChiliMedia(ctx, ref, out.ID, port)
			if mode == "source_changed" || mode == "cancelled" {
				if port.writes != 0 || err == nil && result.Status == "succeeded" {
					t.Fatal("revoked or changed source submitted", result, err, port.writes)
				}
				return
			}
			expected := "succeeded"
			if mode == "unknown" {
				expected = "unknown"
			}
			if err != nil || result.Status != expected || port.writes != 1 {
				t.Fatal("dispatch", result, err, port.writes)
			}
			if mode == "quote" && port.dispatch.Message.Quote == nil ||
				mode == "mention" && port.dispatch.Message.Mention == nil ||
				mode == "private" && port.dispatch.Peer != "peer-string" {
				t.Fatal("platform structure was lost", port.dispatch)
			}
			if _, err = service.DispatchChiliMedia(ctx, ref, out.ID, port); err != nil || port.writes != 1 {
				t.Fatal("existing operation was replayed", err, port.writes)
			}
		})
	}
}
