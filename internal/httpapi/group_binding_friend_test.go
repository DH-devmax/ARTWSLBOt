package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

const bindingFriendFixtureGateEvidence = "acceptance:SYNTHETIC-FRIEND-PROOF-REGRESSION"

// Legacy native ports remain available for historical read-only reconciliation.
// Every identity and native observation here is synthetic.
type bindingFriendNativeFixture struct {
	*onboardingNativeFixture
	lookups, approvals, rejections, submits, reconciles atomic.Int32
	beforeApproval                                      func(context.Context, control.GroupBindingFriendApproval) error
	onSubmit                                            func(context.Context, control.GroupBindingFriendApproval) error
	beforeLookup                                        func()
	finalEvidence                                       func(*control.GroupBindingFriendEvidence)
	unknown, duplicateFence, noFence                    bool
	expiredContact                                      bool
	reconcileState                                      string
	last                                                control.GroupBindingFriendApproval
	mu                                                  sync.Mutex
}

func (n *bindingFriendNativeFixture) BindingRobot(ctx context.Context, scope control.GroupBindingNativeScope) (control.GroupBindingRobot, error) {
	out := control.GroupBindingRobot{ID: scope.AccountID}
	err := n.db.Platform.QueryRow(ctx, `SELECT native_uid,name FROM accounts WHERE id=$1`, scope.AccountID).
		Scan(&out.NativeID, &out.Name)
	return out, err
}

func (n *bindingFriendNativeFixture) friendEvidence(ctx context.Context, r control.GroupBindingFriendRequest) (control.GroupBindingFriendEvidence, error) {
	proof, err := n.connection(ctx, r.GroupBindingNativeScope)
	var self string
	if err == nil {
		err = n.db.Platform.QueryRow(ctx, `SELECT native_uid FROM accounts WHERE id=$1`, r.AccountID).Scan(&self)
	}
	out := control.GroupBindingFriendEvidence{GroupBindingConnectionEvidence: proof, ApplicationID: "synthetic-friend-application",
		SenderID: n.owner, RecipientID: self, State: "pending", ExpiresAt: time.Now().Add(time.Minute)}
	if r.Contact != nil {
		out.Name, out.RequestedAt = r.Contact.Name, r.Contact.Until.Add(-time.Microsecond)
		if n.expiredContact {
			out.RequestedAt = r.Contact.Since.Add(-24 * time.Hour)
		}
	}
	return out, err
}

func (n *bindingFriendNativeFixture) BindingFriend(ctx context.Context, r control.GroupBindingFriendRequest) (control.GroupBindingFriendEvidence, error) {
	n.lookups.Add(1)
	if n.beforeLookup != nil {
		n.beforeLookup()
	}
	if r.Contact == nil && (!strings.HasPrefix(r.ChallengeText, "群接入 ") ||
		len(strings.TrimPrefix(r.ChallengeText, "群接入 ")) != 20) {
		return control.GroupBindingFriendEvidence{}, errors.New("synthetic fixture requires exact friend text")
	}
	return n.friendEvidence(ctx, r)
}

func (n *bindingFriendNativeFixture) ApproveBindingFriend(ctx context.Context, r control.GroupBindingFriendApproval, fence control.GroupBindingFriendFence) error {
	n.approvals.Add(1)
	return n.decideFriend(ctx, r, fence)
}

func (n *bindingFriendNativeFixture) RejectBindingFriend(ctx context.Context, r control.GroupBindingFriendApproval, fence control.GroupBindingFriendFence) error {
	n.rejections.Add(1)
	if !r.RejectExpired || r.Contact == nil || !r.Contact.RejectExpired {
		return errors.New("synthetic fixture refuses an unscoped rejection")
	}
	return n.decideFriend(ctx, r, fence)
}

func (n *bindingFriendNativeFixture) decideFriend(ctx context.Context, r control.GroupBindingFriendApproval, fence control.GroupBindingFriendFence) error {
	n.mu.Lock()
	n.last = r
	n.mu.Unlock()
	if n.beforeApproval != nil {
		if err := n.beforeApproval(ctx, r); err != nil {
			return err
		}
	}
	if n.noFence {
		return nil
	}
	e, err := n.friendEvidence(ctx, r.GroupBindingFriendRequest)
	if err != nil {
		return err
	}
	e.ExpiresAt = r.ExpiresAt
	if n.finalEvidence != nil {
		n.finalEvidence(&e)
	}
	submit := func(work context.Context) error {
		if err := work.Err(); err != nil {
			return err
		}
		n.submits.Add(1)
		if n.onSubmit != nil {
			if err := n.onSubmit(work, r); err != nil {
				return err
			}
		}
		if n.unknown {
			return errors.New("synthetic native outcome unavailable")
		}
		return nil
	}
	if err = fence(ctx, e, submit); err != nil {
		return err
	}
	if n.duplicateFence {
		return fence(ctx, e, submit)
	}
	return nil
}

