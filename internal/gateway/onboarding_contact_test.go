package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

var _ onboardingContactClient = (*nativeauth.Client)(nil)
var _ onboardingContactClient = chiliOnboardingFriendClient{}
var _ onboardingContactRejectClient = (*nativeauth.Client)(nil)
var _ onboardingContactRejectClient = chiliOnboardingFriendClient{}

type syntheticOnboardingContactClient struct {
	*syntheticOnboardingFriendClient
	contactReads, contactReconciles, contactApprovals int
	selector                                          nativeauth.OnboardingContactSelector
	expected                                          nativeauth.OnboardingFriendEvidence
	readHook                                          func()
	skipFence, repeatFence                            bool
}

type syntheticOnboardingContactRejectClient struct {
	*syntheticOnboardingContactClient
	contactRejections int
	nativeHook        func()
	fenceContext      context.Context
}

func (c *syntheticOnboardingContactRejectClient) RejectOnboardingContact(ctx context.Context,
	selector nativeauth.OnboardingContactSelector, expected nativeauth.OnboardingFriendEvidence,
	_ time.Time, fence nativeauth.OnboardingFriendFence) error {
	c.contactRejections++
	c.selector, c.expected = selector, expected
	if c.nativeHook != nil {
		c.nativeHook()
	}
	if c.skipFence {
		return c.err
	}
	submit := func(context.Context) error {
		c.submissions++
		return c.err
	}
	if c.fenceContext != nil {
		ctx = c.fenceContext
	}
	err := fence(ctx, c.proof, submit)
	if c.repeatFence {
		return fence(ctx, c.proof, submit)
	}
	return err
}

func (c *syntheticOnboardingContactClient) OnboardingContact(_ context.Context,
	selector nativeauth.OnboardingContactSelector) (nativeauth.OnboardingFriendEvidence, error) {
	c.contactReads++
	c.selector = selector
	if c.readHook != nil {
		c.readHook()
	}
	return c.proof, c.err
}

func (c *syntheticOnboardingContactClient) ReconcileOnboardingContact(_ context.Context,
	selector nativeauth.OnboardingContactSelector, expected nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	c.contactReconciles++
	c.selector, c.expected = selector, expected
	return c.proof, c.err
}

func (c *syntheticOnboardingContactClient) ApproveOnboardingContact(ctx context.Context,
	selector nativeauth.OnboardingContactSelector, expected nativeauth.OnboardingFriendEvidence,
	_ time.Time, fence nativeauth.OnboardingFriendFence) error {
	c.contactApprovals++
	c.selector, c.expected = selector, expected
	if c.skipFence {
		return c.err
	}
	submit := func(context.Context) error {
		c.submissions++
		return c.err
	}
	err := fence(ctx, c.proof, submit)
	if c.repeatFence {
		return fence(ctx, c.proof, submit)
	}
	return err
}

func onboardingContactFixture(platform string) (OnboardingFriendApproval, SettingSnapshot, *syntheticOnboardingContactClient) {
	req, base, legacy := onboardingFriendFixture()
	at := time.Date(2026, 11, 8, 12, 1, 0, 123000000, time.UTC)
	req.ChallengeText = ""
	req.Contact = &nativeauth.OnboardingContactSelector{Name: "synthetic contact", Since: at.Add(-time.Minute), Until: at.Add(time.Minute)}
	req.Platform, base.Platform = platform, platform
	if platform == "lajiaoliao" {
		req.SenderID, req.RecipientID, base.SelfID = "customer-string", "self-string", "self-string"
		req.ApplicationID = "1794139260123"
		base.DeploymentVersion = 0
	}
	legacy.proof = req.expected()
	legacy.proof.RequestedAt = at
	return req, base, &syntheticOnboardingContactClient{syntheticOnboardingFriendClient: legacy}
}

