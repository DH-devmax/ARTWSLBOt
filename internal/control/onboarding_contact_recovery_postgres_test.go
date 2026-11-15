package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wangshangliao/internal/nativeauth"
)

// These synthetic tests inherit the serial wsl_test-only fixture. They neither
// contact a native account nor change or replay the retained proof operation.
type onboardingContactPG struct {
	*chiliNoticeProofFixture
	old     string
	request SettingRequest
}

func newOnboardingContactPG(t *testing.T) *onboardingContactPG {
	t.Helper()
	base := newChiliNoticeProofFixture(t)
	old, _, _ := base.restartChallenge(t)
	base.f.exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, base.window.ID)
	req := SettingRequest{
		Account: base.claimant, Workspace: "lajiaoliao", Key: NewID(), Epoch: 1, Version: 1,
		Digest: strings.Repeat("b", 64), TestWindowID: NewID(),
		Change: nativeauth.SettingChange{Kind: "friend_delete", Group: base.nativeID, Target: base.recipient},
	}
	epochs, _ := json.Marshal(map[string]int64{req.Account: req.Epoch})
	base.f.exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,
		account_epochs,expires_at,budget,admission_mode)
		VALUES($1,$2,$3,ARRAY[$4::uuid],ARRAY[$5],ARRAY['friend_delete'],$6,
		now()+interval '300 seconds',1,'native_debug')`,
		req.TestWindowID, base.actor.ID, base.scope.GroupID, req.Account, req.Change.Target, epochs)
	return &onboardingContactPG{chiliNoticeProofFixture: base, old: old, request: req}
}

func (f *onboardingContactPG) contactProof() SettingEvidence {
	proof := f.proof(f.request)
	proof.UID, proof.SocialAllowed = f.sender, true
	return proof
}

func (f *onboardingContactPG) requireOldUnchanged(t *testing.T, original []byte) {
	t.Helper()
	if !bytes.Equal(original, f.originalAttempt(t, f.old)) {
		t.Fatal("contact recovery changed the old operation or its sealed attempt")
	}
}

func (f *onboardingContactPG) replaceWaitingChallenge(t *testing.T) {
	t.Helper()
	f.f.exec(`UPDATE group_binding_challenges SET state='cancelled',version=version+1 WHERE id=$1`, f.challenge)
	next := NewID()
	f.f.exec(`INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,applicant_mode,platform,account_id,
		account_epoch,account_version,applicant_version,token_hash,private_token_hash,create_key,request_hash,state,
		group_id,binding_version,controlled_scope_id)
		SELECT $1,s.tenant_id,s.actor_id,'operator_demo',s.platform,s.account_id,s.account_epoch,s.account_version,
		s.actor_version,$3,$4,$1::uuid::text,$3,'waiting',s.group_id,b.version,s.id
		FROM onboarding_test_scopes s JOIN group_bindings b ON b.group_id=s.group_id WHERE s.id=$2`,
		next, f.scopeID, bindingTokenDigest("replacement-group:"+next), bindingTokenDigest("replacement-private:"+next))
}

func TestOnboardingContactRecoveryIsServerOnly(t *testing.T) {
	var req SettingRequest
	if err := json.Unmarshal([]byte(`{"contactRecovery":{"actor":"forged"},
		"onboarding_contact_recovery":{"Scope":"forged","RetiredOperation":"forged"}}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.contactRecovery != nil {
		t.Fatal("browser JSON created a recovery authorization")
	}
	req.contactRecovery = &onboardingContactRecovery{actor: "internal", snapshot: &onboardingContactSnapshot{
		Scope: "internal-scope", RetiredOperation: "internal-operation",
	}}
	raw, err := json.Marshal(req)
	if err != nil || bytes.Contains(raw, []byte("internal")) || bytes.Contains(raw, []byte("recovery")) {
		t.Fatal("request identity or idempotency hash included private recovery context", err)
	}
	if (*onboardingContactRecovery)(nil).excludedOperation() != "" {
		t.Fatal("ordinary settings acquired an excluded operation")
	}
}