func (n *bindingFriendNativeFixture) ReconcileBindingFriend(ctx context.Context, r control.GroupBindingFriendApproval) (control.GroupBindingFriendEvidence, error) {
	n.reconciles.Add(1)
	n.mu.Lock()
	last := n.last
	n.mu.Unlock()
	if r.OperationID != last.OperationID || r.ApplicationID != last.ApplicationID || r.SenderID != last.SenderID ||
		r.RecipientID != last.RecipientID || r.ChallengeText != last.ChallengeText || r.RejectExpired != last.RejectExpired {
		return control.GroupBindingFriendEvidence{}, errors.New("reconciliation changed original request")
	}
	e, err := n.friendEvidence(ctx, r.GroupBindingFriendRequest)
	e.ExpiresAt, e.State = r.ExpiresAt, n.reconcileState
	return e, err
}

func friendFixture(t *testing.T, platform string) (*onboardingFixture, *bindingFriendNativeFixture, testClient, control.SessionRef) {
	t.Helper()
	f := newOnboardingFixture(t)
	n := &bindingFriendNativeFixture{onboardingNativeFixture: f.native, reconcileState: "approved"}
	f.app.GroupBinding.Native = n
	client := f.alice
	client.workspace = platform
	return f, n, client, onboardingRef(t, client, platform)
}

func friendOpenGate(t *testing.T, f *onboardingFixture, platform string) {
	t.Helper()
	admin := f.admin
	admin.workspace = platform
	onboardingAPI(t, admin, "PUT", "/system/group-binding/friend-gates/"+f.accounts[platform], "",
		control.GroupBindingFriendGateInput{Version: 0, State: "open", Evidence: []string{bindingFriendFixtureGateEvidence}}, 200)
}

func friendCount(t *testing.T, f *onboardingFixture, query string, args ...any) int {
	t.Helper()
	var count int
	if err := f.db.Platform.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func friendFailure(t *testing.T, err error, status int, code string) {
	t.Helper()
	var failure *control.Failure
	if !errors.As(err, &failure) || failure.Status != status || failure.Code != code {
		t.Fatalf("wanted %d %s, got %v", status, code, err)
	}
}

func TestGroupBindingFriendFixtureGateEvidence(t *testing.T) {
	if !control.ValidBusinessEvidenceRefs([]string{bindingFriendFixtureGateEvidence}) {
		t.Fatal("synthetic friend fixture evidence violates the admission gate contract")
	}
}

func TestGroupBindingFriendPostgresRetiredNewSubmissions(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := friendFixture(t, platform)
			c := f.createChallenge(t, client, platform, "friend-retired-create-"+platform)
			if c.FriendChallenge != "" || c.CanVerifyFriend {
				t.Fatal("new challenge still advertises incoming friend-note verification")
			}
			path := "/group-binding/challenges/" + c.ID + "/friend-verify"
			for _, field := range []string{"sender_id", "account_id", "challenge_text"} {
				onboardingAPI(t, client, "POST", path, "friend-retired-field",
					map[string]any{"version": c.Version, field: "browser-selected"}, 400)
			}
			for _, in := range []control.GroupBindingFriendInput{
				{Version: 0, Key: "friend-invalid-version"},
				{Version: c.Version, Key: "short"},
			} {
				_, err := f.app.GroupBinding.VerifyFriend(context.Background(), ref, c.ID, in)
				friendFailure(t, err, 400, "group_binding_friend_invalid")
			}
			for _, open := range []bool{false, true} {
				if open {
					friendOpenGate(t, f, platform)
				}
				onboardingAPI(t, client, "POST", path, "friend-retired-consent",
					map[string]any{"version": c.Version}, 410)
				_, err := f.app.GroupBinding.VerifyFriend(context.Background(), ref, c.ID,
					control.GroupBindingFriendInput{Version: c.Version, Key: "friend-retired-service"})
				friendFailure(t, err, 410, "group_binding_friend_flow_retired")
			}
			state, err := f.app.GroupBinding.FriendState(context.Background(), ref, c.ID)
			if err != nil || state.CanVerify || state.Attempt != nil || state.IdentityProven {
				t.Fatal("retired flow remains actionable", state, err)
			}
			if n.lookups.Load() != 0 || n.approvals.Load() != 0 || n.submits.Load() != 0 ||
				n.reconciles.Load() != 0 || friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 {
				t.Fatal("retired submission reached native code or persisted a new attempt")
			}
		})
	}
}

