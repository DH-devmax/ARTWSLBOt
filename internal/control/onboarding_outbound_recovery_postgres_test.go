package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

type recoveryOutboundNative struct {
	GroupBindingNativePort
	f             *chiliNoticeProofFixture
	before        func()
	writes, reads int
	unknown       bool
}

func (n *recoveryOutboundNative) BindingRoles(ctx context.Context, r GroupBindingNativeScope) (GroupBindingRoleEvidence, error) {
	read, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return n.GroupBindingNativePort.BindingRoles(read, r)
}

func (n *recoveryOutboundNative) evidence(ctx context.Context, r GroupBindingNativeScope) (GroupBindingOutboundEvidence, error) {
	e := GroupBindingOutboundEvidence{SelfID: n.f.sender, State: "available",
		Target: GroupBindingFriendTarget{NativeID: n.f.recipient, AddAccount: "synthetic-recovery-receiver", Name: "Synthetic receiver"}}
	e.Platform, e.AccountID, e.AccountEpoch, e.AccountVersion =
		r.Platform, r.AccountID, r.AccountEpoch, r.AccountVersion
	e.ObservedAt = time.Now()
	var credential []byte
	err := n.f.f.db.Platform.QueryRow(ctx, `SELECT a.credential_sealed,r.owner,r.revision
		FROM accounts a JOIN account_receivers r ON r.account_id=a.id WHERE a.id=$1`, r.AccountID).
		Scan(&credential, &e.ReceiverOwner, &e.ReceiverRevision)
	e.CredentialDigest = sha256.Sum256(credential)
	return e, err
}

func (n *recoveryOutboundNative) LookupBindingFriend(ctx context.Context, r GroupBindingOutboundLookup,
	fence GroupBindingOutboundLookupFence) (GroupBindingOutboundEvidence, error) {
	e, err := n.evidence(ctx, r.GroupBindingNativeScope)
	if err == nil {
		err = fence(ctx, func(context.Context) error { n.reads++; return nil })
	}
	return e, err
}

func (n *recoveryOutboundNative) SendBindingFriend(ctx context.Context, r GroupBindingOutboundRequest,
	fence GroupBindingOutboundFence) error {
	if n.before != nil {
		n.before()
	}
	e, err := n.evidence(ctx, r.GroupBindingNativeScope)
	if err != nil {
		return err
	}
	return fence(ctx, e, func(context.Context) error {
		n.writes++
		if n.unknown {
			return errors.New("synthetic uncertain transport")
		}
		return nil
	})
}

func (n *recoveryOutboundNative) ReconcileBindingOutbound(ctx context.Context,
	r GroupBindingOutboundRequest) (GroupBindingOutboundEvidence, error) {
	e, err := n.evidence(ctx, r.GroupBindingNativeScope)
	e.State = "pending"
	n.reads++
	return e, err
}