func onboardingExpiredContactFixture(platform string) (OnboardingFriendApproval, SettingSnapshot, *syntheticOnboardingContactRejectClient) {
	req, base, client := onboardingContactFixture(platform)
	req.RejectExpired, req.Contact.RejectExpired = true, true
	client.proof.RequestedAt = req.Contact.Since.Add(-10 * time.Minute)
	req.RequestedAt = client.proof.RequestedAt
	if platform == "lajiaoliao" {
		req.ApplicationID = fmt.Sprint(client.proof.RequestedAt.UnixMilli())
		client.proof.ApplicationID = req.ApplicationID
	}
	return req, base, &syntheticOnboardingContactRejectClient{syntheticOnboardingContactClient: client}
}

func TestOnboardingContactGatewayReadReconcileAndDesktopScopeWithoutMobile(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			req, base, client := onboardingContactFixture(platform)
			checks := 0
			check := func(ctx context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
				checks++
				if scope != req.OnboardingNativeScope || !scope.matches(snapshot) || snapshot.SampledAt.IsZero() ||
					snapshot.MobileSessionVersion != 0 || snapshot.MobileDeploymentVersion != 0 {
					t.Fatal("contact evidence changed connection scope or required a mobile session")
				}
				return ctx.Err()
			}
			proof, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, nil, client, base, check)
			if err != nil || !req.Contact.Matches(proof.OnboardingFriendEvidence) || proof.State != "pending" ||
				client.contactReads != 1 || client.selector != *req.Contact || checks != 1 ||
				proof.ExpiresAt.Before(time.Now().Add(9*time.Minute)) {
				t.Fatal("contact read did not use fresh bounded native evidence", proof, err)
			}
			req.ExpiresAt = time.Now().Add(-time.Minute)
			client.proof.State = "approved"
			result, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, &req, client, base, check)
			if err != nil || result.State != "approved" || result.Name != proof.Name ||
				!result.RequestedAt.Equal(proof.RequestedAt) || !result.ExpiresAt.Equal(req.ExpiresAt) ||
				client.contactReconciles != 1 || client.contactApprovals != 0 || client.submissions != 0 ||
				client.reads != 0 || client.reconciles != 0 || client.approvals != 0 || checks != 2 {
				t.Fatal("reconciliation lost original contact or called a historical/mutating API", err)
			}
		})
	}
}

func TestOnboardingContactGatewayRejectsMalformedScopeAndContactEvidence(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"invalid_name", "invalid_interval", "epoch", "version", "platform",
			"recipient", "self_sender", "name", "missing_timestamp", "before_since", "at_until",
			"processed", "revoked", "unknown_read", "mobile_scope"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, base, client := onboardingContactFixture(platform)
				// A valid historical challenge may never rescue an invalid Contact.
				req.ChallengeText = "synthetic-old-challenge"
				switch mode {
				case "invalid_name":
					req.Contact.Name = " bad\n"
				case "invalid_interval":
					req.Contact.Until = req.Contact.Since.Add(11 * time.Minute)
				case "epoch":
					base.AccountEpoch++
				case "version":
					base.AccountVersion++
				case "platform":
					base.Platform = "another"
				case "recipient":
					client.proof.RecipientID = "other"
				case "self_sender":
					client.proof.SenderID = base.SelfID
				case "name":
					client.proof.Name = "another"
				case "missing_timestamp":
					client.proof.RequestedAt = time.Time{}
				case "before_since":
					client.proof.RequestedAt = req.Contact.Since.Add(-time.Nanosecond)
				case "at_until":
					client.proof.RequestedAt = req.Contact.Until
				case "processed":
					client.proof.State = "approved"
				case "unknown_read":
					client.err = errors.New("synthetic-native-unavailable")
				case "mobile_scope":
					if platform != "wsl" {
						return
					}
					base.MobileSessionVersion, base.MobileDeploymentVersion = 3, 4
				}
				check := func(ctx context.Context, _ OnboardingNativeScope, _ SettingSnapshot) error {
					if mode == "revoked" {
						return ErrMemberScope
					}
					return ctx.Err()
				}
				proof, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, nil, client, base, check)
				if err == nil || proof != (OnboardingFriendSnapshot{}) || client.submissions != 0 ||
					client.reads != 0 || client.reconciles != 0 || client.approvals != 0 {
					t.Fatal("unverified contact or scope escaped the gateway", proof, err)
				}
				if (mode == "invalid_name" || mode == "invalid_interval" || mode == "epoch" || mode == "version" ||
					mode == "platform" || mode == "mobile_scope") && client.contactReads != 0 {
					t.Fatal("invalid selection reached upstream")
				}
			})
		}
	}
}

