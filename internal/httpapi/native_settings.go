package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func (s *Server) nativeSettingsService() *control.NativeSettingsService {
	return &control.NativeSettingsService{DB: s.DB, Vault: s.Vault}
}

func (s *Server) nativeSettings(w http.ResponseWriter, r *http.Request, p Principal) error {
	if s.Receiver == nil {
		return fail(503, "native_read_owner_unavailable")
	}
	group, target := r.URL.Query().Get("group_id"), r.URL.Query().Get("target_id")
	if group != "" || target != "" {
		if err := control.ValidateNativeChange(requestWorkspace(r), nativeauth.SettingChange{Kind: "member_nickname", Group: group, Target: target}); err != nil {
			return fail(400, "native_setting_request_invalid")
		}
	}
	view, err := s.Receiver.Settings(r.Context(), r.PathValue("id"), group, target)
	if err != nil {
		return nativeSettingsError(w, err)
	}
	if err = s.memberResultAccess(r, p, gateway.MemberSnapshot{
		AccountEpoch: view.AccountEpoch, DeploymentVersion: view.DeploymentVersion,
	}); err != nil {
		return err
	}
	avatarURL, err := s.nativeAccountAvatarURL(r.PathValue("id"), p.ID, requestWorkspace(r), view)
	if err != nil {
		return err
	}
	if requestWorkspace(r) == "lajiaoliao" {
		send(w, 200, map[string]any{"snapshot": view, "avatar_url": avatarURL, "native_ready": false, "mode": "manual_single_setting",
			"acceptance": "pending", "security": map[string]bool{"avatar": true, "password": false, "phone": false}})
		return nil
	}
	send(w, 200, map[string]any{"snapshot": view, "avatar_url": avatarURL, "native_ready": false,
		"avatar_upload": "implemented_pending_acceptance", "mode": "manual_single_setting", "acceptance": "pending",
		"security": map[string]any{"avatar": true, "password": true, "phone": true,
			"secondary_first_set": false, "identity_status_read": false, "identity_submit": false,
			"phone_contract": "official_api_pending_desktop_acceptance"}})
	return nil
}

func (s *Server) changeNativeSetting(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Change  nativeauth.SettingChange `json:"change"`
		Digest  string                   `json:"digest"`
		Epoch   int64                    `json:"account_epoch"`
		Version int64                    `json:"account_version"`
	}
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
	req := control.SettingRequest{Account: r.PathValue("id"), Workspace: requestWorkspace(r),
		Key: r.Header.Get("Idempotency-Key"), Digest: in.Digest, Epoch: in.Epoch, Version: in.Version, Change: in.Change}
	result, err := s.nativeSettingsService().Execute(r.Context(), ref, req,
		func(ctx context.Context, fence control.SettingFence) error {
			return s.Receiver.ChangeSetting(ctx, req.Account, req.Key, req.Change,
				func(work context.Context, snapshot gateway.SettingSnapshot, submit func(context.Context) error) error {
					return fence(work, settingEvidence(snapshot), submit)
				})
		})
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"operation": result, "native_ready": false})
	return nil
}

func settingEvidence(snapshot gateway.SettingSnapshot) control.SettingEvidence {
	out := control.SettingEvidence{UID: snapshot.SelfID, Role: snapshot.SelfRole, Digest: snapshot.Digest,
		Locked: snapshot.NicknameLocked, Epoch: snapshot.AccountEpoch, Version: snapshot.AccountVersion,
		DeploymentVersion: snapshot.DeploymentVersion, CredentialDigest: snapshot.CredentialDigest,
		ReceiverOwner: snapshot.ReceiverOwner, ReceiverRevision: snapshot.ReceiverRevision,
		ObservedAt: snapshot.SampledAt}
	if snapshot.Member != nil {
		out.Target, out.TargetRole = snapshot.Member.ID, snapshot.Member.Role
	}
	return out
}

