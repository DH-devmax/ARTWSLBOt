package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"time"
	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func syntheticPrivatePNG(t *testing.T) []byte {
	t.Helper()
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func seedPrivateConversation(t *testing.T, db *database.DB, vault *security.Vault, req *control.TextRequest, transport string) {
	t.Helper()
	ctx := context.Background()
	var uid uint64
	if err := db.Platform.QueryRow(ctx, `SELECT native_uid::bigint FROM accounts WHERE id=$1`, req.AccountID).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	seal := func(aad string, value any) []byte {
		t.Helper()
		raw, _ := json.Marshal(value)
		sealed, err := vault.Seal(aad, raw)
		if err != nil {
			t.Fatal(err)
		}
		return sealed
	}
	saved := map[string]any{"deployment_version": 77, "session": map[string]any{"uid": uid, "nim_id": "synthetic-me"}}
	if _, err := db.Platform.Exec(ctx, `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`,
		req.AccountID, seal("native-session:"+req.AccountID, saved)); err != nil {
		t.Fatal(err)
	}
	message := nativewire.PrivateText{SenderID: 8, PeerID: uid, ClientID: control.NewID(),
		Text: "synthetic private source", CreatedAt: time.Now().UTC()}
	attachment, err := nativewire.SealPrivateText(bytes.Repeat([]byte{17}, 32), message, 111, 222)
	if err != nil {
		t.Fatal(err)
	}
	key := control.NewID()
	raw := nativewire.IMMessage{Scene: 0, Target: "synthetic-me", Sender: transport, ServerID: "8001",
		Fields: map[uint32]string{0: "0", 1: "synthetic-me", 2: transport, 7: "1700000000123",
			8: "100", 10: attachment, 11: message.ClientID, 12: "8001"}}
	if err := db.Platform.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
        VALUES($1,$2,'private',clock_timestamp(),$3) RETURNING id`, req.AccountID, key,
		seal("native-event:"+req.AccountID+":"+key, raw)).Scan(&req.PrivateEventID); err != nil {
		t.Fatal(err)
	}
	inner, _ := nativewire.OpenMessage(bytes.Repeat([]byte{17}, 32), attachment)
	projection := gateway.DecodedMessage{Message: inner, TransportPeer: transport, TransportTo: "synthetic-me",
		NativeFormat: "100", ServerID: "8001", Mapping: "authenticated_header"}
	req.PrivateConversation = nativewire.PrivateConversationKey(nativewire.PrivateTarget{ID: 8, Transport: transport})
	if _, err := db.Platform.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
        deployment_version,payload_sealed,received_at) VALUES($1,$2,'decoded','private',$3,77,$4,clock_timestamp())`,
		req.PrivateEventID, req.AccountID, req.PrivateConversation,
		seal(gateway.DecodedAAD(req.AccountID, req.PrivateEventID), projection)); err != nil {
		t.Fatal(err)
	}
}

func insertPrivateEcho(t *testing.T, db *database.DB, vault *security.Vault, account, attachment, client, mode string) {
	t.Helper()
	scene, target, kind := 0, "synthetic-peer-8", "private"
	if mode == "group_echo" {
		scene, target, kind = 1, "789", "group"
	}
	if mode == "other_peer_echo" {
		target = "another-peer"
	}
	raw := nativewire.IMMessage{Scene: scene, Sender: "synthetic-me", Target: target, ServerID: "9001",
		Fields: map[uint32]string{0: strconv.Itoa(scene), 1: target, 2: "synthetic-me", 7: "1700000000123",
			8: "100", 10: attachment, 11: client, 12: "9001"}}
	key := control.NewID()
	blob, _ := json.Marshal(raw)
	sealed, err := vault.Seal("native-event:"+account+":"+key, blob)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(context.Background(), `INSERT INTO native_received_events(account_id,event_key,kind,
        received_at,payload_sealed) VALUES($1,$2,$3,clock_timestamp(),$4)`, account, key, kind, sealed); err != nil {
		t.Fatal(err)
	}
}