func TestOnboardingContactGatewayApprovalFencesRevocationCancellationAndNoReplay(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"approved", "changed_id", "changed_name", "missing_timestamp", "outside_interval",
			"processed", "expired", "expired_in_fence", "revoked_before_fence", "revoked_in_fence",
			"cancelled_final", "cancelled_parent", "unknown", "duplicate_submit", "duplicate_fence",
			"no_native_fence", "no_submit", "concurrent_submit", "selector_mutated_in_fence"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, base, client := onboardingContactFixture(platform)
				original := *req.Contact
				switch mode {
				case "changed_id":
					client.proof.ApplicationID = "12"
				case "changed_name":
					client.proof.Name = "changed"
				case "missing_timestamp":
					client.proof.RequestedAt = time.Time{}
				case "outside_interval":
					client.proof.RequestedAt = req.Contact.Until
				case "processed":
					client.proof.State = "approved"
				case "expired":
					req.ExpiresAt = time.Now().Add(-time.Second)
				case "expired_in_fence":
					req.ExpiresAt = time.Now().Add(25 * time.Millisecond)
				case "unknown":
					client.err = &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
				case "duplicate_fence":
					client.repeatFence = true
				case "no_native_fence":
					client.skipFence = true
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				revoked, fences := false, 0
				check := func(work context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
					if mode == "revoked_before_fence" || revoked {
						return ErrMemberScope
					}
					if scope != req.OnboardingNativeScope || !scope.matches(snapshot) {
						t.Fatal("contact approval changed account/platform scope")
					}
					return work.Err()
				}
				err := approveOnboardingFriend(ctx, req, client, base, check,
					func(work context.Context, proof OnboardingFriendSnapshot, send func(context.Context) error) error {
						fences++
						if !original.Matches(proof.OnboardingFriendEvidence) || proof.State != "pending" ||
							proof.ApplicationID != req.ApplicationID || !proof.ExpiresAt.Equal(req.ExpiresAt) {
							t.Fatal("changed contact reached the control fence")
						}
						switch mode {
						case "revoked_in_fence":
							revoked = true
						case "expired_in_fence":
							timer := time.NewTimer(time.Until(req.ExpiresAt) + time.Millisecond)
							<-timer.C
						case "cancelled_final":
							stopped, stop := context.WithCancel(work)
							stop()
							return send(stopped)
						case "cancelled_parent":
							cancel()
							return send(context.Background())
						case "no_submit":
							return nil
						case "selector_mutated_in_fence":
							req.Contact.Name = "late caller edit"
						case "concurrent_submit":
							var wg sync.WaitGroup
							var success atomic.Int32
							for range 8 {
								wg.Go(func() {
									if send(work) == nil {
										success.Add(1)
									}
								})
							}
							wg.Wait()
							if success.Load() != 1 {
								t.Fatal("concurrent approval did not submit exactly once")
							}
							return nil
						}
						err := send(work)
						if mode == "duplicate_submit" {
							return send(work)
						}
						return err
					})
				success := mode == "approved" || mode == "concurrent_submit" || mode == "selector_mutated_in_fence"
				wantWrites := 0
				if success || mode == "unknown" || mode == "duplicate_submit" || mode == "duplicate_fence" {
					wantWrites = 1
				}
				if (err == nil) != success || client.submissions != wantWrites ||
					client.contactApprovals > 1 || client.approvals != 0 || client.reads != 0 || client.reconciles != 0 {
					t.Fatal("contact approval bypassed a fence or was replayed", client.submissions, err)
				}
				if fences > 1 || client.selector != original && mode != "expired" {
					t.Fatal("native fence was repeated or its immutable selection changed")
				}
				if mode == "unknown" {
					var failure *nativeauth.Error
					if !errors.As(err, &failure) || !failure.Submitted {
						t.Fatal("unknown native result lost submission state", err)
					}
				}
			})
		}
	}
}

