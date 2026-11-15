package httpapi

import (
	"context"
	"strconv"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func (s *Server) executeAssistantAction(ctx context.Context, ref control.SessionRef, plan control.AssistantActionPlan) (control.SettingOperation, error) {
	return s.executeHumanAction(ctx, ref, plan, false)
}

func (s *Server) executeManualAction(ctx context.Context, ref control.SessionRef, plan control.AssistantActionPlan) (control.SettingOperation, error) {
	return s.executeHumanAction(ctx, ref, plan, true)
}

func (s *Server) executeHumanAction(ctx context.Context, ref control.SessionRef, plan control.AssistantActionPlan, manual bool) (control.SettingOperation, error) {
	if s.Receiver == nil {
		return control.SettingOperation{}, control.Denied(503, "native_read_owner_unavailable")
	}
	service := s.nativeSettingsService()
	var change nativeauth.SettingChange
	var err error
	if manual {
		change, err = service.ManualChange(ctx, ref, s.ManualActions, plan.Scope.GroupID, plan.ID)
	} else {
		change, err = service.AssistantChange(ctx, ref, s.Assistant, plan.ID)
	}
	if err != nil {
		return control.SettingOperation{}, err
	}
	req := control.SettingRequest{Account: plan.Scope.AccountID, Workspace: plan.Scope.Platform, Key: plan.ID, Epoch: plan.AccountEpoch, Change: change}
	var port control.SettingPort
	if change.Kind == "member_nickname" {
		snapshot, e := s.Receiver.Settings(ctx, req.Account, change.Group, change.Target)
		if e != nil {
			return control.SettingOperation{}, e
		}
		req.Digest, req.Version = snapshot.Digest, snapshot.AccountVersion
		port = func(work context.Context, fence control.SettingFence) error {
			// Read before ChangeSetting takes the account's serial read slot.
			// Nesting Moderation in its callback would deadlock that slot.
			roles, e := s.Receiver.Moderation(work, req.Account, change.Group)
			if e != nil {
				return e
			}
			return s.Receiver.ChangeSetting(work, req.Account, req.Key, change, func(final context.Context, snapshot gateway.SettingSnapshot, submit func(context.Context) error) error {
				proof, e := s.assistantNativeEvidence(final, req, roles)
				if e != nil {
					return e
				}
				card, e := assistantMemberEvidence(snapshot, proof, change.Target)
				if e != nil {
					return e
				}
				return fence(final, card, submit)
			})
		}
	} else if change.Kind == "send_text" || change.Kind == "recall_message" {
		snapshot, e := s.Receiver.Moderation(ctx, req.Account, change.Group)
		if e != nil {
			return control.SettingOperation{}, e
		}
		req.Digest, req.Version = snapshot.Digest, snapshot.AccountVersion
		port = func(work context.Context, fence control.SettingFence) error {
			var attempt control.AssistantMessageAttempt
			var e error
			if manual {
				attempt, e = service.ManualAttempt(work, ref, s.ManualActions, plan.Scope.GroupID, plan.ID)
			} else {
				attempt, e = service.AssistantAttempt(work, ref, s.Assistant, plan.ID)
			}
			if e != nil {
				return e
			}
			// Take fresh role evidence after the durable intent exists.
			current, e := s.Receiver.Moderation(work, req.Account, change.Group)
			if e != nil {
				return e
			}
			proof, e := s.assistantNativeEvidence(work, req, current)
			if e != nil {
				return e
			}
			if proof.ReceiverOwner != attempt.Owner || proof.ReceiverRevision != attempt.Revision {
				return control.Denied(409, "native_text_connection_changed")
			}
			proof.ModerationAllowed = current.Present && (current.SelfRole == "member" || current.SelfRole == "admin" || current.SelfRole == "owner")
			intent := gateway.AssistantMessageIntent{Account: req.Account, Owner: attempt.Owner, Sender: attempt.Sender,
				Group: change.Group, ClientID: attempt.ClientID, Text: plan.Text, Epoch: plan.AccountEpoch, Revision: attempt.Revision}
			if change.Kind == "recall_message" {
				reference, e := nativeauth.ReadMessageReference(change.Text, plan.Scope.Platform, change.Group)
				if e != nil {
					return e
				}
				intent.Reference = &reference
				proof.TargetRole = current.TargetRole(reference.Sender)
			}
			guard := func(final context.Context, write func(context.Context) error) error {
				return fence(final, proof, write)
			}
			if plan.Scope.Platform == "lajiaoliao" {
				return s.Receiver.SubmitAssistantChiliMessage(work, intent, guard)
			}
			if intent.Reference != nil {
				return s.Receiver.RecallAssistantWSL(work, intent, guard)
			}
			sender, e := strconv.ParseUint(attempt.Sender, 10, 30)
			if e != nil {
				return e
			}
			group, e := strconv.ParseUint(change.Group, 10, 30)
			if e != nil {
				return e
			}
			result, e := s.Receiver.SubmitText(work, gateway.TextIntent{
				AccountID: req.Account, ReceiverOwner: attempt.Owner, AccountEpoch: plan.AccountEpoch, Revision: attempt.Revision,
				Message: nativewire.GroupText{SenderID: sender, GroupID: group, ClientID: attempt.ClientID, Text: plan.Text, CreatedAt: attempt.CreatedAt},
				Nonce:   attempt.Nonce, AAD: attempt.AAD,
				RecordRoute: func(record context.Context, route nativewire.TextRoute) error {
					if manual {
						return service.RecordManualRoute(record, attempt, route)
					}
					return service.RecordAssistantRoute(record, attempt, route)
				},
			}, guard)
			if e == nil && result.Code != 200 {
				return &nativeauth.Error{Code: "native_business_rejected", Submitted: result.Attempted}
			}
			return e
		}
	} else if nativeauth.ModerationKind(change.Kind) {
		snapshot, e := s.Receiver.Moderation(ctx, req.Account, change.Group)
		if e != nil {
			return control.SettingOperation{}, e
		}
		req.Digest, req.Version = snapshot.Digest, snapshot.AccountVersion
		port = func(work context.Context, fence control.SettingFence) error {
			return s.Receiver.ChangeModeration(work, req.Account, req.Key, change, func(final context.Context, snapshot gateway.ModerationSnapshot, submit func(context.Context) error) error {
				proof, e := s.assistantNativeEvidence(final, req, snapshot)
				if e != nil {
					return e
				}
				return fence(final, proof, submit)
			})
		}
	} else {
		return control.SettingOperation{}, control.Denied(409, "assistant_action_executor_unavailable")
	}
	if manual {
		return service.ExecuteManual(ctx, ref, s.ManualActions, plan.Scope.GroupID, plan.ID, req, port)
	}
	return service.ExecuteAssistant(ctx, ref, s.Assistant, plan.ID, req, port)
}

func (s *Server) assistantNativeEvidence(ctx context.Context, req control.SettingRequest, snapshot gateway.ModerationSnapshot) (control.SettingEvidence, error) {
	proof := moderationEvidence(snapshot, req.Change)
	if snapshot.Group != req.Change.Group || !snapshot.Present || proof.OwnerID == "" ||
		(snapshot.SelfRole == "owner") != (proof.OwnerID == snapshot.SelfID) {
		return proof, control.Denied(409, "group_ownership_evidence_required")
	}
	connection, err := (groupBindingAdapter{server: s}).connection(ctx, control.GroupBindingNativeScope{
		Platform: req.Workspace, AccountID: req.Account, AccountEpoch: req.Epoch, AccountVersion: req.Version,
		NativeGroupID: req.Change.Group,
	}, snapshot.SettingSnapshot)
	if err != nil {
		return proof, err
	}
	proof.ReceiverOwner, proof.ReceiverRevision = connection.ReceiverOwner, connection.ReceiverRevision
	return proof, nil
}

func assistantMemberEvidence(snapshot gateway.SettingSnapshot, proof control.SettingEvidence, target string) (control.SettingEvidence, error) {
	card := settingEvidence(snapshot)
	if proof.OwnerID == "" || proof.ReceiverOwner == "" || proof.ReceiverRevision < 1 ||
		target == "" || proof.Target != target || proof.TargetRole == "" ||
		card.UID != proof.UID || card.Role != proof.Role || card.Epoch != proof.Epoch ||
		card.Version != proof.Version || card.DeploymentVersion != proof.DeploymentVersion ||
		card.CredentialDigest != proof.CredentialDigest || card.Target != target || card.TargetRole != proof.TargetRole {
		return card, control.Denied(409, "native_setting_snapshot_changed")
	}
	card.OwnerID, card.ReceiverOwner, card.ReceiverRevision = proof.OwnerID, proof.ReceiverOwner, proof.ReceiverRevision
	if proof.ObservedAt.Before(card.ObservedAt) {
		card.ObservedAt = proof.ObservedAt
	}
	return card, nil
}