// Seed an already-existing pre-060 record in wsl_test. This exercises migration
// compatibility without using the retired production mutation to manufacture it.
func legacyFriendHistory(t *testing.T, f *onboardingFixture, n *bindingFriendNativeFixture, client testClient, platform, status string) (control.GroupBindingChallenge, control.GroupBindingFriendAttempt) {
	t.Helper()
	ctx := context.Background()
	friendOpenGate(t, f, platform)
	id, operation, proofID := control.NewID(), control.NewID(), control.NewID()
	text := "群接入 ABCDEFGHIJKLMNOPQRST"
	sealedText, err := f.vault.Seal("group-binding-friend-text:"+id, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Platform.Exec(ctx, `INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,platform,
		account_id,account_epoch,account_version,applicant_version,token_hash,private_token_hash,
		friend_token_hash,friend_text_sealed,create_key,request_hash,friend_flow)
		SELECT $1,$2,u.id,$3,a.id,a.epoch,a.version,u.access_version,$6,$7,$8,$9,$10,$6,'legacy'
		FROM accounts a JOIN users u ON u.id=$4 WHERE a.id=$5`, id, f.tenantA, platform, f.aliceID,
		f.accounts[platform], security.Digest(id), security.Digest(operation), security.Digest(text), sealedText, "legacy-history-"+id)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.app.GroupBinding.Read(ctx, onboardingRef(t, client, platform), id)
	if err != nil {
		t.Fatal(err)
	}
	var epoch, accountVersion, applicantVersion int64
	if err = f.db.Platform.QueryRow(ctx, `SELECT account_epoch,account_version,applicant_version
		FROM group_binding_challenges WHERE id=$1`, id).Scan(&epoch, &accountVersion, &applicantVersion); err != nil {
		t.Fatal(err)
	}
	request := control.GroupBindingFriendRequest{GroupBindingNativeScope: control.GroupBindingNativeScope{
		Platform: platform, AccountID: f.accounts[platform], AccountEpoch: epoch, AccountVersion: accountVersion}, ChallengeText: text}
	evidence, err := n.friendEvidence(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	approval := control.GroupBindingFriendApproval{GroupBindingFriendRequest: request, OperationID: operation,
		ApplicationID: evidence.ApplicationID, SenderID: evidence.SenderID, RecipientID: evidence.RecipientID,
		ExpiresAt: c.ExpiresAt.UTC().Truncate(time.Microsecond)}
	evidence.ExpiresAt = approval.ExpiresAt
	raw, err := json.Marshal(struct {
		Challenge, Actor, Tenant string
		ChallengeVersion         int64
		ApplicantVersion         int64
		ChallengeText            string
		Approval                 control.GroupBindingFriendApproval
	}{id, f.aliceID, f.tenantA, c.Version, applicantVersion, text, approval})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := f.vault.Seal("group-binding-friend-attempt:"+operation, raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Platform.Exec(ctx, `INSERT INTO group_binding_friend_attempts(id,challenge_id,actor_id,tenant_id,
		platform,account_id,account_epoch,account_version,applicant_version,challenge_version,gate_version,
		request_hash,payload_sealed,idempotency_key,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1,$11,$12,$13,$14)`,
		operation, id, f.aliceID, f.tenantA, platform, f.accounts[platform], epoch, accountVersion,
		applicantVersion, c.Version, security.Digest(operation), payload, "legacy-consent-"+operation, approval.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := f.vault.Seal("group-binding-friend-sender:"+proofID, []byte(n.owner))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	sealedEvidence, err := f.vault.Seal("group-binding-friend-evidence:"+proofID, raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Platform.Exec(ctx, `INSERT INTO group_binding_friend_proofs(id,attempt_id,challenge_id,actor_id,
		tenant_id,platform,account_id,account_epoch,account_version,applicant_version,challenge_version,
		native_sender_sealed,evidence_sealed) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		proofID, operation, id, f.aliceID, f.tenantA, platform, f.accounts[platform], epoch, accountVersion,
		applicantVersion, c.Version, sender, sealedEvidence)
	if err == nil && status != "prepared" {
		_, err = f.db.Platform.Exec(ctx, `UPDATE group_binding_friend_attempts SET status=$2,attempts_used=1 WHERE id=$1`, operation, status)
	}
	if err != nil {
		t.Fatal(err)
	}
	n.last = approval
	c.FriendChallenge = text
	return c, control.GroupBindingFriendAttempt{ID: operation, Status: status, Version: c.Version}
}

func TestGroupBindingFriendPostgresHistoricalReadAndReconciliationRemain(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := friendFixture(t, platform)
			c, historical := legacyFriendHistory(t, f, n, client, platform, "unknown")
			path := "/group-binding/challenges/" + c.ID
			for range 2 {
				raw := onboardingAPI(t, client, "GET", path+"/friend-verify", "", nil, 200)
				var state control.GroupBindingFriendState
				if json.Unmarshal(raw, &state) != nil || state.Attempt == nil ||
					state.Attempt.ID != historical.ID || state.Attempt.Status != "unknown" || state.CanVerify {
					t.Fatal("migration hid or re-enabled a historical incoming attempt")
				}
				onboardingAPI(t, client, "GET", path, "", nil, 200)
			}
			if n.reconciles.Load() != 0 || n.lookups.Load() != 0 || n.approvals.Load() != 0 {
				t.Fatal("reading migrated history caused native work")
			}
			onboardingAPI(t, client, "POST", path+"/friend-verify", "retired-historical-replay",
				map[string]any{"version": c.Version}, 410)
			bob := f.bob
			bob.workspace = platform
			onboardingAPI(t, bob, "GET", path+"/friend-verify", "", nil, 404)
			onboardingAPI(t, bob, "POST", path+"/friend-attempts/"+historical.ID+"/reconcile", "", nil, 404)
			n.reconcileState = "pending"
			pending, err := f.app.GroupBinding.ReconcileFriend(context.Background(), ref, c.ID, historical.ID)
			if err != nil || pending.Status != "unknown" || pending.ID != historical.ID {
				t.Fatal("pending historical approval was silently retried", pending, err)
			}
			n.reconcileState = "approved"
			resolved, err := f.app.GroupBinding.ReconcileFriend(context.Background(), ref, c.ID, historical.ID)
			if err != nil || resolved.Status != "succeeded" || resolved.ID != historical.ID ||
				n.reconciles.Load() != 2 || n.submits.Load() != 0 || n.approvals.Load() != 0 {
				t.Fatal("historical reconciliation changed or resent its original operation", resolved, err)
			}
			before := n.reconciles.Load()
			again, err := f.app.GroupBinding.ReconcileFriend(context.Background(), ref, c.ID, historical.ID)
			if err != nil || again.ID != historical.ID || n.reconciles.Load() != before {
				t.Fatal("resolved historical read repeated upstream work", again, err)
			}
			if friendCount(t, f, `SELECT count(*) FROM group_bindings`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM native_received_events`) != 0 {
				t.Fatal("historical friend proof fabricated group ownership or ingress")
			}
			if friendCount(t, f, `SELECT count(*) FROM audit_events WHERE detail::text LIKE '%'||$1||'%'
				OR detail::text LIKE '%'||$2||'%'`, c.FriendChallenge, n.owner) != 0 {
				t.Fatal("historical reconciliation leaked private evidence")
			}
			var payload []byte
			if err = f.db.Platform.QueryRow(context.Background(), `SELECT payload_sealed
				FROM group_binding_friend_attempts WHERE id=$1`, historical.ID).Scan(&payload); err != nil ||
				bytes.Contains(payload, []byte(n.owner)) || bytes.Contains(payload, []byte(c.FriendChallenge)) {
				t.Fatal("historical payload was rewritten as plaintext", err)
			}
		})
	}
}

func TestGroupBindingFriendPostgresInterruptedHistoricalWorkIsNeverDispatched(t *testing.T) {
	f, n, client, ref := friendFixture(t, "wsl")
	c, old := legacyFriendHistory(t, f, n, client, "wsl", "prepared")
	for range 2 {
		if err := f.app.GroupBinding.RecoverFriends(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	state, err := f.app.GroupBinding.FriendState(context.Background(), ref, c.ID)
	if err != nil || state.Attempt == nil || state.Attempt.ID != old.ID || state.Attempt.Status != "unknown" ||
		n.lookups.Load() != 0 || n.approvals.Load() != 0 || n.submits.Load() != 0 || n.reconciles.Load() != 0 {
		t.Fatal("restart recovery dispatched or lost historical incoming work", state, err)
	}
}