func TestOnboardingContactGatewayMissingAdapterAndSelectorIsolation(t *testing.T) {
	req, base, client := onboardingContactFixture("wsl")
	check := func(ctx context.Context, _ OnboardingNativeScope, _ SettingSnapshot) error { return ctx.Err() }
	legacy := client.syntheticOnboardingFriendClient
	if _, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, nil, legacy, base, check); err == nil {
		t.Fatal("missing contact adapter silently fell back to historical note matching")
	}
	if err := approveOnboardingFriend(context.Background(), req, legacy, base, check,
		func(work context.Context, _ OnboardingFriendSnapshot, send func(context.Context) error) error {
			return send(work)
		}); err == nil || legacy.reads != 0 || legacy.approvals != 0 || legacy.submissions != 0 {
		t.Fatal("missing contact adapter silently performed a historical approval")
	}
	original := *req.Contact
	client.readHook = func() { req.Contact.Name = "late draft" }
	proof, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, nil, client, base, check)
	if err != nil || !original.Matches(proof.OnboardingFriendEvidence) || client.selector != original {
		t.Fatal("late caller draft changed an in-flight selection", err)
	}
	req.Contact = &original
	other := req
	other.OnboardingFriendRequest = req.OnboardingFriendRequest.snapshot()
	other.Contact.Until = other.Contact.Until.Add(-time.Second)
	if _, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, &other, client, base, check); err == nil {
		t.Fatal("reconciliation substituted another selector")
	}
}

func TestOnboardingContactGatewayRequestsDoNotLogOrSerializeSelector(t *testing.T) {
	req, _, _ := onboardingContactFixture("wsl")
	for _, value := range []any{req.OnboardingFriendRequest, req} {
		raw, _ := json.Marshal(value)
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("synthetic", "request", value)
		for _, text := range []string{string(raw), fmt.Sprintf("%v %+v %#v", value, value, value), log.String()} {
			if bytes.Contains([]byte(text), []byte(req.Contact.Name)) || bytes.Contains([]byte(text), []byte("Contact")) ||
				bytes.Contains([]byte(text), []byte(req.Contact.Since.Format(time.RFC3339Nano))) {
				t.Fatal("contact selector escaped request JSON or logging")
			}
		}
	}
}

func TestOnboardingContactGatewayApprovalCannotUseExpiredRejection(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"reject_intent", "reject_without_selector", "forged_selector", "fresh_with_selector"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, base, client := onboardingExpiredContactFixture(platform)
				switch mode {
				case "reject_without_selector":
					req.Contact = nil
					req.ChallengeText = "synthetic-old-challenge"
				case "forged_selector":
					req.RejectExpired = false
				case "fresh_with_selector":
					req.RejectExpired = false
					client.proof.RequestedAt = req.Contact.Since
					req.RequestedAt = client.proof.RequestedAt
				}
				fences := 0
				check := func(ctx context.Context, _ OnboardingNativeScope, _ SettingSnapshot) error { return ctx.Err() }
				fence := func(work context.Context, _ OnboardingFriendSnapshot, send func(context.Context) error) error {
					fences++
					return send(work)
				}
				if req.RejectExpired {
					// No queue or DB: the public approval entry must reject the intent first.
					if err := (&ReceiverManager{}).ApproveOnboardingFriend(context.Background(), req, fence); !errors.Is(err, ErrMemberScope) {
						t.Fatal("reject intent reached the approval queue", err)
					}
				}
				err := approveOnboardingFriend(context.Background(), req, client, base, check, fence)
				success := mode == "fresh_with_selector"
				wantWrites := 0
				if success {
					wantWrites = 1
				}
				if (err == nil) != success || client.submissions != wantWrites || fences != wantWrites ||
					client.contactRejections != 0 || client.approvals != 0 || client.contactApprovals > 1 {
					t.Fatal("expired rejection became approval or rejected a current contact", err)
				}
				if req.RejectExpired && client.contactApprovals != 0 {
					t.Fatal("reject intent reached the native approval client")
				}
			})
		}
	}
}

