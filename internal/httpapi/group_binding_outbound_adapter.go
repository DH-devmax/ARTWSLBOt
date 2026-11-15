package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

var _ control.GroupBindingOutboundNativePort = groupBindingAdapter{}

func bindingOutboundRequest(req control.GroupBindingOutboundRequest) gateway.OnboardingOutboundRequest {
	return gateway.OnboardingOutboundRequest{
		OnboardingNativeScope: bindingFriendScope(req.GroupBindingNativeScope),
		OperationID:           req.OperationID, SelfID: req.SelfID, ExpiresAt: req.ExpiresAt,
		Target: gateway.OnboardingOutboundTarget{NativeID: req.Target.NativeID, AddAccount: req.Target.AddAccount,
			Name: req.Target.Name, AlreadyFriend: req.Target.AlreadyFriend},
		ControlledPeer: bindingOutboundPeer(req.ControlledPeer),
	}
}

func bindingOutboundPeer(peer *control.GroupBindingOutboundPeer) *gateway.OnboardingOutboundPeer {
	if peer == nil {
		return nil
	}
	return &gateway.OnboardingOutboundPeer{OnboardingNativeScope: bindingFriendScope(peer.GroupBindingNativeScope),
		NativeID: peer.NativeID, Connection: gateway.OnboardingPeerConnection{
			ReceiverOwner: peer.Connection.ReceiverOwner, ReceiverRevision: peer.Connection.ReceiverRevision,
			DeploymentVersion: peer.Connection.DeploymentVersion, CredentialDigest: peer.Connection.CredentialDigest}}
}

func (a groupBindingAdapter) outboundEvidence(ctx context.Context, scope control.GroupBindingNativeScope,
	snapshot gateway.OnboardingOutboundSnapshot) (control.GroupBindingOutboundEvidence, error) {
	out := control.GroupBindingOutboundEvidence{
		Target: control.GroupBindingFriendTarget{NativeID: snapshot.Target.NativeID,
			AddAccount: snapshot.Target.AddAccount, Name: snapshot.Target.Name, AlreadyFriend: snapshot.Target.AlreadyFriend},
		SelfID: snapshot.SelfID, ApplicationID: snapshot.ApplicationID, State: snapshot.State,
	}
	var err error
	out.GroupBindingConnectionEvidence, err = a.connection(ctx, scope, snapshot.SettingSnapshot)
	if err == nil {
		if peerScope, peerSnapshot, ok := snapshot.SettingSnapshot.OnboardingPeer(); ok {
			nativeScope := control.GroupBindingNativeScope{Platform: peerScope.Platform, AccountID: peerScope.AccountID,
				AccountEpoch: peerScope.AccountEpoch, AccountVersion: peerScope.AccountVersion}
			connection, peerErr := a.connection(ctx, nativeScope, peerSnapshot)
			if peerErr != nil {
				return out, peerErr
			}
			// The adapter must retain the owner observed by the gateway rather than
			// replacing it with a newer receiver that happened to become current.
			if connection.ReceiverOwner != peerSnapshot.ReceiverOwner ||
				connection.ReceiverRevision != peerSnapshot.ReceiverRevision {
				return out, control.Denied(409, "onboarding_connection_changed")
			}
			out.Peer = &control.GroupBindingOutboundPeer{GroupBindingNativeScope: nativeScope,
				NativeID: snapshot.Target.NativeID, Connection: connection}
		}
	}
	return out, err
}

func (a groupBindingAdapter) LookupBindingFriend(ctx context.Context,
	req control.GroupBindingOutboundLookup, fence control.GroupBindingOutboundLookupFence) (control.GroupBindingOutboundEvidence, error) {
	if fence == nil {
		return control.GroupBindingOutboundEvidence{}, control.Denied(http.StatusConflict, "native_test_window_invalid")
	}
	if a.server.Receiver == nil {
		return control.GroupBindingOutboundEvidence{}, control.Denied(http.StatusServiceUnavailable, "native_read_owner_unavailable")
	}
	snapshot, err := a.server.Receiver.LookupOnboardingOutboundFriend(ctx, gateway.OnboardingOutboundLookup{
		OnboardingNativeScope: bindingFriendScope(req.GroupBindingNativeScope), AddAccount: req.AddAccount,
		ControlledPeer: bindingOutboundPeer(req.ControlledPeer),
	}, gateway.OnboardingOutboundLookupFence(fence))
	if err != nil {
		return control.GroupBindingOutboundEvidence{}, bindingOutboundLookupError(err)
	}
	return a.outboundEvidence(ctx, req.GroupBindingNativeScope, snapshot)
}

