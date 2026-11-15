package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

type syntheticTextPort struct {
	before     func()
	write      func()
	mode       string
	calls      atomic.Int64
	attachment string
	message    nativewire.GroupText
}

type syntheticUploadPort struct {
	before func()
	mode   string
	calls  int
}

func (p *syntheticUploadPort) UploadMedia(ctx context.Context, d control.MediaUploadDispatch,
	guard func(context.Context) error) (nativewire.OutboundMedia, error) {
	if p.before != nil {
		p.before()
	}
	if err := guard(ctx); err != nil {
		return nativewire.OutboundMedia{}, err
	}
	p.calls++
	if p.mode == "unknown" {
		return nativewire.OutboundMedia{}, errors.New("synthetic-upload-disconnect")
	}
	meta, err := nativeauth.InspectUpload(d.Input)
	meta.Asset = "9007199254740993"
	return meta, err
}

func TestNativeTextFoundationDoesNotOpenProductionAPI(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"group_id": control.NewID(), "account_id": control.NewID(),
		"kind": "send_text", "text": "synthetic"})
	req := httptest.NewRequest("POST", "/api/v1/operations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", control.NewID())
	err := (&Server{}).createOperation(httptest.NewRecorder(), req, Principal{})
	var failure *apiError
	if !errors.As(err, &failure) || failure.code != "native_compatibility_not_verified" || failure.status != 409 {
		t.Fatal("text foundation opened a production effect", err)
	}
	for _, handler := range []func(http.ResponseWriter, *http.Request, Principal) error{
		(&Server{}).uploadConversationMedia, (&Server{}).stageNativeMedia,
	} {
		err = handler(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), Principal{})
		if !errors.As(err, &failure) || failure.code != "native_media_acceptance_pending" || failure.status != 409 {
			t.Fatal("media foundation opened a production effect", err)
		}
	}
}

