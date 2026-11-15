package control

import (
	"bytes"
	"strings"
	"testing"

	"wangshangliao/internal/lajiaoliao"
)

func (f *chiliNoticeProofFixture) restartChallenge(t *testing.T) (string, string, string) {
	t.Helper()
	op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
	if err != nil {
		t.Fatal(err)
	}
	port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID, unknown: true}
	if result, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port); err != nil ||
		result.Status != "unknown" || port.writes != 1 {
		t.Fatal("fixture did not retain its original unknown write", result, err)
	}
	oldScope, oldChallenge := f.scopeID, f.challenge
	f.f.exec(`UPDATE operations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, op.ID)
	f.f.exec(`UPDATE test_windows SET cancelled=true,created_at=clock_timestamp()-interval '10 minutes',
		expires_at=clock_timestamp()-interval '6 minutes' WHERE id=$1`, f.window.ID)
	f.f.exec(`UPDATE group_binding_challenges SET state='cancelled',version=version+1 WHERE id=$1`, oldChallenge)
	f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, oldScope)
	f.scopeID, f.challenge = NewID(), NewID()
	f.privateText, f.groupText = "群接入 "+strings.Repeat("D", 43), "群绑定 "+strings.Repeat("C", 43)
	f.f.exec(`INSERT INTO onboarding_test_scopes(id,actor_id,tenant_id,platform,account_id,claimant_account_id,group_id,
		actor_version,account_epoch,account_version,claimant_epoch,claimant_version,claimant_native_id,
		claimant_role,robot_role,owner_identity_hash,create_key,request_hash,expires_at)
		SELECT $1,actor_id,tenant_id,platform,account_id,claimant_account_id,group_id,actor_version,account_epoch,
		account_version,claimant_epoch,claimant_version,claimant_native_id,claimant_role,robot_role,
		owner_identity_hash,$1::uuid::text,request_hash,clock_timestamp()+interval '10 minutes'
		FROM onboarding_test_scopes WHERE id=$2`, f.scopeID, oldScope)
	f.f.exec(`INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,applicant_mode,platform,account_id,
		account_epoch,account_version,applicant_version,token_hash,private_token_hash,create_key,request_hash,state,
		group_id,binding_version,controlled_scope_id)
		SELECT $1,s.tenant_id,s.actor_id,'operator_demo',s.platform,s.account_id,s.account_epoch,s.account_version,
		s.actor_version,$3,$4,$1::uuid::text,$3,'waiting',s.group_id,b.version,s.id
		FROM onboarding_test_scopes s JOIN group_bindings b ON b.group_id=s.group_id WHERE s.id=$2`,
		f.challenge, f.scopeID, bindingTokenDigest(strings.Repeat("C", 43)), bindingTokenDigest(strings.Repeat("D", 43)))
	f.window, err = f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge,
		f.input(t, "private", "synthetic-recovery-private-window"))
	if err != nil {
		t.Fatal(err)
	}
	f.req.TestWindowID, f.req.Text = f.window.ID, f.privateText
	f.req.BindingEpoch = f.window.BindingEpoch
	f.req.IdempotencyKey = "synthetic-recovery-private-send"
	return op.ID, oldScope, oldChallenge
}

func (f *chiliNoticeProofFixture) originalAttempt(t *testing.T, id string) []byte {
	t.Helper()
	var out []byte
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT jsonb_build_object('operation',to_jsonb(o),
		'attempt',to_jsonb(a))::text FROM operations o JOIN lajiaoliao_media_attempts a ON a.operation_id=o.id
		WHERE o.id=$1`, id).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *chiliNoticeProofFixture) completeIndependentProof(t *testing.T) (*chiliNoticeProofPort, *chiliNoticeProofPort) {
	t.Helper()
	op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
	if err != nil || op.Status != "queued" {
		t.Fatal("fresh authorized challenge was blocked by its expired predecessor", op, err)
	}
	port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID}
	if result, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port); err != nil ||
		result.Status != "succeeded" || port.writes != 1 {
		t.Fatal("new challenge failed its normal submission fences", result, err)
	}
	f.provePrivate(t)
	window, err := f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge,
		f.input(t, "group", "synthetic-recovery-group-window"))
	if err != nil {
		t.Fatal(err)
	}
	group := f.req
	group.TestWindowID, group.Text = window.ID, f.groupText
	group.BindingEpoch, group.IdempotencyKey = window.BindingEpoch, "synthetic-recovery-group-send"
	group.PrivateConversation, group.PrivateEventID = "", 0
	op, err = f.text.StageChiliMessage(f.f.ctx, f.ref, group)
	if err != nil {
		t.Fatal("fresh group proof was blocked by the retired private proof", err)
	}
	groupPort := &chiliNoticeProofPort{t: t}
	if result, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, groupPort); err != nil ||
		result.Status != "succeeded" || groupPort.writes != 1 {
		t.Fatal("new group proof failed its normal submission fences", result, err)
	}
	f.ingress(t, "group", f.nativeID, f.groupText)
	return port, groupPort
}

