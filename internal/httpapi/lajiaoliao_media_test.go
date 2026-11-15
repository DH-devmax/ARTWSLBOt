package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

type syntheticChiliMediaPort struct {
	beforeUpload, beforeSend   func()
	unknownUpload, unknownSend bool
	uploads, writes            int
	dispatch                   control.ChiliMediaDispatch
}

func (p *syntheticChiliMediaPort) UploadChiliMedia(ctx context.Context, d control.ChiliMediaDispatch, guard func(context.Context) error) (lajiaoliao.MediaAsset, error) {
	if p.beforeUpload != nil {
		p.beforeUpload()
	}
	if err := guard(ctx); err != nil {
		return lajiaoliao.MediaAsset{}, err
	}
	p.uploads++
	if p.unknownUpload {
		return lajiaoliao.MediaAsset{}, errors.New("synthetic lost upload response")
	}
	meta, err := nativeauth.InspectUpload(d.Input)
	if err != nil {
		return lajiaoliao.MediaAsset{}, err
	}
	out := lajiaoliao.MediaAsset{ID: d.ID, Kind: d.Input.Kind, URL: "https://lj-im-chat-01.lajiaoliao.com/synthetic",
		Size: int64(len(d.Input.Data)), Duration: int64(meta.Duration)}
	if d.Input.Kind == "video" {
		out.SnapshotURL, out.SnapshotSize = out.URL+"-cover", int64(len(d.Input.Thumbnail))
		out.Width, out.Height, err = lajiaoliao.SnapshotDimensions(d.Input.Thumbnail)
	}
	return out, err
}

func (p *syntheticChiliMediaPort) SubmitChiliMedia(ctx context.Context, d control.ChiliMediaDispatch, fence nativewire.SubmissionFence) (lajiaoliao.Receipt, error) {
	p.dispatch = d
	if p.beforeSend != nil {
		p.beforeSend()
	}
	if err := fence(ctx, func(context.Context) error { p.writes++; return nil }); err != nil {
		return lajiaoliao.Receipt{}, err
	}
	if p.unknownSend {
		return lajiaoliao.Receipt{}, errors.New("synthetic lost send response")
	}
	return lajiaoliao.Receipt{ClientID: d.ClientID, ServerID: "string-server-id", Timestamp: 1700000000123}, nil
}

type syntheticChiliEvidence struct {
	query lajiaoliao.MediaEvidenceQuery
	found bool
}

func (p *syntheticChiliEvidence) ChiliMediaEvidence(_ context.Context, q lajiaoliao.MediaEvidenceQuery) (lajiaoliao.MediaEvidence, error) {
	p.query = q
	return lajiaoliao.MediaEvidence{Found: p.found, EventID: 1,
		Receipt: lajiaoliao.Receipt{ClientID: q.ClientID, ServerID: "string-server-id", Timestamp: 1700000000123}}, nil
}

func chiliSyntheticWave() []byte {
	data := make([]byte, 16044)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 16000)
	return data
}

func chiliSyntheticVideo(t *testing.T) nativeauth.MessageUpload {
	t.Helper()
	box := func(kind string, body []byte) []byte {
		out := binary.BigEndian.AppendUint32(nil, uint32(len(body)+8))
		return append(append(out, kind...), body...)
	}
	mvhd, tkhd, hdlr := make([]byte, 20), make([]byte, 84), make([]byte, 12)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	binary.BigEndian.PutUint32(mvhd[16:], 2000)
	binary.BigEndian.PutUint32(tkhd[76:], 320<<16)
	binary.BigEndian.PutUint32(tkhd[80:], 180<<16)
	copy(hdlr[8:], "vide")
	trak := box("trak", append(box("tkhd", tkhd), box("mdia", box("hdlr", hdlr))...))
	data := box("ftyp", []byte("isom\x00\x00\x02\x00isommp42"))
	data = append(data, box("moov", append(box("mvhd", mvhd), trak...))...)
	data = append(data, box("mdat", []byte("synthetic-not-playable"))...)
	var cover bytes.Buffer
	if err := jpeg.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 64, 32)), nil); err != nil {
		t.Fatal(err)
	}
	return nativeauth.MessageUpload{Kind: "video", Name: "synthetic.mp4", Data: data, Thumbnail: cover.Bytes()}
}

