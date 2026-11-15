package httpapi

import (
	"context"
	"strconv"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/features"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func businessNativeEvidence(scope control.BusinessScope, target string, snapshot gateway.ModerationSnapshot) control.BusinessEvidence {
	out := control.BusinessEvidence{Platform: scope.Platform, Account: scope.Account, Group: snapshot.Group,
		SelfID: snapshot.SelfID, Target: target, SelfRole: snapshot.SelfRole, TargetRole: snapshot.TargetRole(target),
		AccountEpoch: snapshot.AccountEpoch, AccountVersion: snapshot.AccountVersion,
		DeploymentVersion: snapshot.DeploymentVersion, CredentialDigest: snapshot.CredentialDigest, ObservedAt: snapshot.SampledAt}
	for _, member := range snapshot.Members {
		if member.Role == "owner" {
			if out.OwnerID != "" {
				out.OwnerID = ""
				return out
			}
			out.OwnerID = member.ID
		}
	}
	return out
}

func (s *Server) RunBusinessRules(ctx context.Context) (<-chan struct{}, error) {
	if s.Receiver == nil {
		return nil, control.Denied(503, "business_receiver_unavailable")
	}
	service := s.BusinessRules
	service.Conversation = func(work context.Context, scope control.BusinessScope) (string, error) {
		return s.Receiver.ChatConversation(work, scope.Platform, scope.Account, scope.NativeGroup)
	}
	service.ReadSource = func(scope control.BusinessScope, key string, event int64, projection, source []byte) (control.BusinessMessage, error) {
		m, err := s.Receiver.ReadBusinessSource(scope.Platform, scope.Account, key, event, projection, source)
		return control.BusinessMessage{EventID: event, Group: m.Group, Sender: m.Sender, Text: m.Text,
			ClientID: m.ClientID, Reference: m.Reference}, err
	}
	service.FreshRole = func(work context.Context, scope control.BusinessScope, target string) (control.BusinessEvidence, error) {
		snapshot, err := s.Receiver.Moderation(work, scope.Account, scope.NativeGroup)
		if err != nil {
			s.observeBusinessAbsence(work, scope, err)
		}
		return businessNativeEvidence(scope, target, snapshot), err
	}
	service.Execute = s.executeBusinessRule
	service.Observe = s.observeBusinessReply
	if err := service.Recover(ctx); err != nil {
		return nil, err
	}
	return service.Run(ctx), nil
}

func (s *Server) observeBusinessReply(ctx context.Context, attempt control.BusinessAttempt) (bool, error) {
	if s.Receiver == nil || attempt.Action != features.BusinessReply || attempt.ClientID == "" || attempt.MessageAt.IsZero() {
		return false, nil
	}
	scope := attempt.Scope
	conversation, err := s.Receiver.ChatConversation(ctx, scope.Platform, scope.Account, scope.NativeGroup)
	if err != nil {
		return false, err
	}
	rows, err := s.DB.Platform.Query(ctx, `SELECT e.id,e.event_key,d.payload_sealed,e.payload_sealed
		FROM native_received_events e JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
		WHERE e.account_id=$1 AND e.kind='group' AND d.scene='group' AND d.conversation_key=$2
		AND d.state IN('decoded','unsupported') AND e.payload_sealed IS NOT NULL
		AND e.received_at>=$3 AND e.received_at<=clock_timestamp()
		AND e.received_at>clock_timestamp()-interval '7 days' ORDER BY e.id DESC LIMIT 201`,
		scope.Account, conversation, attempt.MessageAt.Add(-5*time.Second))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found, count, size := false, 0, 0
	for rows.Next() {
		var id int64
		var key string
		var projection, source []byte
		if err = rows.Scan(&id, &key, &projection, &source); err != nil {
			return false, err
		}
		count++
		size += len(projection) + len(source)
		if count > 200 || size > 8<<20 {
			return false, control.Denied(409, "business_observation_truncated")
		}
		echo, decodeErr := s.Receiver.ReadChatSource(scope.Platform, scope.Account, key, id, projection, source)
		if decodeErr == nil && businessReplyMatches(attempt, echo) {
			found = true
		}
	}
	return found, rows.Err()
}