func TestPostgresChiliContactRecoveryDeletesOnceWithoutPrivateProof(t *testing.T) {
	f := newOnboardingContactPG(t)
	original := f.originalAttempt(t, f.old)
	writes := 0
	port := func(work context.Context, fence SettingFence) error {
		return fence(work, f.contactProof(), func(context.Context) error { writes++; return nil })
	}
	out, err := f.native.ExecuteSocial(f.f.ctx, f.ref, f.request, port)
	if err != nil || out.Status != "succeeded" || out.Code != "native_friend_delete_acknowledged" || writes != 1 {
		t.Fatal("independent delete did not pass its own singleton grant", out, writes, err)
	}
	f.requireOldUnchanged(t, original)
	if f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND state='waiting'
		AND identity_event_id IS NULL AND identity_sealed IS NULL AND proof_event_id IS NULL`, f.challenge) != 1 {
		t.Fatal("deleting a friend created private or group proof")
	}
	if f.f.count(`SELECT count(*) FROM operations`) != 1 ||
		f.f.count(`SELECT count(*) FROM lajiaoliao_media_attempts`) != 1 ||
		f.f.count(`SELECT used FROM test_windows WHERE id=$1`, f.request.TestWindowID) != 1 {
		t.Fatal("contact recovery replayed a message or changed the singleton budget")
	}
	var sealed []byte
	if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT payload_sealed FROM native_setting_operations WHERE id=$1`,
		out.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := f.bindings.Vault.Open("native-setting:"+out.ID, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		SettingRequest
		Recovery onboardingContactSnapshot `json:"onboarding_contact_recovery"`
	}
	err = json.Unmarshal(raw, &recorded)
	clear(raw)
	if err != nil || recorded.Recovery.Scope != f.scopeID || recorded.Recovery.ScopeVersion != 1 ||
		recorded.Recovery.Challenge != f.challenge || recorded.Recovery.ChallengeVersion != 1 ||
		recorded.Recovery.Window != f.request.TestWindowID || recorded.Recovery.RetiredOperation != f.old ||
		recorded.Recovery.Account != f.claimant || recorded.Recovery.Receiver != f.scope.AccountID ||
		recorded.Recovery.Recipient != f.recipient {
		t.Fatal("preparation did not seal its exact server-resolved authorization", err)
	}
	restarted := &NativeSettingsService{DB: f.f.db, Vault: f.bindings.Vault}
	again, err := restarted.ExecuteSocial(f.f.ctx, f.ref, f.request, func(context.Context, SettingFence) error {
		t.Fatal("duplicate confirmation entered a native port")
		return nil
	})
	if err != nil || again != out || writes != 1 {
		t.Fatal("duplicate confirmation did not return the original operation", again, err)
	}
	f.requireOldUnchanged(t, original)
}

