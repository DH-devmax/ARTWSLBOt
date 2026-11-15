package httpapi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
)

type onboardingControlledQueueFixture struct {
	*onboardingClaimantNativeFixture
	scheduler *gateway.ReadScheduler
}

var _ control.GroupBindingOutboundNativePort = (*onboardingControlledQueueFixture)(nil)

func (n *onboardingControlledQueueFixture) LookupBindingFriend(ctx context.Context, in control.GroupBindingOutboundLookup,
	fence control.GroupBindingOutboundLookupFence) (control.GroupBindingOutboundEvidence, error) {
	value, err := n.scheduler.Value(ctx, in.AccountID, "", gateway.ReadRefresh,
		func(work context.Context) (any, error) {
			return n.onboardingClaimantNativeFixture.LookupBindingFriend(work, in, fence)
		})
	if err != nil {
		return control.GroupBindingOutboundEvidence{}, err
	}
	out, ok := value.(control.GroupBindingOutboundEvidence)
	if !ok {
		return control.GroupBindingOutboundEvidence{}, errors.New("synthetic queued lookup returned invalid evidence")
	}
	return out, nil
}

func (n *onboardingControlledQueueFixture) SendBindingFriend(ctx context.Context, in control.GroupBindingOutboundRequest,
	fence control.GroupBindingOutboundFence) error {
	return n.scheduler.Do(ctx, in.AccountID, "", gateway.ReadRefresh,
		func(work context.Context) error {
			return n.onboardingClaimantNativeFixture.SendBindingFriend(work, in, fence)
		})
}

func installOnboardingControlledQueue(t *testing.T, f *onboardingClaimantFixture) *gateway.ReadScheduler {
	t.Helper()
	// Use the manager's background parent, not the caller's private control context.
	scheduler := gateway.NewReadScheduler(context.Background())
	t.Cleanup(scheduler.Close)
	f.app.GroupBinding.Native = &onboardingControlledQueueFixture{
		onboardingClaimantNativeFixture: f.outbound, scheduler: scheduler,
	}
	return scheduler
}

func holdOnboardingControlledQueue(t *testing.T, scheduler *gateway.ReadScheduler, account string) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- scheduler.Do(ctx, account, "controlled-onboarding-blocker", gateway.ReadRenewal,
			func(work context.Context) error {
				close(started)
				select {
				case <-release:
					return nil
				case <-work.Done():
					return work.Err()
				}
			})
	}()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error("synthetic queue blocker failed", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("synthetic queue blocker did not stop")
		}
	})
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("synthetic queue blocker did not start")
	}
	return unblock
}

func waitOnboardingControlledQueueDepth(t *testing.T, scheduler *gateway.ReadScheduler, account string, depth int) {
	t.Helper()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if scheduler.Snapshot(account).QueueDepth == depth {
			return
		}
		select {
		case <-timeout.C:
			t.Fatal("controlled onboarding did not reach the expected queue depth", depth, scheduler.Snapshot(account))
		case <-tick.C:
		}
	}
}

func TestOnboardingControlledQueuePostgresLookupSendSingleEffect(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingClaimantFixture(t, platform, "owner")
			installOnboardingControlledQueue(t, f)
			f.openWindow(t)
			prepared := f.prepare(t)
			f.assertUnproven(t, prepared)
			f.assertUsage(t, 0)
			if prepared.FriendOutbound.Status != "prepared" || f.outbound.outboundLookups.Load() != 1 ||
				f.outbound.outboundSends.Load() != 0 || f.outbound.outboundEffects.Load() != 0 {
				t.Fatal("queued lookup lost its authorization or dispatched a mutation", prepared)
			}

			sent := f.send(t, "queued-claimant-confirm")
			if sent.FriendOutbound == nil || sent.FriendOutbound.ID != prepared.FriendOutbound.ID ||
				sent.FriendOutbound.Status != "submitted" || f.outbound.outboundLookups.Load() != 1 ||
				f.outbound.outboundSends.Load() != 1 || f.outbound.outboundEffects.Load() != 1 {
				t.Fatal("queued send lost its authorization or repeated its effect", sent)
			}
			f.outbound.outboundMu.Lock()
			request := f.outbound.lastOutbound
			f.outbound.outboundMu.Unlock()
			if request.AccountID != f.claimant || request.SelfID != f.nativeClaimant ||
				request.Target.NativeID != f.outbound.target.NativeID {
				t.Fatal("queued send changed the selected applicant or receiving robot", request)
			}
			f.assertUnproven(t, sent)
			f.assertUsage(t, 1)

			before := outboundCalls(f.outbound.bindingOutboundNativeFixture)
			replay := f.send(t, "queued-claimant-confirm")
			if replay.FriendOutbound == nil || replay.FriendOutbound.ID != sent.FriendOutbound.ID ||
				replay.FriendOutbound.Status != "submitted" ||
				outboundCalls(f.outbound.bindingOutboundNativeFixture) != before {
				t.Fatal("duplicate queued confirmation repeated native work", replay)
			}
			f.assertUnproven(t, replay)
			f.assertUsage(t, 1)
		})
	}
}

func TestOnboardingControlledQueuePostgresInvalidatedBeforeLookup(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, change := range []string{"scope-close", "cancel"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				f := newOnboardingClaimantFixture(t, platform, "owner")
				scheduler := installOnboardingControlledQueue(t, f)
				f.openWindow(t)
				unblock := holdOnboardingControlledQueue(t, scheduler, f.claimant)
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				returned := false
				t.Cleanup(func() {
					cancel()
					unblock()
					if !returned {
						select {
						case <-done:
						case <-time.After(10 * time.Second):
							t.Error("queued controlled lookup did not stop")
						}
					}
				})
				go func() {
					_, err := f.app.GroupBinding.PrepareControlledOutboundFriend(ctx, f.ref, f.challenge.ID,
						control.OnboardingControlledTargetInput{Window: f.window.ID, Version: f.challenge.Version,
							Account: f.outbound.target.AddAccount, Key: "queued-claimant-prepare"})
					done <- err
				}()
				waitOnboardingControlledQueueDepth(t, scheduler, f.claimant, 1)
				if change == "scope-close" {
					closed, err := f.app.GroupBinding.CloseControlledScope(context.Background(), f.ref, f.scope.ID, f.scope.Version)
					if err != nil || closed.State != "closed" {
						t.Fatal("could not close the scope while its lookup was queued", closed, err)
					}
					unblock()
				} else {
					cancel()
				}
				select {
				case err := <-done:
					returned = true
					if err == nil || change == "cancel" && !errors.Is(err, context.Canceled) {
						t.Fatal("invalidated queued lookup did not reject its original request", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("invalidated queued lookup did not return")
				}
				unblock()
				waitOnboardingControlledQueueDepth(t, scheduler, f.claimant, 0)
				if f.outbound.outboundLookups.Load() != 0 || f.outbound.outboundSends.Load() != 0 ||
					f.outbound.outboundEffects.Load() != 0 {
					t.Fatal("closed or cancelled queued lookup reached an upstream read or mutation")
				}
				f.assertUsage(t, 0)
				f.assertUnproven(t, f.readChallenge(t, f.challenge))
			})
		}
	}
}