func (f *chiliNoticeProofFixture) confirmIndependentProof(t *testing.T) (GroupBindingChallenge, error) {
	t.Helper()
	current, err := f.bindings.Read(f.f.ctx, f.ref, f.challenge)
	if err != nil {
		return current, err
	}
	current, err = f.bindings.Confirm(f.f.ctx, f.ref, f.challenge,
		GroupBindingConfirmation{Version: current.Version, Key: "synthetic-recovery-confirm"})
	if err != nil || current.State != "review" {
		return current, err
	}
	return f.bindings.Review(f.f.ctx, f.ref, f.challenge,
		GroupBindingReview{Version: current.Version, Approve: true})
}

func TestPostgresChiliExpiredProofAllowsIndependentChallengeOnly(t *testing.T) {
	f := newChiliNoticeProofFixture(t)
	old, _, _ := f.restartChallenge(t)
	immutable := f.originalAttempt(t, old)
	port, groupPort := f.completeIndependentProof(t)
	tx, err := f.f.db.Platform.Begin(f.f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = bindingOperationsResolved(f.f.ctx, tx, f.scope.GroupID)
	tx.Rollback(f.f.ctx)
	capabilityError(t, err, "group_binding_operation_unresolved")
	bound, err := f.confirmIndependentProof(t)
	if err != nil || bound.State != "bound" || bound.ProofKind != "controlled_test" {
		t.Fatal("independent private/group proof could not finish normal confirmation and review", bound, err)
	}
	if f.f.count(`SELECT count(*) FROM group_bindings WHERE group_id=$1 AND active
		AND NOT management_enabled AND NOT ai_enabled`, f.scope.GroupID) != 1 {
		t.Fatal("recovered binding enabled background business")
	}
	if !bytes.Equal(immutable, f.originalAttempt(t, old)) {
		t.Fatal("recovery reclassified, replayed or edited the old unknown submission")
	}
	evidence := &chiliNoticeProofEvidence{t: t, want: lajiaoliao.MediaEvidenceQuery{
		Account: f.claimant, Sender: f.sender, Group: f.nativeID, Peer: f.recipient,
		Epoch: f.req.AccountEpoch, CurrentEpoch: f.req.AccountEpoch}}
	if result, err := f.text.ReconcileChiliMedia(f.f.ctx, f.ref, old, evidence); err != nil ||
		result.Status != "unknown" || evidence.reads != 1 || port.writes != 1 || groupPort.writes != 1 {
		t.Fatal("old proof lost its read-only reconciliation path", result, err)
	}
}

func TestPostgresChiliProofRecoveryBindingRejectsUnrelatedOrChangedWork(t *testing.T) {
	for _, change := range []string{"running", "second-unknown", "old-actor", "old-payload",
		"old-challenge-live", "missing-receipt", "missing-preparation", "closed-new-scope", "changed-private-source"} {
		t.Run(change, func(t *testing.T) {
			f := newChiliNoticeProofFixture(t)
			old, _, challenge := f.restartChallenge(t)
			f.completeIndependentProof(t)
			switch change {
			case "running":
				f.f.exec(`UPDATE operations SET status='running' WHERE id=$1`, old)
			case "second-unknown":
				f.f.exec(`INSERT INTO operations(id,tenant_id,actor_id,account_id,group_id,test_window_id,idempotency_key,
					request_hash,kind,payload_sealed,account_epoch,binding_epoch,status,expires_at,result_code,conversation_key)
					SELECT $1,tenant_id,actor_id,account_id,group_id,test_window_id,'synthetic-other-binding-work',
					request_hash,kind,payload_sealed,account_epoch,binding_epoch,status,expires_at,result_code,conversation_key
					FROM operations WHERE id=$2`, NewID(), old)
			case "old-actor":
				f.f.exec(`UPDATE operations SET actor_id=NULL WHERE id=$1`, old)
			case "old-payload":
				f.f.exec(`UPDATE operations SET payload_sealed='synthetic-unreadable'::bytea WHERE id=$1`, old)
			case "old-challenge-live":
				f.f.exec(`UPDATE group_binding_challenges SET state='bound',version=version+1 WHERE id=$1`, challenge)
			case "missing-receipt":
				f.f.exec(`UPDATE lajiaoliao_media_attempts SET receipt_sealed=NULL WHERE operation_id IN
					(SELECT id FROM operations WHERE status='succeeded' AND conversation_key='')`)
			case "missing-preparation":
				f.f.exec(`UPDATE audit_events SET resource_id='synthetic-unrelated-window'
					WHERE action='onboarding_controlled_proof_window_created' AND detail->>'challenge_id'=$1`, f.challenge)
			case "closed-new-scope":
				f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
			case "changed-private-source":
				f.f.exec(`UPDATE native_received_events SET payload_sealed='synthetic-unreadable'::bytea
					WHERE id=(SELECT identity_event_id FROM group_binding_challenges WHERE id=$1)`, f.challenge)
			}
			immutable := f.originalAttempt(t, old)
			result, err := f.confirmIndependentProof(t)
			if err == nil || result.State == "bound" {
				t.Fatal("binding accepted unrelated work or changed proof", result, err)
			}
			if !bytes.Equal(immutable, f.originalAttempt(t, old)) {
				t.Fatal("rejected binding modified the old submission")
			}
		})
	}
}

func TestPostgresChiliProofRecoveryRejectsUnrelatedOrLiveWork(t *testing.T) {
	for _, change := range []string{"old-challenge-not-retired", "old-window-live", "old-operation-live",
		"old-kind", "old-actor", "old-route", "old-payload", "missing-preparation", "running",
		"second-unknown", "same-text", "closed-new-scope"} {
		t.Run(change, func(t *testing.T) {
			f := newChiliNoticeProofFixture(t)
			old, _, challenge := f.restartChallenge(t)
			switch change {
			case "old-challenge-not-retired":
				f.f.exec(`UPDATE group_binding_challenges SET state='bound',version=version+1 WHERE id=$1`, challenge)
			case "old-window-live":
				f.f.exec(`UPDATE test_windows SET created_at=clock_timestamp(),
					expires_at=clock_timestamp()+interval '5 minutes' WHERE id=(SELECT test_window_id FROM operations WHERE id=$1)`, old)
			case "old-operation-live":
				f.f.exec(`UPDATE operations SET expires_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, old)
			case "old-kind":
				f.f.exec(`UPDATE operations SET kind='send_image' WHERE id=$1`, old)
			case "old-actor":
				f.f.exec(`UPDATE operations SET actor_id=NULL WHERE id=$1`, old)
			case "old-route":
				f.f.exec(`UPDATE lajiaoliao_media_attempts SET native_sender='another-identity' WHERE operation_id=$1`, old)
			case "old-payload":
				f.f.exec(`UPDATE operations SET payload_sealed='synthetic-unreadable'::bytea WHERE id=$1`, old)
			case "missing-preparation":
				f.f.exec(`UPDATE audit_events SET resource_id='unrelated-window'
					WHERE action='onboarding_controlled_proof_window_created'
					AND resource_id=(SELECT test_window_id::text FROM operations WHERE id=$1)`, old)
			case "running":
				f.f.exec(`UPDATE operations SET status='running' WHERE id=$1`, old)
			case "second-unknown":
				f.f.exec(`INSERT INTO operations(id,tenant_id,actor_id,account_id,group_id,test_window_id,idempotency_key,
					request_hash,kind,payload_sealed,account_epoch,binding_epoch,status,expires_at,result_code,conversation_key)
					SELECT $1,tenant_id,actor_id,account_id,group_id,test_window_id,'synthetic-second-unknown',
					request_hash,kind,payload_sealed,account_epoch,binding_epoch,status,expires_at,result_code,conversation_key
					FROM operations WHERE id=$2`, NewID(), old)
			case "same-text":
				f.req.Text = "群接入 " + strings.Repeat("B", 43)
			case "closed-new-scope":
				f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
			}
			if _, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req); err == nil {
				t.Fatal("recovery admitted an unrelated, repeated or still live operation")
			}
		})
	}
}

func TestPostgresChiliProofRecoveryRechecksBeforeDispatch(t *testing.T) {
	f := newChiliNoticeProofFixture(t)
	old, _, _ := f.restartChallenge(t)
	op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
	if err != nil {
		t.Fatal(err)
	}
	f.f.exec(`UPDATE operations SET status='running' WHERE id=$1`, old)
	port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID}
	if _, err = f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port); err == nil || port.writes != 0 {
		t.Fatal("dispatch did not recheck unresolved work after staging", err)
	}
}
