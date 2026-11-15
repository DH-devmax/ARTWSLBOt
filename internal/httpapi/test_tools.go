package httpapi

import (
	"context"
	"net/http"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

// These tools are an independent superadmin acceptance surface. They are not
// registered with the tenant management assistant or the heatbot runtime.
func (s *Server) testToolOperations(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) != "wsl" && requestWorkspace(r) != "lajiaoliao" {
		return fail(403, "workspace_route_forbidden")
	}
	if !control.ValidID(r.URL.Query().Get("account_id")) {
		return fail(400, "invalid_test_scope")
	}
	service := s.nativeSettingsService()
	var allowed bool
	if err := s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1
		AND workspace=$2 AND platform=$2)`, r.URL.Query().Get("account_id"), requestWorkspace(r)).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return fail(404, "account_not_found")
	}
	out, err := service.TestOperations(r.Context(), assistantRef(r), r.URL.Query().Get("account_id"))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	send(w, 200, out)
	return nil
}

func (s *Server) testToolCatalog(w http.ResponseWriter, r *http.Request, p Principal) error {
	if requestWorkspace(r) != "wsl" && requestWorkspace(r) != "lajiaoliao" {
		return fail(403, "workspace_route_forbidden")
	}
	tools := []map[string]any{}
	add := func(name, description string, properties map[string]any, required []string, effect bool) {
		tools = append(tools, map[string]any{"name": name, "description": description,
			"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
			"annotations": map[string]bool{"readOnlyHint": !effect, "destructiveHint": effect, "idempotentHint": true}})
	}
	text := func() map[string]any { return map[string]any{"type": "string", "minLength": 1, "maxLength": 100} }
	grant := map[string]any{"account_id": text(), "test_window_id": text()}
	add("read_group", "读取已授权隔离群的成员、角色、群禁言及个人禁言状态。", grant, []string{"account_id", "test_window_id"}, false)
	invitations := "读取账号可见窗口中属于授权隔离群的邀请及入群记录；不是完整历史或奖励依据。"
	socialActions := []string{"group_invite", "group_leave", "group_accept", "friend_approve", "friend_delete"}
	moderationActions := []string{"mute_member", "unmute_member", "mute_group", "unmute_group", "kick_member",
		"publish_notice", "grant_admin", "revoke_admin"}
	socialDescription := "在授权窗口内邀请一位好友、接受本人的指定群邀请或自行退群；好友同意需锁定申请编号、申请人及一次预算，删除好友仅限独立单次原生调试窗口。未知结果只核对原操作。"
	if requestWorkspace(r) == "lajiaoliao" {
		invitations = "读取授权隔离群当前可见的入群申请；不提供邀请人归属或完整历史。"
		moderationActions = append(moderationActions, "member_nickname", "group_info")
		moderationActions = append(moderationActions, "kick_member_ban", "remove_group_ban")
		add("read_group_bans", "读取本群原生永久禁入名单；与业务黑名单独立。", grant, []string{"account_id", "test_window_id"}, false)
	} else {
		moderationActions = append(moderationActions, "group_info", "member_nickname")
	}
	add("read_invitations", invitations, grant, []string{"account_id", "test_window_id"}, false)
	add("read_membership", "只读核验授权群的入退群状态、当前角色及邀请所需好友关系。", grant, []string{"account_id", "test_window_id"}, false)
	add("run_social", socialDescription, map[string]any{
		"account_id": text(), "test_window_id": text(),
		"action":    map[string]any{"type": "string", "enum": socialActions},
		"target_id": text(), "text": text(),
	}, []string{"account_id", "test_window_id", "action"}, true)
	add("run_moderation", "在超管授权窗口内提交一次指定群管动作；未知结果不得重发。", map[string]any{
		"account_id": text(), "test_window_id": text(), "action": map[string]any{"type": "string", "enum": moderationActions},
		"target_id": text(), "text": map[string]any{"type": "string", "maxLength": 2400},
		"minutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
	}, []string{"account_id", "test_window_id", "action"}, true)
	for _, name := range []string{"inspect_operation", "reconcile_operation"} {
		add(name, "仅查询本人原操作或只读核对期望状态，不再次提交。", map[string]any{"account_id": text(),
			"operation_id": text(), "key": text()}, []string{"account_id"}, false)
	}
	unavailable := []string{"send_file: not_in_product"}
	sendEndpoint, recallEndpoint := "/api/v1/accounts/{id}/conversations/{key}/text-operations", "/api/v1/test-tools/recall"
	if requestWorkspace(r) != "wsl" {
		sendEndpoint = "/api/v1/test-tools/text"
		unavailable = []string{"send_image: acceptance_pending", "send_voice: acceptance_pending", "send_video: acceptance_pending",
			"send_file: not_in_product"}
	}
	send(w, 200, map[string]any{"tools": tools, "transport": "authenticated_json_tools_v1",
		"call_endpoint": "/api/v1/test-tools/call", "grant_creation": "explicit_superadmin_only",
		"limits":          map[string]int{"seconds": 300, "actions": 10, "mute_minutes": 5},
		"send_endpoint":   sendEndpoint,
		"recall_endpoint": recallEndpoint,
		"unavailable":     unavailable,
		"native_ready":    false, "ai_ready": false})
	return nil
}

func moderationEvidence(snapshot gateway.ModerationSnapshot, change nativeauth.SettingChange) control.SettingEvidence {
	proof := settingEvidence(snapshot.SettingSnapshot)
	for _, member := range snapshot.Members {
		if member.Role == "owner" {
			if proof.OwnerID != "" || member.ID == "" {
				proof.OwnerID = ""
				break
			}
			proof.OwnerID = member.ID
		}
	}
	proof.Target, proof.TargetRole = change.Target, snapshot.TargetRole(change.Target)
	proof.ModerationAllowed = snapshot.ValidateChange(change) == nil
	proof.Matches = snapshot.Matches(change)
	proof.GroupBanKnown, proof.TargetBanned = snapshot.BansKnown, snapshot.Banned(change.Target)
	return proof
}

func (s *Server) testToolCall(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Name string `json:"name"`
		Args struct {
			Account string `json:"account_id"`
			Window  string `json:"test_window_id"`
			Action  string `json:"action"`
			Target  string `json:"target_id"`
			Text    string `json:"text"`
			Minutes int    `json:"minutes"`
			ID      string `json:"operation_id"`
			Key     string `json:"key"`
		} `json:"arguments"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	workspace := requestWorkspace(r)
	if !control.ValidID(in.Args.Account) || workspace != "wsl" && workspace != "lajiaoliao" {
		return fail(400, "invalid_test_scope")
	}
	var accountScope bool
	if err := s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM accounts
        WHERE id=$1 AND workspace=$2 AND platform=$2)`, in.Args.Account, workspace).Scan(&accountScope); err != nil {
		return err
	}
	if !accountScope {
		return fail(404, "account_not_found")
	}
	ref := assistantRef(r)
	service := s.nativeSettingsService()
	if in.Name == "inspect_operation" || in.Name == "reconcile_operation" {
		if in.Args.Window != "" || in.Args.Action != "" || in.Args.Target != "" || in.Args.Text != "" || in.Args.Minutes != 0 {
			return fail(400, "invalid_tool_arguments")
		}
		out, err := service.Inspect(r.Context(), ref, in.Args.Account, in.Args.ID, in.Args.Key)
		if err != nil {
			return err
		}
		messageOperation := out.Kind == "recall_message" || workspace == "lajiaoliao" && out.Kind == "send_text"
		if !messageOperation && !control.NativeModerationActionForPlatform(workspace, out.Kind) && !nativeauth.GroupMembershipKind(out.Kind) && out.Kind != "friend_delete" && out.Kind != "friend_approve" &&
			!(workspace == "wsl" && out.Kind == "member_nickname") {
			return fail(400, "invalid_tool_operation")
		}
		if in.Name == "reconcile_operation" && out.Status == "unknown" {
			if messageOperation {
				return fail(409, "native_message_readback_required")
			}
			if s.Receiver == nil {
				return fail(503, "native_read_owner_unavailable")
			}
			out, err = service.Reconcile(r.Context(), ref, in.Args.Account, out.ID,
				func(ctx context.Context, req control.SettingRequest) (control.SettingEvidence, error) {
					if req.Workspace != workspace || req.TestWindowID == "" {
						return control.SettingEvidence{}, fail(403, "invalid_tool_operation")
					}
					if nativeauth.GroupMembershipKind(req.Change.Kind) || req.Change.Kind == "friend_delete" || req.Change.Kind == "friend_approve" {
						var view gateway.SocialSnapshot
						var err error
						if req.Change.Kind == "friend_approve" {
							view, err = s.Receiver.SocialApplication(ctx, req.Account, req.Change.Group)
						} else {
							view, err = s.Receiver.Social(ctx, req.Account, req.Change.Group)
						}
						if err != nil {
							return control.SettingEvidence{}, nativeSettingsError(w, err)
						}
						return socialEvidence(view, req.Change), nil
					}
					if workspace == "wsl" && req.Change.Kind == "member_nickname" {
						view, err := s.Receiver.Settings(ctx, req.Account, req.Change.Group, req.Change.Target)
						if err != nil {
							return control.SettingEvidence{}, nativeSettingsError(w, err)
						}
						proof := settingEvidence(view)
						proof.Matches = view.Member != nil && view.Member.ID == req.Change.Target && view.Member.Card == req.Change.Text
						return proof, nil
					}
					var view gateway.ModerationSnapshot
					var err error
					if lajiaoliao.BanAction(req.Change.Kind) {
						view, err = s.Receiver.GroupBans(ctx, req.Account, req.Change.Group)
					} else {
						view, err = s.Receiver.Moderation(ctx, req.Account, req.Change.Group)
					}
					if err != nil {
						return control.SettingEvidence{}, nativeSettingsError(w, err)
					}
					return moderationEvidence(view, req.Change), nil
				})
			if err != nil {
				return err
			}
		}
		send(w, 200, map[string]any{"operation": out, "read_only": true})
		return nil
	}
	if in.Name != "read_group" && in.Name != "read_group_bans" && in.Name != "read_invitations" && in.Name != "run_moderation" &&
		in.Name != "read_membership" && in.Name != "run_social" || in.Args.ID != "" || in.Args.Key != "" {
		return fail(400, "unknown_test_tool")
	}
	change := nativeauth.SettingChange{Kind: in.Args.Action, Target: in.Args.Target, Text: in.Args.Text, Minutes: in.Args.Minutes}
	wslCard := workspace == "wsl" && change.Kind == "member_nickname"
	if workspace == "lajiaoliao" && change.Kind != "" && !control.NativeDebugActionForPlatform(workspace, change.Kind) {
		return fail(400, "lajiaoliao_action_unavailable")
	}
	if in.Name != "run_moderation" && in.Name != "run_social" && (change.Kind != "" || change.Target != "" || change.Text != "" || change.Minutes != 0) {
		return fail(400, "invalid_tool_arguments")
	}
	if in.Name == "run_moderation" || in.Name == "run_social" {
		if in.Name == "run_moderation" && ((!control.NativeModerationActionForPlatform(workspace, change.Kind) && !wslCard) || change.Minutes > 5) ||
			in.Name == "run_social" && ((!nativeauth.GroupMembershipKind(change.Kind) && change.Kind != "friend_delete" && change.Kind != "friend_approve") ||
				change.Kind != "group_accept" && change.Kind != "friend_approve" && change.Text != "" || change.Minutes != 0) {
			return fail(400, "native_moderation_request_invalid")
		}
		if out, found, err := service.InspectTestTool(r.Context(), ref, in.Args.Account,
			r.Header.Get("Idempotency-Key"), in.Args.Window, change); err != nil {
			return err
		} else if found {
			send(w, 200, map[string]any{"operation": out, "recovered": true})
			return nil
		}
	}
	scopeAction := change.Kind
	if in.Name == "read_membership" {
		scopeAction = "read_membership"
	}
	readScope := func() (control.TestScope, error) {
		if change.Kind == "friend_approve" {
			return service.TestScopeForApplication(r.Context(), ref, workspace, in.Args.Window, in.Args.Account,
				scopeAction, change.Target, change.Text)
		}
		return service.TestScope(r.Context(), ref, workspace, in.Args.Window, in.Args.Account, scopeAction, change.Target)
	}
	scope, err := readScope()
	if err != nil {
		return err
	}
	if s.Receiver == nil {
		return fail(503, "native_read_owner_unavailable")
	}
	if in.Name == "read_group_bans" {
		if workspace != "lajiaoliao" {
			return fail(400, "lajiaoliao_action_unavailable")
		}
		view, err := s.Receiver.GroupBans(r.Context(), in.Args.Account, scope.NativeGroup)
		if err != nil {
			return nativeSettingsError(w, err)
		}
		if err = s.memberResultAccessForAccount(r, p, in.Args.Account, gateway.MemberSnapshot{
			AccountEpoch: view.AccountEpoch, DeploymentVersion: view.DeploymentVersion}); err != nil {
			return err
		}
		if _, err = service.TestScope(r.Context(), ref, workspace, in.Args.Window, in.Args.Account, "", ""); err != nil {
			return err
		}
		send(w, 200, map[string]any{"snapshot": view, "scope": scope, "read_only": true})
		return nil
	}
	if in.Name == "read_membership" || in.Name == "run_social" {
		change.Group = scope.NativeGroup
		if in.Name == "run_social" && control.ValidateNativeChange(workspace, change) != nil {
			return fail(400, "native_social_request_invalid")
		}
		var view gateway.SocialSnapshot
		if scope.FriendApplicationID != "" {
			view, err = s.Receiver.SocialApplication(r.Context(), in.Args.Account, scope.NativeGroup)
		} else {
			view, err = s.Receiver.Social(r.Context(), in.Args.Account, scope.NativeGroup)
		}
		if err != nil {
			return nativeSettingsError(w, err)
		}
		if err = s.memberResultAccessForAccount(r, p, in.Args.Account, gateway.MemberSnapshot{
			AccountEpoch: view.AccountEpoch, DeploymentVersion: view.DeploymentVersion}); err != nil {
			return err
		}
		if _, err = readScope(); err != nil {
			return err
		}
		if in.Name == "read_membership" {
			send(w, 200, map[string]any{"snapshot": view, "scope": scope, "read_only": true})
			return nil
		}
		req := control.SettingRequest{Account: in.Args.Account, Workspace: workspace, TestWindowID: in.Args.Window,
			Key: r.Header.Get("Idempotency-Key"), Digest: view.Digest, Epoch: view.AccountEpoch, Version: view.AccountVersion, Change: change}
		out, err := service.ExecuteSocial(r.Context(), ref, req, func(ctx context.Context, fence control.SettingFence) error {
			return s.Receiver.ChangeSocial(ctx, req.Account, req.Key, change,
				func(work context.Context, snapshot gateway.SocialSnapshot, submit func(context.Context) error) error {
					return fence(work, socialEvidence(snapshot, change), submit)
				})
		})
		if err != nil {
			return err
		}
		send(w, 200, map[string]any{"operation": out, "native_ready": false})
		return nil
	}
	if in.Name == "read_invitations" {
		view, err := s.Receiver.Invitations(r.Context(), in.Args.Account, scope.NativeGroup)
		if err != nil {
			return nativeSettingsError(w, err)
		}
		if err = s.memberResultAccessForAccount(r, p, in.Args.Account, gateway.MemberSnapshot{AccountEpoch: view.AccountEpoch, DeploymentVersion: view.DeploymentVersion}); err != nil {
			return err
		}
		if _, err = service.TestScope(r.Context(), ref, workspace, in.Args.Window, in.Args.Account, "", ""); err != nil {
			return err
		}
		send(w, 200, map[string]any{"snapshot": view, "read_only": true})
		return nil
	}
	if in.Name == "run_moderation" {
		change.Group = scope.NativeGroup
		if (!control.NativeModerationActionForPlatform(workspace, change.Kind) && !wslCard) || control.ValidateNativeChange(workspace, change) != nil || change.Minutes > 5 {
			return fail(400, "native_moderation_request_invalid")
		}
	}
	if in.Name == "run_moderation" && wslCard {
		view, err := s.Receiver.Settings(r.Context(), in.Args.Account, scope.NativeGroup, change.Target)
		if err != nil {
			return nativeSettingsError(w, err)
		}
		if err = s.memberResultAccessForAccount(r, p, in.Args.Account, gateway.MemberSnapshot{
			AccountEpoch: view.AccountEpoch, DeploymentVersion: view.DeploymentVersion}); err != nil {
			return err
		}
		req := control.SettingRequest{Account: in.Args.Account, Workspace: workspace, TestWindowID: in.Args.Window,
			Key: r.Header.Get("Idempotency-Key"), Digest: view.Digest, Epoch: view.AccountEpoch, Version: view.AccountVersion, Change: change}
		out, err := service.ExecuteTestSetting(r.Context(), ref, req, func(ctx context.Context, fence control.SettingFence) error {
			return s.Receiver.ChangeSetting(ctx, req.Account, req.Key, change,
				func(work context.Context, snapshot gateway.SettingSnapshot, submit func(context.Context) error) error {
					return fence(work, settingEvidence(snapshot), submit)
				})
		})
		if err != nil {
			return err
		}
		send(w, 200, map[string]any{"operation": out, "native_ready": false})
		return nil
	}
	var view gateway.ModerationSnapshot
	if lajiaoliao.BanAction(change.Kind) {
		view, err = s.Receiver.GroupBans(r.Context(), in.Args.Account, scope.NativeGroup)
	} else {
		view, err = s.Receiver.Moderation(r.Context(), in.Args.Account, scope.NativeGroup)
	}
	if err != nil {
		return nativeSettingsError(w, err)
	}
	if err = s.memberResultAccessForAccount(r, p, in.Args.Account, gateway.MemberSnapshot{AccountEpoch: view.AccountEpoch, DeploymentVersion: view.DeploymentVersion}); err != nil {
		return err
	}
	if _, err = service.TestScope(r.Context(), ref, workspace, in.Args.Window, in.Args.Account, change.Kind, change.Target); err != nil {
		return err
	}
	if in.Name == "read_group" {
		send(w, 200, map[string]any{"snapshot": view, "notices": view.Notices, "scope": scope, "read_only": true})
		return nil
	}
	req := control.SettingRequest{Account: in.Args.Account, Workspace: workspace, TestWindowID: in.Args.Window,
		Key: r.Header.Get("Idempotency-Key"), Digest: view.Digest, Epoch: view.AccountEpoch, Version: view.AccountVersion, Change: change}
	out, err := service.ExecuteModeration(r.Context(), ref, req, func(ctx context.Context, fence control.SettingFence) error {
		return s.Receiver.ChangeModeration(ctx, req.Account, req.Key, change,
			func(work context.Context, snapshot gateway.ModerationSnapshot, submit func(context.Context) error) error {
				return fence(work, moderationEvidence(snapshot, change), submit)
			})
	})
	if err != nil {
		return err
	}
	send(w, 200, map[string]any{"operation": out, "native_ready": false})
	return nil
}
