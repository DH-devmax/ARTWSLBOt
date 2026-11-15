package gateway

import (
	"context"
	"encoding/hex"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

type OnboardingOutboundTarget struct {
	NativeID      string
	AddAccount    string
	Name          string
	AlreadyFriend bool
}

type OnboardingOutboundLookup struct {
	OnboardingNativeScope
	AddAccount     string                  `json:"-"`
	ControlledPeer *OnboardingOutboundPeer `json:"-"`
}

type OnboardingOutboundPeer struct {
	OnboardingNativeScope
	NativeID   string
	Connection OnboardingPeerConnection
}

func (OnboardingOutboundLookup) String() string   { return "OnboardingOutboundLookup(redacted)" }
func (OnboardingOutboundLookup) GoString() string { return "OnboardingOutboundLookup(redacted)" }

type OnboardingOutboundRequest struct {
	OnboardingNativeScope
	OperationID    string                   `json:"-"`
	Target         OnboardingOutboundTarget `json:"-"`
	SelfID         string                   `json:"-"`
	ExpiresAt      time.Time                `json:"-"`
	ControlledPeer *OnboardingOutboundPeer  `json:"-"`
}

func (OnboardingOutboundRequest) String() string   { return "OnboardingOutboundRequest(redacted)" }
func (OnboardingOutboundRequest) GoString() string { return "OnboardingOutboundRequest(redacted)" }

type OnboardingOutboundSnapshot struct {
	SettingSnapshot
	Target        OnboardingOutboundTarget
	ApplicationID string
	State         string
}

type OnboardingOutboundFence func(context.Context, OnboardingOutboundSnapshot, func(context.Context) error) error

type OnboardingOutboundLookupFence func(context.Context, func(context.Context) error) error

type onboardingOutboundClient interface {
	Lookup(context.Context, string) (nativeauth.OnboardingOutboundTarget, error)
	Send(context.Context, nativeauth.OnboardingOutboundRequest, nativeauth.OnboardingOutboundFence) (nativeauth.OnboardingOutboundEvidence, error)
	Reconcile(context.Context, nativeauth.OnboardingOutboundRequest) (nativeauth.OnboardingOutboundEvidence, error)
	Close()
}

func validOnboardingOutboundAccount(platform, account string) bool {
	if platform == "wsl" {
		return nativeauth.ValidOnboardingOutboundAccount(account)
	}
	return platform == "lajiaoliao" && lajiaoliao.ValidOnboardingOutboundAccount(account)
}

func validOnboardingOutboundTarget(platform string, target OnboardingOutboundTarget) bool {
	if !validOnboardingOutboundAccount(platform, target.AddAccount) || target.NativeID == "" ||
		len(target.Name) > 1000 || !utf8.ValidString(target.Name) || strings.ContainsFunc(target.Name, unicode.IsControl) {
		return false
	}
	if platform == "lajiaoliao" {
		return lajiaoliao.ValidID(target.NativeID)
	}
	uid, err := strconv.ParseUint(target.NativeID, 10, 30)
	return err == nil && uid > 0 && strconv.FormatUint(uid, 10) == target.NativeID
}

func validOnboardingOperation(operation string) bool {
	if len(operation) != 36 || operation[8] != '-' || operation[13] != '-' ||
		operation[18] != '-' || operation[23] != '-' {
		return false
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(operation, "-", ""))
	return err == nil && len(raw) == 16
}

func (r OnboardingOutboundRequest) valid() bool {
	return r.OnboardingNativeScope.valid() && validOnboardingOperation(r.OperationID) && validOnboardingOutboundTarget(r.Platform, r.Target) &&
		!r.Target.AlreadyFriend && r.SelfID != "" && r.SelfID != r.Target.NativeID &&
		(r.ControlledPeer == nil || validOnboardingPeer(r.OnboardingNativeScope, *r.ControlledPeer) &&
			r.ControlledPeer.NativeID == r.Target.NativeID)
}

func outboundTarget(target nativeauth.OnboardingOutboundTarget) OnboardingOutboundTarget {
	return OnboardingOutboundTarget{NativeID: target.ID, AddAccount: target.Account,
		Name: target.Name, AlreadyFriend: target.IsFriend}
}

func outboundNativeTarget(target OnboardingOutboundTarget) nativeauth.OnboardingOutboundTarget {
	return nativeauth.OnboardingOutboundTarget{ID: target.NativeID, Account: target.AddAccount,
		Name: target.Name, IsFriend: target.AlreadyFriend}
}

func outboundNativeRequest(req OnboardingOutboundRequest) nativeauth.OnboardingOutboundRequest {
	return nativeauth.OnboardingOutboundRequest{OperationID: req.OperationID, SelfID: req.SelfID,
		Target: outboundNativeTarget(req.Target), ExpiresAt: req.ExpiresAt}
}

func outboundUnavailable(target nativeauth.OnboardingOutboundTarget) error {
	switch target.BlockedReason {
	case "native_onboarding_outbound_pending", "native_onboarding_outbound_incoming",
		"native_onboarding_outbound_policy", "native_onboarding_outbound_blocked",
		"native_onboarding_outbound_changed", "native_onboarding_outbound_read_incomplete":
		return &nativeauth.Error{Code: target.BlockedReason}
	default:
		return &nativeauth.Error{Code: "native_onboarding_outbound_unavailable"}
	}
}

func (m *ReceiverManager) LookupOnboardingOutboundFriend(ctx context.Context, req OnboardingOutboundLookup,
	fence OnboardingOutboundLookupFence) (OnboardingOutboundSnapshot, error) {
	if !req.OnboardingNativeScope.valid() || fence == nil {
		return OnboardingOutboundSnapshot{}, ErrMemberScope
	}
	if !validOnboardingOutboundAccount(req.Platform, req.AddAccount) {
		return OnboardingOutboundSnapshot{}, &nativeauth.Error{Code: "native_onboarding_outbound_account_invalid"}
	}
	value, err := m.onboardingRead(ctx, req.AccountID, func(work context.Context) (any, error) {
		client, base, err := m.openOnboardingOutboundPeer(work, req.OnboardingNativeScope, req.ControlledPeer, req.AddAccount)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var result OnboardingOutboundSnapshot
		var dispatched atomic.Bool
		err = fence(work, func(final context.Context) error {
			if !dispatched.CompareAndSwap(false, true) {
				return ErrMemberScope
			}
			if err := final.Err(); err != nil {
				return err
			}
			if err := m.checkOnboardingAccount(final, req.OnboardingNativeScope, base); err != nil {
				return err
			}
			var readErr error
			result, readErr = readOnboardingOutbound(final, req, nil, client, base, m.checkOnboardingAccount)
			return readErr
		})
		if err == nil && !dispatched.Load() {
			err = ErrMemberScope
		}
		return result, err
	})
	return onboardingOutboundResult(value, err)
}

func (m *ReceiverManager) ReconcileOnboardingOutboundFriend(ctx context.Context, req OnboardingOutboundRequest) (OnboardingOutboundSnapshot, error) {
	if !req.valid() {
		return OnboardingOutboundSnapshot{}, ErrMemberScope
	}
	value, err := m.onboardingRead(ctx, req.AccountID, func(work context.Context) (any, error) {
		client, base, err := m.openOnboardingOutboundPeer(work, req.OnboardingNativeScope, req.ControlledPeer, req.Target.AddAccount)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return readOnboardingOutbound(work, OnboardingOutboundLookup{
			OnboardingNativeScope: req.OnboardingNativeScope, AddAccount: req.Target.AddAccount,
			ControlledPeer: req.ControlledPeer,
		}, &req, client, base, m.checkOnboardingAccount)
	})
	return onboardingOutboundResult(value, err)
}

func onboardingOutboundResult(value any, err error) (OnboardingOutboundSnapshot, error) {
	if err != nil {
		return OnboardingOutboundSnapshot{}, err
	}
	out, ok := value.(OnboardingOutboundSnapshot)
	if !ok {
		return OnboardingOutboundSnapshot{}, ErrMemberScope
	}
	return out, nil
}

func readOnboardingOutbound(ctx context.Context, req OnboardingOutboundLookup, original *OnboardingOutboundRequest,
	client onboardingOutboundClient, base SettingSnapshot,
	check func(context.Context, OnboardingNativeScope, SettingSnapshot) error) (OnboardingOutboundSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return OnboardingOutboundSnapshot{}, err
	}
	if !req.OnboardingNativeScope.matches(base) || !validOnboardingOutboundAccount(req.Platform, req.AddAccount) {
		return OnboardingOutboundSnapshot{}, ErrMemberScope
	}
	out := OnboardingOutboundSnapshot{SettingSnapshot: base}
	if original == nil {
		target, err := client.Lookup(ctx, req.AddAccount)
		if err != nil {
			return OnboardingOutboundSnapshot{}, err
		}
		out.Target = outboundTarget(target)
		if target.IsFriend {
			out.State = "friends"
		} else if target.CanRequest {
			out.State = "available"
		} else {
			return OnboardingOutboundSnapshot{}, outboundUnavailable(target)
		}
	} else {
		if !original.valid() || original.OnboardingNativeScope != req.OnboardingNativeScope ||
			original.Target.AddAccount != req.AddAccount || original.SelfID != base.SelfID ||
			!sameOnboardingPeer(original.ControlledPeer, req.ControlledPeer) {
			return OnboardingOutboundSnapshot{}, ErrMemberScope
		}
		evidence, err := client.Reconcile(ctx, outboundNativeRequest(*original))
		if err != nil {
			return OnboardingOutboundSnapshot{}, err
		}
		if evidence.OperationID != original.OperationID || evidence.SenderID != base.SelfID || evidence.RecipientID != original.Target.NativeID ||
			evidence.IsFriend != (evidence.State == "friends") {
			return OnboardingOutboundSnapshot{}, ErrMemberScope
		}
		switch evidence.State {
		case "pending", "friends", "rejected", "missing", "unknown":
		default:
			return OnboardingOutboundSnapshot{}, ErrMemberScope
		}
		out.Target, out.State, out.ApplicationID = original.Target, evidence.State, evidence.ApplicationID
		out.Target.AlreadyFriend = evidence.IsFriend
	}
	if !validOnboardingOutboundTarget(req.Platform, out.Target) || out.Target.AddAccount != req.AddAccount ||
		out.Target.NativeID == base.SelfID {
		return OnboardingOutboundSnapshot{}, ErrMemberScope
	}
	out.SampledAt = time.Now().UTC()
	if err := check(ctx, req.OnboardingNativeScope, out.SettingSnapshot); err != nil {
		return OnboardingOutboundSnapshot{}, err
	}
	return out, nil
}

