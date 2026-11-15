package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

type syntheticOnboardingFriendClient struct {
	proof                  nativeauth.OnboardingFriendEvidence
	reads, reconciles      int
	approvals, submissions int
	err                    error
}

func (c *syntheticOnboardingFriendClient) OnboardingFriend(context.Context, string) (nativeauth.OnboardingFriendEvidence, error) {
	c.reads++
	return c.proof, c.err
}
func (c *syntheticOnboardingFriendClient) ReconcileOnboardingFriend(context.Context, string,
	nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	c.reconciles++
	return c.proof, c.err
}
func (c *syntheticOnboardingFriendClient) ApproveOnboardingFriend(ctx context.Context, _ string,
	_ nativeauth.OnboardingFriendEvidence, _ time.Time, fence nativeauth.OnboardingFriendFence) error {
	c.approvals++
	return fence(ctx, c.proof, func(context.Context) error {
		c.submissions++
		return c.err
	})
}
func (*syntheticOnboardingFriendClient) Close() {}

func onboardingFriendFixture() (OnboardingFriendApproval, SettingSnapshot, *syntheticOnboardingFriendClient) {
	req := OnboardingFriendApproval{
		OnboardingFriendRequest: OnboardingFriendRequest{OnboardingNativeScope: OnboardingNativeScope{
			Platform: "wsl", AccountID: "synthetic-account", AccountEpoch: 3, AccountVersion: 4},
			ChallengeText: "proof-synthetic-private"},
		OperationID: "synthetic-operation", ApplicationID: "11", SenderID: "8", RecipientID: "7",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	base := SettingSnapshot{Platform: req.Platform, AccountEpoch: req.AccountEpoch, AccountVersion: req.AccountVersion,
		SelfID: "7", SampledAt: time.Now().UTC(), DeploymentVersion: 5}
	return req, base, &syntheticOnboardingFriendClient{proof: req.expected()}
}

func TestOnboardingFriendGatewayFreshEvidenceAndReadOnlyReconciliation(t *testing.T) {
	req, base, client := onboardingFriendFixture()
	checks := 0
	check := func(ctx context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
		checks++
		if scope != req.OnboardingNativeScope || !scope.matches(snapshot) || snapshot.SampledAt.IsZero() {
			t.Fatal("evidence escaped account epoch/version/platform")
		}
		return ctx.Err()
	}
	proof, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, nil, client, base, check)
	if err != nil || proof.State != "pending" || proof.RecipientID != base.SelfID ||
		proof.ExpiresAt.Before(time.Now().Add(9*time.Minute)) || client.reads != 1 || checks != 1 {
		t.Fatal("bounded fresh preparation failed", err)
	}
	client.proof.State = "approved"
	// Unknown operations remain queryable after the local submission deadline.
	req.ExpiresAt = time.Now().Add(-time.Minute)
	result, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, &req, client, base, check)
	if err != nil || result.State != "approved" || !result.ExpiresAt.Equal(req.ExpiresAt) ||
		client.reconciles != 1 || client.approvals != 0 || client.submissions != 0 {
		t.Fatal("reconciliation performed a native approval or renewed expiry", err)
	}
}

func TestOnboardingFriendGatewayRejectsLateAndCrossScopeReads(t *testing.T) {
	for _, mode := range []string{"epoch", "version", "platform", "recipient", "self_sender", "processed", "stale_session", "unknown_read"} {
		t.Run(mode, func(t *testing.T) {
			req, base, client := onboardingFriendFixture()
			switch mode {
			case "epoch":
				base.AccountEpoch++
			case "version":
				base.AccountVersion++
			case "platform":
				base.Platform = "lajiaoliao"
			case "recipient":
				client.proof.RecipientID = "9"
			case "self_sender":
				client.proof.SenderID = base.SelfID
			case "processed":
				client.proof.State = "approved"
			case "unknown_read":
				client.err = errors.New("synthetic-upstream-failed")
			}
			check := func(context.Context, OnboardingNativeScope, SettingSnapshot) error {
				if mode == "stale_session" {
					return ErrMemberScope
				}
				return nil
			}
			proof, err := readOnboardingFriend(context.Background(), req.OnboardingFriendRequest, nil, client, base, check)
			if err == nil || proof != (OnboardingFriendSnapshot{}) || client.submissions != 0 {
				t.Fatal("unverified or late evidence escaped")
			}
			if (mode == "epoch" || mode == "version" || mode == "platform") && client.reads != 0 {
				t.Fatal("mismatched scope reached upstream")
			}
		})
	}
}

