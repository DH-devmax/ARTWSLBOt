package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/security"
)

// These fixtures inherit businessIntegrationDB's serial wsl_test-only guard.
// All credentials, proof records, models and submission ports are synthetic.
func controlledExecutionProof(t *testing.T, f *businessFixture, actor Actor, tenant, platform, account, group string,
	expires time.Time, bound bool) string {
	t.Helper()
	claimant := f.account(platform, 9)
	f.exec(`UPDATE accounts SET testing_enabled=true WHERE id IN ($1,$2)`, account, claimant)
	f.exec(`UPDATE native_groups SET isolated_test=true WHERE id=$1`, group)
	id := NewID()
	f.exec(`INSERT INTO onboarding_test_scopes(id,actor_id,tenant_id,platform,account_id,claimant_account_id,group_id,
		actor_version,account_epoch,account_version,claimant_epoch,claimant_version,claimant_native_id,
		claimant_role,robot_role,owner_identity_hash,create_key,request_hash,expires_at)
		SELECT $1,u.id,$3,$4,a.id,c.id,$7,u.access_version,a.epoch,a.version,c.epoch,c.version,c.native_uid,
		'admin','admin',$8,$1::uuid::text,$9,$10
		FROM users u JOIN accounts a ON a.id=$5 JOIN accounts c ON c.id=$6 WHERE u.id=$2`,
		id, actor.ID, tenant, platform, account, claimant, group,
		bindingIdentityDigest(platform, group, "native-owner"), security.Digest(id), expires)
	if !bound {
		return id
	}
	challenge := NewID()
	f.exec(`INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,applicant_mode,platform,account_id,
		account_epoch,account_version,applicant_version,token_hash,private_token_hash,create_key,request_hash,state,
		group_id,proof_event_id,proof_sealed,proof_role,controlled_scope_id)
		SELECT $1,s.tenant_id,s.actor_id,'operator_demo',s.platform,s.account_id,s.account_epoch,s.account_version,
		s.actor_version,$3,$4,$1::uuid::text,$3,'bound',s.group_id,1,$5,'admin',s.id
		FROM onboarding_test_scopes s WHERE s.id=$2`,
		challenge, id, security.Digest(challenge), security.Digest("private:"+challenge), []byte("synthetic-proof"))
	tx, err := f.db.Platform.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	event := int64(1)
	bindings := GroupBindingService{DB: f.db, Vault: f.service.Vault}
	err = bindings.saveOwnership(f.ctx, tx,
		bindingRecord{ID: challenge, Tenant: tenant, Platform: platform, Group: group, ProofEvent: &event, ControlledScope: id},
		bindingProof{Event: event, Owner: "native-owner", Sender: "10009", SenderRole: "admin", RobotRole: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

type controlledExecutionFixture struct {
	*assistantBusinessFixture
	actor         Actor
	scopeID       string
	tenant        string
	customerScope assistant.Scope
}

func newControlledExecutionFixture(t *testing.T, platform string, bound bool, lifetime time.Duration) *controlledExecutionFixture {
	t.Helper()
	f := newAssistantOutcomesFixture(t, platform)
	customerScope := f.scope
	actor, ref := groupBusinessAccessOperator(t, f, 113)
	workspace, err := OperatorWorkspace(f.f.ctx, f.f.db.Platform, actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	scope := f.f.group(platform, f.f.account(platform, 2), workspace.ID, 2, false)
	f.f.exec(`UPDATE group_business_runtime SET enabled=true WHERE group_id=$1`, scope.Group)
	f.f.exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
		VALUES($1,$2,'admin',clock_timestamp(),'native')`, scope.Account, scope.Group)
	f.scope = assistant.Scope{Platform: platform, Kind: "group", AccountID: scope.Account, GroupID: scope.Group}
	f.nativeID, f.ref = scope.NativeGroup, ref
	id := controlledExecutionProof(t, f.f, actor, workspace.ID, platform, scope.Account, scope.Group, time.Now().Add(lifetime), bound)
	return &controlledExecutionFixture{assistantBusinessFixture: f, actor: actor, scopeID: id, tenant: workspace.ID, customerScope: customerScope}
}

func controlledUsageTx(t *testing.T, f *businessFixture, reserve func(pgx.Tx) error) error {
	t.Helper()
	tx, err := f.db.Platform.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err = reserve(tx); err != nil {
		return err
	}
	return tx.Commit(f.ctx)
}

func assertControlledUsage(t *testing.T, f *businessFixture, scope string, native, model int, amount int64) {
	t.Helper()
	n, m, total, err := ReadControlledUsage(f.ctx, f.db.Platform, scope)
	if err != nil || n != native || m != model || total != amount {
		t.Fatalf("usage = (%d,%d,%d), %v; want (%d,%d,%d)", n, m, total, err, native, model, amount)
	}
}

func TestControlledExecutionCurrencyAndOrdinarySnapshots(t *testing.T) {
	for _, row := range []struct{ credits, usdt int64 }{
		{1, 1}, {999, 1}, {1000, 1}, {1001, 2}, {20_000_000, 20_000}, {20_000_001, 20_001},
		{math.MaxInt64, math.MaxInt64/1000 + 1},
	} {
		if got := controlledModelMicroUSDT(row.credits); got != row.usdt {
			t.Fatalf("%d micro-credits = %d micro-USDT, want %d", row.credits, got, row.usdt)
		}
	}
	for _, snapshot := range []any{BusinessScope{}, HeatScope{}, TextRequest{}, assistantActionPayload{}, planContent{}} {
		raw, err := json.Marshal(snapshot)
		if err != nil || bytes.Contains(raw, []byte("controlled_scope")) {
			t.Fatal("ordinary snapshot changed its persisted format", string(raw), err)
		}
	}
	var prior TextRequest
	if err := json.Unmarshal([]byte(`{"AccountEpoch":1,"BindingEpoch":2}`), &prior); err != nil ||
		prior.controlledExecutionFence != (controlledExecutionFence{}) {
		t.Fatal("ordinary historical task acquired test authorization", prior, err)
	}
}

type controlledExecutionQuery func(context.Context, string, ...any) pgx.Row

func (q controlledExecutionQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return q(ctx, sql, args...)
}

func TestControlledExecutionSubmissionDeadline(t *testing.T) {
	fence := controlledExecutionFence{ControlledScopeID: NewID(), ControlledScopeVersion: 3}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	q := controlledExecutionQuery(func(_ context.Context, _ string, args ...any) pgx.Row {
		if len(args) != 4 || args[0] != fence.ControlledScopeID || args[1] != fence.ControlledScopeVersion ||
			args[2] != float64(8) || args[3] != deadline {
			t.Fatal("submission lost its exact scope, write window or context deadline", args)
		}
		return capabilityRow{values: []any{false}}
	})
	capabilityError(t, fence.requireSubmission(ctx, q, 8*time.Second), "controlled_onboarding_scope_inactive")
	capabilityError(t, fence.requireSubmission(context.Background(), nil, 0), "controlled_onboarding_scope_inactive")
	ordinary := controlledExecutionFence{}
	if err := ordinary.requireSubmission(ctx, nil, 8*time.Second); err != nil {
		t.Fatal("ordinary execution acquired a controlled scope requirement", err)
	}
}

func TestControlledExecutionModelDeadline(t *testing.T) {
	fence := controlledExecutionFence{ControlledScopeID: NewID(), ControlledScopeVersion: 3}
	for _, earlier := range []string{"scope", "parent"} {
		t.Run(earlier, func(t *testing.T) {
			expiry, parentExpiry := time.Now().Add(time.Minute), time.Now().Add(2*time.Minute)
			if earlier == "parent" {
				parentExpiry = time.Now().Add(30 * time.Second)
			}
			parent, cancel := context.WithDeadline(context.Background(), parentExpiry)
			defer cancel()
			q := &capabilityQueries{t: t, rows: []capabilityRow{{values: []any{expiry}}}}
			work, stop, err := fence.modelContext(parent, q)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			if deadline, ok := work.Deadline(); !ok || !deadline.Equal(minHeatDeadline(expiry, parentExpiry)) {
				t.Fatal("model context extended the earlier deadline", deadline)
			}
			cancel()
			if !errors.Is(work.Err(), context.Canceled) {
				t.Fatal("model context lost parent cancellation", work.Err())
			}
		})
	}
	expiry := time.Now().Add(30 * time.Millisecond)
	q := &capabilityQueries{t: t, rows: []capabilityRow{{values: []any{expiry}}}}
	work, stop, err := fence.modelContext(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	select {
	case <-work.Done():
		if !errors.Is(work.Err(), context.DeadlineExceeded) {
			t.Fatal("model scope expiry did not cancel generation", work.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("model generation outlived its scope")
	}
	ordinary := controlledExecutionFence{}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	work, stop, err = ordinary.modelContext(parent, nil)
	if err != nil || work != parent {
		t.Fatal("ordinary model context changed", err)
	}
	stop()
	if parent.Err() != nil {
		t.Fatal("ordinary model cleanup cancelled its caller")
	}
}

func TestPostgresControlledExecutionSubmissionWindows(t *testing.T) {
	f := newControlledExecutionFixture(t, "wsl", true, time.Minute)
	fence, err := controlledExecutionScope(f.f.ctx, f.f.db.Platform, f.actor.ID, "wsl", f.scope.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT expires_at FROM onboarding_test_scopes WHERE id=$1`,
		f.scopeID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		deadline time.Time
		window   time.Duration
		allowed  bool
	}{
		{"socket", time.Now().Add(2 * time.Second), 2 * time.Second, true},
		{"automatic", time.Now().Add(8 * time.Second), 8 * time.Second, true},
		{"scope equals model deadline", expiry, 0, true},
		{"context crosses expiry", expiry.Add(time.Second), 2 * time.Second, false},
		{"write window crosses expiry", time.Now().Add(2 * time.Second), 2 * time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(f.f.ctx, tc.deadline)
			defer cancel()
			err := fence.requireSubmission(ctx, f.f.db.Platform, tc.window)
			if tc.allowed {
				if err != nil {
					t.Fatal("scope covers the submission but was denied", err)
				}
			} else {
				capabilityError(t, err, "controlled_onboarding_scope_inactive")
			}
			if err = RequireControlledBinding(f.f.ctx, f.f.db.Platform, f.actor.ID, "wsl", f.scope.GroupID); err != nil {
				t.Fatal("insufficient submission time closed a still-live read scope", err)
			}
		})
	}
	capabilityError(t, fence.requireSubmission(f.f.ctx, f.f.db.Platform, 2*time.Minute),
		"controlled_onboarding_scope_inactive")
	customer := Actor{ID: f.f.actor, TenantID: f.f.tenant, Role: "user"}
	if err = RequireGroup(f.f.ctx, f.f.db.Platform, customer, f.customerScope.GroupID); err != nil {
		t.Fatal("controlled window checks blocked ordinary customer access", err)
	}
	assertControlledUsage(t, f.f, f.scopeID, 0, 0, 0)
}

func TestPostgresControlledExecutionNativeNearExpiry(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newControlledExecutionFixture(t, platform, true, 2*time.Second)
			session, err := f.assistant.CreateSession(f.f.ctx, f.ref, f.scope)
			if err != nil {
				t.Fatal(err)
			}
			action := groupBusinessAccessActionPlan(t, f.assistantBusinessFixture, f.ref, session)
			called, writes := false, 0
			f.assistant.ActionExecutor = func(ctx context.Context, ref SessionRef, plan AssistantActionPlan) (SettingOperation, error) {
				req := SettingRequest{Account: f.scope.AccountID, Workspace: platform, Key: plan.ID,
					Epoch: plan.AccountEpoch, Digest: strings.Repeat("a", 64),
					Change: plan.settingChange(f.nativeID), business: &assistantBusiness{service: f.assistant, plan: plan}}
				var uid string
				if err := f.f.db.Platform.QueryRow(ctx, `SELECT version,native_uid FROM accounts WHERE id=$1`,
					req.Account).Scan(&req.Version, &uid); err != nil {
					return SettingOperation{}, err
				}
				return f.native.execute(ctx, ref, req, func(work context.Context, fence SettingFence) error {
					called = true
					if err := RequireControlledBinding(work, f.f.db.Platform, f.actor.ID, platform, f.scope.GroupID); err != nil {
						t.Fatal("scope already expired before the final window check", err)
					}
					proof := f.proof(req)
					proof.UID = uid
					return fence(work, proof, func(context.Context) error { writes++; return nil })
				}, nil)
			}
			out, err := f.assistant.ApproveAction(f.f.ctx, f.ref, action.ID, action.Digest, action.Version, "near-expiry-final-write")
			if err != nil || !called || writes != 0 || out.Status != "failed" ||
				out.ResultCode != "controlled_onboarding_scope_inactive" {
				t.Fatal("near-expiry scope reached native I/O", out, called, writes, err)
			}
			// The separately committed attempt remains conservative even when
			// the final window rejects before the synthetic socket is called.
			assertControlledUsage(t, f.f, f.scopeID, 1, 0, 0)
		})
	}
}

