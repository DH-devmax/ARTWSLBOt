package control

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type TestScope struct {
	GroupID             string    `json:"group_id"`
	NativeGroup         string    `json:"native_group"`
	AccountEpoch        int64     `json:"account_epoch"`
	BindingEpoch        int64     `json:"binding_epoch"`
	ExpiresAt           time.Time `json:"expires_at"`
	Remaining           int       `json:"remaining"`
	AdmissionMode       string    `json:"admission_mode"`
	FriendApplicationID string    `json:"friend_application_id,omitempty"`
}

type TestOperation struct {
	SettingOperation
	WindowID  string    `json:"test_window_id"`
	GroupID   string    `json:"group_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TestOperations survives expired grants and browser storage loss. It exposes
// only the current website actor's bounded operation metadata, never payloads.
func (s *NativeSettingsService) TestOperations(ctx context.Context, ref SessionRef, account string) ([]TestOperation, error) {
	if !ValidID(account) {
		return nil, Denied(400, "invalid_test_scope")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return nil, err
	}
	if actor.Role != "superadmin" {
		return nil, Denied(403, "superadmin_required")
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts
        WHERE id=$1 AND workspace=platform AND platform IN('wsl','lajiaoliao') AND purpose='management')`, account).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, Denied(404, "account_not_found")
	}
	rows, err := tx.Query(ctx, `SELECT o.id::text,o.status,o.result_code,o.kind,o.test_window_id::text,
        o.group_id::text,o.created_at,o.updated_at FROM native_setting_operations o
        JOIN test_windows w ON w.id=o.test_window_id AND w.actor_id=o.actor_id
        JOIN native_groups g ON g.id=o.group_id AND g.id=w.group_id
        JOIN accounts a ON a.id=o.account_id AND a.platform=g.platform AND a.workspace=g.platform
        WHERE o.actor_id=$1 AND o.account_id=$2
        AND o.kind IN('mute_member','unmute_member','mute_group','unmute_group','kick_member','kick_member_ban','remove_group_ban','publish_notice','grant_admin','revoke_admin','group_invite','group_leave','group_accept','friend_approve','friend_delete','member_nickname','group_info','recall_message','send_text')
        ORDER BY (o.status IN('preparing','submitting','unknown')) DESC,o.created_at DESC,o.id DESC LIMIT 50`,
		actor.ID, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TestOperation{}
	for rows.Next() {
		var item TestOperation
		if err = rows.Scan(&item.ID, &item.Status, &item.Code, &item.Kind, &item.WindowID,
			&item.GroupID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func TestAction(action string) bool {
	return NativeUploadAction(action) || nativeauth.ModerationKind(action) || action == "recall_message" || action == "send_text" || action == "send_private_text" || action == "send_private_image" ||
		nativewire.MediaKind(strings.TrimPrefix(action, "send_")) && strings.HasPrefix(action, "send_") || action == "recall" ||
		action == "mute" || action == "unmute" || action == "member_nickname" || action == "kick_member_ban" ||
		action == "remove_group_ban" || action == "friend_approve" || action == "friend_delete" || nativeauth.GroupMembershipKind(action)
}

// Upload-only grants cannot authorize staging or sending a message.
func NativeUploadAction(action string) bool {
	return action == "upload_image" || action == "upload_voice" || action == "upload_video" ||
		action == "upload_private_image"
}

// Debug admission covers implemented durable Tools, never production workers.
func NativeDebugAction(action string) bool {
	return NativeUploadAction(action) || nativeauth.ModerationKind(action) || nativeauth.GroupMembershipKind(action) || action == "friend_approve" || action == "friend_delete" || action == "member_nickname" ||
		action == "send_text" || action == "send_image" || action == "send_voice" || action == "send_video" || action == "recall_message" ||
		action == "send_private_text" || action == "send_private_image"
}

// ExecuteTestSetting reuses the settings fence without admitting ungranted
// profile changes or treating a self nickname as a moderation target.
func (s *NativeSettingsService) ExecuteTestSetting(ctx context.Context, ref SessionRef, req SettingRequest, port SettingPort) (SettingOperation, error) {
	if req.Workspace != "wsl" || req.Change.Kind != "member_nickname" || !ValidID(req.TestWindowID) {
		return SettingOperation{}, Denied(400, "native_test_setting_required")
	}
	return s.execute(ctx, ref, req, port, nil)
}

func verifyTestWindow(ctx context.Context, tx pgx.Tx, actor, workspace, window, account, action, target, application string,
	epoch int64, reserved bool) (TestScope, error) {
	var out TestScope
	if !ValidID(window) || !ValidID(account) || workspace != "wsl" && workspace != "lajiaoliao" {
		return out, Denied(400, "invalid_test_scope")
	}
	debugActions := []string{"mute_member", "unmute_member", "mute_group", "unmute_group", "kick_member",
		"publish_notice", "grant_admin", "revoke_admin", "group_invite", "group_leave", "friend_approve", "friend_delete", "member_nickname", "recall_message", "send_text", "group_info"}
	if workspace == "wsl" {
		debugActions = append(debugActions, "group_accept", "group_info",
			"upload_image", "upload_voice", "upload_video", "upload_private_image",
			"send_text", "send_image", "send_voice", "send_video", "recall_message",
			"send_private_text", "send_private_image")
	}
	if workspace == "lajiaoliao" {
		debugActions = append(debugActions, "group_accept", "upload_image", "upload_voice", "upload_video", "upload_private_image",
			"send_image", "send_voice", "send_video", "send_private_text", "send_private_image", "kick_member_ban", "remove_group_ban")
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT w.group_id::text,g.native_id,b.epoch,w.expires_at,greatest(0,w.budget-w.used),
        w.actor_id=$3 AND a.workspace=$4 AND a.platform=g.platform AND a.purpose='management'
        AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL AND a.testing_enabled AND a.connection_enabled
        AND NOT w.cancelled AND w.expires_at>clock_timestamp()+interval '3 seconds'
        AND ($5='' OR ($5=ANY(w.actions) OR $5='mute_member' AND 'mute'=ANY(w.actions)
            OR $5='unmute_member' AND 'unmute'=ANY(w.actions)
            OR $5='read_membership' AND w.actions && ARRAY['group_invite','group_leave','group_accept','friend_approve','friend_delete']))
        AND ($6='' OR $6=ANY(w.target_ids)) AND w.account_epochs->>a.id::text=a.epoch::text
        AND ($5<>'group_leave' OR a.native_uid=ANY(w.target_ids))
        AND ($5<>'group_accept' OR a.native_uid=$6 AND a.native_uid=ANY(w.target_ids))
        AND (NOT 'friend_delete'=ANY(w.actions) OR w.admission_mode='native_debug'
            AND w.actions=ARRAY['friend_delete']::text[] AND cardinality(w.account_ids)=1
            AND cardinality(w.target_ids)=1 AND w.budget=1 AND NOT a.native_uid=ANY(w.target_ids))
        AND (NOT 'friend_approve'=ANY(w.actions) OR w.admission_mode='native_debug'
            AND w.actions=ARRAY['friend_approve']::text[] AND cardinality(w.account_ids)=1
            AND cardinality(w.target_ids)=1 AND w.budget=1 AND NOT a.native_uid=ANY(w.target_ids)
            AND w.friend_application_id<>'' AND ($5 IN('','read_membership') OR $5='friend_approve' AND $10=w.friend_application_id))
        AND ($7=0 OR a.epoch=$7) AND w.used<=w.budget AND ($8 OR w.used<w.budget)
        AND g.isolated_test AND g.platform=$4 AND b.active
        AND (w.admission_mode='native_debug' AND w.actions <@ $9::text[]
            OR w.admission_mode='package' AND EXISTS(SELECT 1 FROM subscriptions sub JOIN plans plan ON plan.id=sub.plan_id
            WHERE sub.tenant_id=b.tenant_id AND sub.enabled
            AND sub.expires_at>clock_timestamp()+interval '3 seconds'
            AND plan.group_limit>0 AND $4=ANY(plan.allowed_platforms)
            AND (CASE WHEN $5 IN('group_invite','group_leave','group_accept','read_membership')
                THEN 'friends'=ANY(plan.modules)
                WHEN $5='member_nickname' THEN 'cards'=ANY(plan.modules)
                WHEN $5='' THEN 'moderation'=ANY(plan.modules) OR
                    ('friends'=ANY(plan.modules) AND w.actions && ARRAY['group_invite','group_leave','group_accept'])
                ELSE 'moderation'=ANY(plan.modules) END)))
        ,a.epoch,w.admission_mode,w.friend_application_id FROM test_windows w JOIN native_groups g ON g.id=w.group_id
        JOIN group_bindings b ON b.group_id=g.id JOIN accounts a ON a.id=$2 AND a.id=ANY(w.account_ids)
        WHERE w.id=$1 FOR UPDATE OF w FOR SHARE OF a,b,g`,
		window, account, actor, workspace, action, target, epoch, reserved,
		debugActions, application).
		Scan(&out.GroupID, &out.NativeGroup, &out.BindingEpoch, &out.ExpiresAt, &out.Remaining, &allowed, &out.AccountEpoch, &out.AdmissionMode, &out.FriendApplicationID)
	if err != nil || !allowed {
		return out, Denied(409, "native_test_window_invalid")
	}
	return out, nil
}

// TestScope checks a server-created grant. Tool clients cannot create or widen
// grants, substitute an actor, or authorize themselves through model output.
func (s *NativeSettingsService) TestScope(ctx context.Context, ref SessionRef, workspace, window, account, action, target string) (TestScope, error) {
	return s.testScope(ctx, ref, workspace, window, account, action, target, "")
}

// Approval must match both the applicant and the persisted native application.
func (s *NativeSettingsService) TestScopeForApplication(ctx context.Context, ref SessionRef, workspace, window, account, action, target, application string) (TestScope, error) {
	if action != "friend_approve" || application == "" {
		return TestScope{}, Denied(400, "native_friend_application_required")
	}
	return s.testScope(ctx, ref, workspace, window, account, action, target, application)
}

func (s *NativeSettingsService) testScope(ctx context.Context, ref SessionRef, workspace, window, account, action, target, application string) (TestScope, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return TestScope{}, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return TestScope{}, err
	}
	if actor.Role != "superadmin" {
		return TestScope{}, Denied(403, "superadmin_required")
	}
	out, err := verifyTestWindow(ctx, tx, actor.ID, workspace, window, account, action, target, application, 0, action == "" || action == "read_membership")
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *NativeSettingsService) InspectTestTool(ctx context.Context, ref SessionRef, account, key, window string,
	change nativeauth.SettingChange) (SettingOperation, bool, error) {
	out, err := s.Inspect(ctx, ref, account, "", key)
	if failure, ok := err.(*Failure); ok && failure.Code == "operation_not_found" {
		return SettingOperation{}, false, nil
	}
	if err != nil {
		return out, false, err
	}
	var sealed []byte
	if err = s.DB.Platform.QueryRow(ctx, `SELECT payload_sealed FROM native_setting_operations WHERE id=$1`, out.ID).Scan(&sealed); err != nil {
		return out, true, err
	}
	plain, err := s.Vault.Open("native-setting:"+out.ID, sealed)
	if err != nil {
		return out, true, Denied(409, "native_setting_evidence_expired")
	}
	defer clear(plain)
	var req SettingRequest
	if json.Unmarshal(plain, &req) != nil {
		return SettingOperation{}, true, Denied(409, "idempotency_conflict")
	}
	if change.Group == "" {
		change.Group = req.Change.Group
	}
	if req.TestWindowID != window || req.Change != change {
		return SettingOperation{}, true, Denied(409, "idempotency_conflict")
	}
	return out, true, nil
}