func TestPostgresChiliContactRecoveryFromNormalChallenge(t *testing.T) {
	f := newOnboardingContactPG(t)
	original := f.originalAttempt(t, f.old)
	f.f.exec(`UPDATE group_binding_challenges SET state='cancelled',version=version+1 WHERE id=$1`, f.challenge)
	challenge, err := f.bindings.Create(f.f.ctx, f.ref, GroupBindingCreate{
		Platform: "lajiaoliao", ControlledScopeID: f.scopeID, Key: "synthetic-normal-contact-challenge",
	})
	if err != nil || challenge.State != "waiting" || challenge.Robot.ID != f.scope.AccountID ||
		challenge.ControlledScope == nil || challenge.ControlledScope.ID != f.scopeID ||
		challenge.IdentityProven || challenge.CanConfirm {
		t.Fatal("normal wizard did not create an unproven controlled challenge", challenge, err)
	}
	f.challenge = challenge.ID
	if f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND binding_version=0
		AND identity_event_id IS NULL AND identity_sealed IS NULL AND proof_event_id IS NULL`, f.challenge) != 1 {
		t.Fatal("normal wizard manufactured a binding or identity proof")
	}
	writes := 0
	out, err := f.native.ExecuteSocial(f.f.ctx, f.ref, f.request, func(work context.Context, fence SettingFence) error {
		return fence(work, f.contactProof(), func(context.Context) error { writes++; return nil })
	})
	if err != nil || out.Status != "succeeded" || out.Code != "native_friend_delete_acknowledged" || writes != 1 {
		t.Fatal("normal wizard challenge could not use its independently authorized contact operation", out, writes, err)
	}
	var sealed []byte
	if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT payload_sealed FROM native_setting_operations WHERE id=$1`,
		out.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := f.bindings.Vault.Open("native-setting:"+out.ID, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Recovery onboardingContactSnapshot `json:"onboarding_contact_recovery"`
	}
	err = json.Unmarshal(raw, &recorded)
	clear(raw)
	if err != nil || recorded.Recovery.Challenge != challenge.ID || recorded.Recovery.BindingVersion < 1 ||
		recorded.Recovery.BindingEpoch < 1 || recorded.Recovery.RetiredOperation != f.old {
		t.Fatal("unproven challenge did not pin the current binding in its sealed operation", err)
	}
	restarted := &NativeSettingsService{DB: f.f.db, Vault: f.bindings.Vault}
	again, err := restarted.ExecuteSocial(f.f.ctx, f.ref, f.request, func(context.Context, SettingFence) error {
		t.Fatal("normal wizard duplicate entered a native port")
		return nil
	})
	if err != nil || again != out || writes != 1 ||
		f.f.count(`SELECT used FROM test_windows WHERE id=$1`, f.request.TestWindowID) != 1 ||
		f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND state='waiting'
			AND identity_event_id IS NULL AND identity_sealed IS NULL AND proof_event_id IS NULL`, f.challenge) != 1 {
		t.Fatal("contact completion replayed work or established identity", again, writes, err)
	}
	f.requireOldUnchanged(t, original)
}

func TestPostgresChiliContactRecoveryUnknownDeleteIsNotRetried(t *testing.T) {
	f := newOnboardingContactPG(t)
	original := f.originalAttempt(t, f.old)
	writes := 0
	port := func(work context.Context, fence SettingFence) error {
		return fence(work, f.contactProof(), func(context.Context) error {
			writes++
			return errors.New("synthetic lost delete receipt")
		})
	}
	out, err := f.native.ExecuteSocial(f.f.ctx, f.ref, f.request, port)
	if err != nil || out.Status != "unknown" || writes != 1 {
		t.Fatal("uncertain delete did not retain its single attempt", out, writes, err)
	}
	again, err := f.native.ExecuteSocial(f.f.ctx, f.ref, f.request, port)
	if err != nil || again != out || writes != 1 ||
		f.f.count(`SELECT used FROM test_windows WHERE id=$1`, f.request.TestWindowID) != 1 {
		t.Fatal("unknown delete was retried or its budget refunded", again, writes, err)
	}
	f.requireOldUnchanged(t, original)
}

func TestPostgresChiliContactRecoveryKeepsOtherPathsBlocked(t *testing.T) {
	for _, kind := range []string{"nickname", "friend_approve", "ordinary-friend-delete", "other-platform"} {
		t.Run(kind, func(t *testing.T) {
			f := newOnboardingContactPG(t)
			req := f.request
			switch kind {
			case "nickname":
				req.TestWindowID = ""
				req.Change = nativeauth.SettingChange{Kind: "nickname", Text: "synthetic"}
			case "friend_approve":
				req.Change.Kind, req.Change.Text = "friend_approve", "1700000000123"
				f.f.exec(`UPDATE test_windows SET actions=ARRAY['friend_approve'],friend_application_id=$2 WHERE id=$1`,
					req.TestWindowID, req.Change.Text)
			case "ordinary-friend-delete":
				f.f.exec(`UPDATE group_binding_challenges SET state='cancelled',version=version+1 WHERE id=$1`, f.challenge)
			case "other-platform":
				req.Workspace = "wsl"
			}
			original := f.originalAttempt(t, f.old)
			ports := 0
			port := func(context.Context, SettingFence) error { ports++; return nil }
			var err error
			if kind == "nickname" {
				_, err = f.native.Execute(f.f.ctx, f.ref, req, port)
			} else {
				_, err = f.native.ExecuteSocial(f.f.ctx, f.ref, req, port)
			}
			if err == nil || ports != 0 || f.f.count(`SELECT count(*) FROM native_setting_operations`) != 0 {
				t.Fatal("independent delete exception widened another settings path", ports, err)
			}
			f.requireOldUnchanged(t, original)
		})
	}
}

func TestPostgresChiliContactRecoveryRejectsUnrelatedWorkOrAuthorization(t *testing.T) {
	for _, change := range []string{"running", "second-unknown", "second-running", "group-unknown", "group-running", "scope-closed",
		"challenge-cancelled", "challenge-proven", "window-cancelled",
		"other-actor", "source-epoch", "receiver-epoch", "receiver-offline", "account-cooling",
		"challenge-binding", "unrelated-old-payload"} {
		t.Run(change, func(t *testing.T) {
			f := newOnboardingContactPG(t)
			ref := f.ref
			switch change {
			case "running":
				f.f.exec(`UPDATE operations SET status='running' WHERE id=$1`, f.old)
			case "second-unknown", "second-running", "group-unknown", "group-running":
				status := "unknown"
				if change == "second-running" || change == "group-running" {
					status = "running"
				}
				account := f.claimant
				if strings.HasPrefix(change, "group-") {
					account = f.scope.AccountID
				}
				f.f.exec(`INSERT INTO operations(id,tenant_id,actor_id,account_id,group_id,test_window_id,idempotency_key,
					request_hash,kind,payload_sealed,account_epoch,binding_epoch,status,expires_at,result_code,conversation_key)
					SELECT $1,tenant_id,actor_id,$4,group_id,test_window_id,'synthetic-other-contact-work',
					request_hash,kind,payload_sealed,account_epoch,binding_epoch,$3,expires_at,result_code,conversation_key
					FROM operations WHERE id=$2`, NewID(), f.old, status, account)
			case "scope-closed":
				f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
			case "challenge-cancelled":
				f.f.exec(`UPDATE group_binding_challenges SET state='cancelled',version=version+1 WHERE id=$1`, f.challenge)
			case "challenge-proven":
				f.f.exec(`UPDATE group_binding_challenges SET state='proven',version=version+1 WHERE id=$1`, f.challenge)
			case "window-cancelled":
				f.f.exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, f.request.TestWindowID)
			case "other-actor":
				_, ref = groupBusinessAccessOperator(t, f.assistantBusinessFixture, 123)
			case "source-epoch":
				f.f.exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.claimant)
			case "receiver-epoch":
				f.f.exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.scope.AccountID)
			case "receiver-offline":
				f.f.exec(`UPDATE account_receivers SET snapshot='{"state":"blocked"}' WHERE account_id=$1`, f.scope.AccountID)
			case "account-cooling":
				f.f.exec(`INSERT INTO business_rule_account_cooldowns(account_id,next_write_at)
					VALUES($1,clock_timestamp()+interval '1 minute')`, f.claimant)
			case "challenge-binding":
				f.f.exec(`UPDATE group_binding_challenges SET binding_version=binding_version+1 WHERE id=$1`, f.challenge)
			case "unrelated-old-payload":
				f.f.exec(`UPDATE operations SET payload_sealed='synthetic-unrelated'::bytea WHERE id=$1`, f.old)
			}
			original := f.originalAttempt(t, f.old)
			ports := 0
			_, err := f.native.ExecuteSocial(f.f.ctx, ref, f.request, func(context.Context, SettingFence) error {
				ports++
				return nil
			})
			if err == nil || ports != 0 || f.f.count(`SELECT count(*) FROM native_setting_operations`) != 0 ||
				f.f.count(`SELECT used FROM test_windows WHERE id=$1`, f.request.TestWindowID) != 0 {
				t.Fatal("invalid contact work reached preparation or native transport", ports, err)
			}
			f.requireOldUnchanged(t, original)
		})
	}
}

func TestPostgresChiliContactRecoveryPinsPreparationScopeAndChallenge(t *testing.T) {
	for _, change := range []string{"scope-closed", "scope-version", "challenge-version", "new-challenge",
		"binding-version", "binding-epoch", "role"} {
		t.Run(change, func(t *testing.T) {
			f := newOnboardingContactPG(t)
			original := f.originalAttempt(t, f.old)
			writes := 0
			out, err := f.native.ExecuteSocial(f.f.ctx, f.ref, f.request, func(work context.Context, fence SettingFence) error {
				switch change {
				case "scope-closed":
					f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
				case "scope-version":
					f.f.exec(`UPDATE onboarding_test_scopes SET version=version+1 WHERE id=$1`, f.scopeID)
				case "challenge-version":
					f.f.exec(`UPDATE group_binding_challenges SET version=version+1 WHERE id=$1`, f.challenge)
				case "new-challenge":
					f.replaceWaitingChallenge(t)
				case "binding-version":
					f.f.exec(`UPDATE group_binding_challenges SET binding_version=0 WHERE id=$1`, f.challenge)
					f.f.exec(`UPDATE group_bindings SET version=version+1 WHERE group_id=$1`, f.scope.GroupID)
				case "binding-epoch":
					f.f.exec(`UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, f.scope.GroupID)
				}
				proof := f.contactProof()
				if change == "role" {
					proof.Role = "member"
				}
				return fence(work, proof, func(context.Context) error { writes++; return nil })
			})
			if err != nil || out.Status != "failed" || writes != 0 ||
				f.f.count(`SELECT used FROM test_windows WHERE id=$1`, f.request.TestWindowID) != 1 {
				t.Fatal("prepared delete borrowed changed authorization or refunded its attempt", out, writes, err)
			}
			f.requireOldUnchanged(t, original)
		})
	}
}