func privateConversationHTTP(t *testing.T, server *Server, token, actor string, req control.TextRequest, mode string) control.TextOperation {
	t.Helper()
	principal := Principal{ID: actor, Role: "superadmin"}
	invoke := func(handler endpoint, method, operation string, input any) (*httptest.ResponseRecorder, error) {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, "/", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", req.IdempotencyKey)
		r.Header.Set("X-Workspace", "wsl")
		r.AddCookie(&http.Cookie{Name: "wsl_session", Value: token})
		r.SetPathValue("id", req.AccountID)
		r.SetPathValue("conversation", req.PrivateConversation)
		r.SetPathValue("operation", operation)
		w := httptest.NewRecorder()
		err := handler(w, r, principal)
		return w, err
	}
	w, err := invoke(server.nativeSending, "GET", "", nil)
	if err != nil || !bytes.Contains(w.Body.Bytes(), []byte(`"enabled":true`)) ||
		!bytes.Contains(w.Body.Bytes(), []byte(`"media_kinds":["image"]`)) ||
		bytes.Contains(w.Body.Bytes(), []byte("synthetic-peer-8")) {
		t.Fatal("private context unavailable or leaked route", err, w.Body.String())
	}
	input := map[string]any{"text": req.Text, "test_window_id": req.TestWindowID, "account_epoch": req.AccountEpoch,
		"binding_epoch": req.BindingEpoch}
	if mode == "http_scope_forgery" {
		for _, change := range []map[string]any{{"peer_id": "8"}, {"tenant_id": actor}, {"group_id": req.GroupID},
			{"binding_epoch": 2}, {"test_window_id": control.NewID()}, {"reply_to": "1"},
			{"mentions": []nativewire.MessageMention{{UID: 8, Start: 0, End: 1, Name: "x"}}}} {
			altered := map[string]any{}
			for k, v := range input {
				altered[k] = v
			}
			for k, v := range change {
				altered[k] = v
			}
			if _, err := invoke(server.stageNativeText, "POST", "", altered); err == nil {
				t.Fatal("private HTTP accepted forged scope", change)
			}
		}
		return control.TextOperation{}
	}
	if mode == "http_image" {
		var data bytes.Buffer
		form := multipart.NewWriter(&data)
		request, _ := json.Marshal(map[string]any{"kind": "image", "test_window_id": req.TestWindowID,
			"account_epoch": req.AccountEpoch, "binding_epoch": req.BindingEpoch})
		form.WriteField("request", string(request))
		file, _ := form.CreateFormFile("file", "synthetic.png")
		file.Write(syntheticPrivatePNG(t))
		form.Close()
		r := httptest.NewRequest("POST", "/", &data)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Idempotency-Key", control.NewID())
		r.AddCookie(&http.Cookie{Name: "wsl_session", Value: token})
		r.SetPathValue("id", req.AccountID)
		r.SetPathValue("conversation", req.PrivateConversation)
		w = httptest.NewRecorder()
		if err := server.uploadConversationMedia(w, r, principal); err != nil {
			t.Fatal("private upload", err)
		}
		var result struct {
			Data control.MediaUploadOperation `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.Status != "succeeded" {
			t.Fatal(w.Body.String())
		}
		delete(input, "text")
		input["upload_id"] = result.Data.ID
		w, err = invoke(server.stageNativeMedia, "POST", "", input)
	} else {
		w, err = invoke(server.stageNativeText, "POST", "", input)
	}
	if err != nil {
		t.Fatal("private HTTP stage", err)
	}
	var result struct {
		Data control.TextOperation `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.Status != "queued" {
		t.Fatal(w.Body.String())
	}
	w, err = invoke(server.dispatchNativeText, "POST", result.Data.ID, map[string]any{})
	if err != nil || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("private HTTP dispatch", err)
	}
	return result.Data
}
