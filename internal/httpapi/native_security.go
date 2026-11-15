package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

func (s *Server) nativeSecurityChallenge(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) != "lajiaoliao" || s.Receiver == nil {
		return fail(409, "lajiaoliao_action_unavailable")
	}
	purpose := map[string]int{"password_sms": 2, "phone_old_sms": 6, "phone_new_sms": 7}[r.URL.Query().Get("kind")]
	if purpose == 0 {
		return fail(400, "invalid_request")
	}
	challenge, err := s.Receiver.ChiliSecurityChallenge(r.Context(), r.PathValue("id"), purpose)
	if err != nil {
		return nativeSettingsError(w, err)
	}
	send(w, 200, map[string]any{"challenge": challenge, "native_ready": false})
	return nil
}

func (s *Server) changeNativeSecurity(w http.ResponseWriter, r *http.Request, p Principal) error {
	select {
	case s.mediaSlots <- struct{}{}:
		defer func() { <-s.mediaSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		return fail(429, "native_security_busy")
	}
	var in struct {
		Kind    string                   `json:"kind"`
		Input   nativeauth.SecurityInput `json:"input"`
		Digest  string                   `json:"digest"`
		Epoch   int64                    `json:"account_epoch"`
		Version int64                    `json:"account_version"`
	}
	if r.Header.Get("Content-Type") != "application/json" {
		return fail(415, "json_required")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1500*1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
		return fail(400, "invalid_request")
	}
	defer clear(in.Input.Image)
	if in.Kind == "secondary_security_code" {
		return fail(409, "native_capability_deferred")
	}
	if err := control.ValidateSecurityInput(requestWorkspace(r), in.Kind, in.Input); err != nil {
		return fail(400, err.Error())
	}
	if s.Receiver == nil {
		return fail(503, "native_read_owner_unavailable")
	}
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return fail(401, "authentication_required")
	}
	ref, err := control.WebsiteSession(cookie.Value)
	if err != nil {
		return fail(401, "authentication_required")
	}
	req := control.SettingRequest{Account: r.PathValue("id"), Workspace: requestWorkspace(r),
		Key: r.Header.Get("Idempotency-Key"), Digest: in.Digest, Epoch: in.Epoch, Version: in.Version,
		Change: nativeauth.SettingChange{Kind: in.Kind}}
	out, err := s.nativeSettingsService().ExecuteSecurity(r.Context(), ref, req, in.Input,
		func(ctx context.Context, state *nativeauth.SecurityState, fence control.SettingFence) error {
			return s.Receiver.ChangeSecurity(ctx, req.Account, req.Key, in.Kind, in.Input, state,
				func(ctx context.Context, snapshot gateway.SettingSnapshot, submit func(context.Context) error) error {
					return fence(ctx, settingEvidence(snapshot), submit)
				})
		})
	in.Input.OldPassword, in.Input.NewPassword, in.Input.Code, in.Input.Captcha = "", "", "", ""
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"operation": out, "native_ready": false, "acceptance": "pending"})
	return nil
}

func (s *Server) nativeSecurityFlow(w http.ResponseWriter, r *http.Request, p Principal) error {
	if !control.ValidID(r.PathValue("flow")) {
		return fail(400, "invalid_operation")
	}
	var out control.SecurityFlow
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT f.id::text,f.stage,f.expires_at FROM native_security_flows f
		JOIN accounts a ON a.id=f.account_id WHERE f.id=$1 AND f.account_id=$2 AND f.actor_id=$3
		AND f.workspace=$4 AND f.account_epoch=a.epoch AND f.expires_at>clock_timestamp()
		AND a.auth_state='authenticated'`, r.PathValue("flow"), r.PathValue("id"), p.ID, requestWorkspace(r)).Scan(&out.ID, &out.Stage, &out.ExpiresAt)
	if err == pgx.ErrNoRows {
		return fail(409, "native_security_flow_expired")
	}
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"flow": out})
	return nil
}
