package gateway

import (
	"context"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

type nativeOnboardingOutboundClient interface {
	LookupOnboardingOutboundFriend(context.Context, string) (nativeauth.OnboardingOutboundTarget, error)
	SendOnboardingOutboundFriend(context.Context, nativeauth.OnboardingOutboundRequest, nativeauth.OnboardingOutboundFence) (nativeauth.OnboardingOutboundEvidence, error)
	ReconcileOnboardingOutboundFriend(context.Context, nativeauth.OnboardingOutboundRequest) (nativeauth.OnboardingOutboundEvidence, error)
	Close()
}

type wslOnboardingOutboundClient struct{ nativeOnboardingOutboundClient }

func (c wslOnboardingOutboundClient) Lookup(ctx context.Context, account string) (nativeauth.OnboardingOutboundTarget, error) {
	return c.LookupOnboardingOutboundFriend(ctx, account)
}

func (c wslOnboardingOutboundClient) Send(ctx context.Context, req nativeauth.OnboardingOutboundRequest,
	fence nativeauth.OnboardingOutboundFence) (nativeauth.OnboardingOutboundEvidence, error) {
	return c.SendOnboardingOutboundFriend(ctx, req, fence)
}

func (c wslOnboardingOutboundClient) Reconcile(ctx context.Context, req nativeauth.OnboardingOutboundRequest) (nativeauth.OnboardingOutboundEvidence, error) {
	e, err := c.ReconcileOnboardingOutboundFriend(ctx, req)
	// An old accepted application does not prove a current relationship.
	// Preserve that uncertainty in the shared contact-only projection.
	switch e.State {
	case "approved":
		e.State = "unknown"
	case "deleted", "expired":
		e.State = "rejected"
	}
	return e, err
}

type chiliOnboardingOutboundClient struct {
	*lajiaoliao.Client
	session lajiaoliao.Session
}

func chiliOutboundTarget(target lajiaoliao.OnboardingOutboundTarget) nativeauth.OnboardingOutboundTarget {
	return nativeauth.OnboardingOutboundTarget{ID: target.ID, Account: target.Account, Name: target.Name,
		IsFriend: target.IsFriend, CanRequest: target.CanRequest, BlockedReason: target.BlockedReason}
}

func chiliOutboundEvidence(e lajiaoliao.OnboardingOutboundEvidence) nativeauth.OnboardingOutboundEvidence {
	return nativeauth.OnboardingOutboundEvidence{OperationID: e.OperationID, ApplicationID: e.ApplicationID,
		SenderID: e.SenderID, RecipientID: e.RecipientID, State: e.State, IsFriend: e.IsFriend}
}

func chiliOutboundRequest(req nativeauth.OnboardingOutboundRequest) lajiaoliao.OnboardingOutboundRequest {
	return lajiaoliao.OnboardingOutboundRequest{OperationID: req.OperationID, SelfID: req.SelfID, ExpiresAt: req.ExpiresAt,
		Target: lajiaoliao.OnboardingOutboundTarget{ID: req.Target.ID, Account: req.Target.Account, Name: req.Target.Name,
			IsFriend: req.Target.IsFriend, CanRequest: req.Target.CanRequest, BlockedReason: req.Target.BlockedReason},
	}
}

func (c chiliOnboardingOutboundClient) Lookup(ctx context.Context, account string) (nativeauth.OnboardingOutboundTarget, error) {
	target, err := c.LookupOnboardingOutboundFriend(ctx, c.session, account)
	return chiliOutboundTarget(target), err
}

func (c chiliOnboardingOutboundClient) Send(ctx context.Context, req nativeauth.OnboardingOutboundRequest,
	fence nativeauth.OnboardingOutboundFence) (nativeauth.OnboardingOutboundEvidence, error) {
	e, err := c.SendOnboardingOutboundFriend(ctx, c.session, chiliOutboundRequest(req),
		func(work context.Context, target lajiaoliao.OnboardingOutboundTarget, submit func(context.Context) error) error {
			return fence(work, chiliOutboundTarget(target), submit)
		})
	return chiliOutboundEvidence(e), err
}

func (c chiliOnboardingOutboundClient) Reconcile(ctx context.Context, req nativeauth.OnboardingOutboundRequest) (nativeauth.OnboardingOutboundEvidence, error) {
	e, err := c.ReconcileOnboardingOutboundFriend(ctx, c.session, chiliOutboundRequest(req))
	return chiliOutboundEvidence(e), err
}

func (m *ReceiverManager) openOnboardingOutbound(ctx context.Context, scope OnboardingNativeScope) (onboardingOutboundClient, SettingSnapshot, error) {
	if scope.Platform == "lajiaoliao" {
		session, base, err := m.openChiliNative(ctx, scope.AccountID)
		if err != nil {
			return nil, base, err
		}
		if err = m.checkOnboardingAccount(ctx, scope, base); err != nil {
			return nil, base, err
		}
		return chiliOnboardingOutboundClient{Client: lajiaoliao.NewClient(), session: session}, base, nil
	}
	return m.openMobileOnboarding(ctx, scope)
}