func TestChiliMediaDurableControl(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{43}, 32)))
	service := &control.NativeTextService{DB: db, Vault: vault}
	for _, mode := range []string{"valid", "video", "bad_cover", "http_valid", "http_workspace", "http_unbound", "http_private",
		"unknown_upload", "unknown_send", "revoke_upload", "revoke_send",
		"epoch", "binding", "owner", "cancel", "role", "expired", "budget", "wrong_platform", "scope", "repeat", "restart"} {
		t.Run(mode, func(t *testing.T) {
			for _, table := range []string{"operations", "test_windows", "inbox", "group_bindings", "account_groups", "accounts"} {
				if _, err := db.Platform.Exec(ctx, `DELETE FROM `+table); err != nil {
					t.Fatal(err)
				}
			}
			req := control.TextRequest{AccountID: control.NewID(), GroupID: control.NewID(), TestWindowID: control.NewID(),
				AccountEpoch: 1, BindingEpoch: 1, IdempotencyKey: control.NewID()}
			actor, tenant, plan := control.NewID(), control.NewID(), control.NewID()
			token, _ := security.Token()
			ref, _ := control.WebsiteSession(token)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,$2,'synthetic','superadmin')`, actor, actor)
			exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
				security.Digest(token), security.Digest("synthetic-csrf"), actor)
			exec(`INSERT INTO tenants(id,name) VALUES($1,'synthetic')`, tenant)
			exec(`INSERT INTO plans(id,name,group_limit,ai_group_limit,modules,allowed_platforms)
				VALUES($1,'synthetic',100,0,ARRAY['moderation'],ARRAY['lajiaoliao'])`, plan)
			exec(`INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, tenant, plan)
			fixturePlatform := "lajiaoliao"
			if mode == "wrong_platform" {
				fixturePlatform = "wsl"
			}
			exec(`INSERT INTO accounts(id,name,platform,workspace,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
				VALUES($1,'synthetic',$4,$4,$2,'authenticated',$3,true,true)`,
				req.AccountID, "string-self-"+req.AccountID, []byte("synthetic sealed placeholder"), fixturePlatform)
			snapshot, _ := json.Marshal(map[string]any{"state": "connected", "heartbeat_at": time.Now().UTC()})
			exec(`INSERT INTO account_receivers(account_id,enabled,owner,lease_until,observed_at,snapshot)
				VALUES($1,true,'synthetic-owner',now()+interval '1 hour',now(),$2)`, req.AccountID, snapshot)
			exec(`INSERT INTO native_groups(id,platform,native_id,name,isolated_test)
				VALUES($1,$3,$2,'synthetic',true)`, req.GroupID, "string-group-"+req.GroupID, fixturePlatform)
			exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled)
				VALUES($1,$2,$3,true)`, req.GroupID, tenant, req.AccountID)
			exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
				VALUES($1,$2,'admin',now(),'native')`, req.AccountID, req.GroupID)
			epochs, _ := json.Marshal(map[string]int64{req.AccountID: 1})
			exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget)
				VALUES($1,$2,$3,$4::uuid[],'{}',ARRAY['send_voice','send_video'],$5,now()+interval '5 minutes',4)`,
				req.TestWindowID, actor, req.GroupID, []string{req.AccountID}, epochs)
			port := &syntheticChiliMediaPort{unknownUpload: mode == "unknown_upload", unknownSend: mode == "unknown_send"}
			if mode == "revoke_upload" {
				port.beforeUpload = func() { exec(`DELETE FROM sessions WHERE user_id=$1`, actor) }
			}
			if mode == "scope" {
				req.PrivateConversation = "forbidden"
			}
			input := nativeauth.MessageUpload{Kind: "voice", Name: "synthetic.wav", Data: chiliSyntheticWave()}
			if mode == "video" || mode == "bad_cover" {
				input = chiliSyntheticVideo(t)
				if mode == "bad_cover" {
					input.Thumbnail = []byte("not-an-image")
				}
			}
			if strings.HasPrefix(mode, "http_") {
				s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
				defer s.Native.Close()
				defer s.StopAssistant()
				s.chiliMediaTestPort = port
				key := strings.Repeat("d", 64)
				scene := "group"
				if mode == "http_private" {
					scene = "private"
				}
				var event int64
				if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events
					(account_id,event_key,kind,received_at,payload_sealed)
					VALUES($1,'synthetic-http',$2,now(),'\x01') RETURNING id`, req.AccountID, scene).Scan(&event); err != nil {
					t.Fatal(err)
				}
				target := "string-group-" + req.GroupID
				if mode == "http_unbound" {
					target = "unbound-string-group"
				}
				raw, _ := json.Marshal(gateway.DecodedMessage{NativeTarget: target})
				sealed, _ := vault.Seal(gateway.DecodedAAD(req.AccountID, event), raw)
				exec(`INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,payload_sealed,
					deployment_version,codec_version,received_at,decoded_at,decode_ms)
					VALUES($1,$2,'decoded',$3,$4,$5,0,6,now(),now(),0)`,
					event, req.AccountID, scene, key, sealed)
				var form bytes.Buffer
				writer := multipart.NewWriter(&form)
				request, _ := json.Marshal(map[string]any{"kind": "voice", "test_window_id": req.TestWindowID,
					"account_epoch": 1, "binding_epoch": 1})
				writer.WriteField("request", string(request))
				part, _ := writer.CreateFormFile("file", input.Name)
				part.Write(input.Data)
				writer.Close()
				base := "/api/v1/accounts/" + req.AccountID + "/conversations/" + key
				call := func(method, path, contentType string, data []byte) *httptest.ResponseRecorder {
					t.Helper()
					r := httptest.NewRequest(method, path, bytes.NewReader(data))
					r.Header.Set("Origin", "http://127.0.0.1:5173")
					r.Header.Set("Content-Type", contentType)
					r.Header.Set("Idempotency-Key", control.NewID())
					r.Header.Set("X-Workspace", "lajiaoliao")
					if mode == "http_workspace" {
						r.Header.Set("X-Workspace", "wsl")
					}
					r.Header.Set("X-CSRF-Token", "synthetic-csrf")
					r.AddCookie(&http.Cookie{Name: "wsl_session", Value: token})
					r.AddCookie(&http.Cookie{Name: "wsl_csrf", Value: "synthetic-csrf"})
					out := httptest.NewRecorder()
					s.Handler().ServeHTTP(out, r)
					return out
				}
				response := call("POST", base+"/media-uploads", writer.FormDataContentType(), form.Bytes())
				if mode != "http_valid" {
					if response.Code < 400 || port.uploads != 0 {
						t.Fatal("HTTP scope reached upload", response.Code, port.uploads)
					}
					return
				}
				if response.Code != 200 || port.uploads != 1 {
					t.Fatal("HTTP upload", response.Code, response.Body.String())
				}
				var uploaded control.MediaUploadOperation
				var uploadEnvelope struct{ Data control.MediaUploadOperation }
				if json.Unmarshal(response.Body.Bytes(), &uploadEnvelope) != nil || uploadEnvelope.Data.Status != "succeeded" {
					t.Fatal("HTTP upload receipt missing")
				}
				uploaded = uploadEnvelope.Data
				body, _ := json.Marshal(map[string]any{"upload_id": uploaded.ID, "test_window_id": req.TestWindowID,
					"account_epoch": 1, "binding_epoch": 1})
				response = call("POST", base+"/media-operations", "application/json", body)
				var staged control.TextOperation
				var stageEnvelope struct{ Data control.TextOperation }
				if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &stageEnvelope) != nil || stageEnvelope.Data.Status != "queued" {
					t.Fatal("HTTP media stage", response.Code, response.Body.String())
				}
				staged = stageEnvelope.Data
				response = call("GET", base+"/sending", "", nil)
				if response.Code != 200 || !bytes.Contains(response.Body.Bytes(), []byte(`"kind":"send_voice"`)) ||
					!bytes.Contains(response.Body.Bytes(), []byte(`"test_window_id":"`+req.TestWindowID+`"`)) {
					t.Fatal("HTTP media restore metadata missing", response.Code, response.Body.String())
				}
				response = call("POST", base+"/media-operations/"+staged.ID+"/dispatch", "application/json", []byte(`{}`))
				if response.Code != 200 || port.writes != 1 || !bytes.Contains(response.Body.Bytes(), []byte(`"succeeded"`)) {
					t.Fatal("HTTP dispatch", response.Code, response.Body.String())
				}
				return
			}
			uploaded, err := service.UploadChiliMedia(ctx, ref, req, input, port)
			if mode == "wrong_platform" || mode == "scope" || mode == "revoke_upload" || mode == "bad_cover" {
				if err == nil && uploaded.Status != "failed" || port.uploads != 0 {
					t.Fatal("invalid upload reached native", uploaded, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := service.UploadChiliMedia(ctx, ref, req, input, port)
			if err != nil || repeated.ID != uploaded.ID || port.uploads != 1 {
				t.Fatal("upload replay", err)
			}
			if mode == "unknown_upload" {
				if uploaded.Status != "unknown" {
					t.Fatal(uploaded)
				}
				return
			}
			req.IdempotencyKey, req.MediaUploadID = control.NewID(), uploaded.ID
			staged, err := service.StageChiliMedia(ctx, ref, req)
			if err != nil {
				t.Fatal(err)
			}
			repeat, err := service.StageChiliMedia(ctx, ref, req)
			if err != nil || repeat.ID != staged.ID {
				t.Fatal("stage idempotency", err)
			}
			unsubmitted, err := service.ReconcileChiliMedia(ctx, ref, staged.ID, &syntheticChiliEvidence{})
			if err != nil || unsubmitted.Status != "queued" {
				t.Fatal("unsubmitted nullable timestamp was not preserved", unsubmitted, err)
			}
			port.beforeSend = func() {
				switch mode {
				case "revoke_send":
					exec(`DELETE FROM sessions WHERE user_id=$1`, actor)
				case "epoch":
					exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, req.AccountID)
				case "binding":
					exec(`UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, req.GroupID)
				case "owner":
					exec(`UPDATE account_receivers SET owner='new-owner' WHERE account_id=$1`, req.AccountID)
				case "cancel":
					exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, req.TestWindowID)
				case "role":
					exec(`UPDATE account_groups SET role='member' WHERE account_id=$1`, req.AccountID)
				case "expired":
					exec(`UPDATE native_media_uploads SET expires_at=now()-interval '1 second' WHERE id=$1`, uploaded.ID)
				}
			}
			if mode == "budget" {
				exec(`UPDATE test_windows SET budget=used WHERE id=$1`, req.TestWindowID)
				if _, err = service.DispatchChiliMedia(ctx, ref, staged.ID, port); err == nil || port.writes != 0 {
					t.Fatal("exhausted budget submitted", err)
				}
				return
			}
			if mode == "restart" {
				exec(`UPDATE operations SET status='running' WHERE id=$1`, staged.ID)
				exec(`UPDATE lajiaoliao_media_attempts SET submitted_at=now() WHERE operation_id=$1`, staged.ID)
				if err = service.RecoverInterrupted(ctx); err != nil {
					t.Fatal(err)
				}
			}
			finished, err := service.DispatchChiliMedia(ctx, ref, staged.ID, port)
			if err != nil {
				t.Fatal(err)
			}
			denied := mode == "revoke_send" || mode == "epoch" || mode == "binding" || mode == "owner" ||
				mode == "cancel" || mode == "role" || mode == "expired" || mode == "budget"
			if denied {
				if port.writes != 0 || finished.Status != "failed" {
					t.Fatal("changed scope wrote", finished, port.writes)
				}
				return
			}
			if mode == "unknown_send" || mode == "restart" {
				if finished.Status != "unknown" {
					t.Fatal("unknown lost", finished)
				}
				evidence := &syntheticChiliEvidence{}
				same, err := service.ReconcileChiliMedia(ctx, ref, staged.ID, evidence)
				if err != nil || same.Status != "unknown" {
					t.Fatal("absence proved success", same, err)
				}
				evidence.found = true
				same, err = service.ReconcileChiliMedia(ctx, ref, staged.ID, evidence)
				if err != nil || same.Status != "succeeded" {
					t.Fatal("matching echo not recovered", same, err)
				}
				if evidence.query.Sender != "string-self-"+req.AccountID || evidence.query.Group != "string-group-"+req.GroupID ||
					len(evidence.query.Digest) != 32 {
					t.Fatal("string identity or digest changed")
				}
			} else if finished.Status != "succeeded" || port.writes != 1 {
				t.Fatal("send", finished, port.writes)
			}
			if mode == "video" && (port.dispatch.Asset.Width != 64 || port.dispatch.Asset.Height != 32 ||
				port.dispatch.Asset.Duration != 2 || port.dispatch.Asset.SnapshotSize != int64(len(input.Thumbnail))) {
				t.Fatal("video and cover metadata were conflated")
			}
			before := port.writes
			if _, err = service.DispatchChiliMedia(ctx, ref, staged.ID, port); err != nil || port.writes != before {
				t.Fatal("send replay", err)
			}
			var used, nonceRecords int
			if err = db.Platform.QueryRow(ctx, `SELECT used FROM test_windows WHERE id=$1`, req.TestWindowID).Scan(&used); err != nil {
				t.Fatal(err)
			}
			if mode != "restart" && used != 2 {
				t.Fatal("upload and send did not consume separate attempts", used)
			}
			if err = db.Platform.QueryRow(ctx, `SELECT count(*) FROM native_text_attempts WHERE operation_id=$1`, staged.ID).
				Scan(&nonceRecords); err != nil || nonceRecords != 0 {
				t.Fatal("Chili touched WSL nonce allocation", err)
			}
			if mode == "valid" {
				var count int
				err := db.Tenant.QueryRow(ctx, `SELECT count(*) FROM lajiaoliao_media_attempts`).Scan(&count)
				var denied *pgconn.PgError
				if !errors.As(err, &denied) || denied.Code != "42501" {
					t.Fatal("tenant could read platform media attempts", err)
				}
				var forced bool
				if err := db.Platform.QueryRow(ctx, `SELECT relrowsecurity AND relforcerowsecurity FROM pg_class
					WHERE oid='lajiaoliao_media_attempts'::regclass`).Scan(&forced); err != nil || !forced {
					t.Fatal("Chili attempt RLS not forced", err)
				}
			}
		})
	}
}
