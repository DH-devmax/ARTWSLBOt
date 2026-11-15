package httpapi

import (
	"context"
	"net/http"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

func (s *Server) nativeSocial(w http.ResponseWriter, r *http.Request, p Principal) error {
	if s.Receiver == nil {
		return fail(503, "native_read_owner_unavailable")
	}
	group := r.URL.Query().Get("group_id")
	if group != "" && control.ValidateNativeChange(requestWorkspace(r), nativeauth.SettingChange{Kind: "group_leave", Group: group}) != nil {
		return fail(400, "native_social_request_invalid")
	}
	view, err := s.Receiver.Social(r.Context(), r.PathValue("id"), group)
	if err != nil {
		return nativeSettingsError(w, err)
	}
	if err = s.memberResultAccess(r, p, gateway.MemberSnapshot{AccountEpoch: view.AccountEpoch, DeploymentVersion: view.DeploymentVersion}); err != nil {
		return err
	}
	send(w, 200, map[string]any{"snapshot": view, "native_ready": false,
		"acceptance": "pending", "application_scope": "server_returned_window"})
	return nil
}

func socialEvidence(snapshot gateway.SocialSnapshot, change nativeauth.SettingChange) control.SettingEvidence {
	proof := settingEvidence(snapshot.SettingSnapshot)
	_, err := snapshot.ValidateChange(change)
	proof.SocialAllowed = err == nil
	proof.Target = change.Target
	proof.Matches = snapshot.Matches(change)
	return proof
}

func (s *Server) changeNativeSocial(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Change  nativeauth.SettingChange `json:"change"`
		Digest  string                   `json:"digest"`
		Epoch   int64                    `json:"account_epoch"`
		Version int64                    `json:"account_version"`
		Window  string                   `json:"test_window_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Change.Kind == "friend_delete" {
		return fail(403, "native_friend_delete_controlled_only")
	}
	if in.Change.Kind == "friend_approve" {
		return fail(403, "native_friend_approve_controlled_only")
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
		Key: r.Header.Get("Idempotency-Key"), Digest: in.Digest, Epoch: in.Epoch, Version: in.Version, Change: in.Change,
		TestWindowID: in.Window}
	result, err := s.nativeSettingsService().ExecuteSocial(r.Context(), ref, req,
		func(ctx context.Context, fence control.SettingFence) error {
			return s.Receiver.ChangeSocial(ctx, req.Account, req.Key, req.Change,
				func(work context.Context, snapshot gateway.SocialSnapshot, submit func(context.Context) error) error {
					return fence(work, socialEvidence(snapshot, req.Change), submit)
				})
		})
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"operation": result, "native_ready": false})
	return nil
}

func (s *Server) reconcileNativeSocial(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct{}
	if err := decode(w, r, &in); err != nil {
		return err
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
	out, err := s.nativeSettingsService().Reconcile(r.Context(), ref, r.PathValue("id"), r.PathValue("operation"),
		func(ctx context.Context, req control.SettingRequest) (control.SettingEvidence, error) {
			if !nativeauth.SocialKind(req.Change.Kind) || req.Workspace != requestWorkspace(r) {
				return control.SettingEvidence{}, fail(403, "workspace_route_forbidden")
			}
			reader := s.Receiver.Social
			if req.Change.Kind == "friend_approve" && req.TestWindowID != "" {
				reader = s.Receiver.SocialApplication
			}
			view, err := reader(ctx, req.Account, req.Change.Group)
			if err != nil {
				return control.SettingEvidence{}, nativeSettingsError(w, err)
			}
			return socialEvidence(view, req.Change), nil
		})
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"operation": out, "read_only": true, "native_ready": false})
	return nil
}