func TestOnboardingExpiredContactGatewayRejectRequiresExplicitScopedIntent(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"no_intent", "no_contact", "no_selector_flag", "missing_expected_timestamp", "invalid_name", "invalid_interval",
			"empty_account", "epoch", "version", "platform", "recipient", "self_sender", "mobile_scope",
			"nil_fence", "deadline", "missing_adapter", "legacy_adapter"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, base, client := onboardingExpiredContactFixture(platform)
				switch mode {
				case "no_intent":
					req.RejectExpired = false
				case "no_contact":
					req.Contact = nil
					req.ChallengeText = "synthetic-old-challenge"
				case "no_selector_flag":
					req.Contact.RejectExpired = false
				case "missing_expected_timestamp":
					req.RequestedAt = time.Time{}
				case "invalid_name":
					req.Contact.Name = "invalid\n"
				case "invalid_interval":
					req.Contact.Until = req.Contact.Since.Add(11 * time.Minute)
				case "empty_account":
					req.AccountID = ""
				case "epoch":
					base.AccountEpoch++
				case "version":
					base.AccountVersion++
				case "platform":
					base.Platform = "another"
				case "recipient":
					base.SelfID = "another"
				case "self_sender":
					req.SenderID = req.RecipientID
				case "mobile_scope":
					if platform != "wsl" {
						return
					}
					base.MobileSessionVersion, base.MobileDeploymentVersion = 3, 4
				case "deadline":
					req.ExpiresAt = time.Now().Add(-time.Second)
				}
				checks, fences := 0, 0
				check := func(ctx context.Context, _ OnboardingNativeScope, _ SettingSnapshot) error {
					checks++
					return ctx.Err()
				}
				var fence OnboardingFriendFence = func(work context.Context, _ OnboardingFriendSnapshot, send func(context.Context) error) error {
					fences++
					return send(work)
				}
				if mode == "nil_fence" {
					fence = nil
				}
				if mode == "no_intent" || mode == "no_contact" || mode == "no_selector_flag" ||
					mode == "missing_expected_timestamp" ||
					mode == "invalid_name" || mode == "invalid_interval" || mode == "empty_account" ||
					mode == "nil_fence" || mode == "deadline" || mode == "self_sender" {
					if err := (&ReceiverManager{}).RejectOnboardingFriend(context.Background(), req, fence); err == nil {
						t.Fatal("invalid rejection reached a queue or database")
					}
				}
				var adapter onboardingFriendClient = client
				if mode == "missing_adapter" {
					adapter = client.syntheticOnboardingContactClient
				}
				if mode == "legacy_adapter" {
					adapter = client.syntheticOnboardingFriendClient
				}
				err := rejectOnboardingFriend(context.Background(), req, adapter, base, check, fence)
				if err == nil || checks != 0 || fences != 0 || client.contactRejections != 0 ||
					client.submissions != 0 || client.contactApprovals != 0 || client.approvals != 0 {
					t.Fatal("rejection escaped scope or fell back to another mutation", err)
				}
			})
		}
	}
}