type controlledExecutionModel func(context.Context) (assistant.Response, error)

func (p controlledExecutionModel) Generate(ctx context.Context, _ []assistant.Message, _ []assistant.Tool,
	_ func(string) error) (assistant.Response, error) {
	return p(ctx)
}

func TestPostgresControlledExecutionGatewayDeadline(t *testing.T) {
	for _, mode := range []string{"ordinary", "scope", "parent"} {
		t.Run(mode, func(t *testing.T) {
			h := newWorkspaceHeatBillingFixture(t, "wsl", true)
			f := h.base.f
			var scopeID string
			if mode != "ordinary" {
				var receiver string
				if err := f.db.Platform.QueryRow(f.ctx, `SELECT primary_account_id::text FROM group_bindings WHERE group_id=$1`,
					h.scope.Group).Scan(&receiver); err != nil {
					t.Fatal(err)
				}
				expires := time.Now().Add(2 * time.Second)
				if mode == "parent" {
					expires = time.Now().Add(time.Minute)
				}
				scopeID = controlledExecutionProof(t, f, h.base.admin, h.scope.Tenant, "wsl", receiver, h.scope.Group,
					expires, true)
			}
			_, token := h.job(t)
			timeout := 10 * time.Second
			ctx, cancel := context.WithTimeout(f.ctx, timeout)
			defer cancel()
			expected, _ := ctx.Deadline()
			if mode == "scope" {
				if err := f.db.Platform.QueryRow(f.ctx, `SELECT expires_at FROM onboarding_test_scopes WHERE id=$1`,
					scopeID).Scan(&expected); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			h.base.gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) {
				return controlledExecutionModel(func(work context.Context) (assistant.Response, error) {
					calls++
					if deadline, ok := work.Deadline(); !ok || !deadline.Equal(expected) {
						t.Fatal("gateway model did not receive the bounded deadline", deadline, expected)
					}
					if mode == "parent" {
						// Cancel after admission so database scheduling cannot decide
						// whether this case exercises cancellation inside the model.
						cancel()
					}
					if mode != "ordinary" {
						<-work.Done()
						return assistant.Response{}, work.Err()
					}
					return assistant.Response{Message: assistant.Message{Role: "assistant", Content: "ordinary response"},
						Usage: assistant.Usage{PromptTokens: 8, CompletionTokens: 1, TotalTokens: 9}}, nil
				}), nil
			}
			_, err := h.base.gateway.HeatGenerate(ctx, token, HeatGatewayInput{ModelAlias: "default",
				ModelVersion: h.scope.RouteVersion, Messages: []assistant.Message{{Role: "user", Content: "bounded request"}}})
			if calls != 1 {
				t.Fatal("model submission count changed", calls, err)
			}
			if mode == "ordinary" {
				if err != nil || f.count(`SELECT count(*) FROM controlled_onboarding_usage`) != 0 {
					t.Fatal("ordinary gateway acquired controlled limits", err)
				}
			} else {
				var wantErr error = context.DeadlineExceeded
				if mode == "parent" {
					wantErr = context.Canceled
				}
				if !errors.Is(err, wantErr) {
					t.Fatal("controlled model ignored its deadline", err)
				}
				n, m, held, err := ReadControlledUsage(f.ctx, f.db.Platform, scopeID)
				if err != nil || n != 0 || m != 1 || held < 1 ||
					f.count(`SELECT count(*) FROM ai_calls WHERE status='unknown'`) != 1 {
					t.Fatal("cancelled model refunded its reservation", n, m, held, err)
				}
			}
		})
	}
}

