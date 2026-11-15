package gateway

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

type OnboardingNativeScope struct {
	Platform       string
	AccountID      string
	AccountEpoch   int64
	AccountVersion int64
}

type OnboardingFriendRequest struct {
	OnboardingNativeScope
	ChallengeText string                                `json:"-"`
	Contact       *nativeauth.OnboardingContactSelector `json:"-"`
}

func (OnboardingFriendRequest) String() string   { return "OnboardingFriendRequest(redacted)" }
func (OnboardingFriendRequest) GoString() string { return "OnboardingFriendRequest(redacted)" }

type OnboardingFriendApproval struct {
	OnboardingFriendRequest
	OperationID   string
	ApplicationID string
	SenderID      string
	RecipientID   string
	ExpiresAt     time.Time
	// RequestedAt preserves the sealed application timestamp for exact reconciliation.
	RequestedAt time.Time `json:"-"`
	// RejectExpired is restored only from control's sealed controlled operation.
	RejectExpired bool `json:"-"`
}

type OnboardingFriendSnapshot struct {
	SettingSnapshot
	nativeauth.OnboardingFriendEvidence
	ExpiresAt time.Time
}

type OnboardingRobotSnapshot struct {
	SettingSnapshot
	Name         string
	AddAccount   string
	AddAvailable bool
	AddReason    string
}

type OnboardingFriendFence func(context.Context, OnboardingFriendSnapshot, func(context.Context) error) error

type onboardingFriendClient interface {
	OnboardingFriend(context.Context, string) (nativeauth.OnboardingFriendEvidence, error)
	ReconcileOnboardingFriend(context.Context, string, nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error)
	ApproveOnboardingFriend(context.Context, string, nativeauth.OnboardingFriendEvidence, time.Time, nativeauth.OnboardingFriendFence) error
	Close()
}

type onboardingContactClient interface {
	OnboardingContact(context.Context, nativeauth.OnboardingContactSelector) (nativeauth.OnboardingFriendEvidence, error)
	ReconcileOnboardingContact(context.Context, nativeauth.OnboardingContactSelector, nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error)
	ApproveOnboardingContact(context.Context, nativeauth.OnboardingContactSelector, nativeauth.OnboardingFriendEvidence, time.Time, nativeauth.OnboardingFriendFence) error
}

type onboardingContactRejectClient interface {
	RejectOnboardingContact(context.Context, nativeauth.OnboardingContactSelector, nativeauth.OnboardingFriendEvidence, time.Time, nativeauth.OnboardingFriendFence) error
}

type chiliOnboardingFriendClient struct {
	*lajiaoliao.Client
	session lajiaoliao.Session
}

func (c chiliOnboardingFriendClient) OnboardingFriend(ctx context.Context, text string) (nativeauth.OnboardingFriendEvidence, error) {
	return c.Client.OnboardingFriend(ctx, c.session, text)
}

func (c chiliOnboardingFriendClient) ReconcileOnboardingFriend(ctx context.Context, text string,
	expected nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	return c.Client.ReconcileOnboardingFriend(ctx, c.session, text, expected)
}

func (c chiliOnboardingFriendClient) ApproveOnboardingFriend(ctx context.Context, text string,
	expected nativeauth.OnboardingFriendEvidence, deadline time.Time, fence nativeauth.OnboardingFriendFence) error {
	return c.Client.ApproveOnboardingFriend(ctx, c.session, text, expected, deadline, fence)
}

func (c chiliOnboardingFriendClient) OnboardingContact(ctx context.Context,
	selector nativeauth.OnboardingContactSelector) (nativeauth.OnboardingFriendEvidence, error) {
	return c.Client.OnboardingContact(ctx, c.session, selector)
}

func (c chiliOnboardingFriendClient) ReconcileOnboardingContact(ctx context.Context,
	selector nativeauth.OnboardingContactSelector, expected nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	return c.Client.ReconcileOnboardingContact(ctx, c.session, selector, expected)
}

func (c chiliOnboardingFriendClient) ApproveOnboardingContact(ctx context.Context,
	selector nativeauth.OnboardingContactSelector, expected nativeauth.OnboardingFriendEvidence,
	deadline time.Time, fence nativeauth.OnboardingFriendFence) error {
	return c.Client.ApproveOnboardingContact(ctx, c.session, selector, expected, deadline, fence)
}