func (a groupBindingAdapter) SendBindingFriend(ctx context.Context, req control.GroupBindingOutboundRequest,
	fence control.GroupBindingOutboundFence) error {
	if a.server.Receiver == nil || fence == nil || !control.ValidID(req.OperationID) {
		return control.Denied(http.StatusConflict, "group_binding_outbound_scope_changed")
	}
	err := a.server.Receiver.SendOnboardingOutboundFriend(ctx, bindingOutboundRequest(req),
		func(work context.Context, snapshot gateway.OnboardingOutboundSnapshot, submit func(context.Context) error) error {
			out, err := a.outboundEvidence(work, req.GroupBindingNativeScope, snapshot)
			if err != nil {
				return err
			}
			return fence(work, out, submit)
		})
	return bindingOutboundSendError(err)
}

func (a groupBindingAdapter) ReconcileBindingOutbound(ctx context.Context,
	req control.GroupBindingOutboundRequest) (control.GroupBindingOutboundEvidence, error) {
	if a.server.Receiver == nil || !control.ValidID(req.OperationID) {
		return control.GroupBindingOutboundEvidence{}, control.Denied(http.StatusConflict, "group_binding_outbound_scope_changed")
	}
	snapshot, err := a.server.Receiver.ReconcileOnboardingOutboundFriend(ctx, bindingOutboundRequest(req))
	if err != nil {
		return control.GroupBindingOutboundEvidence{}, bindingOutboundReadError(err)
	}
	return a.outboundEvidence(ctx, req.GroupBindingNativeScope, snapshot)
}