func TestPostgresControlledExecutionSharedBudgets(t *testing.T) {
	f := newControlledExecutionFixture(t, "wsl", true, 10*time.Minute)
	db := f.f
	native := func(source, key string) error {
		return controlledUsageTx(t, db, func(tx pgx.Tx) error {
			return ReserveControlledNativeTx(db.ctx, tx, "wsl", f.scope.GroupID, source, key)
		})
	}
	model := func(key string, amount int64) error {
		return controlledUsageTx(t, db, func(tx pgx.Tx) error {
			return ReserveControlledModelTx(db.ctx, tx, "wsl", f.scope.GroupID, key, amount)
		})
	}
	for i := 0; i < 3; i++ {
		if err := native("manual", "stable-operation"); err != nil {
			t.Fatal(err)
		}
	}
	if err := native("heatbot", "background-operation"); err != nil {
		t.Fatal(err)
	}
	capabilityError(t, native("automatic", "third-operation"), "controlled_onboarding_native_budget_exhausted")
	if err := native("manual", "stable-operation"); err != nil {
		t.Fatal("retry of existing reservation counted again", err)
	}
	if err := model("human-model", 10_001); err != nil {
		t.Fatal(err)
	}
	capabilityError(t, model("background-model", 10_000), "controlled_onboarding_model_budget_exhausted")
	assertControlledUsage(t, db, f.scopeID, 2, 1, 10_001)
	if err := model("background-model", 9_999); err != nil {
		t.Fatal(err)
	}
	if err := model("background-model", 9_999); err != nil {
		t.Fatal("model duplicate counted again", err)
	}
	capabilityError(t, model("background-model", 9_998), "idempotency_conflict")
	capabilityError(t, model("third-model", 1), "controlled_onboarding_model_budget_exhausted")
	assertControlledUsage(t, db, f.scopeID, 2, 2, 20_000)
	if _, err := db.db.Platform.Exec(db.ctx, `DELETE FROM controlled_onboarding_usage WHERE scope_id=$1`, f.scopeID); err == nil {
		t.Fatal("failed or unknown attempts could be refunded")
	}
	assertControlledUsage(t, db, f.scopeID, 2, 2, 20_000)
}