func TestOnboardingExpiredContactGatewayFreshProofMustBeExactPendingAndExpired(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"at_cutoff", "before_cutoff", "changed_timestamp", "inside_current_window", "after_cutoff",
			"at_until", "missing_timestamp", "application", "sender", "recipient", "self_sender", "name",
			"approved", "rejected", "expired"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, base, client := onboardingExpiredContactFixture(platform)
				switch mode {
				case "before_cutoff":
					client.proof.RequestedAt = client.proof.RequestedAt.Add(-time.Nanosecond)
					req.RequestedAt = client.proof.RequestedAt
				case "changed_timestamp":
					client.proof.RequestedAt = client.proof.RequestedAt.Add(-time.Second)
				case "inside_current_window":
					client.proof.RequestedAt = req.Contact.Since
				case "after_cutoff":
					client.proof.RequestedAt = client.proof.RequestedAt.Add(time.Nanosecond)
				case "at_until":
					client.proof.RequestedAt = req.Contact.Until
				case "missing_timestamp":
					client.proof.RequestedAt = time.Time{}
				case "application":
					client.proof.ApplicationID = "12"
				case "sender":
					client.proof.SenderID = "9"
				case "recipient":
					client.proof.RecipientID = "9"
				case "self_sender":
					client.proof.SenderID = base.SelfID
				case "name":
					client.proof.Name = "another contact"
				case "approved", "rejected", "expired":
					client.proof.State = mode
				}
				fences, checks := 0, 0
				check := func(ctx context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
					checks++
					if scope != req.OnboardingNativeScope || !scope.matches(snapshot) {
						t.Fatal("rejection changed its account or platform")
					}
					return ctx.Err()
				}
				err := rejectOnboardingFriend(context.Background(), req, client, base, check,
					func(work context.Context, proof OnboardingFriendSnapshot, send func(context.Context) error) error {
						fences++
						if !req.Contact.IsExpired(proof.OnboardingFriendEvidence) || proof.State != "pending" ||
							proof.ApplicationID != req.ApplicationID || proof.SenderID != req.SenderID || proof.RecipientID != req.RecipientID {
							t.Fatal("unverified expired proof reached control")
						}
						return send(work)
					})
				success := mode == "at_cutoff" || mode == "before_cutoff"
				wantWrites := 0
				if success {
					wantWrites = 1
				}
				if (err == nil) != success || client.submissions != wantWrites || fences != wantWrites ||
					checks != 2*wantWrites || client.contactRejections != 1 ||
					client.contactApprovals != 0 || client.approvals != 0 || client.selector != *req.Contact ||
					client.expected != req.expected() || !req.Contact.IsExpired(client.expected) {
					t.Fatal("rejection accepted a current, changed or processed application", err)
				}
			})
		}
	}
}