func bindingOutboundReadError(err error) error {
	if err == nil {
		return nil
	}
	var denied *control.Failure
	if errors.As(err, &denied) {
		return denied
	}
	switch {
	case errors.Is(err, gateway.ErrReadOwner):
		return control.Denied(http.StatusServiceUnavailable, "native_read_owner_unavailable")
	case errors.Is(err, gateway.ErrReadBusy):
		return control.Denied(http.StatusTooManyRequests, "group_binding_outbound_busy")
	case errors.Is(err, gateway.ErrMemberScope):
		return control.Denied(http.StatusConflict, "group_binding_outbound_scope_changed")
	case errors.Is(err, context.Canceled):
		return control.Denied(http.StatusRequestTimeout, "group_binding_outbound_cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return control.Denied(http.StatusGatewayTimeout, "group_binding_outbound_timeout")
	}
	var native *nativeauth.Error
	var chili *lajiaoliao.Failure
	code := ""
	if errors.As(err, &native) {
		code = native.Code
		if native.Code == "native_reauth_required" {
			return control.Denied(http.StatusConflict, "native_mobile_reauth_required")
		}
	} else if errors.As(err, &chili) {
		code = chili.Code
	}
	switch code {
	case "native_mobile_search_required", "native_mobile_reauth_required", "native_mobile_search_changed",
		"native_mobile_configuration_invalid", "native_mobile_configuration_changed":
		return control.Denied(http.StatusConflict, "native_mobile_search_required")
	case "native_onboarding_outbound_account_invalid", "lajiaoliao_onboarding_outbound_account_invalid",
		"native_onboarding_outbound_invalid", "lajiaoliao_onboarding_outbound_invalid":
		return control.Denied(http.StatusBadRequest, "group_binding_outbound_account_invalid")
	case "native_onboarding_outbound_not_found", "lajiaoliao_onboarding_outbound_not_found":
		return control.Denied(http.StatusNotFound, "group_binding_outbound_target_not_found")
	case "native_onboarding_outbound_ambiguous", "lajiaoliao_onboarding_outbound_ambiguous":
		return control.Denied(http.StatusConflict, "group_binding_outbound_target_ambiguous")
	case "native_onboarding_outbound_self", "lajiaoliao_onboarding_outbound_self":
		return control.Denied(http.StatusConflict, "group_binding_outbound_self")
	case "native_onboarding_outbound_shape", "lajiaoliao_onboarding_outbound_shape",
		"native_social_shape", "native_social_limit_or_shape":
		return control.Denied(http.StatusBadGateway, "group_binding_outbound_response_invalid")
	case "native_onboarding_outbound_changed":
		return control.Denied(http.StatusConflict, "group_binding_outbound_target_inconsistent")
	case "lajiaoliao_onboarding_outbound_changed",
		"native_onboarding_outbound_identity", "lajiaoliao_onboarding_outbound_identity",
		"native_onboarding_outbound_operation_invalid":
		return control.Denied(http.StatusConflict, "group_binding_outbound_scope_changed")
	case "native_onboarding_outbound_read_incomplete", "lajiaoliao_onboarding_outbound_incomplete":
		return control.Denied(http.StatusConflict, "group_binding_outbound_read_incomplete")
	case "native_onboarding_outbound_already_submitted", "lajiaoliao_onboarding_outbound_already_submitted":
		return control.Denied(http.StatusConflict, "group_binding_outbound_operation_unresolved")
	case "native_onboarding_outbound_pending", "lajiaoliao_onboarding_outbound_pending":
		return control.Denied(http.StatusConflict, "group_binding_outbound_pending")
	case "native_onboarding_outbound_already_friend", "lajiaoliao_onboarding_outbound_already_friend":
		return control.Denied(http.StatusConflict, "group_binding_outbound_already_friend")
	case "native_onboarding_outbound_incoming", "lajiaoliao_onboarding_outbound_incoming":
		return control.Denied(http.StatusConflict, "group_binding_outbound_incoming_pending")
	case "native_onboarding_outbound_policy", "lajiaoliao_onboarding_outbound_policy":
		return control.Denied(http.StatusConflict, "group_binding_outbound_policy")
	case "native_onboarding_outbound_blocked", "lajiaoliao_onboarding_outbound_blocked":
		return control.Denied(http.StatusConflict, "group_binding_outbound_blocked")
	case "native_onboarding_outbound_expired", "lajiaoliao_onboarding_outbound_expired":
		return control.Denied(http.StatusConflict, "group_binding_challenge_expired")
	case "native_rate_limited", "lajiaoliao_rate_limited":
		return control.Denied(http.StatusTooManyRequests, "group_binding_outbound_rate_limited")
	case "native_request_parameters_rejected":
		return control.Denied(http.StatusConflict, "group_binding_outbound_parameters_rejected")
	case "native_captcha_rejected", "native_security_required", "native_security_setup_required",
		"native_device_verification_required":
		return control.Denied(http.StatusConflict, "group_binding_outbound_security_required")
	case "native_client_version_rejected":
		return control.Denied(http.StatusConflict, "group_binding_outbound_client_version")
	case "native_ip_restricted":
		return control.Denied(http.StatusConflict, "group_binding_outbound_ip_restricted")
	case "native_onboarding_lookup_permission_denied":
		return control.Denied(http.StatusForbidden, "group_binding_outbound_lookup_forbidden")
	case "native_onboarding_lookup_account_unavailable", "native_account_clearing":
		return control.Denied(http.StatusConflict, "group_binding_outbound_account_unavailable")
	case "native_transport_unknown":
		return control.Denied(http.StatusServiceUnavailable, "group_binding_outbound_unavailable")
	case "native_response_invalid", "lajiaoliao_response_invalid":
		return control.Denied(http.StatusBadGateway, "group_binding_outbound_response_invalid")
	case "native_business_rejected", "lajiaoliao_rejected", "lajiaoliao_request_rejected":
		return control.Denied(http.StatusConflict, "group_binding_outbound_rejected")
	case "lajiaoliao_reauth_required", "lajiaoliao_token_expired", "lajiaoliao_kicked":
		return control.Denied(http.StatusConflict, "lajiaoliao_reauth_required")
	default:
		return control.Denied(http.StatusServiceUnavailable, "group_binding_outbound_unavailable")
	}
}

func bindingOutboundLookupError(err error) error {
	mapped := bindingOutboundReadError(err)
	var chiliLookup *lajiaoliao.OnboardingLookupError
	var chili *lajiaoliao.Failure
	var mappedFailure *control.Failure
	if errors.As(err, &chiliLookup) && errors.As(err, &chili) && errors.As(mapped, &mappedFailure) {
		category := ""
		switch chili.Code {
		case "lajiaoliao_onboarding_outbound_shape", "lajiaoliao_response_invalid":
			category = "response_invalid"
		case "lajiaoliao_rate_limited":
			category = "rate_limited"
		case "lajiaoliao_reauth_required", "lajiaoliao_token_expired", "lajiaoliao_kicked":
			category = "session_expired"
		case "lajiaoliao_rejected", "lajiaoliao_request_rejected":
			category = "upstream_rejected"
		case "lajiaoliao_result_unknown", "lajiaoliao_transport_unavailable":
			category = "transport_unavailable"
		}
		diagnostic, ok := control.SafeGroupBindingLookupDiagnostic(control.GroupBindingLookupDiagnostic{
			Version: 1, Platform: "lajiaoliao", Stage: chiliLookup.Stage, Category: category,
			ObservedAt:        time.Now().UTC(),
			RetryAfterSeconds: int((chili.RetryAfter + time.Second - 1) / time.Second),
		})
		if ok {
			return &control.GroupBindingLookupFailure{Failure: mappedFailure, Diagnostic: diagnostic}
		}
		return mapped
	}
	var lookup *nativeauth.OnboardingLookupError
	var native *nativeauth.Error
	var failure *control.Failure
	if !errors.As(err, &lookup) || !errors.As(err, &native) || !errors.As(mapped, &failure) {
		return mapped
	}
	category := ""
	switch native.Code {
	case "native_rate_limited":
		category = "rate_limited"
	case "native_reauth_required":
		category = "session_expired"
	case "native_request_parameters_rejected":
		category = "parameters_rejected"
	case "native_captcha_rejected", "native_security_required", "native_security_setup_required",
		"native_device_verification_required":
		category = "security_verification"
	case "native_client_version_rejected":
		category = "client_version"
	case "native_ip_restricted":
		category = "ip_restricted"
	case "native_onboarding_lookup_permission_denied":
		category = "permission_denied"
	case "native_onboarding_lookup_account_unavailable", "native_account_clearing":
		category = "account_unavailable"
	case "native_business_rejected", "native_http_rejected":
		category = "upstream_rejected"
	case "native_transport_unknown":
		category = "transport_unavailable"
	case "native_response_invalid", "native_onboarding_outbound_changed":
		category = "response_invalid"
	default:
		return mapped
	}
	reason := native.Diagnostic
	if native.Code == "native_onboarding_outbound_changed" {
		reason = ""
		if lookup.Stage == "friend_info" {
			reason = nativeauth.SafeOnboardingLookupReason(lookup.Reason)
		}
	}
	if reason == "" && native.Number > 0 && native.Number < 600 {
		reason = "http_status"
	}
	diagnostic, ok := control.SafeGroupBindingLookupDiagnostic(control.GroupBindingLookupDiagnostic{
		Version: 1, Platform: "wsl", Stage: lookup.Stage, Category: category,
		ProviderCode: native.Number, Reason: reason, ObservedAt: time.Now().UTC(),
		RetryAfterSeconds: int((native.RetryAfter + time.Second - 1) / time.Second),
	})
	if !ok {
		return mapped
	}
	return &control.GroupBindingLookupFailure{Failure: failure, Diagnostic: diagnostic}
}

// Retain the known submitted outcome classes for control's durable recovery.
// Wrapped errors and unreviewed provider text never become a public diagnosis.
func bindingOutboundSendError(err error) error {
	if err == nil {
		return nil
	}
	var native *nativeauth.Error
	var chili *lajiaoliao.Failure
	var lookup *nativeauth.OnboardingLookupError
	var chiliLookup *lajiaoliao.OnboardingLookupError
	if errors.As(err, &lookup) || errors.As(err, &chiliLookup) {
		// A pre-submit read returning HTTP/business errors is not an outgoing
		// friend mutation, even though the read itself reached the platform.
		return bindingOutboundReadError(err)
	}
	if errors.As(err, &native) && native.Submitted {
		switch native.Code {
		case "native_transport_unknown", "native_response_invalid", "native_business_rejected",
			"native_http_rejected", "native_reauth_required", "native_rate_limited":
			number := native.Number
			if number < 1 || number > 9999 {
				number = 0
			}
			return &nativeauth.Error{Code: native.Code, Number: number, Submitted: true}
		}
	}
	if errors.As(err, &chili) && chili.Submitted {
		switch chili.Code {
		case "lajiaoliao_result_unknown", "lajiaoliao_response_invalid", "lajiaoliao_rejected",
			"lajiaoliao_rate_limited", "lajiaoliao_reauth_required":
			return &lajiaoliao.Failure{Code: chili.Code, Submitted: true}
		}
	}
	return bindingOutboundReadError(err)
}