func (s *Server) inspectNativeSetting(w http.ResponseWriter, r *http.Request, p Principal) error {
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return fail(401, "authentication_required")
	}
	ref, err := control.WebsiteSession(cookie.Value)
	if err != nil {
		return fail(401, "authentication_required")
	}
	if r.PathValue("operation") == "" && r.URL.Query().Get("key") == "" {
		items, err := s.nativeSettingsService().Pending(r.Context(), ref, r.PathValue("id"))
		if err != nil {
			return err
		}
		send(w, 200, map[string]any{"pending": items})
		return nil
	}
	result, err := s.nativeSettingsService().Inspect(r.Context(), ref, r.PathValue("id"), r.PathValue("operation"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"operation": result})
	return nil
}

func nativeSettingsError(w http.ResponseWriter, err error) error {
	var chili *lajiaoliao.Failure
	if errors.As(err, &chili) {
		if chili.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(chili.RetryAfter.Seconds()))))
			return fail(429, chili.Code)
		}
		return fail(409, chili.Code)
	}
	var native *nativeauth.Error
	if errors.As(err, &native) {
		if native.Code == "native_rate_limited" {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(native.RetryAfter.Seconds()))))
			return fail(429, native.Code)
		}
		return fail(409, native.Code)
	}
	if errors.Is(err, gateway.ErrReadBusy) {
		return fail(429, err.Error())
	}
	if errors.Is(err, gateway.ErrMemberDirectory) || errors.Is(err, gateway.ErrMemberScope) {
		return fail(409, err.Error())
	}
	return fail(503, "native_settings_unavailable")
}

func (s *Server) reconcileNativeSetting(w http.ResponseWriter, r *http.Request, p Principal) error {
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
			if req.Workspace != requestWorkspace(r) {
				return control.SettingEvidence{}, fail(403, "workspace_route_forbidden")
			}
			view, err := s.Receiver.SettingsForReconciliation(ctx, req.Account, req.Change)
			if err != nil {
				return control.SettingEvidence{}, nativeSettingsError(w, err)
			}
			proof := settingEvidence(view)
			c := req.Change
			if c.Kind == "member_nickname" {
				proof.Matches = view.Member != nil && view.Member.Card == c.Text
			} else if v := view.Settings; v != nil {
				switch c.Kind {
				case "avatar":
					if req.Workspace == "lajiaoliao" {
						proof.Matches = view.AvatarResource != "" && view.AvatarResource == c.Text
					} else {
						proof.Matches = v.Avatar == c.Text || v.Avatar == c.Text+".jpg"
					}
				case "nickname":
					proof.Matches = v.Nickname == c.Text
				case "search":
					proof.Matches = v.SearchableAccount != nil && v.SearchableMobile != nil &&
						*v.SearchableAccount == c.SearchAccount && *v.SearchableMobile == c.SearchMobile
				case "friend_policy":
					proof.Matches = v.FriendPolicy == c.Text
				case "group_policy":
					proof.Matches = v.GroupPolicy == c.Text
				case "audio_video":
					proof.Matches = v.AudioVideo != nil && *v.AudioVideo == c.Enabled
				case "profile_gender", "profile_birth", "profile_signature", "push_notifications":
					var detail lajiaoliao.ProfileDetails
					if json.Unmarshal(v.ProfileDetails, &detail) == nil {
						switch c.Kind {
						case "profile_gender":
							proof.Matches = detail.Gender != nil && strconv.Itoa(*detail.Gender) == c.Text
						case "profile_birth":
							proof.Matches = detail.Birth != nil && strconv.FormatInt(*detail.Birth, 10) == c.Text
						case "profile_signature":
							proof.Matches = detail.Signature != nil && *detail.Signature == c.Text
						case "push_notifications":
							var expected lajiaoliao.Notifications
							if json.Unmarshal([]byte(c.Text), &expected) == nil && detail.Push != nil && detail.Push.Complete() {
								a, _ := json.Marshal(detail.Push)
								b, _ := json.Marshal(expected)
								proof.Matches = string(a) == string(b)
							}
						}
					}
				}
			}
			return proof, nil
		})
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"operation": out, "read_only": true})
	return nil
}
