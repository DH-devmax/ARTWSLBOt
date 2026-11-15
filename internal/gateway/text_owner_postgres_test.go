package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

type syntheticTextReader struct {
	groups  []nativeauth.DirectoryGroup
	roster  nativeauth.MemberDirectory
	after   func()
	started time.Time
	friends []nativeauth.Friend
	uploads int
}

func (r *syntheticTextReader) Directory(context.Context, uint64) ([]nativeauth.DirectoryGroup, error) {
	r.started = time.Now()
	return r.groups, nil
}
func (r *syntheticTextReader) Members(context.Context, string) (nativeauth.MemberDirectory, error) {
	if r.after != nil {
		r.after()
	}
	return r.roster, nil
}
func (*syntheticTextReader) Close() {}
func (r *syntheticTextReader) Friends(context.Context) ([]nativeauth.Friend, error) {
	return r.friends, nil
}
func (r *syntheticTextReader) UploadMessage(ctx context.Context, input nativeauth.MessageUpload, guard func(context.Context) error) (nativewire.OutboundMedia, error) {
	if err := guard(ctx); err != nil {
		return nativewire.OutboundMedia{}, err
	}
	r.uploads++
	return nativewire.OutboundMedia{Kind: "image", Asset: "9007199254740993", Size: 100, Width: 8, Height: 8}, nil
}

