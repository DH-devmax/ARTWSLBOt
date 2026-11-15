package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativeauth"
)

// ExecuteTestRecall shares durable status and the final write fence with
// settings, without borrowing assistant production admission.
func (s *NativeSettingsService) ExecuteTestRecall(ctx context.Context, ref SessionRef, req SettingRequest, port SettingPort) (SettingOperation, error) {
	if req.Workspace != "wsl" && req.Workspace != "lajiaoliao" || req.Change.Kind != "recall_message" || !ValidID(req.TestWindowID) ||
		req.ReceiverOwner == "" || req.ReceiverRevision < 1 {
		return SettingOperation{}, Denied(400, "native_test_recall_required")
	}
	return s.execute(ctx, ref, req, port, nil)
}

func (s *NativeSettingsService) ExecuteTestChiliText(ctx context.Context, ref SessionRef, req SettingRequest, port SettingPort) (SettingOperation, error) {
	if req.Workspace != "lajiaoliao" || req.Change.Kind != "send_text" || !ValidID(req.TestWindowID) ||
		req.ReceiverOwner == "" || req.ReceiverRevision < 1 {
		return SettingOperation{}, Denied(400, "native_test_text_required")
	}
	return s.execute(ctx, ref, req, port, nil)
}

func (s *NativeSettingsService) authorizeTestRecall(ctx context.Context, tx pgx.Tx, req SettingRequest,
	proof *SettingEvidence, expectedGroup string, expectedBinding int64, debug bool) (string, int64, error) {
	if !debug || req.Workspace != "wsl" && req.Workspace != "lajiaoliao" {
		return "", 0, Denied(403, "native_test_recall_required")
	}
	var reference nativeauth.MessageReference
	if req.Change.Kind == "recall_message" {
		var err error
		reference, err = nativeauth.ReadMessageReference(req.Change.Text, req.Workspace, req.Change.Group)
		if err != nil || !reference.Fresh(time.Now()) {
			return "", 0, Denied(409, "native_message_reference_expired")
		}
	} else if req.Workspace != "lajiaoliao" || req.Change.Kind != "send_text" {
		return "", 0, Denied(403, "native_test_text_required")
	}
	var group string
	var binding int64
	err := tx.QueryRow(ctx, `SELECT g.id::text,b.epoch FROM native_groups g
		JOIN group_bindings b ON b.group_id=g.id JOIN account_receivers r ON r.account_id=b.primary_account_id
		WHERE g.native_id=$1 AND g.platform=$5 AND g.isolated_test AND b.active
		AND b.primary_account_id=$2 AND r.enabled AND r.owner=$3 AND r.revision=$4
		AND r.lease_until>clock_timestamp()+interval '3 seconds' FOR SHARE OF g,b,r`,
		req.Change.Group, req.Account, req.ReceiverOwner, req.ReceiverRevision, req.Workspace).Scan(&group, &binding)
	if err != nil || expectedGroup != "" && (group != expectedGroup || binding != expectedBinding) {
		return "", 0, Denied(409, "native_setting_group_binding_required")
	}
	if proof != nil {
		if req.Change.Kind == "send_text" {
			if !proof.ModerationAllowed || proof.Role != "owner" && proof.Role != "admin" && proof.Role != "member" {
				return "", 0, Denied(403, "native_text_role_required")
			}
			return group, binding, nil
		}
		if proof.Target != reference.Sender || proof.Role != "owner" && proof.Role != "admin" ||
			reference.Sender != proof.UID && (proof.TargetRole == "" || proof.TargetRole == "owner" ||
				proof.Role == "admin" && proof.TargetRole != "member") {
			return "", 0, Denied(403, "native_moderation_target_protected")
		}
	}
	return group, binding, nil
}