func TestPostgresControlledExecutionConcurrentDedup(t *testing.T) {
	f := newControlledExecutionFixture(t, "lajiaoliao", true, 10*time.Minute)
	var pending sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		pending.Add(1)
		go func() {
			defer pending.Done()
			tx, err := f.f.db.Platform.Begin(f.f.ctx)
			if err != nil {
				results <- err
				return
			}
			defer tx.Rollback(f.f.ctx)
			err = ReserveControlledNativeTx(f.f.ctx, tx, "lajiaoliao", f.scope.GroupID, "manual", "one-concurrent-key")
			if err == nil {
				err = tx.Commit(f.f.ctx)
			}
			results <- err
		}()
	}
	pending.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent duplicate failed", err)
		}
	}
	assertControlledUsage(t, f.f, f.scopeID, 1, 0, 0)
}

func TestPostgresControlledExecutionPrebindingScopeIsolation(t *testing.T) {
	f := newControlledExecutionFixture(t, "wsl", false, 10*time.Minute)
	for _, group := range []string{f.scope.GroupID, f.customerScope.GroupID} {
		err := controlledUsageTx(t, f.f, func(tx pgx.Tx) error {
			// An unrelated ordinary operation is never inferred from a pending
			// scope on the same group.
			return ReserveControlledNativeTx(f.f.ctx, tx, "wsl", group, "manual", "ordinary-operation")
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	assertControlledUsage(t, f.f, f.scopeID, 0, 0, 0)
	for _, source := range []string{"frienddecision", "invitation"} {
		if err := controlledUsageTx(t, f.f, func(tx pgx.Tx) error {
			return ReserveControlledScopeNativeTx(f.f.ctx, tx, f.scopeID, source, "stable-prepared-attempt")
		}); err != nil {
			t.Fatal(err)
		}
	}
	err := controlledUsageTx(t, f.f, func(tx pgx.Tx) error {
		return ReserveControlledScopeNativeTx(f.f.ctx, tx, f.scopeID, "frienddecision", "third-prepared-attempt")
	})
	capabilityError(t, err, "controlled_onboarding_native_budget_exhausted")
	f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',closed_at=clock_timestamp(),version=version+1 WHERE id=$1`, f.scopeID)
	err = controlledUsageTx(t, f.f, func(tx pgx.Tx) error {
		return ReserveControlledScopeNativeTx(f.f.ctx, tx, f.scopeID, "invitation", "stable-prepared-attempt")
	})
	capabilityError(t, err, "controlled_onboarding_scope_inactive")
	assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
}

func TestPostgresControlledExecutionReadPlanRuntimeFences(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, revoke := range []string{"closed", "deadline", "claimant_version"} {
			t.Run(platform+"/"+revoke, func(t *testing.T) {
				lifetime := 10 * time.Minute
				if revoke == "deadline" {
					lifetime = 2 * time.Second
				}
				f := newControlledExecutionFixture(t, platform, true, lifetime)
				db := f.f
				customer := Actor{ID: db.actor, TenantID: db.tenant, Role: "user"}
				active, err := activeCustomerOwnership(db.ctx, db.db.Platform, platform, f.customerScope.GroupID)
				if err != nil || !active || RequireGroup(db.ctx, db.db.Platform, customer, f.customerScope.GroupID) != nil {
					t.Fatal("ordinary customer ownership changed", active, err)
				}
				active, err = activeCustomerOwnership(db.ctx, db.db.Platform, platform, f.scope.GroupID)
				if err != nil || active {
					t.Fatal("controlled proof became formal customer ownership", active, err)
				}
				if err = RequireHumanGroupOwnership(db.ctx, db.db.Platform, f.actor, platform, f.scope.GroupID); err != nil {
					t.Fatal("own live controlled scope was not admitted", err)
				}
				other, _ := groupBusinessAccessOperator(t, f.assistantBusinessFixture, 115)
				capabilityError(t, RequireControlledBinding(db.ctx, db.db.Platform, other.ID, platform, f.scope.GroupID),
					"controlled_onboarding_scope_inactive")
				// A still-live legacy claim must never be a fallback after close.
				groupBusinessAccessClaim(t, f.assistantBusinessFixture, f.actor, false)
				capabilityError(t, requireOperatorDemoClaim(db.ctx, db.db.Platform, f.actor.ID, platform, f.scope.GroupID, "", nil),
					"controlled_onboarding_legacy_claim_forbidden")
				session, err := f.assistant.CreateSession(db.ctx, f.ref, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				groupBusinessAccessQuery(t, f.assistantBusinessFixture, f.ref, session)
				groupBusinessAccessHistory(t, f.assistantBusinessFixture, f.ref, session)
				var turn string
				if err = db.db.Platform.QueryRow(db.ctx, `SELECT id::text FROM assistant_turns WHERE session_id=$1`, session.ID).Scan(&turn); err != nil {
					t.Fatal(err)
				}
				result, err := f.assistant.propose(db.ctx, f.ref, session, assistantOutcomesConfigCall(t, f.assistantBusinessFixture))
				config, ok := result.Data.(AssistantPlan)
				if err != nil || !ok || config.controlled.ControlledScopeID != f.scopeID {
					t.Fatal("configuration plan lost its scope fence", result, err)
				}
				action := groupBusinessAccessActionPlan(t, f.assistantBusinessFixture, f.ref, session)
				if action.controlled.ControlledScopeID != f.scopeID {
					t.Fatal("native plan lost its scope fence")
				}
				if _, _, err = db.service.scope(db.ctx, db.db.Platform, f.scope.GroupID); err != nil {
					// The initial background scope must be valid before revocation.
					t.Fatal(err)
				}
				switch revoke {
				case "closed":
					db.exec(`UPDATE onboarding_test_scopes SET state='closed',closed_at=clock_timestamp(),version=version+1 WHERE id=$1`, f.scopeID)
				case "claimant_version":
					db.exec(`UPDATE accounts SET version=version+1 WHERE id=(SELECT claimant_account_id FROM onboarding_test_scopes WHERE id=$1)`, f.scopeID)
				case "deadline":
					var expiry time.Time
					if err = db.db.Platform.QueryRow(db.ctx, `SELECT expires_at FROM onboarding_test_scopes WHERE id=$1`, f.scopeID).Scan(&expiry); err != nil {
						t.Fatal(err)
					}
					time.Sleep(max(time.Until(expiry)+10*time.Millisecond, 0))
				}
				for _, gate := range []func() error{
					func() error { return RequireGroup(db.ctx, db.db.Platform, f.actor, f.scope.GroupID) },
					func() error {
						return RequireHumanGroupOwnership(db.ctx, db.db.Platform, f.actor, platform, f.scope.GroupID)
					},
					func() error { return RequireGroupRuntime(db.ctx, db.db.Platform, f.scope.GroupID) },
					func() error {
						_, err := f.assistant.Context(db.ctx, f.ref, f.scope.GroupID, "", 0, 50)
						return err
					},
					func() error { _, err := f.assistant.Transcript(db.ctx, f.ref, session.ID); return err },
					func() error { _, _, err := f.assistant.Events(db.ctx, f.ref, turn, 0); return err },
					func() error {
						_, err := f.assistant.queryTool(db.ctx, f.ref, session,
							assistantOutcomesCall(t, "get_group_config", map[string]string{"group_id": f.scope.GroupID}))
						return err
					},
					func() error {
						_, err := f.assistant.Approve(db.ctx, f.ref, config.ID, config.Digest, config.Version)
						return err
					},
					func() error {
						_, err := f.assistant.ApproveAction(db.ctx, f.ref, action.ID, action.Digest, action.Version, "old-controlled-plan")
						return err
					},
					func() error {
						_, err := groupBusinessAccessSave(t, f.assistantBusinessFixture, f.actor, "fields", 1)
						return err
					},
					func() error {
						bindings := GroupBindingService{DB: db.db, Vault: db.service.Vault, Catalog: f.assistant.Catalog}
						_, err := bindings.SaveBusinessRuntime(db.ctx, f.ref, f.scope.GroupID, true, 1)
						return err
					},
					func() error { _, _, err := db.service.scope(db.ctx, db.db.Platform, f.scope.GroupID); return err },
				} {
					capabilityError(t, gate(), "controlled_onboarding_scope_inactive")
				}
				directory, err := f.assistant.Context(db.ctx, f.ref, "", "", 0, 50)
				if err != nil || bytes.Contains(directory.Groups, []byte(f.scope.GroupID)) {
					t.Fatal("directory exposed an expired controlled group", string(directory.Groups), err)
				}
				if err = RequireGroup(db.ctx, db.db.Platform, customer, f.customerScope.GroupID); err != nil {
					t.Fatal("controlled expiry blocked an ordinary customer", err)
				}
				assertControlledUsage(t, db, f.scopeID, 0, 0, 0)
				if db.count(`SELECT count(*) FROM test_windows`) != 0 || db.count(`SELECT count(*) FROM native_setting_operations`) != 0 {
					t.Fatal("read or approval revocation borrowed test execution")
				}
			})
		}
	}
}

func TestPostgresControlledExecutionScopeVersionInvalidatesSnapshots(t *testing.T) {
	f := newControlledExecutionFixture(t, "wsl", true, 10*time.Minute)
	expected, err := controlledExecutionScope(f.f.ctx, f.f.db.Platform, f.actor.ID, "wsl", f.scope.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.assistant.CreateSession(f.f.ctx, f.ref, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	action := groupBusinessAccessActionPlan(t, f.assistantBusinessFixture, f.ref, session)
	f.f.exec(`UPDATE onboarding_test_scopes SET version=version+1 WHERE id=$1`, f.scopeID)
	capabilityError(t, expected.require(f.f.ctx, f.f.db.Platform, "", "wsl", f.scope.GroupID),
		"controlled_onboarding_scope_changed")
	_, err = f.assistant.ApproveAction(f.f.ctx, f.ref, action.ID, action.Digest, action.Version, "old-scope-version")
	capabilityError(t, err, "controlled_onboarding_scope_changed")
	if err = RequireControlledBinding(f.f.ctx, f.f.db.Platform, f.actor.ID, "wsl", f.scope.GroupID); err != nil {
		t.Fatal("scope version change closed a still-live scope", err)
	}
}

func TestPostgresControlledExecutionNativeFailureKeepsCommittedBudget(t *testing.T) {
	f := newControlledExecutionFixture(t, "wsl", true, 10*time.Minute)
	session, err := f.assistant.CreateSession(f.f.ctx, f.ref, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	action := groupBusinessAccessActionPlan(t, f.assistantBusinessFixture, f.ref, session)
	writes := 0
	f.assistant.ActionExecutor = func(ctx context.Context, ref SessionRef, plan AssistantActionPlan) (SettingOperation, error) {
		req := SettingRequest{Account: f.scope.AccountID, Workspace: "wsl", Key: plan.ID,
			Epoch: plan.AccountEpoch, Digest: strings.Repeat("a", 64),
			Change: plan.settingChange(f.nativeID), business: &assistantBusiness{service: f.assistant, plan: plan}}
		var uid string
		if err := f.f.db.Platform.QueryRow(ctx, `SELECT version,native_uid FROM accounts WHERE id=$1`, req.Account).Scan(&req.Version, &uid); err != nil {
			return SettingOperation{}, err
		}
		return f.native.execute(ctx, ref, req, func(work context.Context, fence SettingFence) error {
			proof := f.proof(req)
			proof.UID = uid
			return fence(work, proof, func(context.Context) error {
				writes++
				assertControlledUsage(t, f.f, f.scopeID, 1, 0, 0)
				return errors.New("synthetic socket outcome unknown")
			})
		}, nil)
	}
	approved, err := f.assistant.ApproveAction(f.f.ctx, f.ref, action.ID, action.Digest, action.Version, "controlled-failed-write")
	if err != nil || writes != 1 || approved.Status != "unknown" {
		t.Fatal("synthetic native failure did not reach the final transaction", approved, writes, err)
	}
	assertControlledUsage(t, f.f, f.scopeID, 1, 0, 0)
	again, err := f.assistant.ApproveAction(f.f.ctx, f.ref, action.ID, action.Digest, approved.Version, "controlled-failed-write")
	if err != nil || again.Status != "unknown" || writes != 1 {
		t.Fatal("unknown submission was replayed", again, writes, err)
	}
	assertControlledUsage(t, f.f, f.scopeID, 1, 0, 0)
}

func TestPostgresControlledExecutionHeatGatewayNoOverride(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			h := newWorkspaceHeatBillingFixture(t, platform, true)
			f := h.base.f
			var receiver string
			if err := f.db.Platform.QueryRow(f.ctx, `SELECT primary_account_id::text FROM group_bindings WHERE group_id=$1`,
				h.scope.Group).Scan(&receiver); err != nil {
				t.Fatal(err)
			}
			id := controlledExecutionProof(t, f, h.base.admin, h.scope.Tenant, platform, receiver, h.scope.Group,
				time.Now().Add(10*time.Minute), true)
			_, token := h.job(t)
			f.exec(`UPDATE business_capability_gates SET state='closed',version=version+1 WHERE platform=$1 AND source='heatbot'`, platform)
			if _, err := h.generate(token); err == nil || h.model.calls != 0 {
				t.Fatal("controlled scope overrode heat production source admission", h.model.calls, err)
			}
			assertControlledUsage(t, f, id, 0, 0, 0)
			f.exec(`UPDATE business_capability_gates SET state='open',version=version+1 WHERE platform=$1 AND source='heatbot'`, platform)
			_, token = h.job(t)
			if _, err := h.generate(token); err != nil || h.model.calls != 1 {
				t.Fatal("live controlled model call failed", h.model.calls, err)
			}
			h.model.usage = assistant.Usage{}
			_, token = h.job(t)
			if _, err := h.generate(token); err != nil || h.model.calls != 2 {
				t.Fatal("unknown usage was not retained", h.model.calls, err)
			}
			n, m, held, err := ReadControlledUsage(f.ctx, f.db.Platform, id)
			if err != nil || n != 0 || m != 2 || held < 1 || held > 20_000 {
				t.Fatal("gateway used the wrong currency or refunded unknown usage", n, m, held, err)
			}
			_, token = h.job(t)
			_, err = h.generate(token)
			capabilityError(t, err, "controlled_onboarding_model_budget_exhausted")
			if h.model.calls != 2 {
				t.Fatal("third controlled generation reached the model")
			}
			f.exec(`UPDATE onboarding_test_scopes SET state='closed',closed_at=clock_timestamp(),version=version+1 WHERE id=$1`, id)
			_, err = h.heat.heatScope(f.ctx, f.db.Platform, h.scope.Group, true)
			capabilityError(t, err, "controlled_onboarding_scope_inactive")
			if f.count(`SELECT count(*) FROM test_windows`) != 0 || f.count(`SELECT count(*) FROM heat_chat_send_attempts`) != 0 {
				t.Fatal("background generation acquired native test execution")
			}
		})
	}
}