func (p *syntheticTextPort) SubmitText(ctx context.Context, d control.TextDispatch, fence nativewire.SubmissionFence) (nativewire.TextSendResult, error) {
	p.calls.Add(1)
	p.message = d.Message
	if p.before != nil {
		p.before()
	}
	// Only synthetic key/payload; no native endpoint, credentials or login.
	var sealed string
	var err error
	if d.PrivatePeer != nil {
		sealed, err = nativewire.SealPrivateText(bytes.Repeat([]byte{17}, 32), nativewire.PrivateText{
			SenderID: d.Message.SenderID, PeerID: d.PrivatePeer.ID, ClientID: d.Message.ClientID,
			Text: d.Message.Text, CreatedAt: d.Message.CreatedAt, Media: d.Message.Media,
		}, d.Nonce, d.AAD)
	} else {
		sealed, err = nativewire.SealGroupText(bytes.Repeat([]byte{17}, 32), d.Message, d.Nonce, d.AAD)
	}
	if err != nil {
		return nativewire.TextSendResult{}, err
	}
	p.attachment = sealed
	decoded, err := nativewire.OpenMessage(bytes.Repeat([]byte{17}, 32), sealed)
	if err != nil || decoded.ClientID != d.Message.ClientID {
		return nativewire.TextSendResult{}, errors.New("synthetic_vector_failed")
	}
	result := nativewire.TextSendResult{}
	hash := sha256.Sum256([]byte(sealed))
	route := nativewire.TextRoute{Team: "789", Sender: "synthetic-me",
		DeploymentVersion: 77, Digest: hash[:], ObservedAt: time.Now().UTC()}
	if d.PrivatePeer != nil {
		route.PrivatePeer = d.PrivatePeer.Transport
	}
	if err = d.RecordRoute(ctx, route); err != nil {
		return result, err
	}
	err = fence(ctx, func(context.Context) error {
		result.Attempted = true
		if p.write != nil {
			p.write()
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if p.mode == "unknown" {
		return result, errors.New("synthetic_receipt_lost")
	}
	result.Code = 200
	result.Receipt = nativewire.SendReceipt{ClientID: d.Message.ClientID, ServerID: "9001", TimeMS: "1700000000123"}
	if p.mode == "rejected" {
		result.Code, result.Receipt = 403, nativewire.SendReceipt{}
	}
	if p.mode == "mismatch" {
		result.Receipt.ClientID = control.NewID()
	}
	return result, nil
}

func TestNativeTextFoundationSyntheticControl(t *testing.T) {
	if !nativewire.Available() {
		t.Skip("synthetic send fixture requires sodium")
	}
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{43}, 32)))
	service := &control.NativeTextService{DB: db, Vault: vault}
	if _, err := db.Platform.Exec(ctx, `INSERT INTO native_deployment(singleton,sealed,version)
        VALUES(true,$1,77) ON CONFLICT(singleton) DO UPDATE SET sealed=excluded.sealed,version=77`,
		[]byte("synthetic-deployment-placeholder")); err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		req   control.TextRequest
		ref   control.SessionRef
		actor string
		token string
	}
	seed := func(preserve ...bool) fixture {
		t.Helper()
		// Each case uses a fresh account. Remove only this test's previous
		// account graph, so fixtures obey the production ten-account pool.
		if len(preserve) == 0 {
			for _, table := range []string{"operations", "test_windows", "inbox", "group_bindings", "account_groups", "accounts"} {
				if _, err := db.Platform.Exec(ctx, `DELETE FROM `+table); err != nil {
					t.Fatal(err)
				}
			}
		}
		f := fixture{}
		tenant, plan := control.NewID(), control.NewID()
		f.actor, f.req.AccountID, f.req.GroupID, f.req.TestWindowID = control.NewID(), control.NewID(), control.NewID(), control.NewID()
		f.req.AccountEpoch, f.req.BindingEpoch = 1, 1
		f.req.Text, f.req.IdempotencyKey = "synthetic bounded native text", control.NewID()
		f.token, _ = security.Token()
		f.ref, _ = control.WebsiteSession(f.token)
		tx, err := db.Platform.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := tx.Exec(ctx, sql, args...); err != nil {
				t.Fatal("synthetic fixture failed", err)
			}
		}
		exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,$2,'synthetic','superadmin')`, f.actor, f.actor)
		exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
			security.Digest(f.token), security.Digest("synthetic-csrf"), f.actor)
		exec(`INSERT INTO tenants(id,name) VALUES($1,'synthetic')`, tenant)
		exec(`INSERT INTO plans(id,name,group_limit,ai_group_limit,modules) VALUES($1,'synthetic',100,0,ARRAY['moderation'])`, plan)
		exec(`INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, tenant, plan)
		// IDs only need to be canonical, unique and within the reviewed 30-bit
		// application header. They are never actual provider account identities.
		var uid, gid string
		if err := tx.QueryRow(ctx, `SELECT (100000+count(*))::text FROM accounts`).Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, `SELECT (200000+count(*))::text FROM native_groups`).Scan(&gid); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO accounts(id,name,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
            VALUES($1,'synthetic',$2,'authenticated',$3,true,true)`, f.req.AccountID, uid, []byte("synthetic-sealed-placeholder"))
		snapshot, _ := json.Marshal(map[string]any{"state": "connected", "heartbeat_at": time.Now().UTC()})
		exec(`INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
            VALUES($1,true,'synthetic-owner',now()+interval '1 hour',now(),$2)`, f.req.AccountID, snapshot)
		exec(`INSERT INTO native_groups(id,native_id,name,isolated_test) VALUES($1,$2,'synthetic',true)`, f.req.GroupID, gid)
		exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled) VALUES($1,$2,$3,true)`,
			f.req.GroupID, tenant, f.req.AccountID)
		exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
            VALUES($1,$2,'admin',now(),'native')`, f.req.AccountID, f.req.GroupID)
		epochs, _ := json.Marshal(map[string]int64{f.req.AccountID: 1})
		exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget)
            VALUES($1,$2,$3,$4::uuid[],'{}',ARRAY['send_text'],$5,now()+interval '5 minutes',1)`,
			f.req.TestWindowID, f.actor, f.req.GroupID, []string{f.req.AccountID}, epochs)
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return f
	}
	t.Run("owned_member_debug_text_does_not_require_primary_or_package", func(t *testing.T) {
		f := seed()
		primary := seed(true)
		for _, statement := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE group_bindings SET primary_account_id=$2,management_enabled=false WHERE group_id=$1`,
				[]any{f.req.GroupID, primary.req.AccountID}},
			{`UPDATE account_groups SET role='member' WHERE account_id=$1`, []any{f.req.AccountID}},
			{`UPDATE test_windows SET admission_mode='native_debug',actions=ARRAY['send_text'] WHERE id=$1`, []any{f.req.TestWindowID}},
			{`UPDATE subscriptions SET enabled=false`, nil},
		} {
			if _, err := db.Platform.Exec(ctx, statement.sql, statement.args...); err != nil {
				t.Fatal(err)
			}
		}
		op, err := service.Stage(ctx, f.ref, f.req)
		if err != nil {
			t.Fatal("explicit owned member test could not stage", err)
		}
		port := &syntheticTextPort{}
		sent, err := service.Dispatch(ctx, f.ref, op.ID, port)
		if err != nil || sent.Status != "succeeded" {
			t.Fatal("owned member text failed", sent, err)
		}
		if _, err = db.Platform.Exec(ctx, `UPDATE test_windows SET admission_mode='package',budget=2 WHERE id=$1`, f.req.TestWindowID); err != nil {
			t.Fatal(err)
		}
		f.req.IdempotencyKey = control.NewID()
		if _, err = service.Stage(ctx, f.ref, f.req); err == nil {
			t.Fatal("debug member authorization leaked into package sending")
		}
	})
	t.Run("debug_send_and_recall_keep_independent_final_fences", func(t *testing.T) {
		f := seed()
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`UPDATE subscriptions SET enabled=false`)
		exec(`UPDATE group_bindings SET management_enabled=false`)
		exec(`UPDATE test_windows SET admission_mode='native_debug',actions=ARRAY['send_text','recall_message'],
			target_ids=ARRAY['100000'],budget=5 WHERE id=$1`, f.req.TestWindowID)
		first, err := service.Stage(ctx, f.ref, f.req)
		if err != nil {
			t.Fatal("debug send required commercial activation", err)
		}
		port := &syntheticTextPort{}
		sent, err := service.Dispatch(ctx, f.ref, first.ID, port)
		if err != nil || sent.Status != "succeeded" || port.calls.Load() != 1 {
			t.Fatal("debug send did not use durable dispatch", sent, err)
		}
		if _, err = service.Dispatch(ctx, f.ref, first.ID, port); err != nil || port.calls.Load() != 1 {
			t.Fatal("debug dispatch repeated the socket write", err)
		}
		exec(`DELETE FROM operations WHERE id=$1`, first.ID)
		settings := &control.NativeSettingsService{DB: db, Vault: vault}
		reference := nativeauth.MessageReference{Platform: "wsl", EventID: 1, Group: "200000", Sender: "100000",
			SourceDigest: strings.Repeat("a", 64), CreatedAt: time.Now().UnixMilli(),
			Route: nativewire.ReplyRoute{Sender: "synthetic-me", Target: "789", TimeMS: "1700000000123",
				ServerID: "9001", ClientID: control.NewID()}}
		var gid string
		if err = db.Platform.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1`, f.req.GroupID).Scan(&gid); err != nil {
			t.Fatal(err)
		}
		reference.Group = gid
		encoded, _ := json.Marshal(reference)
		req := control.SettingRequest{Account: f.req.AccountID, Workspace: "wsl", TestWindowID: f.req.TestWindowID,
			Key: control.NewID(), Digest: strings.Repeat("b", 64), Epoch: 1, Version: 1,
			ReceiverOwner: "synthetic-owner",
			Change:        nativeauth.SettingChange{Kind: "recall_message", Group: gid, Text: string(encoded)}}
		if err = db.Platform.QueryRow(ctx, `SELECT revision FROM account_receivers WHERE account_id=$1`,
			req.Account).Scan(&req.ReceiverRevision); err != nil {
			t.Fatal(err)
		}
		calls, role := 0, "member"
		recallPort := func(work context.Context, fence control.SettingFence) error {
			proof := control.SettingEvidence{UID: "100000", Role: role, Target: "100000", TargetRole: "owner",
				Epoch: 1, Version: 1, DeploymentVersion: 77, Digest: req.Digest,
				CredentialDigest: sha256.Sum256([]byte("synthetic-sealed-placeholder")), ObservedAt: time.Now()}
			return fence(work, proof, func(context.Context) error { calls++; return nil })
		}
		if _, err = settings.Execute(ctx, f.ref, req, recallPort); err == nil {
			t.Fatal("ordinary settings endpoint inherited a test grant")
		}
		out, err := settings.ExecuteTestRecall(ctx, f.ref, req, recallPort)
		if err != nil || out.Status != "failed" || calls != 0 {
			t.Fatal("recall skipped the final native role check", out, err)
		}
		role, req.Key = "owner", control.NewID()
		out, err = settings.ExecuteTestRecall(ctx, f.ref, req, recallPort)
		if err != nil || out.Status != "succeeded" || calls != 1 {
			t.Fatal("controlled recall failed", out, err)
		}
		if _, err = settings.ExecuteTestRecall(ctx, f.ref, req, recallPort); err != nil || calls != 1 {
			t.Fatal("recall was replayed", err)
		}
		req.Key = control.NewID()
		exec(`UPDATE group_bindings SET epoch=epoch+1`)
		out, err = settings.ExecuteTestRecall(ctx, f.ref, req, func(work context.Context, fence control.SettingFence) error {
			exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, req.TestWindowID)
			return recallPort(work, fence)
		})
		if err != nil || out.Status != "failed" || calls != 1 {
			t.Fatal("cancelled recall reached the transport", out, err)
		}
	})
	t.Run("debug_upload_cannot_authorize_message_sending", func(t *testing.T) {
		for _, mode := range []string{"valid", "wrong_action", "cancelled", "epoch_changed", "binding_changed",
			"expired", "exhausted", "revoke_at_submission", "unknown"} {
			t.Run(mode, func(t *testing.T) {
				f := seed()
				exec := func(sql string, args ...any) {
					t.Helper()
					if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
						t.Fatal(err)
					}
				}
				exec(`UPDATE test_windows SET admission_mode='native_debug',actions=ARRAY['upload_image'],budget=1 WHERE id=$1`, f.req.TestWindowID)
				exec(`UPDATE group_bindings SET management_enabled=false WHERE group_id=$1`, f.req.GroupID)
				exec(`UPDATE subscriptions SET enabled=false`)
				switch mode {
				case "wrong_action":
					exec(`UPDATE test_windows SET actions=ARRAY['upload_voice'] WHERE id=$1`, f.req.TestWindowID)
				case "cancelled":
					exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, f.req.TestWindowID)
				case "epoch_changed":
					exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.req.AccountID)
				case "binding_changed":
					exec(`UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, f.req.GroupID)
				case "expired":
					exec(`UPDATE test_windows SET created_at=now()-interval '1 minute',
						expires_at=now()-interval '1 second' WHERE id=$1`, f.req.TestWindowID)
				case "exhausted":
					exec(`UPDATE test_windows SET used=budget WHERE id=$1`, f.req.TestWindowID)
				}
				f.req.Text = ""
				input := nativeauth.MessageUpload{Kind: "image", Name: "synthetic.png", Data: syntheticPrivatePNG(t)}
				port := &syntheticUploadPort{}
				if mode == "unknown" {
					port.mode = "unknown"
				}
				if mode == "revoke_at_submission" {
					port.before = func() { exec(`DELETE FROM sessions WHERE user_id=$1`, f.actor) }
				}
				uploaded, err := service.UploadMedia(ctx, f.ref, f.req, input, port)
				if mode != "valid" && mode != "unknown" {
					if err == nil && uploaded.Status != "failed" || port.calls != 0 {
						t.Fatalf("invalid debug upload reached transport: %s %v %+v", mode, err, uploaded)
					}
					return
				}
				expected := "succeeded"
				if mode == "unknown" {
					expected = "unknown"
				}
				if err != nil || uploaded.Status != expected || port.calls != 1 {
					t.Fatalf("debug upload: %v %+v calls=%d", err, uploaded, port.calls)
				}
				inspected, err := service.InspectMediaUpload(ctx, f.ref, f.req.AccountID, uploaded.ID, "")
				if err != nil || inspected.ID != uploaded.ID || inspected.Status != expected || port.calls != 1 {
					t.Fatal("inspection retried or lost upload", err)
				}
				sendReq := f.req
				sendReq.IdempotencyKey = control.NewID()
				sendReq.MediaUploadID = uploaded.ID
				if _, err = service.Stage(ctx, f.ref, sendReq); err == nil {
					t.Fatal("upload-only grant staged a message")
				}
				var used, sends int
				if err = db.Platform.QueryRow(ctx, `SELECT used,(SELECT count(*) FROM operations WHERE account_id=$2)
					FROM test_windows WHERE id=$1`, f.req.TestWindowID, f.req.AccountID).Scan(&used, &sends); err != nil {
					t.Fatal(err)
				}
				if used != 1 || sends != 0 {
					t.Fatalf("upload budget or sending isolation failed: used=%d sends=%d", used, sends)
				}
			})
		}
	})
	t.Run("private_messages_share_intents_and_receipts", func(t *testing.T) {
		for _, admission := range []string{"package", "native_debug"} {
			for _, mode := range []string{"text", "image", "image_unknown", "unapproved", "legacy_group_grant", "upload_only", "expired", "future", "wrong_account", "bad_mapping",
				"revoke_target", "revoke_source", "revoke_session", "binding_changed", "cancelled", "unknown",
				"group_echo", "other_peer_echo", "image_reuse", "private_voice", "restart", "maintenance",
				"http_text", "http_image", "http_scope_forgery"} {
				t.Run(admission+"/"+mode, func(t *testing.T) {
					f := seed()
					seedPrivateConversation(t, db, vault, &f.req, "synthetic-peer-8")
					exec := func(sql string, args ...any) {
						t.Helper()
						if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
							t.Fatal(err)
						}
					}
					exec(`UPDATE test_windows SET target_ids=ARRAY['8'],actions=ARRAY['send_private_text','send_private_image'],budget=4 WHERE id=$1`, f.req.TestWindowID)
					if admission == "native_debug" {
						exec(`UPDATE test_windows SET admission_mode='native_debug',
						actions=array_append(actions,'upload_private_image') WHERE id=$1`, f.req.TestWindowID)
						exec(`UPDATE group_bindings SET management_enabled=false,ai_enabled=false WHERE group_id=$1`, f.req.GroupID)
						exec(`DELETE FROM subscriptions`)
					}
					switch mode {
					case "unapproved":
						exec(`UPDATE test_windows SET target_ids='{}' WHERE id=$1`, f.req.TestWindowID)
					case "legacy_group_grant":
						exec(`UPDATE test_windows SET actions=ARRAY['send_text','send_image'] WHERE id=$1`, f.req.TestWindowID)
					case "upload_only":
						exec(`UPDATE test_windows SET actions=ARRAY['upload_private_image'] WHERE id=$1`, f.req.TestWindowID)
					case "expired":
						exec(`UPDATE native_decoded_events SET received_at=now()-interval '8 days' WHERE event_id=$1`, f.req.PrivateEventID)
					case "future":
						exec(`UPDATE native_received_events SET received_at=now()+interval '1 hour' WHERE id=$1`, f.req.PrivateEventID)
					case "wrong_account":
						f.req.PrivateEventID++
					case "bad_mapping":
						f.req.PrivateConversation = strings.Repeat("f", 64)
					}
					sender := &syntheticTextPort{}
					uploader := &syntheticUploadPort{}
					isImage := mode == "image" || mode == "image_unknown" || mode == "image_reuse" || mode == "http_image"
					if strings.HasPrefix(mode, "http_") {
						server := &Server{DB: db, Vault: vault, nativeTextTestPort: sender, nativeMediaTestPort: uploader,
							mediaSlots: make(chan struct{}, 2)}
						operation := privateConversationHTTP(t, server, f.token, f.actor, f.req, mode)
						if mode == "http_scope_forgery" {
							return
						}
						if operation.Status != "succeeded" || sender.calls.Load() != 1 {
							t.Fatal("private HTTP did not use common executor", operation)
						}
						if isImage && uploader.calls != 1 {
							t.Fatal("private image bypassed upload")
						}
						return
					}
					if isImage || mode == "private_voice" {
						req := f.req
						req.Text = ""
						input := nativeauth.MessageUpload{Kind: "image", Name: "synthetic.png", Data: syntheticPrivatePNG(t)}
						if mode == "private_voice" {
							req.MediaKind = "voice"
							// The request must fail before any upload, regardless of its body.
							input.Kind = "file"
						}
						uploaded, err := service.UploadMedia(ctx, f.ref, req, input, uploader)
						if mode == "private_voice" {
							if err == nil || uploader.calls != 0 {
								t.Fatal("unsupported private media uploaded")
							}
							return
						}
						if err != nil || uploaded.Status != "succeeded" {
							t.Fatal(uploaded, err)
						}
						f.req.Text, f.req.MediaUploadID = "", uploaded.ID
						f.req.IdempotencyKey = control.NewID()
						if mode == "image_reuse" {
							other := f.req
							other.PrivateConversation, other.PrivateEventID = "", 0
							if _, err := service.Stage(ctx, f.ref, other); err == nil {
								t.Fatal("private upload reused in group")
							}
						}
					}
					op, err := service.Stage(ctx, f.ref, f.req)
					if slices.Contains([]string{"unapproved", "legacy_group_grant", "upload_only", "expired", "future", "wrong_account", "bad_mapping"}, mode) {
						if err == nil {
							t.Fatal("unsafe private source staged", mode)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					before := func() {
						switch mode {
						case "revoke_target":
							exec(`UPDATE test_windows SET target_ids='{}' WHERE id=$1`, f.req.TestWindowID)
						case "revoke_source":
							exec(`UPDATE native_decoded_events SET payload_sealed='\x01' WHERE event_id=$1`, f.req.PrivateEventID)
						case "revoke_session":
							exec(`DELETE FROM sessions WHERE user_id=$1`, f.actor)
						case "binding_changed":
							exec(`UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, f.req.GroupID)
						case "cancelled":
							exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, f.req.TestWindowID)
						}
					}
					sender.before = before
					if slices.Contains([]string{"unknown", "image_unknown", "group_echo", "other_peer_echo", "restart", "maintenance"}, mode) {
						sender.mode = "unknown"
					}
					result, err := service.Dispatch(ctx, f.ref, op.ID, sender)
					if err != nil {
						t.Fatal(err)
					}
					if slices.Contains([]string{"revoke_target", "revoke_source", "revoke_session", "binding_changed", "cancelled"}, mode) {
						if result.Status != "failed" {
							t.Fatal("revoked private send crossed fence", result)
						}
						return
					}
					decoded, err := nativewire.OpenMessage(bytes.Repeat([]byte{17}, 32), sender.attachment)
					if err != nil || decoded.Session != 1 || decoded.Target.ID != 8 ||
						isImage && (decoded.Image == nil || decoded.Format != 1) {
						t.Fatal("private destination or content lost", decoded, err)
					}
					if sender.mode == "unknown" {
						if result.Status != "unknown" {
							t.Fatal(result)
						}
						if mode == "restart" || mode == "maintenance" {
							exec(`UPDATE operations SET status='running',expires_at=now()-interval '1 hour' WHERE id=$1`, op.ID)
							if mode == "restart" {
								err = service.RecoverInterrupted(ctx)
							} else {
								err = service.Expire(ctx)
							}
							if err != nil {
								t.Fatal(err)
							}
							var state string
							var recovered, leaked int
							if err = db.Platform.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1`,
								op.ID).Scan(&state); err != nil || state != "unknown" {
								t.Fatal("private recovery lost unknown state", state, err)
							}
							if err = db.Platform.QueryRow(ctx, `SELECT count(*),count(tenant_id)
							FROM audit_events WHERE resource_id=$1 AND action='native_text_recovered'`,
								op.ID).Scan(&recovered, &leaked); err != nil || recovered != 1 || leaked != 0 {
								t.Fatal("private recovery audit leaked tenant scope", recovered, leaked, err)
							}
						} else {
							insertPrivateEcho(t, db, vault, f.req.AccountID, sender.attachment, decoded.ClientID, mode)
							receiver := gateway.NewReceiverManager(db.Platform, vault, nil)
							checked, err := service.Reconcile(ctx, f.ref, op.ID, receiver)
							if err != nil || checked.Status != map[bool]string{true: "succeeded", false: "unknown"}[mode == "unknown" || mode == "image_unknown"] {
								t.Fatal("private echo crossed destination/scene", checked, err)
							}
						}
					} else if result.Status != "succeeded" {
						t.Fatal(result)
					}
					if _, err := service.Dispatch(ctx, f.ref, op.ID, sender); err != nil || sender.calls.Load() != 1 {
						t.Fatal("private operation resent", err)
					}
					if _, err := service.Inspect(ctx, f.ref, f.req.AccountID, f.req.GroupID, op.ID, ""); err == nil {
						t.Fatal("private operation exposed as group send")
					}
					if _, err := service.InspectConversation(ctx, f.ref, f.req.AccountID, strings.Repeat("e", 64), op.ID, ""); err == nil {
						t.Fatal("private status crossed conversation")
					}
					if mode == "text" {
						var tenant string
						if err := db.Platform.QueryRow(ctx, `SELECT tenant_id::text FROM group_bindings WHERE group_id=$1`,
							f.req.GroupID).Scan(&tenant); err != nil {
							t.Fatal(err)
						}
						member := control.NewID()
						exec(`INSERT INTO users(id,username,password_hash,role,tenant_id)
						VALUES($1::uuid,$1::text,'synthetic','user',$2)`, member, tenant)
						exec(`INSERT INTO member_groups(user_id,tenant_id,group_id) VALUES($1,$2,$3)`,
							member, tenant, f.req.GroupID)
						if err := db.ActorTenantTx(ctx, tenant, member, func(tx pgx.Tx) error {
							var groups, operations int
							if err := tx.QueryRow(ctx, `SELECT count(*) FROM group_bindings WHERE group_id=$1`,
								f.req.GroupID).Scan(&groups); err != nil {
								return err
							}
							if err := tx.QueryRow(ctx, `SELECT count(*) FROM operations WHERE id=$1`,
								op.ID).Scan(&operations); err != nil {
								return err
							}
							if groups != 1 || operations != 0 {
								t.Fatalf("private operation leaked into authorized tenant group: groups=%d operations=%d", groups, operations)
							}
							return nil
						}); err != nil {
							t.Fatal(err)
						}
					}
					exec(`UPDATE native_decoded_events SET received_at=now()-interval '8 days' WHERE event_id=$1`, f.req.PrivateEventID)
					if _, err := service.InspectAccount(ctx, f.ref, f.req.AccountID, op.ID, ""); err != nil {
						t.Fatal("expiry hid minimal recovery status", err)
					}
				})
			}
		}
	})
	t.Run("media_multipart_is_bounded_and_server_scoped", func(t *testing.T) {
		for _, mode := range []string{"valid", "duplicate", "trusted_field", "extra_part", "oversized"} {
			t.Run(mode, func(t *testing.T) {
				f := seed()
				if _, err := db.Platform.Exec(ctx, `UPDATE test_windows SET actions=ARRAY['send_image'],budget=4 WHERE id=$1`, f.req.TestWindowID); err != nil {
					t.Fatal(err)
				}
				var nativeGroup uint64
				if err := db.Platform.QueryRow(ctx, `SELECT native_id::bigint FROM native_groups WHERE id=$1`, f.req.GroupID).Scan(&nativeGroup); err != nil {
					t.Fatal(err)
				}
				key := strings.Repeat("d", 64)
				var event int64
				if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
                    VALUES($1,'synthetic-upload-conversation','group',now(),'\x01') RETURNING id`, f.req.AccountID).Scan(&event); err != nil {
					t.Fatal(err)
				}
				body, _ := json.Marshal(gateway.DecodedMessage{Message: nativewire.ApplicationMessage{
					Target: nativewire.MessageSource{ID: nativeGroup}, Session: 2,
				}})
				sealed, _ := vault.Seal(gateway.DecodedAAD(f.req.AccountID, event), body)
				if _, err := db.Platform.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
                    deployment_version,payload_sealed,received_at,decoded_at,decode_ms)
                    VALUES($1,$2,'decoded','group',$3,77,$4,now(),now(),0)`, event, f.req.AccountID, key, sealed); err != nil {
					t.Fatal(err)
				}
				s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
				defer s.Native.Close()
				defer s.StopAssistant()
				port := &syntheticUploadPort{}
				s.nativeMediaTestPort = port
				var data bytes.Buffer
				form := multipart.NewWriter(&data)
				input := map[string]any{"kind": "image", "test_window_id": f.req.TestWindowID, "account_epoch": 1, "binding_epoch": 1}
				if mode == "trusted_field" {
					input["tenant_id"] = control.NewID()
				}
				request, _ := json.Marshal(input)
				form.WriteField("request", string(request))
				if mode == "duplicate" {
					form.WriteField("request", string(request))
				}
				file, _ := form.CreateFormFile("file", "synthetic.png")
				if mode == "oversized" {
					file.Write(make([]byte, nativewire.MaxUploadBytes+1))
				} else {
					file.Write(syntheticPrivatePNG(t))
				}
				if mode == "extra_part" {
					form.WriteField("url", "https://must-not-fetch.example/")
				}
				form.Close()
				req := httptest.NewRequest("POST", "/", &data)
				req.Header.Set("Content-Type", form.FormDataContentType())
				req.Header.Set("Idempotency-Key", control.NewID())
				req.Header.Set("X-Workspace", "wsl")
				req.AddCookie(&http.Cookie{Name: "wsl_session", Value: f.token})
				req.SetPathValue("id", f.req.AccountID)
				req.SetPathValue("conversation", key)
				response := httptest.NewRecorder()
				err := s.uploadConversationMedia(response, req, Principal{ID: f.actor, Role: "superadmin"})
				if mode == "valid" {
					if err != nil || response.Code != 200 || port.calls != 1 || !bytes.Contains(response.Body.Bytes(), []byte(`"succeeded"`)) {
						t.Fatal("multipart upload failed", err, response.Code, port.calls)
					}
				} else if err == nil || port.calls != 0 {
					t.Fatal("unsafe multipart reached native port", mode, err, port.calls)
				}
			})
		}
	})
	t.Run("media_upload_and_send_share_durable_fences", func(t *testing.T) {
		for _, mode := range []string{"valid", "unknown", "revoke_before_upload", "revoke_before_send", "expired", "cross_group", "reused", "missing_window_action"} {
			t.Run(mode, func(t *testing.T) {
				f := seed()
				actions := []string{"send_file"}
				if mode == "missing_window_action" {
					actions = []string{"send_text"}
				}
				if _, err := db.Platform.Exec(ctx, `UPDATE test_windows SET actions=$2,budget=4 WHERE id=$1`, f.req.TestWindowID, actions); err != nil {
					t.Fatal(err)
				}
				port := &syntheticUploadPort{}
				if mode == "unknown" {
					port.mode = "unknown"
				}
				if mode == "revoke_before_upload" {
					port.before = func() {
						if _, err := db.Platform.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, f.actor); err != nil {
							t.Fatal(err)
						}
					}
				}
				req := f.req
				req.Text = ""
				input := nativeauth.MessageUpload{Kind: "file", Name: "synthetic.txt", Data: []byte("synthetic bytes")}
				uploaded, err := service.UploadMedia(ctx, f.ref, req, input, port)
				if mode == "missing_window_action" || mode == "revoke_before_upload" {
					if err == nil && uploaded.Status != "failed" || port.calls != 0 {
						t.Fatal("ungranted upload", err, uploaded)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if mode == "unknown" {
					if uploaded.Status != "unknown" || port.calls != 1 {
						t.Fatal(uploaded)
					}
					again, err := service.UploadMedia(ctx, f.ref, req, input, port)
					if err != nil || again.ID != uploaded.ID || port.calls != 1 {
						t.Fatal("unknown upload retried", err)
					}
					return
				}
				if uploaded.Status != "succeeded" || port.calls != 1 {
					t.Fatal(uploaded)
				}
				again, err := service.UploadMedia(ctx, f.ref, req, input, port)
				if err != nil || again.ID != uploaded.ID || port.calls != 1 {
					t.Fatal("upload idempotence", err)
				}
				changed := input
				changed.Data = []byte("different")
				if _, err = service.UploadMedia(ctx, f.ref, req, changed, port); err == nil {
					t.Fatal("changed body reused key")
				}
				sendReq := req
				sendReq.IdempotencyKey = control.NewID()
				sendReq.MediaUploadID = uploaded.ID
				if mode == "expired" {
					if _, err = db.Platform.Exec(ctx, `UPDATE native_media_uploads SET expires_at=now()-interval '1 second' WHERE id=$1`, uploaded.ID); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "cross_group" {
					sendReq.GroupID = control.NewID()
				}
				staged, err := service.Stage(ctx, f.ref, sendReq)
				if mode == "expired" || mode == "cross_group" {
					if err == nil {
						t.Fatal("invalid scope/expired receipt sent")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if mode == "reused" {
					sendReq.IdempotencyKey = control.NewID()
					if _, err = service.Stage(ctx, f.ref, sendReq); err == nil {
						t.Fatal("upload used for two messages")
					}
					return
				}
				sender := &syntheticTextPort{}
				if mode == "revoke_before_send" {
					sender.before = func() {
						if _, err := db.Platform.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, f.actor); err != nil {
							t.Fatal(err)
						}
					}
				}
				finished, err := service.Dispatch(ctx, f.ref, staged.ID, sender)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "revoke_before_send" {
					if finished.Status == "succeeded" {
						t.Fatal("revoked media sent")
					}
					return
				}
				if finished.Status != "succeeded" || sender.message.Media == nil || sender.message.Media.Kind != "file" || sender.message.Text != "" {
					t.Fatal("media changed or downgraded", finished)
				}
				var used int
				var plaintext bool
				if err = db.Platform.QueryRow(ctx, `SELECT w.used,position(convert_to('synthetic bytes','UTF8') in coalesce(u.payload_sealed,''::bytea))>0
                    FROM test_windows w JOIN native_media_uploads u ON u.test_window_id=w.id WHERE u.id=$1`, uploaded.ID).Scan(&used, &plaintext); err != nil {
					t.Fatal(err)
				}
				if used != 2 || plaintext {
					t.Fatal("wrong budget or body persisted", used)
				}
				if _, err = service.Dispatch(ctx, f.ref, staged.ID, sender); err != nil || sender.calls.Load() != 1 {
					t.Fatal("media resent", err)
				}
			})
		}
	})
	t.Run("retained_quote_and_mentions_use_shared_durable_fence", func(t *testing.T) {
		for _, mode := range []string{"valid", "cross_group", "cross_account", "private", "expired", "future",
			"wrong_deployment", "bad_seal", "long_quote", "mention_range", "target_unapproved",
			"expired_after_stage", "changed_after_stage", "expired_at_fence", "changed_at_fence", "target_revoked_at_fence"} {
			t.Run(mode, func(t *testing.T) {
				f := seed()
				var nativeGroup string
				if err := db.Platform.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1`, f.req.GroupID).
					Scan(&nativeGroup); err != nil {
					t.Fatal(err)
				}
				gid, _ := strconv.ParseUint(nativeGroup, 10, 64)
				account := f.req.AccountID
				if mode == "cross_account" {
					account = seed(true).req.AccountID
				}
				if mode == "cross_group" {
					gid++
				}
				text := "synthetic original reference"
				if mode == "long_quote" {
					text = strings.Repeat("x", 1025)
				}
				original := nativewire.GroupText{SenderID: 8, GroupID: gid, ClientID: control.NewID(),
					Text: text, CreatedAt: time.Now().UTC()}
				attachment, err := nativewire.SealGroupText(bytes.Repeat([]byte{17}, 32), original, 100, 200)
				if err != nil {
					t.Fatal(err)
				}
				raw := nativewire.IMMessage{Scene: 1, Target: "789", Sender: "8", ServerID: "9000",
					Fields: map[uint32]string{0: "1", 1: "789", 2: "8", 7: "1700000000123", 8: "100",
						10: attachment, 11: original.ClientID, 12: "9000"}}
				blob, _ := json.Marshal(raw)
				sealed, _ := vault.Seal("native-event:"+account+":synthetic-reference", blob)
				var event int64
				if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
                    VALUES($1,'synthetic-reference','group',now(),$2) RETURNING id`, account, sealed).Scan(&event); err != nil {
					t.Fatal(err)
				}
				projection := gateway.DecodedMessage{Message: nativewire.ApplicationMessage{
					Sender: nativewire.MessageSource{ID: 8}, Target: nativewire.MessageSource{ID: gid},
					Session: 2, ClientID: original.ClientID, Text: text}, NativeFormat: "100", ServerID: "9000"}
				blob, _ = json.Marshal(projection)
				aad := gateway.DecodedAAD(account, event)
				if mode == "bad_seal" {
					aad += ":wrong"
				}
				sealed, _ = vault.Seal(aad, blob)
				if _, err := db.Platform.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,
                    deployment_version,payload_sealed,received_at)
                    VALUES($1,$2,'decoded','group',77,$3,now())`, event, account, sealed); err != nil {
					t.Fatal(err)
				}
				exec := func(sql string, args ...any) {
					t.Helper()
					if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
						t.Fatal(err)
					}
				}
				f.req.QuoteEventID, f.req.Text = event, "@synthetic-member reply"
				f.req.Mentions = []nativewire.MessageMention{{Start: 0, End: 18, UID: 8, Name: "synthetic-member"}}
				if mode != "target_unapproved" {
					exec(`UPDATE test_windows SET target_ids=ARRAY['8'] WHERE id=$1`, f.req.TestWindowID)
				}
				switch mode {
				case "private":
					exec(`UPDATE native_decoded_events SET scene='private' WHERE event_id=$1`, event)
				case "expired":
					exec(`UPDATE native_decoded_events SET received_at=now()-interval '8 days' WHERE event_id=$1`, event)
				case "future":
					exec(`UPDATE native_received_events SET received_at=now()+interval '1 hour' WHERE id=$1`, event)
				case "wrong_deployment":
					exec(`UPDATE native_decoded_events SET deployment_version=78 WHERE event_id=$1`, event)
				case "mention_range":
					f.req.Mentions[0].End--
				}
				op, err := service.Stage(ctx, f.ref, f.req)
				goodStage := mode == "valid" || strings.Contains(mode, "_after_stage") || strings.Contains(mode, "_at_fence")
				if !goodStage {
					if err == nil {
						t.Fatal("untrusted quote/mention admitted", mode)
					}
					var used, operations int
					if err := db.Platform.QueryRow(ctx, `SELECT used,(SELECT count(*) FROM operations WHERE account_id=$2)
                        FROM test_windows WHERE id=$1`, f.req.TestWindowID, f.req.AccountID).Scan(&used, &operations); err != nil || used != 0 || operations != 0 {
						t.Fatal("rejected reference consumed an action or left an intent", used, operations, err)
					}
					return
				}
				if err != nil {
					t.Fatal("valid component stage failed", mode, err)
				}
				if mode == "valid" {
					repeated, err := service.Stage(ctx, f.ref, f.req)
					if err != nil || repeated.ID != op.ID {
						t.Fatal("quote idempotency lost", repeated, err)
					}
				}
				mutate := func() {
					switch {
					case strings.HasPrefix(mode, "expired_"):
						exec(`UPDATE native_received_events SET received_at=now()-interval '8 days' WHERE id=$1`, event)
					case strings.HasPrefix(mode, "changed_"):
						exec(`UPDATE native_decoded_events SET payload_sealed='\x00' WHERE event_id=$1`, event)
					case mode == "target_revoked_at_fence":
						exec(`UPDATE test_windows SET target_ids='{}' WHERE id=$1`, f.req.TestWindowID)
					}
				}
				port := &syntheticTextPort{}
				if strings.Contains(mode, "_after_stage") {
					mutate()
				} else if strings.Contains(mode, "_at_fence") {
					port.before = mutate
				}
				out, err := service.Dispatch(ctx, f.ref, op.ID, port)
				if strings.Contains(mode, "_after_stage") {
					if err == nil || port.calls.Load() != 0 {
						t.Fatal("stale staged reference reached transport", out, err)
					}
				} else if strings.Contains(mode, "_at_fence") {
					if err != nil || out.Status != "failed" || out.ResultCode != "native_text_not_submitted" {
						t.Fatal("reference/target change bypassed final fence", out, err)
					}
				} else if err != nil || out.Status != "succeeded" || port.message.Quote == nil ||
					port.message.Quote.Text != text || len(port.message.Mentions) != 1 {
					t.Fatal("shared pipeline dropped quote or mentions", out, err)
				}
			})
		}
	})
	stage := func(f fixture) control.TextOperation {
		t.Helper()
		op, err := service.Stage(ctx, f.ref, f.req)
		if err != nil {
			t.Fatal("stage failed", err)
		}
		return op
	}
	t.Run("current_role_evidence_uses_owner_and_epoch_not_a_directory_projection", func(t *testing.T) {
		for _, mode := range []string{"valid", "role_lost", "wrong_owner", "wrong_revision", "epoch", "deployment", "stale", "future", "session_revoked", "superseded"} {
			t.Run(mode, func(t *testing.T) {
				f := seed()
				op := stage(f)
				var uid, nativeGroup string
				if err := db.Platform.QueryRow(ctx, `SELECT a.native_uid,g.native_id FROM accounts a CROSS JOIN native_groups g
                    WHERE a.id=$1 AND g.id=$2`, f.req.AccountID, f.req.GroupID).Scan(&uid, &nativeGroup); err != nil {
					t.Fatal(err)
				}
				number, _ := strconv.ParseUint(uid, 10, 64)
				proof := nativewire.RoleEvidence{AccountID: f.req.AccountID, AccountEpoch: 1, ReceiverOwner: "synthetic-owner",
					ReceiverRevision: 1, DeploymentVersion: 77, UID: number, Sender: "synthetic-me", NativeGroup: nativeGroup,
					Team: "789", Role: "admin", ObservedAt: time.Now().UTC(), Name: "synthetic"}
				switch mode {
				case "role_lost":
					proof.Role = "member"
					proof.IndividualMuted = true
				case "wrong_owner":
					proof.ReceiverOwner = "another-owner"
				case "wrong_revision":
					proof.ReceiverRevision++
				case "epoch":
					proof.AccountEpoch++
				case "deployment":
					proof.DeploymentVersion++
				case "stale":
					proof.ObservedAt = time.Now().Add(-6 * time.Second)
				case "future":
					proof.ObservedAt = time.Now().Add(time.Second)
				case "session_revoked":
					if _, err := db.Platform.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, f.actor); err != nil {
						t.Fatal(err)
					}
				case "superseded":
					if _, err := db.Platform.Exec(ctx, `UPDATE account_groups SET observed_at=now()+interval '1 second'
                        WHERE account_id=$1 AND group_id=$2`, f.req.AccountID, f.req.GroupID); err != nil {
						t.Fatal(err)
					}
				}
				observed, err := control.RecordNativeRole(ctx, db.Platform, f.ref, proof)
				if mode != "valid" && mode != "role_lost" {
					if err == nil {
						t.Fatal("stale or forged identity proof updated role", mode)
					}
					return
				}
				if err != nil || observed.GroupID != f.req.GroupID || observed.Role != proof.Role {
					t.Fatal("fresh owner proof rejected", observed, err)
				}
				var status string
				var management bool
				var epoch int64
				if err := db.Platform.QueryRow(ctx, `SELECT o.status,b.management_enabled,b.epoch FROM operations o
                    JOIN group_bindings b ON b.group_id=o.group_id WHERE o.id=$1`, op.ID).Scan(&status, &management, &epoch); err != nil {
					t.Fatal(err)
				}
				if mode == "role_lost" {
					if status != "cancelled" || management || epoch != 2 || !observed.Changed {
						t.Fatal("lost role did not fence old work", status, management, epoch)
					}
				} else if status != "queued" || !management || epoch != 1 || observed.Changed {
					t.Fatal("unchanged current role reset binding or enabled work")
				}
			})
		}
	})
	t.Run("durable_idempotency_receipt_and_budget", func(t *testing.T) {
		f := seed()
		op := stage(f)
		repeated := stage(f)
		if op.ID != repeated.ID || op.Status != "queued" {
			t.Fatal("idempotency did not preserve the intent")
		}
		inspected, err := service.Inspect(ctx, f.ref, f.req.AccountID, f.req.GroupID, "", f.req.IdempotencyKey)
		if err != nil || inspected.ID != op.ID || inspected.Status != "queued" {
			t.Fatal("lost stage response cannot be recovered by its original key", inspected, err)
		}
		if _, err = service.Inspect(ctx, f.ref, control.NewID(), f.req.GroupID, op.ID, ""); err == nil {
			t.Fatal("operation status crossed account scope")
		}
		if _, err = service.Inspect(ctx, f.ref, f.req.AccountID, control.NewID(), op.ID, ""); err == nil {
			t.Fatal("operation status crossed group scope")
		}
		changed := f.req
		changed.Text = "different"
		if _, err := service.Stage(ctx, f.ref, changed); err == nil {
			t.Fatal("idempotency conflict silently replaced text")
		}
		// Tenant database role cannot query protocol nonce or receipt internals.
		if _, err := db.Tenant.Exec(ctx, `SELECT * FROM native_text_attempts`); err == nil {
			t.Fatal("tenant obtained shared protocol internals")
		}
		var sealed []byte
		if err := db.Platform.QueryRow(ctx, `SELECT payload_sealed FROM operations WHERE id=$1`, op.ID).Scan(&sealed); err != nil ||
			bytes.Contains(sealed, []byte(f.req.Text)) {
			t.Fatal("intent payload not sealed", err)
		}
		var writes int
		port := &syntheticTextPort{write: func() {
			writes++
			var status string
			if err := db.Platform.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1`, op.ID).Scan(&status); err != nil || status != "running" {
				t.Error("socket attempted before running intent committed", err)
			}
		}}
		done, err := service.Dispatch(ctx, f.ref, op.ID, port)
		if err != nil || done.Status != "succeeded" || done.ResultCode != "im_server_accepted" || writes != 1 {
			t.Fatal("synthetic receipt not reconciled", done, err)
		}
		again, err := service.Dispatch(ctx, f.ref, op.ID, port)
		if err != nil || again.Status != "succeeded" || writes != 1 || port.calls.Load() != 1 {
			t.Fatal("completed operation resubmitted")
		}
		var used int
		var serverID string
		if err := db.Platform.QueryRow(ctx, `SELECT w.used,t.server_id::text FROM test_windows w
            JOIN operations o ON o.test_window_id=w.id JOIN native_text_attempts t ON t.operation_id=o.id WHERE o.id=$1`, op.ID).
			Scan(&used, &serverID); err != nil || used != 1 || serverID != "9001" {
			t.Fatal("receipt or test budget lost", err)
		}
		if _, err := service.Stage(ctx, f.ref, control.TextRequest{AccountID: f.req.AccountID,
			GroupID: f.req.GroupID, TestWindowID: f.req.TestWindowID, IdempotencyKey: control.NewID(),
			Text: "another", AccountEpoch: 1, BindingEpoch: 1}); err == nil {
			t.Fatal("test budget exceeded")
		}
	})
	for _, mode := range []string{"accepted", "unknown", "components"} {
		t.Run("conversation_http_"+mode, func(t *testing.T) {
			f := seed()
			key := strings.Repeat("c", 64)
			var nativeID string
			if err := db.Platform.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1`, f.req.GroupID).Scan(&nativeID); err != nil {
				t.Fatal(err)
			}
			target, _ := strconv.ParseUint(nativeID, 10, 64)
			var event int64
			if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
                VALUES($1,'synthetic-send-conversation','group',now(),'\x01') RETURNING id`, f.req.AccountID).Scan(&event); err != nil {
				t.Fatal(err)
			}
			plain, _ := json.Marshal(gateway.DecodedMessage{Message: nativewire.ApplicationMessage{
				Target: nativewire.MessageSource{ID: target}, Sender: nativewire.MessageSource{ID: 8},
				Session: 2, ClientID: control.NewID(), Text: "synthetic original"}, NativeFormat: "100", ServerID: "9000"})
			sealed, _ := vault.Seal(gateway.DecodedAAD(f.req.AccountID, event), plain)
			if _, err := db.Platform.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
                deployment_version,payload_sealed,received_at,decoded_at,decode_ms)
                VALUES($1,$2,'decoded','group',$3,77,$4,now(),now(),0)`, event, f.req.AccountID, key, sealed); err != nil {
				t.Fatal(err)
			}
			port := &syntheticTextPort{mode: mode}
			server := &Server{DB: db, Vault: vault, nativeTextTestPort: port}
			principal := Principal{ID: f.actor, Role: "superadmin"}
			invoke := func(method string, body any, operation string, handler endpoint) []byte {
				t.Helper()
				blob, _ := json.Marshal(body)
				r := httptest.NewRequest(method, "/?key="+f.req.IdempotencyKey, bytes.NewReader(blob))
				r.SetPathValue("id", f.req.AccountID)
				r.SetPathValue("conversation", key)
				r.SetPathValue("operation", operation)
				r.Header.Set("Idempotency-Key", f.req.IdempotencyKey)
				r.Header.Set("Content-Type", "application/json")
				r.AddCookie(&http.Cookie{Name: "wsl_session", Value: f.token})
				w := httptest.NewRecorder()
				if err := handler(w, r, principal); err != nil {
					t.Fatal("synthetic conversation request failed", err)
				}
				return w.Body.Bytes()
			}
			view := invoke("GET", nil, "", server.nativeSending)
			if !bytes.Contains(view, []byte(`"enabled":true`)) {
				t.Fatal("synthetic test window unavailable", string(view))
			}
			in := map[string]any{"text": f.req.Text, "test_window_id": f.req.TestWindowID,
				"account_epoch": 1, "binding_epoch": 1}
			if mode == "components" {
				f.req.Text = "@synthetic-member reply"
				in["text"], in["reply_to"] = f.req.Text, strconv.FormatInt(event, 10)
				in["mentions"] = []nativewire.MessageMention{{Start: 0, End: 18, UID: 8, Name: "synthetic-member"}}
				if _, err := db.Platform.Exec(ctx, `UPDATE test_windows SET target_ids=ARRAY['8'] WHERE id=$1`,
					f.req.TestWindowID); err != nil {
					t.Fatal(err)
				}
			}
			for _, change := range []map[string]any{
				{"tenant_id": control.NewID()},
				{"group_id": f.req.GroupID},
				{"account_epoch": 2},
				{"binding_epoch": 2},
				{"text": strings.Repeat("汉", 1001)},
				{"reply_to": "01"},
				{"reply_to": "-1"},
				{"quote": map[string]string{"server_id": "9000", "text": "browser-forged"}},
				{"ReferenceDigest": []byte{1}},
			} {
				input := map[string]any{}
				for k, v := range in {
					input[k] = v
				}
				for k, v := range change {
					input[k] = v
				}
				blob, _ := json.Marshal(input)
				r := httptest.NewRequest("POST", "/", bytes.NewReader(blob))
				r.SetPathValue("id", f.req.AccountID)
				r.SetPathValue("conversation", key)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Idempotency-Key", f.req.IdempotencyKey)
				r.AddCookie(&http.Cookie{Name: "wsl_session", Value: f.token})
				w := httptest.NewRecorder()
				if err := server.stageNativeText(w, r, principal); err == nil {
					t.Fatal("HTTP stage accepted untrusted scope, stale epoch or oversized text")
				} else {
					sendError(w, err)
				}
				if w.Code != 400 && w.Code != 409 {
					t.Fatal("unexpected stage validation status", w.Code)
				}
				if port.calls.Load() != 0 {
					t.Fatal("HTTP validation failure touched transport")
				}
			}
			raw := invoke("POST", in, "", server.stageNativeText)
			var result struct {
				Data control.TextOperation `json:"data"`
			}
			if err := json.Unmarshal(raw, &result); err != nil || result.Data.ID == "" || result.Data.Status != "queued" {
				t.Fatal("stage response lacks recoverable state", string(raw), err)
			}
			id := result.Data.ID
			raw = invoke("GET", nil, "", server.inspectNativeText)
			if !bytes.Contains(raw, []byte(id)) || port.calls.Load() != 0 {
				t.Fatal("inspection submitted a native effect", string(raw))
			}
			raw = invoke("POST", map[string]any{}, id, server.dispatchNativeText)
			want := "succeeded"
			if mode == "unknown" {
				want = "unknown"
			}
			if err := json.Unmarshal(raw, &result); err != nil || result.Data.Status != want || port.calls.Load() != 1 {
				t.Fatal("dispatch status lost", string(raw), err)
			}
			if mode == "components" && (port.message.Quote == nil || port.message.Quote.Text != "synthetic original" ||
				len(port.message.Mentions) != 1) {
				t.Fatal("HTTP components were dropped before the common executor")
			}
			invoke("POST", map[string]any{}, id, server.dispatchNativeText)
			if port.calls.Load() != 1 {
				t.Fatal("repeated HTTP dispatch resubmitted a message")
			}
			view = invoke("GET", nil, "", server.nativeSending)
			if bytes.Contains(view, []byte(f.req.Text)) || !bytes.Contains(view, []byte(id)) {
				t.Fatal("context leaked text or omitted recovery record", string(view))
			}
			if _, err := db.Platform.Exec(ctx, `UPDATE group_bindings SET active=false,epoch=epoch+1 WHERE group_id=$1`,
				f.req.GroupID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Platform.Exec(ctx, `UPDATE native_decoded_events SET received_at=now()-interval '8 days'
                WHERE event_id=$1`, event); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"", id} {
				raw = invoke("GET", nil, operation, server.inspectAccountNativeText)
				if err := json.Unmarshal(raw, &result); err != nil || result.Data.ID != id || result.Data.Status != want ||
					port.calls.Load() != 1 || bytes.Contains(raw, []byte(f.req.Text)) {
					t.Fatal("unbinding or retention hid status, leaked body or retried", string(raw), err)
				}
			}
			recovery := httptest.NewRequest("GET", "/?conversation="+key, nil)
			recovery.SetPathValue("id", f.req.AccountID)
			recovery.AddCookie(&http.Cookie{Name: "wsl_session", Value: f.token})
			response := httptest.NewRecorder()
			if err := server.inspectAccountNativeText(response, recovery, principal); err != nil {
				t.Fatal("expired conversation could not read original pending IDs", err)
			}
			list := response.Body.Bytes()
			if bytes.Contains(list, []byte(id)) || bytes.Contains(list, []byte(f.req.Text)) || port.calls.Load() != 1 {
				t.Fatal("pending recovery lost scope, leaked text or executed", string(list))
			}
			other := seed(true)
			if _, err := service.InspectAccount(ctx, other.ref, f.req.AccountID, id, ""); err == nil {
				t.Fatal("another administrator queried this actor's native operation")
			}
			if _, err := service.InspectAccount(ctx, f.ref, other.req.AccountID, id, ""); err == nil {
				t.Fatal("status crossed account scope")
			}
		})
	}
	mutations := map[string]func(fixture){
		"account_epoch": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.req.AccountID)
		},
		"binding_epoch": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, f.req.GroupID)
		},
		"management_off": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE group_bindings SET management_enabled=false WHERE group_id=$1`, f.req.GroupID)
		},
		"authorization_revoked": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE group_bindings SET active=false WHERE group_id=$1`, f.req.GroupID)
		},
		"testing_off": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE accounts SET testing_enabled=false WHERE id=$1`, f.req.AccountID)
		},
		"owner_changed": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE account_receivers SET owner='replacement' WHERE account_id=$1`, f.req.AccountID)
		},
		"lease_lost": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE account_receivers SET lease_until=now()-interval '1 second' WHERE account_id=$1`, f.req.AccountID)
		},
		"lease_expiring": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE account_receivers SET lease_until=now()+interval '500 milliseconds' WHERE account_id=$1`, f.req.AccountID)
		},
		"role_stale": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE account_groups SET observed_at=now()-interval '61 seconds' WHERE account_id=$1`, f.req.AccountID)
		},
		"role_lost": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE account_groups SET role='member' WHERE account_id=$1`, f.req.AccountID)
		},
		"role_replay": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE account_groups SET observation_source='replay' WHERE account_id=$1`, f.req.AccountID)
		},
		"session_revoked": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, security.Digest(f.token))
		},
		"actor_disabled": func(f fixture) { _, _ = db.Platform.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, f.actor) },
		"window_cancelled": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE test_windows SET cancelled=true WHERE id=$1`, f.req.TestWindowID)
		},
		"window_expiring": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE test_windows SET expires_at=now()+interval '500 milliseconds' WHERE id=$1`, f.req.TestWindowID)
		},
		"session_expiring": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE sessions SET expires_at=now()+interval '500 milliseconds' WHERE token_hash=$1`, security.Digest(f.token))
		},
		"subscription_disabled": func(f fixture) {
			_, _ = db.Platform.Exec(ctx, `UPDATE subscriptions SET enabled=false WHERE tenant_id=(SELECT tenant_id FROM group_bindings WHERE group_id=$1)`, f.req.GroupID)
		},
	}
	for name, mutate := range mutations {
		t.Run("final_fence_"+name, func(t *testing.T) {
			f := seed()
			op := stage(f)
			writes := 0
			port := &syntheticTextPort{before: func() { mutate(f) }, write: func() { writes++ }}
			out, err := service.Dispatch(ctx, f.ref, op.ID, port)
			if err != nil || out.Status != "failed" || out.ResultCode != "native_text_not_submitted" || writes != 0 {
				t.Fatal("revocation passed the final submission fence", out, err)
			}
		})
	}
	for _, mode := range []string{"unknown", "rejected", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			f := seed()
			op := stage(f)
			writes := 0
			port := &syntheticTextPort{mode: mode, write: func() { writes++ }}
			out, err := service.Dispatch(ctx, f.ref, op.ID, port)
			want := "unknown"
			if mode == "rejected" {
				want = "failed"
			}
			if err != nil || out.Status != want || writes != 1 {
				t.Fatal("submission result misclassified", out, err)
			}
			_, _ = service.Dispatch(ctx, f.ref, op.ID, port)
			if writes != 1 {
				t.Fatal("unknown/rejected submission retried")
			}
			if mode == "unknown" {
				// Even a newly authorized second slot cannot silently evade an
				// account's unresolved submission by inventing another key.
				if _, err := db.Platform.Exec(ctx, `UPDATE test_windows SET budget=2 WHERE id=$1`, f.req.TestWindowID); err != nil {
					t.Fatal(err)
				}
				f.req.IdempotencyKey = control.NewID()
				second := stage(f)
				if _, err := service.Dispatch(ctx, f.ref, second.ID, port); err == nil || writes != 1 {
					t.Fatal("new key bypassed an unknown account submission")
				}
			}
		})
	}
	t.Run("concurrent_dispatch_does_not_duplicate", func(t *testing.T) {
		f := seed()
		op := stage(f)
		entered, release := make(chan struct{}), make(chan struct{})
		port := &syntheticTextPort{before: func() { close(entered); <-release }}
		done := make(chan error, 1)
		go func() { _, err := service.Dispatch(ctx, f.ref, op.ID, port); done <- err }()
		<-entered
		out, err := service.Dispatch(ctx, f.ref, op.ID, port)
		close(release)
		if err != nil || out.Status != "running" {
			t.Fatal("concurrent dispatch failed to return existing intent", err)
		}
		if err := <-done; err != nil || port.calls.Load() != 1 {
			t.Fatal("concurrent intent submitted twice", err)
		}
	})
	t.Run("expired_and_interrupted_are_not_requeued", func(t *testing.T) {
		f := seed()
		op := stage(f)
		if _, err := db.Platform.Exec(ctx, `UPDATE operations SET expires_at=now()-interval '20 seconds' WHERE id=$1`, op.ID); err != nil {
			t.Fatal(err)
		}
		crashed := seed(true)
		running := stage(crashed)
		if _, err := db.Platform.Exec(ctx, `UPDATE operations SET status='running',expires_at=now()-interval '20 seconds' WHERE id=$1`, running.ID); err != nil {
			t.Fatal(err)
		}
		if err := service.Expire(ctx); err != nil {
			t.Fatal(err)
		}
		for id, want := range map[string]string{op.ID: "expired", running.ID: "unknown"} {
			var got string
			if err := db.Platform.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1`, id).Scan(&got); err != nil || got != want {
				t.Fatal("abandoned send was requeued or falsely completed", got, err)
			}
		}
		if err := service.Expire(ctx); err != nil {
			t.Fatal(err)
		}
		var recovered int
		if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM audit_events
            WHERE action='native_text_recovered' AND resource_id=ANY($1)`, []string{op.ID, running.ID}).
			Scan(&recovered); err != nil || recovered != 2 {
			t.Fatal("recovery missing its atomic audit or duplicated recovery", recovered, err)
		}
	})
	t.Run("recovery_is_bounded_and_skips_live_locks", func(t *testing.T) {
		f := seed()
		first := stage(f)
		ids := []string{first.ID}
		for n := 0; n < 7; n++ {
			if n > 0 {
				f = seed(true)
			}
			for j := 0; j < 15; j++ {
				if n == 0 && j == 0 {
					continue
				}
				f.req.IdempotencyKey = control.NewID()
				ids = append(ids, stage(f).ID)
			}
		}
		if _, err := db.Platform.Exec(ctx, `UPDATE operations SET expires_at=now()-interval '1 minute'
            WHERE id=ANY($1::uuid[])`, ids); err != nil {
			t.Fatal(err)
		}
		lock, err := db.Platform.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Rollback(ctx)
		if _, err := lock.Exec(ctx, `SELECT id FROM operations WHERE id=$1 FOR UPDATE`, first.ID); err != nil {
			t.Fatal(err)
		}
		if err := service.Expire(ctx); err != nil {
			t.Fatal("a locked live operation blocked all recovery", err)
		}
		var expired, queued int
		if err := db.Platform.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='expired'),
            count(*) FILTER(WHERE status='queued') FROM operations WHERE id=ANY($1::uuid[])`, ids).
			Scan(&expired, &queued); err != nil || expired != 100 || queued != 5 {
			t.Fatal("recovery exceeded its batch or lost queued work", expired, queued, err)
		}
		if err := lock.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err := service.Expire(ctx); err != nil {
			t.Fatal(err)
		}
		var audited int
		if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='native_text_recovered'
            AND resource_id=ANY($1)`, ids).Scan(&audited); err != nil || audited != 105 {
			t.Fatal("unlocked recovery did not converge", audited, err)
		}
	})
	t.Run("nonce_survives_operation_cleanup", func(t *testing.T) {
		f := seed()
		first := stage(f)
		var before, after string
		if err := db.Platform.QueryRow(ctx, `SELECT nonce::text FROM native_text_attempts WHERE operation_id=$1`, first.ID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Platform.Exec(ctx, `DELETE FROM operations WHERE id=$1`, first.ID); err != nil {
			t.Fatal(err)
		}
		f.req.IdempotencyKey = control.NewID()
		second := stage(f)
		if err := db.Platform.QueryRow(ctx, `SELECT nonce::text FROM native_text_attempts WHERE operation_id=$1`, second.ID).Scan(&after); err != nil || after <= before {
			t.Fatal("nonce high-water mark was reset by intent cleanup", err)
		}
	})
	for _, mode := range []string{"matching", "missing", "ciphertext", "team", "sender", "client", "conflict", "truncated", "other_actor", "epoch_changed"} {
		t.Run("reconcile_"+mode, func(t *testing.T) {
			f := seed()
			op := stage(f)
			port := &syntheticTextPort{mode: "unknown"}
			unknown, err := service.Dispatch(ctx, f.ref, op.ID, port)
			if err != nil || unknown.Status != "unknown" {
				t.Fatal("synthetic unknown was not durable", err)
			}
			if _, err := db.Tenant.Exec(ctx, `SELECT * FROM native_text_routes`); err == nil {
				t.Fatal("tenant read protocol route evidence")
			}
			var clientID string
			if err = db.Platform.QueryRow(ctx, `SELECT client_id::text FROM native_text_attempts WHERE operation_id=$1`,
				op.ID).Scan(&clientID); err != nil {
				t.Fatal(err)
			}
			insert := func(serverID string, fields map[uint32]string) {
				t.Helper()
				message := nativewire.IMMessage{Scene: 1, Target: fields[1], Sender: fields[2], ServerID: serverID, Fields: fields}
				key := "1:" + fields[1] + ":" + serverID
				raw, _ := json.Marshal(message)
				sealed, err := vault.Seal("native-event:"+f.req.AccountID+":"+key, raw)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.Platform.Exec(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
                    VALUES($1,$2,'group',clock_timestamp(),$3)`, f.req.AccountID, key, sealed); err != nil {
					t.Fatal(err)
				}
			}
			fields := map[uint32]string{0: "1", 1: "789", 2: "synthetic-me", 7: "1700000000123", 8: "100",
				10: port.attachment, 11: clientID, 12: "9001"}
			switch mode {
			case "ciphertext":
				fields[10] = "other-ciphertext"
			case "team":
				fields[1] = "790"
			case "sender":
				fields[2] = "someone-else"
			case "client":
				fields[11] = control.NewID()
			}
			if mode != "missing" {
				insert("9001", fields)
			}
			if mode == "conflict" {
				fields[12] = "9002"
				insert("9002", fields)
			}
			if mode == "truncated" {
				for n := 0; n < 201; n++ {
					key := control.NewID()
					other := map[uint32]string{0: "1", 1: "789", 2: "synthetic-me", 7: "1700000000123", 8: "100",
						10: "unrelated", 11: key, 12: fmt.Sprint(10000 + n)}
					insert(other[12], other)
				}
			}
			ref := f.ref
			if mode == "other_actor" {
				ref = seed(true).ref
			}
			if mode == "epoch_changed" {
				_, err = db.Platform.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.req.AccountID)
				if err != nil {
					t.Fatal(err)
				}
			}
			receiver := gateway.NewReceiverManager(db.Platform, vault, nil)
			out, err := service.Reconcile(ctx, ref, op.ID, receiver)
			if mode == "matching" {
				if err != nil || out.Status != "succeeded" || out.ResultCode != "im_server_accepted_reconciled" {
					t.Fatal("durable matching echo did not reconcile", out, err)
				}
				again, err := service.Reconcile(ctx, ref, op.ID, receiver)
				if err != nil || again.Status != "succeeded" {
					t.Fatal("reconciliation not idempotent", err)
				}
			} else {
				var state string
				if err := db.Platform.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1`, op.ID).Scan(&state); err != nil || state != "unknown" {
					t.Fatal("missing/conflicting evidence rewrote unknown", state, err)
				}
			}
			if port.calls.Load() != 1 {
				t.Fatal("reconciliation resent a message")
			}
		})
	}
	t.Run("no_production_or_empty_scope_fallback", func(t *testing.T) {
		f := seed()
		f.req.TestWindowID = ""
		if _, err := service.Stage(ctx, f.ref, f.req); err == nil {
			t.Fatal("production send opened without isolated test window")
		}
		bad, _ := control.WebsiteSession(strings.Repeat("A", 43))
		if _, err := service.Stage(ctx, bad, f.req); err == nil {
			t.Fatal("invalid website session accepted")
		}
		// Other actor, even another superadmin, cannot dispatch this actor's intent.
		f = seed()
		op := stage(f)
		other := seed(true)
		if _, err := service.Inspect(ctx, other.ref, f.req.AccountID, f.req.GroupID, op.ID, ""); err == nil {
			t.Fatal("another actor inspected the intent")
		}
		if _, err := service.Dispatch(ctx, other.ref, op.ID, &syntheticTextPort{}); err == nil {
			t.Fatal("another actor dispatched the intent")
		}
	})
}