func (c chiliOnboardingFriendClient) RejectOnboardingContact(ctx context.Context,
	selector nativeauth.OnboardingContactSelector, expected nativeauth.OnboardingFriendEvidence,
	deadline time.Time, fence nativeauth.OnboardingFriendFence) error {
	return c.Client.RejectOnboardingContact(ctx, c.session, selector, expected, deadline, fence)
}

func (s OnboardingNativeScope) valid() bool {
	return (s.Platform == "wsl" || s.Platform == "lajiaoliao") && s.AccountID != "" &&
		s.AccountEpoch > 0 && s.AccountVersion > 0
}

func (s OnboardingNativeScope) matches(base SettingSnapshot) bool {
	return s.valid() && s.Platform == base.Platform && s.AccountEpoch == base.AccountEpoch &&
		s.AccountVersion == base.AccountVersion && base.SelfID != ""
}

func (a OnboardingFriendApproval) expected() nativeauth.OnboardingFriendEvidence {
	out := nativeauth.OnboardingFriendEvidence{ApplicationID: a.ApplicationID, SenderID: a.SenderID,
		RecipientID: a.RecipientID, State: "pending"}
	if a.Contact != nil {
		out.Name = a.Contact.Name
		out.RequestedAt = a.RequestedAt
	}
	return out
}

func (r OnboardingFriendRequest) valid() bool {
	if !r.OnboardingNativeScope.valid() {
		return false
	}
	if r.Contact != nil {
		return nativeauth.ValidOnboardingContactSelector(*r.Contact)
	}
	return nativeauth.ValidOnboardingFriendChallenge(r.ChallengeText)
}

func (r OnboardingFriendRequest) snapshot() OnboardingFriendRequest {
	if r.Contact != nil {
		selector := *r.Contact
		r.Contact = &selector
	}
	return r
}

func (a OnboardingFriendApproval) valid() bool {
	if !a.OnboardingFriendRequest.valid() ||
		a.RejectExpired && (a.Contact == nil || !a.Contact.RejectExpired) ||
		a.Contact != nil && (a.Contact.RejectExpired || !a.RequestedAt.IsZero()) && !a.Contact.Matches(a.expected()) ||
		a.OperationID == "" || len(a.OperationID) > 200 || a.ApplicationID == "" || a.SenderID == "" ||
		a.RecipientID == "" || a.SenderID == a.RecipientID {
		return false
	}
	change := nativeauth.SettingChange{Kind: "friend_approve", Target: a.SenderID, Text: a.ApplicationID}
	if a.RejectExpired {
		change.Kind = "friend_reject"
	}
	if a.Platform == "lajiaoliao" {
		return lajiaoliao.ValidID(a.RecipientID) && chiliChange(change).Validate() == nil
	}
	recipient, err := strconv.ParseUint(a.RecipientID, 10, 30)
	return err == nil && recipient > 0 && strconv.FormatUint(recipient, 10) == a.RecipientID && change.Validate() == nil
}

