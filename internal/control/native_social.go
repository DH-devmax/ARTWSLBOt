package control

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativeauth"
)

// Social operations share durable idempotency, expiry, recovery and final
// account/session fences with profile operations, but have their own policy.
func (s *NativeSettingsService) ExecuteSocial(ctx context.Context, ref SessionRef, req SettingRequest, port SettingPort) (SettingOperation, error) {
	if !nativeauth.SocialKind(req.Change.Kind) ||
		(nativeauth.GroupMembershipKind(req.Change.Kind) || req.Change.Kind == "friend_delete" || req.Change.Kind == "friend_approve") && !ValidID(req.TestWindowID) {
		return SettingOperation{}, Denied(400, "native_social_request_invalid")
	}
	return s.execute(ctx, ref, req, port, nil)
}

func (s *NativeSettingsService) ExecuteModeration(ctx context.Context, ref SessionRef, req SettingRequest, port SettingPort) (SettingOperation, error) {
	if !NativeModerationActionForPlatform(req.Workspace, req.Change.Kind) || !ValidID(req.TestWindowID) {
		return SettingOperation{}, Denied(400, "native_moderation_request_invalid")
	}
	return s.execute(ctx, ref, req, port, nil)
}

func (s *NativeSettingsService) authorizeSocial(ctx context.Context, tx pgx.Tx, req SettingRequest,
	proof *SettingEvidence, expectedGroup string, expectedBinding int64, observation, debug bool) (string, int64, error) {
	if observation {
		return "", 0, nil
	}
	if req.Change.Kind == "friend_delete" || req.Change.Kind == "friend_approve" {
		if !debug || req.Change.Group == "" || !ValidID(req.TestWindowID) {
			return "", 0, Denied(403, "native_"+req.Change.Kind+"_controlled_only")
		}
		if proof != nil && (proof.UID == req.Change.Target || proof.Role != "owner" && proof.Role != "admin") {
			return "", 0, Denied(403, "native_social_role_required")
		}
	}
	moderation := NativeModerationActionForPlatform(req.Workspace, req.Change.Kind)
	if proof != nil {
		if (!moderation && !proof.SocialAllowed ||
			moderation && !proof.ModerationAllowed) || proof.Target != req.Change.Target {
			return "", 0, Denied(409, "native_social_evidence_required")
		}
		if req.Change.Kind == "kick_member_ban" && (req.Workspace != "lajiaoliao" || !proof.GroupBanKnown) {
			return "", 0, Denied(409, "native_social_evidence_required")
		}
		if req.Change.Kind == "remove_group_ban" {
			if req.Workspace != "lajiaoliao" || !proof.GroupBanKnown || !proof.TargetBanned ||
				proof.Role != "admin" && proof.Role != "owner" || proof.UID == req.Change.Target || proof.TargetRole == "owner" {
				return "", 0, Denied(403, "native_moderation_target_protected")
			}
		} else if moderation && (proof.Role != "admin" && proof.Role != "owner" ||
			req.Change.Target != "" && (proof.UID == req.Change.Target || proof.TargetRole == "owner" ||
				proof.TargetRole != "member" && !(proof.Role == "owner" && proof.TargetRole == "admin"))) {
			return "", 0, Denied(403, "native_moderation_target_protected")
		}
		if req.Change.Kind == "grant_admin" || req.Change.Kind == "revoke_admin" {
			targetRole := "member"
			if req.Change.Kind == "revoke_admin" {
				targetRole = "admin"
			}
			if proof.Role != "owner" || proof.TargetRole != targetRole {
				return "", 0, Denied(403, "native_moderation_role_required")
			}
		}
		if req.Change.Kind == "group_invite" && proof.Role != "admin" && proof.Role != "owner" ||
			req.Change.Kind == "group_leave" && proof.Role != "admin" && proof.Role != "member" {
			return "", 0, Denied(403, "native_social_role_required")
		}
		if req.Change.Kind == "group_accept" && req.Change.Target != proof.UID {
			return "", 0, Denied(403, "native_invitation_self_required")
		}
	}
	if req.Change.Group == "" {
		return "", 0, nil // Shared account contacts remain superadmin-only.
	}
	var group, tenant string
	var binding int64
	var management, ai bool
	lock := " FOR UPDATE OF b"
	if proof != nil {
		lock = " FOR SHARE OF g,b"
	}
	err := tx.QueryRow(ctx, `SELECT g.id::text,b.tenant_id::text,b.epoch,b.management_enabled,b.ai_enabled
        FROM native_groups g JOIN group_bindings b ON b.group_id=g.id JOIN accounts a ON a.id=$2
        WHERE g.platform=$4 AND a.platform=g.platform AND g.native_id=$1 AND b.active
        AND ((a.purpose='management' AND b.primary_account_id=a.id) OR (a.purpose='chat' AND b.ai_account_id=a.id)
            OR ($3<>'' AND a.purpose='management' AND EXISTS(SELECT 1 FROM test_windows w
                WHERE w.id::text=$3 AND w.group_id=g.id AND a.id=ANY(w.account_ids))))`+lock,
		req.Change.Group, req.Account, req.TestWindowID, nativePlatform(req.Workspace)).Scan(&group, &tenant, &binding, &management, &ai)
	if err != nil || expectedGroup != "" && (group != expectedGroup || binding != expectedBinding) {
		return "", 0, Denied(409, "native_setting_group_binding_required")
	}
	if req.Change.Kind == "group_leave" && (management || ai) {
		return "", 0, Denied(409, "native_social_stop_group_first")
	}
	if !debug && (req.Change.Kind == "group_invite" || moderation) && !management {
		return "", 0, Denied(409, "native_setting_group_binding_required")
	}
	if !debug {
		var modules, platforms []string
		err = tx.QueryRow(ctx, `SELECT p.modules,p.allowed_platforms FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
        WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
        AND p.group_limit>0 FOR SHARE OF sub,p`, tenant).Scan(&modules, &platforms)
		module := "friends"
		if moderation {
			module = "moderation"
		}
		if err != nil || !slices.Contains(modules, module) || !slices.Contains(platforms, nativePlatform(req.Workspace)) {
			return "", 0, Denied(403, "native_social_not_in_plan")
		}
	}
	retired := ""
	if req.Change.Kind == "friend_delete" || req.Change.Kind == "friend_approve" {
		var err error
		retired, err = s.onboardingContactWriteAvailable(ctx, tx, req, proof, group, tenant, binding, debug)
		if err != nil {
			return "", 0, err
		}
	}
	var busy bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations WHERE group_id=$1 AND status IN('running','unknown')
        AND id::text<>$3)
        OR EXISTS(SELECT 1 FROM native_setting_operations WHERE group_id=$1
        AND status IN('preparing','submitting','unknown') AND idempotency_key<>$2)`,
		group, req.Key, retired).Scan(&busy)
	if err != nil {
		return "", 0, err
	}
	if busy {
		return "", 0, Denied(409, "native_setting_pending")
	}
	return group, binding, nil
}