func TestOnboardingExpiredContactGatewayFinalFenceOnceCancellationAndNoRetry(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"rejected", "account_before_fence", "account_in_fence", "platform_in_fence",
			"epoch_in_fence", "version_in_fence", "deployment_in_fence", "connection_in_fence",
			"expired_in_fence", "cancelled_before", "cancelled_native", "cancelled_work", "cancelled_parent", "cancelled_final",
			"no_native_fence", "no_submit", "duplicate_fence", "duplicate_submit", "concurrent_submit", "unknown",
			"selector_mutated_native", "selector_mutated_in_fence"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, base, client := onboardingExpiredContactFixture(platform)
				original := req.OnboardingFriendRequest.snapshot()
				account, current, connected := req.AccountID, base, true
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch mode {
				case "account_before_fence":
					account = "changed-account"
				case "expired_in_fence":
					req.ExpiresAt = time.Now().Add(25 * time.Millisecond)
				case "cancelled_before":
					cancel()
				case "cancelled_native":
					client.nativeHook = cancel
				case "cancelled_work":
					stopped, stop := context.WithCancel(context.Background())
					stop()
					client.fenceContext = stopped
				case "no_native_fence":
					client.skipFence = true
				case "duplicate_fence":
					client.repeatFence = true
				case "unknown":
					client.err = &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
				case "selector_mutated_native":
					client.nativeHook = func() { req.Contact.RejectExpired = false }
				}
				check := func(work context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
					if scope != original.OnboardingNativeScope || snapshot.SampledAt.IsZero() {
						t.Fatal("rejection lost immutable account scope or proof freshness")
					}
					if !connected || scope.AccountID != account || !scope.matches(current) ||
						snapshot.SelfID != current.SelfID || snapshot.DeploymentVersion != current.DeploymentVersion ||
						snapshot.CredentialDigest != current.CredentialDigest {
						return ErrMemberScope
					}
					return work.Err()
				}
				fences := 0
				err := rejectOnboardingFriend(ctx, req, client, base, check,
					func(work context.Context, proof OnboardingFriendSnapshot, send func(context.Context) error) error {
						fences++
						if !original.Contact.IsExpired(proof.OnboardingFriendEvidence) || proof.State != "pending" ||
							proof.ApplicationID != req.ApplicationID || !proof.ExpiresAt.Equal(req.ExpiresAt) {
							t.Fatal("rejection changed the sealed application before control")
						}
						switch mode {
						case "account_in_fence":
							account = "changed-account"
						case "platform_in_fence":
							current.Platform = "another"
						case "epoch_in_fence":
							current.AccountEpoch++
						case "version_in_fence":
							current.AccountVersion++
						case "deployment_in_fence":
							current.DeploymentVersion++
						case "connection_in_fence":
							connected = false
						case "expired_in_fence":
							timer := time.NewTimer(time.Until(req.ExpiresAt) + time.Millisecond)
							<-timer.C
						case "cancelled_parent":
							cancel()
							return send(context.Background())
						case "cancelled_final":
							stopped, stop := context.WithCancel(work)
							stop()
							return send(stopped)
						case "no_submit":
							return nil
						case "selector_mutated_in_fence":
							req.Contact.Name = "late draft"
							req.Contact.Since = req.Contact.Since.Add(time.Second)
							req.Contact.Until = req.Contact.Until.Add(time.Second)
							req.Contact.RejectExpired = false
						case "concurrent_submit":
							var wg sync.WaitGroup
							var success atomic.Int32
							for range 8 {
								wg.Go(func() {
									if send(work) == nil {
										success.Add(1)
									}
								})
							}
							wg.Wait()
							if success.Load() != 1 {
								t.Fatal("concurrent rejection submitted more than once")
							}
							return nil
						}
						err := send(work)
						if mode == "duplicate_submit" {
							return send(work)
						}
						return err
					})
				success := mode == "rejected" || mode == "concurrent_submit" ||
					mode == "selector_mutated_native" || mode == "selector_mutated_in_fence"
				wantWrites := 0
				if success || mode == "unknown" || mode == "duplicate_fence" || mode == "duplicate_submit" {
					wantWrites = 1
				}
				if (err == nil) != success || client.submissions != wantWrites || client.contactRejections > 1 ||
					client.contactApprovals != 0 || client.approvals != 0 || fences > 1 {
					t.Fatal("rejection bypassed its final fence, reported false success or retried", err, client.submissions)
				}
				if mode == "cancelled_before" && client.contactRejections != 0 {
					t.Fatal("cancelled rejection reached the native adapter")
				}
				if mode != "cancelled_before" && client.selector != *original.Contact {
					t.Fatal("caller mutated the in-flight selector")
				}
				if mode == "unknown" {
					var failure *nativeauth.Error
					if !errors.As(err, &failure) || !failure.Submitted {
						t.Fatal("unknown rejection lost submission evidence", err)
					}
				}
			})
		}
	}
}