func TestOnboardingFriendGatewayFinalFenceCancellationRevocationAndNoRetry(t *testing.T) {
	for _, mode := range []string{"approved", "revoked_before_fence", "revoked_in_fence", "changed",
		"expired", "expired_in_fence", "cancelled", "unknown", "duplicate_submit"} {
		t.Run(mode, func(t *testing.T) {
			req, base, client := onboardingFriendFixture()
			changed := false
			fences := 0
			if mode == "changed" {
				client.proof.ApplicationID = "12"
			}
			if mode == "expired" {
				req.ExpiresAt = time.Now().Add(-time.Second)
			}
			if mode == "expired_in_fence" {
				req.ExpiresAt = time.Now().Add(30 * time.Millisecond)
			}
			if mode == "unknown" {
				client.err = &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
			}
			check := func(ctx context.Context, scope OnboardingNativeScope, snapshot SettingSnapshot) error {
				if mode == "revoked_before_fence" || changed {
					return ErrMemberScope
				}
				return ctx.Err()
			}
			err := approveOnboardingFriend(context.Background(), req, client, base, check,
				func(work context.Context, proof OnboardingFriendSnapshot, send func(context.Context) error) error {
					fences++
					if proof.OnboardingFriendEvidence != req.expected() || !proof.ExpiresAt.Equal(req.ExpiresAt) {
						t.Fatal("original proof changed before human final fence")
					}
					if mode == "revoked_in_fence" {
						changed = true
					}
					if mode == "expired_in_fence" {
						timer := time.NewTimer(time.Until(req.ExpiresAt) + time.Millisecond)
						<-timer.C
					}
					if mode == "cancelled" {
						cancelled, cancel := context.WithCancel(work)
						cancel()
						return send(cancelled)
					}
					err := send(work)
					if mode == "duplicate_submit" {
						return send(work)
					}
					return err
				})
			wantWrites := 0
			if mode == "approved" || mode == "unknown" || mode == "duplicate_submit" {
				wantWrites = 1
			}
			if client.submissions != wantWrites || client.approvals > 1 || (mode == "approved") != (err == nil) {
				t.Fatal("final fence bypass, false success or retry", err, client.submissions)
			}
			if (mode == "changed" || mode == "revoked_before_fence" || mode == "expired") && fences != 0 {
				t.Fatal("unverified evidence reached control fence")
			}
		})
	}
}

func TestOnboardingFriendGatewayConcurrentSubmitIsSingleUse(t *testing.T) {
	req, base, client := onboardingFriendFixture()
	check := func(context.Context, OnboardingNativeScope, SettingSnapshot) error { return nil }
	err := approveOnboardingFriend(context.Background(), req, client, base, check,
		func(work context.Context, _ OnboardingFriendSnapshot, send func(context.Context) error) error {
			var success atomic.Int32
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					if send(work) == nil {
						success.Add(1)
					}
				})
			}
			wg.Wait()
			if success.Load() != 1 {
				t.Fatal("concurrent fence submitted more than once", success.Load())
			}
			return nil
		})
	if err != nil || client.submissions != 1 {
		t.Fatal("single-use native submission failed", err)
	}
}