func (m *ReceiverManager) SendOnboardingOutboundFriend(ctx context.Context, req OnboardingOutboundRequest, fence OnboardingOutboundFence) error {
	if !req.valid() || fence == nil || !nativeauth.OnboardingFriendDeadline(req.ExpiresAt, time.Now()) {
		return &nativeauth.Error{Code: "native_onboarding_outbound_expired"}
	}
	_, err := m.onboardingRead(ctx, req.AccountID, func(work context.Context) (any, error) {
		client, base, err := m.openOnboardingOutboundPeer(work, req.OnboardingNativeScope, req.ControlledPeer, req.Target.AddAccount)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return nil, sendOnboardingOutbound(work, req, client, base, m.checkOnboardingAccount, fence)
	})
	return err
}

func sendOnboardingOutbound(ctx context.Context, req OnboardingOutboundRequest, client onboardingOutboundClient,
	base SettingSnapshot, check func(context.Context, OnboardingNativeScope, SettingSnapshot) error,
	fence OnboardingOutboundFence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !req.valid() || !req.OnboardingNativeScope.matches(base) || req.SelfID != base.SelfID || fence == nil {
		return ErrMemberScope
	}
	if !nativeauth.OnboardingFriendDeadline(req.ExpiresAt, time.Now()) {
		return &nativeauth.Error{Code: "native_onboarding_outbound_expired"}
	}
	var fenced, submitted atomic.Bool
	result, err := client.Send(ctx, outboundNativeRequest(req),
		func(work context.Context, target nativeauth.OnboardingOutboundTarget, submit func(context.Context) error) error {
			if !fenced.CompareAndSwap(false, true) || submit == nil ||
				outboundTarget(target) != req.Target || !target.CanRequest {
				return ErrMemberScope
			}
			base.SampledAt = time.Now().UTC()
			if err := check(work, req.OnboardingNativeScope, base); err != nil {
				return err
			}
			snapshot := OnboardingOutboundSnapshot{SettingSnapshot: base, Target: req.Target, State: "available"}
			return fence(work, snapshot, func(final context.Context) error {
				if !submitted.CompareAndSwap(false, true) {
					return ErrMemberScope
				}
				if err := final.Err(); err != nil {
					return err
				}
				if !nativeauth.OnboardingFriendDeadline(req.ExpiresAt, time.Now()) ||
					time.Since(snapshot.SampledAt) > 5*time.Second {
					return &nativeauth.Error{Code: "native_onboarding_outbound_expired"}
				}
				if err := check(final, req.OnboardingNativeScope, snapshot.SettingSnapshot); err != nil {
					return err
				}
				return submit(final)
			})
		})
	if err == nil && (!fenced.Load() || !submitted.Load()) {
		if !fenced.Load() && !submitted.Load() && result.OperationID == req.OperationID &&
			result.SenderID == req.SelfID && result.RecipientID == req.Target.NativeID &&
			result.IsFriend && result.State == "friends" {
			return &nativeauth.Error{Code: "native_onboarding_outbound_already_friend"}
		}
		return ErrMemberScope
	}
	if err == nil && (result.OperationID != req.OperationID || result.SenderID != req.SelfID ||
		result.RecipientID != req.Target.NativeID || result.IsFriend != (result.State == "friends") ||
		result.State != "pending" && result.State != "friends") {
		return &nativeauth.Error{Code: "native_response_invalid", Submitted: true}
	}
	return err
}