// Fire immediately before the second SHARE account read, after the durable
// submitting reservation commits and before final authorization locks its scope.
type onboardingContactFenceTrace struct {
	shares atomic.Int32
	change func()
}

func (trace *onboardingContactFenceTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn,
	data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "SELECT native_uid,credential_sealed FROM accounts") &&
		strings.HasSuffix(data.SQL, " FOR SHARE") && trace.shares.Add(1) == 2 {
		trace.change()
	}
	return ctx
}

func (*onboardingContactFenceTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestPostgresChiliContactRecoveryRechecksFinalFence(t *testing.T) {
	for _, change := range []string{"challenge-version", "scope-closed", "receiver-revision",
		"binding-version", "binding-epoch", "second-unknown"} {
		t.Run(change, func(t *testing.T) {
			f := newOnboardingContactPG(t)
			original := f.originalAttempt(t, f.old)
			untraced := f.f.db.Platform
			trace := &onboardingContactFenceTrace{change: func() {
				t.Helper()
				var query string
				args := []any{f.challenge}
				switch change {
				case "challenge-version":
					query = `UPDATE group_binding_challenges SET version=version+1 WHERE id=$1`
				case "scope-closed":
					query, args = `UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, []any{f.scopeID}
				case "receiver-revision":
					query, args = `UPDATE account_receivers SET revision=revision+1 WHERE account_id=$1`, []any{f.scope.AccountID}
				case "binding-version":
					query, args = `UPDATE group_bindings SET version=version+1 WHERE group_id=$1`, []any{f.scope.GroupID}
				case "binding-epoch":
					query, args = `UPDATE group_bindings SET epoch=epoch+1 WHERE group_id=$1`, []any{f.scope.GroupID}
				case "second-unknown":
					query = `INSERT INTO operations(id,tenant_id,actor_id,account_id,group_id,test_window_id,idempotency_key,
						request_hash,kind,payload_sealed,account_epoch,binding_epoch,status,expires_at,result_code,conversation_key)
						SELECT $1,tenant_id,actor_id,account_id,group_id,test_window_id,'synthetic-final-contact-work',
						request_hash,kind,payload_sealed,account_epoch,binding_epoch,status,expires_at,result_code,conversation_key
						FROM operations WHERE id=$2`
					args = []any{NewID(), f.old}
				}
				ctx, cancel := context.WithTimeout(f.f.ctx, time.Second)
				defer cancel()
				if _, err := untraced.Exec(ctx, query, args...); err != nil {
					t.Fatal("could not interleave a final-fence change", err)
				}
			}}
			cfg := untraced.Config()
			cfg.ConnConfig.Tracer = trace
			traced, err := pgxpool.NewWithConfig(f.f.ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			f.f.db.Platform = traced
			t.Cleanup(func() { f.f.db.Platform = untraced; traced.Close() })
			writes := 0
			out, err := f.native.ExecuteSocial(f.f.ctx, f.ref, f.request, func(work context.Context, fence SettingFence) error {
				return fence(work, f.contactProof(), func(context.Context) error { writes++; return nil })
			})
			if err != nil || out.Status != "failed" || writes != 0 || trace.shares.Load() != 2 ||
				f.f.count(`SELECT used FROM test_windows WHERE id=$1`, f.request.TestWindowID) != 1 {
				t.Fatal("final authorization ignored a change after reservation", out, writes, err)
			}
			f.requireOldUnchanged(t, original)
		})
	}
}