func TestPostgresExistingConnectionTextAdapter(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{45}, 32)))
	var oldDeployment, oldProfile []byte
	var oldVersion, oldProfileVersion int64
	de := db.QueryRow(ctx, `SELECT sealed,version FROM native_deployment`).Scan(&oldDeployment, &oldVersion)
	pe := db.QueryRow(ctx, `SELECT sealed,version FROM native_im_profile`).Scan(&oldProfile, &oldProfileVersion)
	defer func() {
		if de == nil {
			_, _ = db.Exec(ctx, `UPDATE native_deployment SET sealed=$1,version=$2`, oldDeployment, oldVersion)
		} else {
			_, _ = db.Exec(ctx, `DELETE FROM native_deployment`)
		}
		if pe == nil {
			_, _ = db.Exec(ctx, `UPDATE native_im_profile SET sealed=$1,version=$2`, oldProfile, oldProfileVersion)
		} else {
			_, _ = db.Exec(ctx, `DELETE FROM native_im_profile`)
		}
	}()
	seal := func(aad string, value any) []byte {
		raw, _ := json.Marshal(value)
		out, err := vault.Seal(aad, raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	d := nativeauth.Deployment{ProtocolVersion: nativeauth.DesktopProtocolVersion,
		MessageKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))}
	for _, mode := range []string{"accepted", "ordinary_member", "unknown_role", "quoted", "quote_team", "quote_expired", "missing_group", "wrong_identity", "owner_replaced", "cancelled",
		"individually_muted", "foreign_mute", "mute_invalid", "private_text", "private_image", "private_bad_mapping",
		"private_not_friend", "private_expired", "private_replaced", "private_upload", "private_upload_no_friend"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := db.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,988)
                ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,version=988`, seal("native-deployment:v1", d)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `INSERT INTO native_im_profile(singleton,sealed) VALUES(true,$1)
                ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed`,
				seal("native-im-profile:v1", map[string]string{"app_key": "synthetic"})); err != nil {
				t.Fatal(err)
			}
			manager := NewReceiverManager(db, vault, nil)
			scheduler := NewReadScheduler(ctx)
			defer scheduler.Close()
			manager.reads = scheduler
			var account string
			if err := db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state) VALUES('synthetic-text-owner','authenticated') RETURNING id::text`).Scan(&account); err != nil {
				t.Fatal(err)
			}
			defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
			saved := savedNativeSession{DeploymentVersion: 988, Session: nativeauth.Session{UID: 7, NIMID: "7", ProtocolVersion: nativeauth.DesktopProtocolVersion}}
			if _, err := db.Exec(ctx, `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`,
				account, seal("native-session:"+account, saved)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
                VALUES($1,true,$2,now()+interval '1 minute')`, account, manager.owner); err != nil {
				t.Fatal(err)
			}
			var writes atomic.Int64
			transport := syntheticSender(t, func(conn *websocket.Conn, frame nativewire.Frame) {
				if strings.HasPrefix(mode, "private_") {
					fields, _, err := nativewire.ReadProperties(frame.Body, 0)
					if err != nil || frame.Service != 7 || frame.Command != 1 || string(fields[0]) != "0" ||
						string(fields[1]) != "peer-8" {
						t.Error("private message used group route")
					}
					message, err := nativewire.OpenMessage(bytes.Repeat([]byte{17}, 32), string(fields[10]))
					if err != nil || message.Session != 1 || message.Target.ID != 8 ||
						mode == "private_image" && message.Image == nil {
						t.Error("private image/text envelope lost", err)
					}
				}
				if mode == "quoted" {
					fields, _, err := nativewire.ReadProperties(frame.Body, 0)
					if err != nil || string(fields[29]) != "8" || string(fields[32]) != "9000" {
						t.Error("trusted quoted source did not reach transport")
					}
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(frame, syntheticReceipt(outboundClientID)))
			}, &writes, func(conn *websocket.Conn, frame nativewire.Frame) {
				body := append([]byte(nil), frame.Body...)
				body = append(body, 0)
				if mode == "individually_muted" || mode == "foreign_mute" {
					body[len(body)-1] = 1
					peer := "7"
					if mode == "foreign_mute" {
						peer = "8"
					}
					row, _ := nativewire.Properties([]nativewire.Property{{Key: 3, Value: []byte(peer)},
						{Key: 9, Value: []byte("1")}, {Key: 13, Value: []byte("1")}})
					body = append(body, row...)
				}
				if mode == "mute_invalid" {
					body = nil
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(frame, body))
			})
			ownerCtx, stop := context.WithCancel(ctx)
			defer stop()
			now := time.Now()
			connection := &ownedTextConnection{ctx: ownerCtx, transport: transport, uid: 7, sender: "7", version: 988}
			w := &receiverWorker{spec: receiverSpec{ID: account, UID: "7", Epoch: 1, Revision: 1},
				connection: connection, leaseUntil: now.Add(time.Minute), snapshot: ReceiverSnapshot{State: "connected", HeartbeatAt: &now}}
			manager.jobs[account] = w
			reader := &syntheticTextReader{groups: []nativeauth.DirectoryGroup{{ID: "123", Team: "789"}},
				roster: nativeauth.MemberDirectory{Group: "123", Complete: true, Members: []nativeauth.GroupMember{{ID: "7", Transport: "7", Role: "admin"}}}}
			switch mode {
			case "missing_group":
				reader.groups = nil
			case "wrong_identity":
				reader.roster.Members[0].Transport = "other"
			case "ordinary_member":
				reader.roster.Members[0].Role = "member"
			case "unknown_role":
				reader.roster.Members[0].Role = "unknown"
			case "owner_replaced":
				reader.after = func() { manager.mu.Lock(); delete(manager.jobs, account); manager.mu.Unlock() }
			case "cancelled":
				reader.after = stop
			}
			recorded := 0
			intent := TextIntent{AccountID: account, ReceiverOwner: manager.owner, AccountEpoch: 1, Revision: 1, Nonce: 100, AAD: 200,
				Message: nativewire.GroupText{SenderID: 7, GroupID: 123, ClientID: outboundClientID, Text: "synthetic", CreatedAt: now},
				RecordRoute: func(_ context.Context, route nativewire.TextRoute) error {
					recorded++
					if route.Sender != "7" || route.Team != "789" || len(route.Digest) != 32 {
						t.Error("wrong route evidence")
					}
					return nil
				}}
			if strings.HasPrefix(mode, "private_") {
				reader.roster.MappingComplete = true
				reader.roster.Members = append(reader.roster.Members, nativeauth.GroupMember{ID: "8", Transport: "peer-8", Role: "member"})
				reader.friends = []nativeauth.Friend{{ID: "8", Name: "synthetic-peer"}}
				intent.PrivatePeer = &nativewire.PrivateTarget{ID: 8, Transport: "peer-8"}
				intent.PrivateConversation = nativewire.PrivateConversationKey(*intent.PrivatePeer)
				original := nativewire.PrivateText{SenderID: 8, PeerID: 7, ClientID: "a391ad75-cbbc-4ce1-a333-b4b38be7e415",
					Text: "synthetic private source", CreatedAt: now}
				attachment, err := nativewire.SealPrivateText(bytes.Repeat([]byte{17}, 32), original, 300, 400)
				if err != nil {
					t.Fatal(err)
				}
				raw := nativewire.IMMessage{Scene: 0, Target: "7", Sender: "peer-8", ServerID: "8000",
					Fields: map[uint32]string{0: "0", 1: "7", 2: "peer-8", 7: "1700000000123", 8: "100",
						10: attachment, 11: original.ClientID, 12: "8000"}}
				if err := db.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
                    VALUES($1,'synthetic-private','private',now(),$2) RETURNING id`, account,
					seal("native-event:"+account+":synthetic-private", raw)).Scan(&intent.PrivateEventID); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
                    deployment_version,payload_sealed,received_at) VALUES($1,$2,'decoded','private',$3,988,'\x01',now())`,
					intent.PrivateEventID, account, intent.PrivateConversation); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "private_image":
					intent.Message.Text = ""
					intent.Message.Media = &nativewire.OutboundMedia{Kind: "image", Asset: "9007199254740993", Size: 100, Width: 8, Height: 8}
				case "private_bad_mapping":
					reader.roster.Members[1].Transport = "different-peer"
				case "private_not_friend", "private_upload_no_friend":
					reader.friends = nil
				case "private_expired":
					if _, err := db.Exec(ctx, `UPDATE native_received_events SET received_at=now()-interval '8 days' WHERE id=$1`, intent.PrivateEventID); err != nil {
						t.Fatal(err)
					}
				case "private_replaced":
					reader.after = stop
				}
			}
			if mode == "quoted" || mode == "quote_team" || mode == "quote_expired" {
				reader.roster.Members = append(reader.roster.Members, nativeauth.GroupMember{ID: "8", Transport: "8", Role: "member"})
				original := nativewire.GroupText{SenderID: 8, GroupID: 123, ClientID: "a391ad75-cbbc-4ce1-a333-b4b38be7e415",
					Text: "synthetic original quote", CreatedAt: now}
				attachment, err := nativewire.SealGroupText(bytes.Repeat([]byte{17}, 32), original, 300, 400)
				if err != nil {
					t.Fatal(err)
				}
				team := "789"
				if mode == "quote_team" {
					team = "790"
				}
				raw := nativewire.IMMessage{Scene: 1, Target: team, Sender: "8", ServerID: "9000",
					Fields: map[uint32]string{0: "1", 1: team, 2: "8", 7: "1700000000123", 8: "100",
						10: attachment, 11: original.ClientID, 12: "9000"}}
				if err := db.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
                    VALUES($1,'synthetic-quote','group',now(),$2) RETURNING id`,
					account, seal("native-event:"+account+":synthetic-quote", raw)).Scan(&intent.QuoteEventID); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,
                    deployment_version,payload_sealed,received_at) VALUES($1,$2,'decoded','group',988,'\x01',now())`,
					intent.QuoteEventID, account); err != nil {
					t.Fatal(err)
				}
				intent.Message.Quote = &nativewire.MessageQuote{ClientID: original.ClientID, ServerID: "9000", Text: original.Text}
				intent.Message.Text = "@member-card reply"
				intent.Message.Mentions = []nativewire.MessageMention{{Start: 0, End: 13, UID: 8, Name: "member-card"}}
				reader.roster.Members[1].Card = "member-card"
				if mode == "quote_expired" {
					if _, err := db.Exec(ctx, `UPDATE native_received_events SET received_at=now()-interval '8 days' WHERE id=$1`,
						intent.QuoteEventID); err != nil {
						t.Fatal(err)
					}
				}
			}
			factory := func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (textReadClient, error) {
				return reader, nil
			}
			if mode == "private_upload" || mode == "private_upload_no_friend" {
				calls := 0
				out, err := manager.uploadMessage(ctx, MediaUploadIntent{AccountID: account, ReceiverOwner: manager.owner,
					AccountEpoch: 1, Revision: 1, Sender: 7, Group: 123, Operation: outboundClientID,
					PrivatePeer: intent.PrivatePeer, PrivateConversation: intent.PrivateConversation, PrivateEventID: intent.PrivateEventID,
					Input: nativeauth.MessageUpload{Kind: "image"}}, func(context.Context) error { calls++; return nil },
					func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (messageUploadClient, error) {
						return reader, nil
					})
				if mode == "private_upload" {
					if err != nil || out.Kind != "image" || calls != 1 || reader.uploads != 1 {
						t.Fatal("private upload did not use scoped lane", out, err, calls)
					}
				} else if err == nil || calls != 0 || reader.uploads != 0 {
					t.Fatal("unverified recipient uploaded", err)
				}
				return
			}
			out, err := manager.submitText(ctx, intent, allowSynthetic, factory)
			if mode == "accepted" || mode == "ordinary_member" || mode == "quoted" || mode == "private_text" || mode == "private_image" {
				if err != nil || !out.Attempted || out.Receipt.ServerID != "9001" || writes.Load() != 1 || recorded != 1 {
					t.Fatal(out, err, writes.Load(), recorded)
				}
				if _, err := transport.heartbeat(ctx); err != nil {
					t.Fatal("existing connection lost after text", err)
				}
			} else if err == nil || out.Attempted || writes.Load() != 0 {
				t.Fatal("invalid routing wrote a native packet", mode, err)
			}
			if mode == "accepted" || mode == "ordinary_member" || mode == "individually_muted" {
				evidence, err := manager.observeRole(ctx, account, "123", factory)
				if err != nil || evidence.AccountID != account || evidence.Sender != "7" || evidence.Team != "789" ||
					evidence.AccountEpoch != 1 || evidence.ReceiverRevision != 1 || evidence.DeploymentVersion != 988 ||
					evidence.IndividualMuted != (mode == "individually_muted") || evidence.Role != reader.roster.Members[0].Role ||
					evidence.ObservedAt.After(reader.started) {
					t.Fatal("role observation lost current or negative evidence", evidence, err)
				}
			}
		})
	}
}
