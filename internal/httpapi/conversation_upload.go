package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

// Upload-only controlled composition; this never registers a message sender.
type nativeMediaPort struct{ receiver *gateway.ReceiverManager }

func (p nativeMediaPort) UploadChiliMedia(ctx context.Context, d control.ChiliMediaDispatch, guard func(context.Context) error) (lajiaoliao.MediaAsset, error) {
	if p.receiver == nil {
		return lajiaoliao.MediaAsset{}, fail(503, "native_read_owner_unavailable")
	}
	return p.receiver.UploadChiliMedia(ctx, gateway.ChiliMediaIntent{AccountID: d.Request.AccountID,
		ReceiverOwner: d.Request.ReceiverOwner, AccountEpoch: d.Request.AccountEpoch, Revision: d.Request.ReceiverRevision,
		Operation: d.ID, Sender: d.Sender, Group: d.Group, Peer: d.Peer, Input: d.Input}, guard)
}

func (p nativeMediaPort) SubmitChiliMedia(ctx context.Context, d control.ChiliMediaDispatch, fence nativewire.SubmissionFence) (lajiaoliao.Receipt, error) {
	if p.receiver == nil {
		return lajiaoliao.Receipt{}, fail(503, "native_read_owner_unavailable")
	}
	return p.receiver.SubmitChiliMedia(ctx, gateway.ChiliMediaIntent{AccountID: d.Request.AccountID,
		ReceiverOwner: d.Request.ReceiverOwner, AccountEpoch: d.Request.AccountEpoch, Revision: d.Request.ReceiverRevision,
		Operation: d.ID, Sender: d.Sender, Group: d.Group, Peer: d.Peer, ClientID: d.ClientID,
		Asset: d.Asset, Message: d.Message}, fence)
}