func TestOnboardingFriendIntervalPreservesAccountCooldownAndCancellation(t *testing.T) {
	reads := NewReadScheduler(context.Background())
	defer reads.Close()
	start := time.Now()
	calls := 0
	_, err := reads.Value(context.Background(), "synthetic-account", "", ReadRefresh, func(work context.Context) (any, error) {
		return withOnboardingInterval(work, "synthetic-account", reads, func(context.Context) (any, error) {
			calls++
			return nil, errors.New("synthetic-proof-not-found")
		})
	})
	if err == nil || calls != 1 {
		t.Fatal("missing proof was retried or silently succeeded")
	}
	snapshot := reads.Snapshot("synthetic-account")
	if snapshot.CooldownUntil == nil || snapshot.CooldownUntil.Before(start.Add(5*time.Second)) {
		t.Fatal("proof attempt lost minimum five-second account interval")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = reads.Value(ctx, "synthetic-account", "", ReadRefresh, func(context.Context) (any, error) {
		calls++
		return nil, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatal("cancelled proof escaped account cooldown", err)
	}
}

func TestOnboardingRobotUsesPublicAddingIdentityWithoutUIDFallback(t *testing.T) {
	for _, tc := range []struct {
		platform, self, account string
		available               bool
	}{
		{"wsl", "7", "456", true}, {"wsl", "7", "", false}, {"wsl", "7", "00456", false},
		{"wsl", "7", "2147483648", false}, {"lajiaoliao", "self-string", "self-string", true},
		{"lajiaoliao", "self-string", "another-string", false},
	} {
		got, err := onboardingRobotProjection(SettingSnapshot{Platform: tc.platform, SelfID: tc.self}, tc.account, "authenticated nickname")
		if err != nil || got.AddAvailable != tc.available || got.Name != "authenticated nickname" ||
			tc.available && got.AddAccount != tc.account ||
			!tc.available && (got.AddAccount != "" || got.AddReason == "") {
			t.Fatal("public adding identity used fallback or lost availability", err)
		}
	}
	if _, err := onboardingRobotProjection(SettingSnapshot{Platform: "wsl", SelfID: "7"}, "456", "invalid\nnickname"); err == nil {
		t.Fatal("unverified nickname admitted to synchronization")
	}
}

func TestOnboardingFriendRequestsNeverSerializeOrLogChallenge(t *testing.T) {
	req, _, _ := onboardingFriendFixture()
	for _, value := range []any{req.OnboardingFriendRequest, req} {
		raw, _ := json.Marshal(value)
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("synthetic", "request", value)
		for _, text := range []string{string(raw), fmt.Sprintf("%v %+v %#v", value, value, value), log.String()} {
			if strings.Contains(text, req.ChallengeText) || strings.Contains(text, "ChallengeText") {
				t.Fatal("private challenge escaped JSON or logging")
			}
		}
	}
}

func TestOnboardingFriendGatewayRejectIntentCannotBeForgedByJSON(t *testing.T) {
	for _, raw := range []string{`{"RejectExpired":true}`, `{"reject_expired":true}`, `{"Contact":{"RejectExpired":true}}`,
		`{"RequestedAt":"2026-11-08T12:00:00Z"}`} {
		var req OnboardingFriendApproval
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			t.Fatal("synthetic request failed to decode", err)
		}
		if req.RejectExpired || req.Contact != nil || !req.RequestedAt.IsZero() {
			t.Fatal("public JSON manufactured sealed rejection intent or evidence")
		}
	}
}

func TestOnboardingFriendGatewayCancelledMutationDoesNotEnterAccountQueue(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, reject := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reject=%t", platform, reject), func(t *testing.T) {
				req, _, _ := onboardingExpiredContactFixture(platform)
				if !reject {
					approval, _, _ := onboardingContactFixture(platform)
					req = approval
				}
				reads := NewReadScheduler(context.Background())
				defer reads.Close()
				manager := &ReceiverManager{reads: reads}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				fences := 0
				fence := func(work context.Context, _ OnboardingFriendSnapshot, send func(context.Context) error) error {
					fences++
					return send(work)
				}
				action := manager.ApproveOnboardingFriend
				if reject {
					action = manager.RejectOnboardingFriend
				}
				err := action(ctx, req, fence)
				if !errors.Is(err, context.Canceled) || fences != 0 || reads.Snapshot(req.AccountID).Requests60s != 0 {
					t.Fatal("cancelled action reached the account queue, database or native transport", err)
				}
			})
		}
	}
}

func TestOnboardingFriendIntervalRejectionAndApprovalShareAccountCooldown(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			req, base, client := onboardingExpiredContactFixture(platform)
			reads := NewReadScheduler(context.Background())
			defer reads.Close()
			start := time.Now()
			check := func(ctx context.Context, _ OnboardingNativeScope, _ SettingSnapshot) error { return ctx.Err() }
			fence := func(work context.Context, _ OnboardingFriendSnapshot, send func(context.Context) error) error {
				return send(work)
			}
			_, err := reads.Value(context.Background(), req.AccountID, "", ReadRefresh, func(work context.Context) (any, error) {
				return withOnboardingInterval(work, req.AccountID, reads, func(work context.Context) (any, error) {
					return nil, rejectOnboardingFriend(work, req, client, base, check, fence)
				})
			})
			cooldown := reads.Snapshot(req.AccountID).CooldownUntil
			if err != nil || client.submissions != 1 || client.contactRejections != 1 ||
				cooldown == nil || cooldown.Before(start.Add(5*time.Second)) {
				t.Fatal("rejection lost the shared five-second account cooldown", err)
			}
			approval, approvalBase, approvalClient := onboardingContactFixture(platform)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			_, err = reads.Value(ctx, approval.AccountID, "", ReadRefresh, func(work context.Context) (any, error) {
				return withOnboardingInterval(work, approval.AccountID, reads, func(work context.Context) (any, error) {
					return nil, approveOnboardingFriend(work, approval, approvalClient, approvalBase, check, fence)
				})
			})
			if !errors.Is(err, context.DeadlineExceeded) || approvalClient.contactApprovals != 0 ||
				approvalClient.submissions != 0 || client.submissions != 1 {
				t.Fatal("approval escaped the rejection cooldown or repeated the rejection", err)
			}
		})
	}
}
