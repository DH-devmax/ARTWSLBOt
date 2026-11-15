package gateway

import (
	"context"
	"strconv"
	"time"

	"wangshangliao/internal/nativeauth"
)

type onboardingPeerSnapshot struct {
	Scope OnboardingNativeScope
	Base  SettingSnapshot
}

type OnboardingPeerConnection struct {
	ReceiverOwner     string
	ReceiverRevision  int64
	DeploymentVersion int64
	CredentialDigest  [32]byte
}

func peerConnection(base SettingSnapshot) OnboardingPeerConnection {
	return OnboardingPeerConnection{ReceiverOwner: base.ReceiverOwner, ReceiverRevision: base.ReceiverRevision,
		DeploymentVersion: base.DeploymentVersion, CredentialDigest: base.CredentialDigest}
}

func validOnboardingPeer(source OnboardingNativeScope, peer OnboardingOutboundPeer) bool {
	return source.valid() && source.Platform == "wsl" && peer.OnboardingNativeScope.valid() &&
		peer.Platform == source.Platform && peer.AccountID != source.AccountID &&
		validOnboardingOutboundTarget("wsl", OnboardingOutboundTarget{
			NativeID: peer.NativeID, AddAccount: "1"})
}

func sameOnboardingPeer(a, b *OnboardingOutboundPeer) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (m *ReceiverManager) openOnboardingOutboundPeer(ctx context.Context, scope OnboardingNativeScope,
	peer *OnboardingOutboundPeer, account string) (onboardingOutboundClient, SettingSnapshot, error) {
	if peer == nil {
		return m.openOnboardingOutbound(ctx, scope)
	}
	if !validOnboardingPeer(scope, *peer) || !nativeauth.ValidOnboardingOutboundAccount(account) {
		return nil, SettingSnapshot{}, ErrMemberScope
	}
	target, saved, other, err := m.openSettingsClient(ctx, peer.AccountID, defaultSettingsClient)
	if err != nil {
		return nil, other, err
	}
	target.Close()
	clear(saved.Session.MAC)
	other.Platform = "wsl"
	if !peer.OnboardingNativeScope.matches(other) || other.SelfID != peer.NativeID ||
		saved.Session.AccountID != account || strconv.FormatUint(saved.Session.UID, 10) != peer.NativeID {
		return nil, other, ErrMemberScope
	}
	err = m.DB.QueryRow(ctx, `SELECT owner,revision FROM account_receivers
		WHERE account_id=$1 AND enabled AND owner<>''
		AND lease_until>clock_timestamp()+interval '3 seconds'
		AND observed_at>clock_timestamp()-interval '15 seconds'`, peer.AccountID).
		Scan(&other.ReceiverOwner, &other.ReceiverRevision)
	if err != nil {
		return nil, other, ErrMemberScope
	}
	if peer.Connection != (OnboardingPeerConnection{}) && peer.Connection != peerConnection(other) {
		return nil, other, ErrMemberScope
	}
	pinned := onboardingPeerSnapshot{Scope: peer.OnboardingNativeScope, Base: other}
	if err = m.checkOnboardingPeer(ctx, pinned); err != nil {
		return nil, other, err
	}
	raw, _, base, err := m.openSettingsClient(ctx, scope.AccountID, defaultSettingsClient)
	if err != nil {
		return nil, base, err
	}
	base.Platform, base.controlledPeer = "wsl", &pinned
	client, ok := raw.(*nativeauth.Client)
	if !ok {
		raw.Close()
		return nil, base, ErrMemberScope
	}
	if err = client.RestrictOnboardingPeer(peer.NativeID, account); err == nil {
		err = m.checkOnboardingAccount(ctx, scope, base)
	}
	if err != nil {
		client.Close()
		return nil, base, err
	}
	return wslOnboardingOutboundClient{nativeOnboardingOutboundClient: client}, base, nil
}

func (base SettingSnapshot) OnboardingPeer() (OnboardingNativeScope, SettingSnapshot, bool) {
	if base.controlledPeer == nil {
		return OnboardingNativeScope{}, SettingSnapshot{}, false
	}
	peer := base.controlledPeer
	snapshot := peer.Base
	snapshot.SampledAt = time.Now().UTC()
	return peer.Scope, snapshot, true
}

func (m *ReceiverManager) checkOnboardingPeer(ctx context.Context, peer onboardingPeerSnapshot) error {
	if peer.Base.controlledPeer != nil || !peer.Scope.matches(peer.Base) ||
		m.checkSettingSession(ctx, peer.Scope.AccountID, peer.Base) != nil {
		return ErrMemberScope
	}
	var current bool
	err := m.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a
		JOIN account_receivers r ON r.account_id=a.id WHERE a.id=$1 AND a.platform='wsl'
		AND a.purpose='management' AND a.testing_enabled AND a.connection_enabled
		AND a.auth_state='authenticated' AND a.epoch=$2 AND a.version=$3 AND a.native_uid=$4
		AND r.enabled AND r.owner=$5 AND r.revision=$6
		AND r.lease_until>clock_timestamp()+interval '3 seconds'
		AND r.observed_at>clock_timestamp()-interval '15 seconds')`, peer.Scope.AccountID,
		peer.Scope.AccountEpoch, peer.Scope.AccountVersion, peer.Base.SelfID,
		peer.Base.ReceiverOwner, peer.Base.ReceiverRevision).Scan(&current)
	if err != nil || !current {
		return ErrMemberScope
	}
	return nil
}