func (p nativeMediaPort) UploadMedia(ctx context.Context, d control.MediaUploadDispatch, guard func(context.Context) error) (nativewire.OutboundMedia, error) {
	if p.receiver == nil {
		return nativewire.OutboundMedia{}, fail(503, "native_read_owner_unavailable")
	}
	if d.Input.Kind != "image" && d.Input.Kind != "voice" && d.Input.Kind != "video" {
		return nativewire.OutboundMedia{}, fail(400, "native_upload_kind_unavailable")
	}
	var group string
	if err := p.receiver.DB.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1 AND platform='wsl'`,
		d.Request.GroupID).Scan(&group); err != nil {
		return nativewire.OutboundMedia{}, err
	}
	gid, err := strconv.ParseUint(group, 10, 30)
	if err != nil {
		return nativewire.OutboundMedia{}, err
	}
	return p.receiver.UploadMessage(ctx, gateway.MediaUploadIntent{AccountID: d.Request.AccountID,
		ReceiverOwner: d.Request.ReceiverOwner, AccountEpoch: d.Request.AccountEpoch, Revision: d.Request.ReceiverRevision,
		Operation: d.ID, Sender: d.Sender, Group: gid, Input: d.Input,
		PrivatePeer: d.Peer, PrivateConversation: d.Request.PrivateConversation, PrivateEventID: d.Request.PrivateEventID}, guard)
}

func (s *Server) uploadConversationMedia(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) == "wsl" && s.nativeMediaTestPort == nil ||
		requestWorkspace(r) != "wsl" && !s.mediaPortAvailable(requestWorkspace(r)) {
		return fail(409, "native_media_acceptance_pending")
	}
	scope, err := s.conversationSendScope(r)
	if err != nil {
		return err
	}
	select {
	case s.mediaSlots <- struct{}{}:
		defer func() { <-s.mediaSlots }()
	default:
		return fail(429, "native_upload_busy")
	}
	// Streaming multipart parser: ParseMultipartForm would spill to disk.
	r.Body = http.MaxBytesReader(w, r.Body, nativewire.MaxUploadBytes+1024*1024+8192)
	reader, err := r.MultipartReader()
	if err != nil {
		return fail(400, "native_upload_invalid")
	}
	var input nativeauth.MessageUpload
	defer func() { clear(input.Data); clear(input.Thumbnail) }()
	var request struct {
		Kind         string `json:"kind"`
		TestWindowID string `json:"test_window_id"`
		AccountEpoch int64  `json:"account_epoch"`
		BindingEpoch int64  `json:"binding_epoch"`
	}
	seen := make(map[string]bool)
	for count := 0; ; count++ {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil || count >= 3 || seen[part.FormName()] {
			return fail(400, "native_upload_invalid")
		}
		name := part.FormName()
		seen[name] = true
		limit := int64(2048)
		if name == "file" {
			limit = nativewire.MaxUploadBytes
		} else if name == "thumbnail" {
			limit = 1024 * 1024
		} else if name != "request" {
			return fail(400, "native_upload_invalid")
		}
		data, e := io.ReadAll(io.LimitReader(part, limit+1))
		part.Close()
		if e != nil || int64(len(data)) > limit {
			clear(data)
			return fail(413, "native_upload_too_large")
		}
		switch name {
		case "file":
			input.Name, input.Data = part.FileName(), data
		case "thumbnail":
			input.Thumbnail = data
		case "request":
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			e = decoder.Decode(&request)
			if e != nil || decoder.Decode(new(any)) != io.EOF {
				clear(data)
				return fail(400, "native_upload_invalid")
			}
			clear(data)
		}
	}
	input.Kind = request.Kind
	if input.Kind == "file" {
		return fail(409, "native_capability_not_in_product")
	}
	if !seen["file"] || !seen["request"] {
		return fail(409, "native_upload_scope_changed")
	}
	textReq, err := s.conversationTextRequest(r, p, scope, request.TestWindowID, request.AccountEpoch, request.BindingEpoch)
	if err != nil {
		return err
	}
	var out control.MediaUploadOperation
	service := s.nativeTextService()
	if scope.Platform == "lajiaoliao" {
		out, err = service.UploadChiliMedia(r.Context(), assistantRef(r), textReq, input, s.chiliMediaTestPort)
	} else {
		out, err = service.UploadMedia(r.Context(), assistantRef(r), textReq, input, s.nativeMediaTestPort)
	}
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}

func (s *Server) inspectMediaUpload(w http.ResponseWriter, r *http.Request, p Principal) error {
	out, err := s.nativeTextService().InspectMediaUpload(r.Context(), assistantRef(r), r.PathValue("id"),
		r.PathValue("upload"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}

func (s *Server) stageNativeMedia(w http.ResponseWriter, r *http.Request, p Principal) error {
	if !s.mediaPortAvailable(requestWorkspace(r)) {
		return fail(409, "native_media_acceptance_pending")
	}
	scope, err := s.conversationSendScope(r)
	if err != nil {
		return err
	}
	var in struct {
		UploadID     string `json:"upload_id"`
		TestWindowID string `json:"test_window_id"`
		AccountEpoch int64  `json:"account_epoch"`
		BindingEpoch int64  `json:"binding_epoch"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	textReq, err := s.conversationTextRequest(r, p, scope, in.TestWindowID, in.AccountEpoch, in.BindingEpoch)
	if err != nil {
		return err
	}
	if !control.ValidID(in.UploadID) {
		return fail(400, "native_upload_invalid")
	}
	textReq.MediaUploadID = in.UploadID
	var out control.TextOperation
	service := s.nativeTextService()
	if scope.Platform == "lajiaoliao" {
		out, err = service.StageChiliMedia(r.Context(), assistantRef(r), textReq)
	} else {
		out, err = service.Stage(r.Context(), assistantRef(r), textReq)
	}
	if err != nil {
		return err
	}
	send(w, 201, out)
	return nil
}

func (s *Server) mediaPortAvailable(platform string) bool {
	if platform == "lajiaoliao" {
		return s.chiliMediaTestPort != nil
	}
	return platform == "wsl" && s.nativeTextTestPort != nil && s.nativeMediaTestPort != nil
}