func TestOnboardingExpiredContactGatewayReadOnlyReconcileKeepsOriginalSelection(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"rejected", "pending", "application", "sender", "recipient", "name",
			"current_timestamp", "changed_timestamp", "missing_expected_timestamp",
			"selector_name", "selector_since", "selector_until", "selector_flag", "intent_flag",
			"account", "platform", "epoch", "version", "cancelled"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				req, base, client := onboardingExpiredContactFixture(platform)
				request := req.OnboardingFriendRequest.snapshot()
				req.ExpiresAt = time.Now().Add(-time.Minute)
				client.proof.State = "rejected"
				switch mode {
				case "pending":
					client.proof.State = "pending"
				case "application":
					client.proof.ApplicationID = "12"
				case "sender":
					client.proof.SenderID = "9"
				case "recipient":
					client.proof.RecipientID = "9"
				case "name":
					client.proof.Name = "another contact"
				case "current_timestamp":
					client.proof.RequestedAt = req.Contact.Since
				case "changed_timestamp":
					client.proof.RequestedAt = client.proof.RequestedAt.Add(-time.Second)
				case "missing_expected_timestamp":
					req.RequestedAt = time.Time{}
				case "selector_name":
					req.Contact.Name = "another contact"
				case "selector_since":
					req.Contact.Since = req.Contact.Since.Add(time.Second)
				case "selector_until":
					req.Contact.Until = req.Contact.Until.Add(time.Second)
				case "selector_flag":
					request.Contact.RejectExpired = false
				case "intent_flag":
					req.RejectExpired = false
					req.Contact.RejectExpired = false
				case "account":
					req.AccountID = "another-account"
				case "platform":
					req.Platform = "another"
				case "epoch":
					req.AccountEpoch++
				case "version":
					req.AccountVersion++
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if mode == "cancelled" {
					cancel()
				}
				checks := 0
				check := func(work context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
					checks++
					if scope != request.OnboardingNativeScope || !scope.matches(snapshot) {
						t.Fatal("reconciliation changed account scope")
					}
					return work.Err()
				}
				proof, err := readOnboardingFriend(ctx, request, &req, client, base, check)
				success := mode == "rejected" || mode == "pending"
				if (err == nil) != success || client.submissions != 0 || client.contactRejections != 0 ||
					client.contactApprovals != 0 || client.approvals != 0 || client.contactReads != 0 ||
					client.reads != 0 || client.reconciles != 0 {
					t.Fatal("reconciliation mutated or substituted an application", proof, err)
				}
				if success && (proof.State != client.proof.State || proof.ApplicationID != req.ApplicationID ||
					!request.Contact.IsExpired(proof.OnboardingFriendEvidence) || !proof.ExpiresAt.Equal(req.ExpiresAt) ||
					client.contactReconciles != 1 || checks != 1 || client.selector != *request.Contact) {
					t.Fatal("reconciliation lost the fixed expired application or renewed its deadline", err)
				}
				if !success && (proof != (OnboardingFriendSnapshot{}) || checks != 0) {
					t.Fatal("unverified reconciliation returned a snapshot", err)
				}
				if (mode == "selector_name" || mode == "selector_since" || mode == "selector_until" ||
					mode == "selector_flag" || mode == "intent_flag" || mode == "missing_expected_timestamp" ||
					mode == "account" || mode == "platform" ||
					mode == "epoch" || mode == "version" || mode == "cancelled") && client.contactReconciles != 0 {
					t.Fatal("changed selector or account reached native reconciliation")
				}
			})
		}
	}
}

func TestOnboardingExpiredContactGatewayRequestsDoNotExposeRejectFlag(t *testing.T) {
	req, _, _ := onboardingExpiredContactFixture("wsl")
	raw, err := json.Marshal(req)
	if err != nil || bytes.Contains(raw, []byte("RejectExpired")) || bytes.Contains(raw, []byte("RequestedAt")) {
		t.Fatal("sealed rejection intent or timestamp became a public request field", err)
	}
}

func TestOnboardingExpiredContactGatewayDiscoveryKeepsImmutableSelectorFlag(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			req, base, client := onboardingExpiredContactFixture(platform)
			original := *req.Contact
			client.readHook = func() {
				req.Contact.RejectExpired = false
				req.Contact.Name = "late draft"
			}
			checks := 0
			check := func(ctx context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
				checks++
				if scope != req.OnboardingNativeScope || !scope.matches(snapshot) {
					t.Fatal("expired discovery changed account scope")
				}
				return ctx.Err()
			}
			proof, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, nil, client, base, check)
			if err != nil || !original.IsExpired(proof.OnboardingFriendEvidence) || proof.State != "pending" ||
				client.selector != original || client.contactReads != 1 || checks != 1 ||
				proof.ExpiresAt.Before(time.Now().Add(9*time.Minute)) || client.contactReconciles != 0 ||
				client.contactRejections != 0 || client.contactApprovals != 0 || client.submissions != 0 {
				t.Fatal("expired discovery lost its immutable flag or performed a mutation", err)
			}
		})
	}
}
