package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"wangshangliao/internal/control"
)

func TestOnboardingControlledLookupPinsConfigurationWithoutBlockingRequestIDs(t *testing.T) {
	f := onboardingClaimantFixtureWithPriorProof(t, "wsl", "owner", false)
	installOnboardingControlledQueue(t, f)
	f.openWindow(t)
	var allocated []uint64
	checkPinned := func(version int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
		defer cancel()
		ids := f.app.nativeRequestIDs(version)
		for range 2 {
			id, err := ids(ctx, uint64(time.Now().UnixNano()))
			if err != nil {
				t.Error("authorized lookup blocked its own durable request ID", err)
				return
			}
			allocated = append(allocated, id)
		}
		writer, err := f.db.Platform.Begin(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		defer writer.Rollback(context.Background())
		var acquired bool
		if err = writer.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(798426)`).Scan(&acquired); err != nil || acquired {
			t.Error("configuration could change during the authorized lookup", acquired, err)
		}
	}
	f.outbound.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) {
		checkPinned(e.DeploymentVersion)
	}
	prepared := f.prepare(t)
	if len(allocated) != 2 || allocated[0] >= allocated[1] || prepared.FriendOutbound == nil ||
		prepared.FriendOutbound.Status != "prepared" || f.outbound.outboundEffects.Load() != 0 {
		t.Fatal("lookup lost its unique IDs or submitted a mutation", allocated, prepared)
	}
	f.assertUsage(t, 0)
	f.outbound.outboundEvidence = nil
	f.outbound.onOutboundEffect = func(ctx context.Context, in control.GroupBindingOutboundRequest) error {
		var version int64
		if err := f.db.Platform.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton`).Scan(&version); err != nil {
			return err
		}
		checkPinned(version)
		return nil
	}
	sent := f.send(t, "deployment-lock-send-once")
	if len(allocated) != 4 || allocated[1] >= allocated[2] || allocated[2] >= allocated[3] ||
		sent.FriendOutbound.Status != "submitted" || f.outbound.outboundEffects.Load() != 1 {
		t.Fatal("submission lost its durable request IDs or exact-once fence", allocated, sent)
	}
	f.assertUsage(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	writer, err := f.db.Platform.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())
	var acquired bool
	if err = writer.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(798426)`).Scan(&acquired); err != nil || !acquired {
		t.Fatal("completed lookup retained the configuration lock", acquired, err)
	}
}

func TestOnboardingControlledLookupRejectsActiveConfigurationWriter(t *testing.T) {
	f := onboardingClaimantFixtureWithPriorProof(t, "wsl", "owner", false)
	f.openWindow(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	writer, err := f.db.Platform.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())
	if _, err = writer.Exec(ctx, `SELECT pg_advisory_xact_lock(798426)`); err != nil {
		t.Fatal(err)
	}
	status, _, code := f.admin.requestWithCaptcha(http.MethodPost,
		"/test-tools/onboarding-friend/challenges/"+f.challenge.ID+"/friend-target",
		map[string]any{
			"version": f.challenge.Version, "test_window_id": f.window.ID, "account": f.outbound.target.AddAccount,
		}, http.Header{"Idempotency-Key": {"configuration-writer-active"}})
	if status != http.StatusConflict || code != "group_binding_evidence_changed" ||
		f.outbound.outboundLookups.Load() != 0 || f.outbound.outboundEffects.Load() != 0 {
		t.Fatal("configuration change raced a native lookup", status, code)
	}
	f.assertUsage(t, 0)
}