func businessReplyMatches(attempt control.BusinessAttempt, echo gateway.ChatSource) bool {
	return attempt.Action == features.BusinessReply && attempt.ClientID != "" && attempt.Text != "" &&
		echo.Group == attempt.Scope.NativeGroup && echo.Sender == attempt.Scope.NativeSelf &&
		echo.ClientID == attempt.ClientID && echo.Text == attempt.Text
}

func (s *Server) observeBusinessAbsence(ctx context.Context, scope control.BusinessScope, rosterErr error) {
	owned, err := s.GroupBinding.OwnershipScope(ctx, scope.Platform, scope.Group)
	if err != nil {
		return
	}
	roles, err := (groupBindingAdapter{s}).bindingAbsence(ctx, owned, rosterErr)
	if err == nil && !roles.Present {
		_ = s.GroupBinding.ObserveGroupOwnership(ctx, owned, roles)
	}
}

func (s *Server) executeBusinessRule(ctx context.Context, in control.BusinessAttempt, fence control.BusinessFence) (bool, error) {
	if s.Receiver == nil || fence == nil {
		return false, control.Denied(503, "business_receiver_unavailable")
	}
	scope := in.Scope
	if in.Action == features.BusinessMute {
		change := nativeauth.SettingChange{Kind: "mute_member", Group: scope.NativeGroup, Target: in.Target, Minutes: in.Minutes}
		err := s.Receiver.ChangeModeration(ctx, scope.Account, in.JobID, change,
			func(work context.Context, snapshot gateway.ModerationSnapshot, write func(context.Context) error) error {
				return fence(work, businessNativeEvidence(scope, in.Sender, snapshot), write)
			})
		return err == nil, err
	}
	if in.Action != features.BusinessReply && in.Action != features.BusinessRecall {
		return false, control.Denied(409, "business_action_invalid")
	}
	current, err := s.Receiver.Moderation(ctx, scope.Account, scope.NativeGroup)
	if err != nil {
		return false, err
	}
	proof := businessNativeEvidence(scope, in.Sender, current)
	guard := func(work context.Context, write func(context.Context) error) error { return fence(work, proof, write) }
	intent := gateway.AssistantMessageIntent{Account: scope.Account, Owner: in.Owner, Sender: scope.NativeSelf,
		Group: scope.NativeGroup, ClientID: in.ClientID, Text: in.Text, Epoch: scope.AccountEpoch, Revision: in.Revision}
	if in.Action == features.BusinessRecall {
		if in.Reference == nil {
			return false, control.Denied(409, "business_message_reference_required")
		}
		intent.Reference = in.Reference
		if scope.Platform == "lajiaoliao" {
			err = s.Receiver.SubmitAssistantChiliMessage(ctx, intent, guard)
		} else if scope.Platform == "wsl" {
			err = s.Receiver.RecallAssistantWSL(ctx, intent, guard)
		} else {
			err = control.Denied(409, "business_platform_invalid")
		}
		return err == nil, err
	}
	if scope.Platform == "lajiaoliao" {
		receipt, err := s.Receiver.SubmitChiliGroupText(ctx, intent, guard)
		return err == nil && receipt.ClientID == in.ClientID && receipt.ServerID != "", err
	}
	if scope.Platform != "wsl" {
		return false, control.Denied(409, "business_platform_invalid")
	}
	sender, err := strconv.ParseUint(scope.NativeSelf, 10, 30)
	if err != nil {
		return false, err
	}
	group, err := strconv.ParseUint(scope.NativeGroup, 10, 30)
	if err != nil {
		return false, err
	}
	result, err := s.Receiver.SubmitText(ctx, gateway.TextIntent{
		AccountID: scope.Account, ReceiverOwner: in.Owner, AccountEpoch: scope.AccountEpoch, Revision: in.Revision,
		Message: nativewire.GroupText{SenderID: sender, GroupID: group, ClientID: in.ClientID, Text: in.Text, CreatedAt: in.MessageAt},
		Nonce:   in.Nonce, AAD: in.AAD, RecordRoute: in.RecordRoute,
	}, guard)
	return err == nil && result.Attempted && result.Code == 200 &&
		result.Receipt.ClientID == in.ClientID && result.Receipt.ServerID != "", err
}