func recoveryOutboundFixture(t *testing.T) (*chiliNoticeProofFixture, *recoveryOutboundNative, string,
	OnboardingControlledWindow, GroupBindingChallenge) {
	t.Helper()
	f := newChiliNoticeProofFixture(t)
	old, _, _ := f.restartChallenge(t)
	n := &recoveryOutboundNative{GroupBindingNativePort: f.bindings.Native, f: f}
	f.bindings.Native = n
	_, err := f.bindings.SaveOutboundFriendGate(f.f.ctx, f.ref, f.claimant,
		GroupBindingFriendGateInput{State: "open", Evidence: []string{"docs/controlled-onboarding.md"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.bindings.Read(f.f.ctx, f.ref, f.challenge)
	if err != nil {
		t.Fatal(err)
	}
	w, err := f.bindings.CreateOnboardingControlledWindow(f.f.ctx, f.ref, OnboardingControlledWindowInput{
		ChallengeID: c.ID, Version: c.Version, GroupID: f.scope.GroupID, Account: "synthetic-recovery-receiver",
		TargetID: f.recipient, Seconds: 300, Budget: 1, AsClaimant: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err = f.bindings.PrepareControlledOutboundFriend(f.f.ctx, f.ref, c.ID,
		OnboardingControlledTargetInput{Window: w.ID, Version: c.Version,
			Account: "synthetic-recovery-receiver", Key: "synthetic-recovery-lookup"})
	if err != nil || c.FriendOutbound == nil || c.FriendOutbound.Status != "prepared" || n.reads != 1 {
		t.Fatal("authorized lookup did not prepare a distinct friend request", c, err)
	}
	return f, n, old, w, c
}

func TestPostgresChiliRetiredProofAllowsOneIndependentFriendRequest(t *testing.T) {
	f, n, old, w, c := recoveryOutboundFixture(t)
	immutable := f.originalAttempt(t, old)
	in := OnboardingControlledConfirmation{Window: w.ID, Version: c.Version, Key: "synthetic-recovery-request"}
	sent, err := f.bindings.SendControlledOutboundFriend(f.f.ctx, f.ref, c.ID, in)
	if err != nil || sent.FriendOutbound == nil || sent.FriendOutbound.Status != "submitted" || n.writes != 1 {
		t.Fatal("independent request did not pass its persisted authorization", sent, err)
	}
	again, err := f.bindings.SendControlledOutboundFriend(f.f.ctx, f.ref, c.ID, in)
	if err != nil || again.FriendOutbound == nil || again.FriendOutbound.ID != sent.FriendOutbound.ID || n.writes != 1 {
		t.Fatal("repeat confirmation created another request", again, err)
	}
	if sent.IdentityProven || sent.CanConfirm || sent.CanAcceptInvitation ||
		f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND
			(identity_event_id IS NOT NULL OR identity_friend_proof_id IS NOT NULL OR proof_event_id IS NOT NULL)`, c.ID) != 0 {
		t.Fatal("friend request was counted as identity or group proof")
	}
	assertControlledUsage(t, f.f, f.scopeID, 1, 0, 0)
	tx, err := f.f.db.Platform.Begin(f.f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = bindingFriendWriteAvailable(f.f.ctx, tx, f.claimant, "", nil)
	tx.Rollback(f.f.ctx)
	if err == nil {
		t.Fatal("ordinary friend writes inherited the recovery exception")
	}
	if !bytes.Equal(immutable, f.originalAttempt(t, old)) {
		t.Fatal("new request modified the retained unknown proof")
	}
}

func TestPostgresChiliIndependentFriendRequestRechecksRetiredProof(t *testing.T) {
	for _, change := range []string{"old-running", "second-unknown", "old-payload", "old-actor",
		"old-live", "old-route", "closed-scope", "changed-scope", "closed-gate", "cancelled-window"} {
		for _, afterPreparation := range []bool{false, true} {
			t.Run(change+map[bool]string{false: "/preparing", true: "/final"}[afterPreparation], func(t *testing.T) {
				f, n, old, w, c := recoveryOutboundFixture(t)
				mutate := func() {
					switch change {
					case "old-running":
						f.f.exec(`UPDATE operations SET status='running' WHERE id=$1`, old)
					case "second-unknown":
						f.f.exec(`INSERT INTO operations(id,tenant_id,actor_id,account_id,group_id,test_window_id,
							idempotency_key,request_hash,kind,payload_sealed,account_epoch,binding_epoch,status,expires_at,
							result_code,conversation_key) SELECT $1,tenant_id,actor_id,account_id,group_id,test_window_id,
							'synthetic-independent-extra',request_hash,kind,payload_sealed,account_epoch,binding_epoch,
							status,expires_at,result_code,conversation_key FROM operations WHERE id=$2`, NewID(), old)
					case "old-payload":
						f.f.exec(`UPDATE operations SET payload_sealed='synthetic-invalid'::bytea WHERE id=$1`, old)
					case "old-actor":
						f.f.exec(`UPDATE operations SET actor_id=NULL WHERE id=$1`, old)
					case "old-live":
						f.f.exec(`UPDATE operations SET expires_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, old)
					case "old-route":
						f.f.exec(`UPDATE lajiaoliao_media_attempts SET native_sender='unrelated' WHERE operation_id=$1`, old)
					case "closed-scope":
						f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
					case "changed-scope":
						f.f.exec(`UPDATE onboarding_test_scopes SET version=version+1 WHERE id=$1`, f.scopeID)
					case "closed-gate":
						f.f.exec(`UPDATE group_binding_outbound_gates SET state='closed',version=version+1 WHERE account_id=$1`, f.claimant)
					case "cancelled-window":
						f.f.exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, w.ID)
					}
				}
				if afterPreparation {
					n.before = mutate
				} else {
					mutate()
				}
				result, err := f.bindings.SendControlledOutboundFriend(f.f.ctx, f.ref, c.ID,
					OnboardingControlledConfirmation{Window: w.ID, Version: c.Version, Key: "synthetic-recovery-denied"})
				if n.writes != 0 || err == nil && f.f.count(`SELECT count(*) FROM group_binding_outbound_attempts
					WHERE challenge_id=$1 AND status='failed'`, c.ID) != 1 {
					t.Fatal("changed authorization or predecessor reached native mutation", result, err, n.writes)
				}
			})
		}
	}
}

func TestPostgresChiliIndependentUnknownFriendRequestIsNotRetried(t *testing.T) {
	f, n, old, w, c := recoveryOutboundFixture(t)
	immutable := f.originalAttempt(t, old)
	n.unknown = true
	in := OnboardingControlledConfirmation{Window: w.ID, Version: c.Version, Key: "synthetic-recovery-unknown"}
	result, err := f.bindings.SendControlledOutboundFriend(f.f.ctx, f.ref, c.ID, in)
	if err != nil || result.FriendOutbound == nil || result.FriendOutbound.Status != "unknown" || n.writes != 1 {
		t.Fatal("unknown request was not retained", result, err)
	}
	result, err = f.bindings.SendControlledOutboundFriend(f.f.ctx, f.ref, c.ID, in)
	if err != nil || result.FriendOutbound == nil || result.FriendOutbound.Status != "unknown" || n.writes != 1 {
		t.Fatal("unknown request was retried", result, err)
	}
	if !bytes.Equal(immutable, f.originalAttempt(t, old)) {
		t.Fatal("unknown friend request changed the predecessor")
	}
}
