package httpapi

import (
	"context"
	"errors"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

type groupBindingAdapter struct{ server *Server }

var _ control.GroupBindingNativePort = groupBindingAdapter{}
var _ control.GroupBindingFriendNativePort = groupBindingAdapter{}
var _ control.GroupBindingExpiredFriendNativePort = groupBindingAdapter{}

func bindingFriendScope(scope control.GroupBindingNativeScope) gateway.OnboardingNativeScope {
	return gateway.OnboardingNativeScope{Platform: scope.Platform, AccountID: scope.AccountID,
		AccountEpoch: scope.AccountEpoch, AccountVersion: scope.AccountVersion}
}

func bindingFriendApproval(req control.GroupBindingFriendApproval) gateway.OnboardingFriendApproval {
	return gateway.OnboardingFriendApproval{
		OnboardingFriendRequest: gateway.OnboardingFriendRequest{
			OnboardingNativeScope: bindingFriendScope(req.GroupBindingNativeScope), ChallengeText: req.ChallengeText,
			Contact: bindingContactSelector(req.Contact)},
		OperationID: req.OperationID, ApplicationID: req.ApplicationID,
		SenderID: req.SenderID, RecipientID: req.RecipientID, ExpiresAt: req.ExpiresAt,
		RejectExpired: req.RejectExpired, RequestedAt: req.RequestedAt,
	}
}

func bindingContactSelector(selector *control.GroupBindingFriendSelector) *nativeauth.OnboardingContactSelector {
	if selector == nil {
		return nil
	}
	return &nativeauth.OnboardingContactSelector{Name: selector.Name, Since: selector.Since, Until: selector.Until,
		RejectExpired: selector.RejectExpired}
}

func (a groupBindingAdapter) friendEvidence(ctx context.Context, scope control.GroupBindingNativeScope,
	snapshot gateway.OnboardingFriendSnapshot) (control.GroupBindingFriendEvidence, error) {
	out := control.GroupBindingFriendEvidence{ApplicationID: snapshot.ApplicationID, SenderID: snapshot.SenderID,
		RecipientID: snapshot.RecipientID, State: snapshot.State, ExpiresAt: snapshot.ExpiresAt,
		Name: snapshot.Name, RequestedAt: snapshot.RequestedAt}
	var err error
	out.GroupBindingConnectionEvidence, err = a.connection(ctx, scope, snapshot.SettingSnapshot)
	return out, err
}

func (a groupBindingAdapter) BindingFriend(ctx context.Context, req control.GroupBindingFriendRequest) (control.GroupBindingFriendEvidence, error) {
	if a.server.Receiver == nil {
		return control.GroupBindingFriendEvidence{}, control.Denied(503, "native_read_owner_unavailable")
	}
	snapshot, err := a.server.Receiver.OnboardingFriend(ctx, gateway.OnboardingFriendRequest{
		OnboardingNativeScope: bindingFriendScope(req.GroupBindingNativeScope), ChallengeText: req.ChallengeText,
		Contact: bindingContactSelector(req.Contact)})
	if err != nil {
		return control.GroupBindingFriendEvidence{}, bindingFriendReadError(err)
	}
	return a.friendEvidence(ctx, req.GroupBindingNativeScope, snapshot)
}

func (a groupBindingAdapter) ApproveBindingFriend(ctx context.Context, req control.GroupBindingFriendApproval,
	fence control.GroupBindingFriendFence) error {
	if a.server.Receiver == nil || fence == nil || !control.ValidID(req.OperationID) {
		return control.Denied(409, "onboarding_friend_scope_changed")
	}
	return a.server.Receiver.ApproveOnboardingFriend(ctx, bindingFriendApproval(req),
		func(work context.Context, snapshot gateway.OnboardingFriendSnapshot, submit func(context.Context) error) error {
			out, err := a.friendEvidence(work, req.GroupBindingNativeScope, snapshot)
			if err != nil {
				return err
			}
			return fence(work, out, submit)
		})
}

func (a groupBindingAdapter) RejectBindingFriend(ctx context.Context, req control.GroupBindingFriendApproval,
	fence control.GroupBindingFriendFence) error {
	if a.server.Receiver == nil || fence == nil || !control.ValidID(req.OperationID) || !req.RejectExpired {
		return control.Denied(409, "onboarding_friend_scope_changed")
	}
	return a.server.Receiver.RejectOnboardingFriend(ctx, bindingFriendApproval(req),
		func(work context.Context, snapshot gateway.OnboardingFriendSnapshot, submit func(context.Context) error) error {
			out, err := a.friendEvidence(work, req.GroupBindingNativeScope, snapshot)
			if err != nil {
				return err
			}
			return fence(work, out, submit)
		})
}

func (a groupBindingAdapter) ReconcileBindingFriend(ctx context.Context,
	req control.GroupBindingFriendApproval) (control.GroupBindingFriendEvidence, error) {
	if a.server.Receiver == nil || !control.ValidID(req.OperationID) {
		return control.GroupBindingFriendEvidence{}, control.Denied(409, "onboarding_friend_scope_changed")
	}
	snapshot, err := a.server.Receiver.ReconcileOnboardingFriend(ctx, bindingFriendApproval(req))
	if err != nil {
		return control.GroupBindingFriendEvidence{}, bindingFriendReadError(err)
	}
	return a.friendEvidence(ctx, req.GroupBindingNativeScope, snapshot)
}

func bindingFriendReadError(err error) error {
	var native *nativeauth.Error
	var chili *lajiaoliao.Failure
	var code string
	if errors.As(err, &native) {
		code = native.Code
	} else if errors.As(err, &chili) {
		code = chili.Code
	}
	switch code {
	case "native_onboarding_contact_not_found", "lajiaoliao_onboarding_contact_not_found":
		return control.Denied(409, "group_binding_friend_contact_not_found")
	case "native_onboarding_contact_outside_window", "lajiaoliao_onboarding_contact_outside_window":
		return control.Denied(409, "group_binding_friend_contact_outside_window")
	case "native_onboarding_contact_processed", "lajiaoliao_onboarding_contact_processed":
		return control.Denied(409, "group_binding_friend_contact_processed")
	case "native_onboarding_contact_ambiguous", "lajiaoliao_onboarding_contact_ambiguous":
		return control.Denied(409, "group_binding_friend_contact_ambiguous")
	case "lajiaoliao_onboarding_contact_incomplete":
		return control.Denied(409, "group_binding_friend_contact_incomplete")
	case "native_onboarding_contact_timestamp_unavailable", "native_onboarding_contact_timestamp_invalid",
		"lajiaoliao_onboarding_contact_timestamp_unavailable", "lajiaoliao_onboarding_contact_timestamp_invalid":
		return control.Denied(409, "group_binding_friend_contact_time_unavailable")
	case "native_onboarding_contact_shape", "lajiaoliao_onboarding_contact_shape":
		return control.Denied(502, "group_binding_friend_response_invalid")
	case "lajiaoliao_onboarding_contact_directory_no_data":
		return control.Denied(502, "group_binding_friend_directory_no_data")
	case "lajiaoliao_onboarding_contact_directory_missing":
		return control.Denied(502, "group_binding_friend_directory_missing")
	case "lajiaoliao_onboarding_contact_directory_count":
		return control.Denied(502, "group_binding_friend_directory_count")
	case "lajiaoliao_onboarding_contact_directory_recipient":
		return control.Denied(502, "group_binding_friend_directory_recipient")
	case "lajiaoliao_onboarding_contact_directory_rows", "lajiaoliao_onboarding_contact_directory_state",
		"lajiaoliao_onboarding_contact_directory_identity":
		return control.Denied(502, "group_binding_friend_directory_record")
	case "lajiaoliao_onboarding_contact_directory_invalid":
		return control.Denied(502, "group_binding_friend_response_invalid")
	case "native_onboarding_contact_changed", "lajiaoliao_onboarding_contact_changed":
		return control.Denied(409, "group_binding_friend_application_changed")
	case "native_onboarding_contact_expired", "lajiaoliao_onboarding_contact_expired":
		return control.Denied(409, "group_binding_challenge_expired")
	case "native_onboarding_contact_desktop_required", "native_onboarding_contact_unavailable":
		return control.Denied(503, "group_binding_friend_unavailable")
	case "native_onboarding_friend_not_found", "lajiaoliao_onboarding_friend_not_found":
		return control.Denied(409, "group_binding_friend_request_not_found")
	case "native_onboarding_friend_ambiguous", "lajiaoliao_onboarding_friend_ambiguous":
		return control.Denied(409, "group_binding_friend_request_ambiguous")
	case "native_onboarding_friend_shape", "lajiaoliao_onboarding_friend_shape",
		"native_social_shape", "native_social_limit_or_shape":
		return control.Denied(502, "group_binding_friend_response_invalid")
	case "native_onboarding_friend_changed", "lajiaoliao_onboarding_friend_changed":
		return control.Denied(409, "group_binding_friend_application_changed")
	case "native_onboarding_friend_expired", "lajiaoliao_onboarding_friend_expired":
		return control.Denied(409, "group_binding_challenge_expired")
	case "native_onboarding_friend_unavailable":
		return control.Denied(503, "group_binding_friend_unavailable")
	default:
		return err
	}
}

func (a groupBindingAdapter) BindingRobot(ctx context.Context, scope control.GroupBindingNativeScope) (control.GroupBindingRobot, error) {
	if a.server.Receiver == nil {
		return control.GroupBindingRobot{}, control.Denied(503, "native_read_owner_unavailable")
	}
	snapshot, err := a.server.Receiver.OnboardingRobot(ctx, bindingFriendScope(scope))
	if err != nil {
		return control.GroupBindingRobot{}, err
	}
	if _, err = a.connection(ctx, scope, snapshot.SettingSnapshot); err != nil {
		return control.GroupBindingRobot{}, err
	}
	return control.GroupBindingRobot{ID: scope.AccountID, Name: snapshot.Name, NativeID: snapshot.SelfID,
		AddAccount: snapshot.AddAccount, AddAvailable: snapshot.AddAvailable, AddReason: snapshot.AddReason}, nil
}

func (a groupBindingAdapter) connection(ctx context.Context, scope control.GroupBindingNativeScope,
	snapshot gateway.SettingSnapshot) (control.GroupBindingConnectionEvidence, error) {
	out := control.GroupBindingConnectionEvidence{
		Platform: scope.Platform, AccountID: scope.AccountID, AccountEpoch: snapshot.AccountEpoch,
		AccountVersion: snapshot.AccountVersion, DeploymentVersion: snapshot.DeploymentVersion,
		MobileDeploymentVersion: snapshot.MobileDeploymentVersion, MobileSessionVersion: snapshot.MobileSessionVersion,
		CredentialDigest: snapshot.CredentialDigest, ObservedAt: snapshot.SampledAt,
	}
	now := time.Now()
	if a.server.Receiver == nil || scope.Platform != "wsl" && scope.Platform != "lajiaoliao" ||
		snapshot.Platform != "" && snapshot.Platform != scope.Platform ||
		snapshot.AccountEpoch != scope.AccountEpoch || snapshot.AccountVersion != scope.AccountVersion ||
		snapshot.SampledAt.IsZero() || snapshot.SampledAt.After(now) || now.Sub(snapshot.SampledAt) > 5*time.Second {
		return out, control.Denied(409, "onboarding_connection_changed")
	}
	live, ok := a.server.Receiver.LiveSnapshot(scope.AccountID)
	if !ok || live.State != "connected" {
		return out, control.Denied(409, "onboarding_connection_unavailable")
	}
	err := a.server.DB.Platform.QueryRow(ctx, `SELECT r.owner,r.revision
		FROM account_receivers r JOIN accounts a ON a.id=r.account_id
		WHERE a.id=$1 AND a.platform=$2 AND a.purpose='management'
		AND a.epoch=$3 AND a.version=$4 AND a.auth_state='authenticated'
		AND r.enabled AND r.owner<>''
		AND r.lease_until>clock_timestamp()+interval '3 seconds'
		AND r.observed_at>clock_timestamp()-interval '15 seconds'`,
		scope.AccountID, scope.Platform, scope.AccountEpoch, scope.AccountVersion).
		Scan(&out.ReceiverOwner, &out.ReceiverRevision)
	if err != nil {
		return out, control.Denied(409, "onboarding_connection_changed")
	}
	return out, nil
}

func (a groupBindingAdapter) BindingRoles(ctx context.Context, scope control.GroupBindingNativeScope) (control.GroupBindingRoleEvidence, error) {
	var out control.GroupBindingRoleEvidence
	if a.server.Receiver == nil || scope.NativeGroupID == "" || scope.NativeSenderID == "" {
		return out, control.Denied(409, "onboarding_native_scope_required")
	}
	snapshot, err := a.server.Receiver.Moderation(ctx, scope.AccountID, scope.NativeGroupID)
	if err != nil {
		return a.bindingAbsence(ctx, scope, err)
	}
	out.GroupBindingConnectionEvidence, err = a.connection(ctx, scope, snapshot.SettingSnapshot)
	if err != nil {
		return out, err
	}
	out.NativeGroupID, out.NativeSenderID = snapshot.Group, scope.NativeSenderID
	out.Present, out.RobotRole, out.SenderRole = snapshot.Present, snapshot.SelfRole, snapshot.TargetRole(scope.NativeSenderID)
	out.GroupName = snapshot.GroupName
	for _, member := range snapshot.Members {
		if member.Role == "owner" {
			if out.NativeOwnerID != "" {
				return out, control.Denied(409, "onboarding_group_owner_ambiguous")
			}
			out.NativeOwnerID = member.ID
		}
	}
	if snapshot.Group != scope.NativeGroupID || !out.Present || out.NativeOwnerID == "" {
		return out, control.Denied(409, "onboarding_group_membership_unverified")
	}
	return out, nil
}

// A failed roster read is not evidence of departure. Only a complete fresh
// directory may prove absence, still bound to the current connection owner.
func (a groupBindingAdapter) bindingAbsence(ctx context.Context, scope control.GroupBindingNativeScope,
	rosterErr error) (control.GroupBindingRoleEvidence, error) {
	out := control.GroupBindingRoleEvidence{NativeGroupID: scope.NativeGroupID, NativeSenderID: scope.NativeSenderID}
	snapshot, err := a.server.Receiver.Social(ctx, scope.AccountID, scope.NativeGroupID)
	if err != nil || !bindingAbsent(snapshot, scope) {
		return out, rosterErr
	}
	out.GroupBindingConnectionEvidence, err = a.connection(ctx, scope, snapshot.SettingSnapshot)
	out.AbsenceVerified = err == nil
	return out, err
}

func bindingAbsent(snapshot gateway.SocialSnapshot, scope control.GroupBindingNativeScope) bool {
	return scope.NativeGroupID != "" && snapshot.Group == scope.NativeGroupID &&
		snapshot.DirectoryKnown && !snapshot.Present && snapshot.SelfID != ""
}

func bindingInvitation(snapshot gateway.SocialSnapshot, scope control.GroupBindingNativeScope,
	id string, expires time.Time) (control.GroupBindingInvitationEvidence, error) {
	var out control.GroupBindingInvitationEvidence
	if scope.NativeGroupID == "" || scope.NativeSenderID == "" ||
		snapshot.Group != scope.NativeGroupID || !snapshot.DirectoryKnown ||
		snapshot.Present || !snapshot.InvitesKnown || snapshot.SelfID == "" {
		return out, control.Denied(409, "onboarding_invitation_unverified")
	}
	matches := 0
	for _, row := range snapshot.Invitations {
		if row.Group != scope.NativeGroupID || row.Applicant.ID != snapshot.SelfID ||
			row.Inviter == nil || row.Inviter.ID != scope.NativeSenderID ||
			row.Type != "INVITATION" || row.State != "MEMBER_STATE_INVITED" ||
			id != "" && row.ID != id {
			continue
		}
		matches++
		out.InvitationID, out.NativeGroupID = row.ID, row.Group
		out.RecipientID, out.InviterID, out.ExpiresAt = row.Applicant.ID, row.Inviter.ID, expires
	}
	if matches != 1 || out.InvitationID == "" ||
		snapshot.Platform == "lajiaoliao" && len(snapshot.Invitations) != 1 {
		return out, control.Denied(409, "onboarding_invitation_ambiguous")
	}
	return out, nil
}

func (a groupBindingAdapter) BindingInvitation(ctx context.Context, req control.GroupBindingInvitationRequest) (control.GroupBindingInvitationEvidence, error) {
	if a.server.Receiver == nil {
		return control.GroupBindingInvitationEvidence{}, control.Denied(503, "native_read_owner_unavailable")
	}
	snapshot, err := a.server.Receiver.Social(ctx, req.AccountID, req.NativeGroupID)
	if err != nil {
		return control.GroupBindingInvitationEvidence{}, err
	}
	out, err := bindingInvitation(snapshot, req.GroupBindingNativeScope, req.InvitationID, time.Now().UTC().Add(10*time.Minute))
	if err != nil {
		return out, err
	}
	out.GroupBindingConnectionEvidence, err = a.connection(ctx, req.GroupBindingNativeScope, snapshot.SettingSnapshot)
	return out, err
}

func (a groupBindingAdapter) AcceptBindingInvitation(ctx context.Context, req control.GroupBindingAcceptRequest,
	fence control.GroupBindingAcceptFence) error {
	if a.server.Receiver == nil || fence == nil || !control.ValidID(req.OperationID) ||
		!req.ExpiresAt.After(time.Now()) || req.ExpiresAt.After(time.Now().Add(10*time.Minute)) {
		return control.Denied(409, "onboarding_invitation_expired")
	}
	change := nativeauth.SettingChange{Kind: "group_accept", Group: req.NativeGroupID,
		Target: req.RecipientID, Text: req.InvitationID}
	return a.server.Receiver.ChangeSocial(ctx, req.AccountID, req.OperationID, change,
		func(work context.Context, snapshot gateway.SocialSnapshot, submit func(context.Context) error) error {
			out, err := bindingInvitation(snapshot, req.GroupBindingNativeScope, req.InvitationID, req.ExpiresAt)
			if err != nil || out.RecipientID != req.RecipientID {
				return control.Denied(409, "onboarding_invitation_changed")
			}
			out.GroupBindingConnectionEvidence, err = a.connection(work, req.GroupBindingNativeScope, snapshot.SettingSnapshot)
			if err != nil {
				return err
			}
			return fence(work, out, submit)
		})
}