func (m *ReceiverManager) checkOnboardingAccount(ctx context.Context, scope OnboardingNativeScope, base SettingSnapshot) error {
	if !scope.matches(base) {
		return ErrMemberScope
	}
	if err := m.checkSettingSession(ctx, scope.AccountID, base); err != nil {
		return err
	}
	var current bool
	err := m.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND platform=$2
		AND purpose='management' AND auth_state='authenticated' AND epoch=$3 AND version=$4 AND native_uid=$5)`,
		scope.AccountID, scope.Platform, scope.AccountEpoch, scope.AccountVersion, base.SelfID).Scan(&current)
	if err != nil || !current {
		return ErrMemberScope
	}
	if base.controlledPeer != nil {
		if err := m.checkOnboardingPeer(ctx, *base.controlledPeer); err != nil {
			return err
		}
	}
	return m.checkMobileOnboarding(ctx, scope, base)
}

// Every job has its own result and final fence; proof text is never a queue key.
// The existing account lane owns serialization, bounded cancellation and 429s.
func (m *ReceiverManager) onboardingRead(ctx context.Context, account string,
	fn func(context.Context) (any, error)) (any, error) {
	return m.readValue(ctx, account, "", ReadRefresh, func(work context.Context) (any, error) {
		m.mu.Lock()
		reads := m.reads
		m.mu.Unlock()
		return withOnboardingInterval(work, account, reads, fn)
	})
}

func withOnboardingInterval(ctx context.Context, account string, reads *ReadScheduler,
	fn func(context.Context) (any, error)) (any, error) {
	defer func() {
		if reads != nil {
			reads.Cooldown(account, time.Now().Add(5*time.Second))
		}
	}()
	return fn(ctx)
}

func (m *ReceiverManager) openOnboardingFriend(ctx context.Context, scope OnboardingNativeScope) (onboardingFriendClient, SettingSnapshot, error) {
	if scope.Platform == "lajiaoliao" {
		session, base, err := m.openChiliNative(ctx, scope.AccountID)
		if err != nil {
			return nil, base, err
		}
		if err = m.checkOnboardingAccount(ctx, scope, base); err != nil {
			return nil, base, err
		}
		return chiliOnboardingFriendClient{Client: lajiaoliao.NewClient(), session: session}, base, nil
	}
	client, _, base, err := m.openSettingsClient(ctx, scope.AccountID, defaultSettingsClient)
	base.Platform = "wsl"
	if err != nil {
		return nil, base, err
	}
	native, ok := client.(onboardingFriendClient)
	if !ok {
		client.Close()
		return nil, base, &nativeauth.Error{Code: "native_onboarding_friend_unavailable"}
	}
	if err = m.checkOnboardingAccount(ctx, scope, base); err != nil {
		client.Close()
		return nil, base, err
	}
	return native, base, nil
}

func (m *ReceiverManager) OnboardingFriend(ctx context.Context, req OnboardingFriendRequest) (OnboardingFriendSnapshot, error) {
	req = req.snapshot()
	if !req.valid() {
		return OnboardingFriendSnapshot{}, ErrMemberScope
	}
	value, err := m.onboardingRead(ctx, req.AccountID, func(work context.Context) (any, error) {
		client, base, err := m.openOnboardingFriend(work, req.OnboardingNativeScope)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return readOnboardingFriend(work, req, nil, client, base, m.checkOnboardingAccount)
	})
	return onboardingFriendResult(value, err)
}

func (m *ReceiverManager) ReconcileOnboardingFriend(ctx context.Context, req OnboardingFriendApproval) (OnboardingFriendSnapshot, error) {
	req.OnboardingFriendRequest = req.OnboardingFriendRequest.snapshot()
	if !req.valid() {
		return OnboardingFriendSnapshot{}, ErrMemberScope
	}
	value, err := m.onboardingRead(ctx, req.AccountID, func(work context.Context) (any, error) {
		client, base, err := m.openOnboardingFriend(work, req.OnboardingNativeScope)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return readOnboardingFriend(work, req.OnboardingFriendRequest, &req, client, base, m.checkOnboardingAccount)
	})
	return onboardingFriendResult(value, err)
}

func onboardingFriendResult(value any, err error) (OnboardingFriendSnapshot, error) {
	if err != nil {
		return OnboardingFriendSnapshot{}, err
	}
	out, ok := value.(OnboardingFriendSnapshot)
	if !ok {
		return out, ErrMemberScope
	}
	return out, nil
}

func readOnboardingFriend(ctx context.Context, req OnboardingFriendRequest, approval *OnboardingFriendApproval,
	client onboardingFriendClient, base SettingSnapshot,
	check func(context.Context, OnboardingNativeScope, SettingSnapshot) error) (OnboardingFriendSnapshot, error) {
	var out OnboardingFriendSnapshot
	req = req.snapshot()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if !req.OnboardingNativeScope.matches(base) || !req.valid() ||
		req.Contact != nil && base.Platform == "wsl" && (base.MobileSessionVersion != 0 || base.MobileDeploymentVersion != 0) {
		return out, ErrMemberScope
	}
	if approval != nil {
		copy := *approval
		copy.OnboardingFriendRequest = copy.OnboardingFriendRequest.snapshot()
		approval = &copy
		if !approval.valid() || approval.OnboardingNativeScope != req.OnboardingNativeScope ||
			(req.Contact == nil) != (approval.Contact == nil) ||
			req.Contact == nil && req.ChallengeText != approval.ChallengeText ||
			req.Contact != nil && *req.Contact != *approval.Contact {
			return out, ErrMemberScope
		}
	}
	var err error
	if req.Contact != nil {
		native, ok := client.(onboardingContactClient)
		if !ok {
			return out, &nativeauth.Error{Code: "native_onboarding_contact_unavailable"}
		}
		if approval == nil {
			out.OnboardingFriendEvidence, err = native.OnboardingContact(ctx, *req.Contact)
		} else {
			out.OnboardingFriendEvidence, err = native.ReconcileOnboardingContact(ctx, *req.Contact, approval.expected())
		}
	} else if approval == nil {
		out.OnboardingFriendEvidence, err = client.OnboardingFriend(ctx, req.ChallengeText)
	} else {
		out.OnboardingFriendEvidence, err = client.ReconcileOnboardingFriend(ctx, req.ChallengeText, approval.expected())
	}
	if err != nil {
		return OnboardingFriendSnapshot{}, err
	}
	if err = ctx.Err(); err != nil {
		return OnboardingFriendSnapshot{}, err
	}
	if out.RecipientID != base.SelfID || out.SenderID == base.SelfID || out.SenderID == "" ||
		out.ApplicationID == "" || approval == nil && out.State != "pending" ||
		req.Contact != nil && !req.Contact.Matches(out.OnboardingFriendEvidence) ||
		approval != nil && approval.RejectExpired && !req.Contact.IsExpired(out.OnboardingFriendEvidence) ||
		approval != nil && (out.ApplicationID != approval.ApplicationID || out.SenderID != approval.SenderID ||
			out.RecipientID != approval.RecipientID ||
			req.Contact != nil && !approval.RequestedAt.IsZero() && !out.RequestedAt.Equal(approval.RequestedAt)) {
		return OnboardingFriendSnapshot{}, ErrMemberScope
	}
	base.SampledAt = time.Now().UTC()
	out.SettingSnapshot = base
	out.ExpiresAt = base.SampledAt.Add(10 * time.Minute)
	if approval != nil {
		out.ExpiresAt = approval.ExpiresAt
	}
	if err = check(ctx, req.OnboardingNativeScope, base); err != nil {
		return OnboardingFriendSnapshot{}, err
	}
	return out, nil
}

func (m *ReceiverManager) ApproveOnboardingFriend(ctx context.Context, req OnboardingFriendApproval, fence OnboardingFriendFence) error {
	if req.RejectExpired {
		return ErrMemberScope
	}
	return m.submitOnboardingFriend(ctx, req, fence)
}

func (m *ReceiverManager) RejectOnboardingFriend(ctx context.Context, req OnboardingFriendApproval, fence OnboardingFriendFence) error {
	if !req.RejectExpired || req.Contact == nil || !req.Contact.RejectExpired {
		return ErrMemberScope
	}
	return m.submitOnboardingFriend(ctx, req, fence)
}

func (m *ReceiverManager) submitOnboardingFriend(ctx context.Context, req OnboardingFriendApproval, fence OnboardingFriendFence) error {
	req.OnboardingFriendRequest = req.OnboardingFriendRequest.snapshot()
	if !req.valid() || fence == nil || !nativeauth.OnboardingFriendDeadline(req.ExpiresAt, time.Now()) {
		return &nativeauth.Error{Code: "native_onboarding_friend_expired"}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := m.onboardingRead(ctx, req.AccountID, func(work context.Context) (any, error) {
		client, base, err := m.openOnboardingFriend(work, req.OnboardingNativeScope)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		if req.RejectExpired {
			return nil, rejectOnboardingFriend(work, req, client, base, m.checkOnboardingAccount, fence)
		}
		return nil, approveOnboardingFriend(work, req, client, base, m.checkOnboardingAccount, fence)
	})
	return err
}

func approveOnboardingFriend(ctx context.Context, req OnboardingFriendApproval, client onboardingFriendClient,
	base SettingSnapshot, check func(context.Context, OnboardingNativeScope, SettingSnapshot) error,
	fence OnboardingFriendFence) error {
	if req.RejectExpired {
		return ErrMemberScope
	}
	return submitOnboardingFriend(ctx, req, client, base, check, fence)
}

func rejectOnboardingFriend(ctx context.Context, req OnboardingFriendApproval, client onboardingFriendClient,
	base SettingSnapshot, check func(context.Context, OnboardingNativeScope, SettingSnapshot) error,
	fence OnboardingFriendFence) error {
	if !req.RejectExpired || req.Contact == nil || !req.Contact.RejectExpired {
		return ErrMemberScope
	}
	return submitOnboardingFriend(ctx, req, client, base, check, fence)
}

func submitOnboardingFriend(ctx context.Context, req OnboardingFriendApproval, client onboardingFriendClient,
	base SettingSnapshot, check func(context.Context, OnboardingNativeScope, SettingSnapshot) error,
	fence OnboardingFriendFence) error {
	req.OnboardingFriendRequest = req.OnboardingFriendRequest.snapshot()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !req.valid() || !req.OnboardingNativeScope.matches(base) || req.RecipientID != base.SelfID ||
		fence == nil || !nativeauth.OnboardingFriendDeadline(req.ExpiresAt, time.Now()) ||
		req.Contact != nil && base.Platform == "wsl" && (base.MobileSessionVersion != 0 || base.MobileDeploymentVersion != 0) {
		return ErrMemberScope
	}
	var fenced, submitted atomic.Bool
	submitFence := func(work context.Context, proof nativeauth.OnboardingFriendEvidence, submit func(context.Context) error) error {
		expected := req.expected()
		matches := proof == expected
		if req.Contact != nil {
			matches = proof.ApplicationID == expected.ApplicationID && proof.SenderID == expected.SenderID &&
				proof.RecipientID == expected.RecipientID && proof.State == "pending" && req.Contact.Matches(proof) &&
				(req.RequestedAt.IsZero() || proof.RequestedAt.Equal(req.RequestedAt))
			if req.RejectExpired {
				matches = matches && req.Contact.IsExpired(proof)
			} else {
				matches = matches && !req.Contact.IsExpired(proof)
			}
		}
		if !fenced.CompareAndSwap(false, true) || !matches || submit == nil {
			return ErrMemberScope
		}
		if err := work.Err(); err != nil {
			return err
		}
		base.SampledAt = time.Now().UTC()
		if err := check(work, req.OnboardingNativeScope, base); err != nil {
			return err
		}
		snapshot := OnboardingFriendSnapshot{SettingSnapshot: base, OnboardingFriendEvidence: proof, ExpiresAt: req.ExpiresAt}
		return fence(work, snapshot, func(final context.Context) error {
			if !submitted.CompareAndSwap(false, true) {
				return ErrMemberScope
			}
			if final.Err() != nil {
				return final.Err()
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := work.Err(); err != nil {
				return err
			}
			if !nativeauth.OnboardingFriendDeadline(req.ExpiresAt, time.Now()) ||
				time.Since(snapshot.SampledAt) > 5*time.Second {
				return &nativeauth.Error{Code: "native_onboarding_friend_expired"}
			}
			if err := check(final, req.OnboardingNativeScope, snapshot.SettingSnapshot); err != nil {
				return err
			}
			return submit(final)
		})
	}
	var err error
	if req.RejectExpired {
		native, ok := client.(onboardingContactRejectClient)
		if !ok {
			return &nativeauth.Error{Code: "native_onboarding_contact_unavailable"}
		}
		err = native.RejectOnboardingContact(ctx, *req.Contact, req.expected(), req.ExpiresAt, submitFence)
	} else if req.Contact != nil {
		native, ok := client.(onboardingContactClient)
		if !ok {
			return &nativeauth.Error{Code: "native_onboarding_contact_unavailable"}
		}
		err = native.ApproveOnboardingContact(ctx, *req.Contact, req.expected(), req.ExpiresAt, submitFence)
	} else {
		err = client.ApproveOnboardingFriend(ctx, req.ChallengeText, req.expected(), req.ExpiresAt, submitFence)
	}
	if err == nil && (!fenced.Load() || !submitted.Load()) {
		return ErrMemberScope
	}
	return err
}

// OnboardingRobot is for explicit Create/setup only. GET challenge projections
// read sealed public identity locally through control and never call this.
func (m *ReceiverManager) OnboardingRobot(ctx context.Context, scope OnboardingNativeScope) (OnboardingRobotSnapshot, error) {
	if !scope.valid() {
		return OnboardingRobotSnapshot{}, ErrMemberScope
	}
	value, err := m.onboardingRead(ctx, scope.AccountID, func(work context.Context) (any, error) {
		var out OnboardingRobotSnapshot
		var err error
		if scope.Platform == "lajiaoliao" {
			session, base, openErr := m.openChiliNative(work, scope.AccountID)
			if openErr != nil {
				return nil, openErr
			}
			if err = m.checkOnboardingAccount(work, scope, base); err != nil {
				return nil, err
			}
			client := lajiaoliao.NewClient()
			defer client.Close()
			profile, readErr := client.BasicProfile(work, session)
			if readErr != nil {
				return nil, readErr
			}
			if profile.ID != session.UserID {
				return nil, ErrMemberScope
			}
			out, err = onboardingRobotProjection(base, session.UserID, profile.Name)
		} else {
			client, saved, base, openErr := m.openSettingsClient(work, scope.AccountID, defaultSettingsClient)
			if openErr != nil {
				return nil, openErr
			}
			defer client.Close()
			base.Platform = "wsl"
			if err = m.checkOnboardingAccount(work, scope, base); err != nil {
				return nil, err
			}
			settings, readErr := client.Settings(work)
			if readErr != nil {
				return nil, readErr
			}
			if settings.UID != base.SelfID {
				return nil, ErrMemberScope
			}
			out, err = onboardingRobotProjection(base, saved.Session.AccountID, settings.Nickname)
		}
		if err != nil {
			return nil, err
		}
		out.SampledAt = time.Now().UTC()
		if err = m.syncOnboardingNickname(work, scope, out); err != nil {
			return nil, err
		}
		return out, nil
	})
	if err != nil {
		return OnboardingRobotSnapshot{}, err
	}
	out, ok := value.(OnboardingRobotSnapshot)
	if !ok {
		return out, ErrMemberScope
	}
	return out, nil
}

func onboardingRobotProjection(base SettingSnapshot, account, nickname string) (OnboardingRobotSnapshot, error) {
	out := OnboardingRobotSnapshot{SettingSnapshot: base, Name: nickname}
	if !utf8.ValidString(nickname) || len(nickname) > 1024 || strings.IndexFunc(nickname, unicode.IsControl) >= 0 {
		return OnboardingRobotSnapshot{}, ErrMemberScope
	}
	switch base.Platform {
	case "wsl":
		n, err := strconv.ParseUint(account, 10, 31)
		out.AddAvailable = err == nil && n > 0 && strconv.FormatUint(n, 10) == account
	case "lajiaoliao":
		out.AddAvailable = lajiaoliao.ValidID(account) && account == base.SelfID
	default:
		return OnboardingRobotSnapshot{}, ErrMemberScope
	}
	if out.AddAvailable {
		out.AddAccount = account
	} else {
		out.AddReason = "onboarding_robot_add_account_unavailable"
	}
	return out, nil
}

func (m *ReceiverManager) syncOnboardingNickname(ctx context.Context, scope OnboardingNativeScope, out OnboardingRobotSnapshot) error {
	var sealed []byte
	if err := m.DB.QueryRow(ctx, `SELECT credential_sealed FROM accounts WHERE id=$1 AND platform=$2
		AND purpose='management' AND auth_state='authenticated' AND epoch=$3 AND version=$4 AND native_uid=$5`,
		scope.AccountID, scope.Platform, scope.AccountEpoch, scope.AccountVersion, out.SelfID).Scan(&sealed); err != nil {
		return ErrMemberScope
	}
	if err := m.checkOnboardingAccount(ctx, scope, out.SettingSnapshot); err != nil {
		return err
	}
	tag, err := m.DB.Exec(ctx, `UPDATE accounts SET native_nickname=$6
		WHERE id=$1 AND platform=$2 AND purpose='management' AND auth_state='authenticated'
		AND epoch=$3 AND version=$4 AND native_uid=$5 AND credential_sealed=$7
		AND (platform='lajiaoliao' AND $8=0 OR platform='wsl' AND EXISTS(SELECT 1 FROM native_deployment WHERE version=$8))`,
		scope.AccountID, scope.Platform, scope.AccountEpoch, scope.AccountVersion, out.SelfID,
		out.Name, sealed, out.DeploymentVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrMemberScope
	}
	return nil
}
