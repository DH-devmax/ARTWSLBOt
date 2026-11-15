package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// A test applicant is resolved from the live owner-created onboarding scope,
// never from a browser's source account or an inferred native roster.
type onboardingControlledClaimant struct {
	Scope, Account, Recipient    string
	ScopeVersion, Epoch, Version int64
	Binding                      onboardingControlledBinding
	BindingVersion               int64 `json:",omitempty"`
	ExpiresAt                    time.Time
}

func (a *onboardingControlledClaimant) matches(c bindingRecord, group, target string) bool {
	return a.matchesIdentity(c, group, target) && a.Binding.Epoch > 0 &&
		a.Binding.Version > 0 && ValidID(a.Binding.Tenant) && ValidID(a.Binding.Account) &&
		(a.BindingVersion == 0 || a.BindingVersion == a.Binding.Version)
}

func (a *onboardingControlledClaimant) matchesIdentity(c bindingRecord, group, target string) bool {
	return a != nil && ValidID(a.Scope) && a.Scope == c.ControlledScope &&
		ValidID(a.Account) && a.Account != c.Account && a.ScopeVersion > 0 &&
		a.Epoch > 0 && a.Version > 0 && onboardingLabel(a.Recipient, 200) &&
		a.Recipient == target && ValidID(group)
}

func (a *onboardingControlledClaimant) matchesRetained(c bindingRecord, group, target string) bool {
	// v125 sealed only the binding version. It remains readable, but cannot
	// satisfy the full binding snapshot required by any new submission.
	return a.matches(c, group, target) || a.matchesIdentity(c, group, target) &&
		a.Binding == (onboardingControlledBinding{}) && a.BindingVersion > 0
}

func sameOnboardingClaimant(a, b *onboardingControlledClaimant) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, y := *a, *b
	x.ExpiresAt, y.ExpiresAt = time.Time{}, time.Time{}
	return x == y && a.ExpiresAt.Equal(b.ExpiresAt)
}

func onboardingOutboundSource(c bindingRecord, a *onboardingControlledAuthorization) bindingRecord {
	if a != nil && a.Claimant != nil {
		c.Account, c.Epoch, c.AccountVersion = a.Claimant.Account, a.Claimant.Epoch, a.Claimant.Version
	}
	return c
}

func onboardingOutboundPeer(c bindingRecord, a *onboardingControlledAuthorization) *GroupBindingOutboundPeer {
	if c.Platform != "wsl" || a == nil || a.Claimant == nil || !a.matches(c) {
		return nil
	}
	return &GroupBindingOutboundPeer{GroupBindingNativeScope: c.scope("", ""), NativeID: a.Target}
}

func validOnboardingOutboundPeer(c bindingRecord, a *onboardingControlledAuthorization, peer *GroupBindingOutboundPeer) bool {
	if peer == nil {
		return true // Retained requests preserve their original transport.
	}
	expected := onboardingOutboundPeer(c, a)
	return expected != nil && expected.GroupBindingNativeScope == peer.GroupBindingNativeScope &&
		expected.NativeID == peer.NativeID
}

func sameOnboardingPeerConnection(a, b *GroupBindingOutboundPeer) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, y := *a, *b
	x.Connection.ObservedAt, y.Connection.ObservedAt = time.Time{}, time.Time{}
	return x == y
}

func (s *GroupBindingService) onboardingClaimant(ctx context.Context, q Queryer, c bindingRecord,
	group string) (*onboardingControlledClaimant, error) {
	if !ValidID(c.ControlledScope) {
		return nil, Denied(409, "controlled_onboarding_scope_inactive")
	}
	var out onboardingControlledClaimant
	err := q.QueryRow(ctx, `SELECT s.id::text,s.version,c.id::text,c.epoch,c.version,a.native_uid,s.expires_at
		FROM onboarding_test_scopes s JOIN accounts a ON a.id=s.account_id
		JOIN accounts c ON c.id=s.claimant_account_id JOIN native_groups g ON g.id=s.group_id
		JOIN users u ON u.id=s.actor_id JOIN tenants t ON t.id=s.tenant_id
		WHERE s.id=$1 AND s.actor_id=$2 AND s.tenant_id=$3 AND s.platform=$4
		AND s.account_id=$5 AND s.group_id=$6 AND s.account_epoch=$7 AND s.account_version=$8
		AND onboarding_test_scope_active(s.id)
		AND a.purpose='management' AND c.purpose='management'
		AND a.connection_enabled AND c.connection_enabled
		FOR SHARE OF s,a,c,g,u,t`, c.ControlledScope, c.Applicant, c.Tenant, c.Platform,
		c.Account, group, c.Epoch, c.AccountVersion).
		Scan(&out.Scope, &out.ScopeVersion, &out.Account, &out.Epoch, &out.Version, &out.Recipient, &out.ExpiresAt)
	if err == pgx.ErrNoRows {
		return nil, Denied(409, "controlled_onboarding_scope_inactive")
	}
	if err != nil {
		return nil, err
	}
	out.Binding, err = loadOnboardingControlledBinding(ctx, q, group, c.Tenant, out.Account, c.Account, c.Platform)
	if err != nil {
		return nil, err
	}
	out.BindingVersion = out.Binding.Version
	return &out, nil
}

func (s *GroupBindingService) requireOnboardingClaimant(ctx context.Context, q pgx.Tx, c bindingRecord,
	a *onboardingControlledAuthorization) error {
	if a == nil || !a.Claimant.matches(c, a.Group, a.Target) {
		return Denied(409, "onboarding_controlled_target_changed")
	}
	current, err := s.onboardingClaimant(ctx, q, c, a.Group)
	if err != nil {
		return err
	}
	if !sameOnboardingClaimant(a.Claimant, current) {
		return Denied(409, "controlled_onboarding_scope_changed")
	}
	if !a.ExpiresAt.IsZero() && a.ExpiresAt.After(current.ExpiresAt) ||
		!current.ExpiresAt.After(time.Now().Add(bindingFriendSubmissionFence)) {
		return Denied(409, "controlled_onboarding_scope_inactive")
	}
	_, err = s.connection(ctx, q, c, nil)
	return err
}
