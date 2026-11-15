package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func TestOperatorEntitlementRequiresLiveServerActor(t *testing.T) {
	admin := Actor{ID: NewID(), Role: "superadmin"}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		q := &capabilityQueries{t: t, rows: []capabilityRow{{values: []any{true}}}}
		e, err := ResolveHumanEntitlement(context.Background(), q, admin, BusinessPermissionCatalog(), platform, "")
		if err != nil || !e.OperatorDemo || !e.Active || e.Mode != "operator_demo" ||
			e.BillingSource != "platform_demo" || e.TenantID != "" || q.calls != 1 ||
			!slices.Contains(e.Permissions, "group_action:mute_member") ||
			e.RequireModule("management_assistant") != nil {
			t.Fatal("live operator entitlement", e, err)
		}
		q = &capabilityQueries{t: t, rows: []capabilityRow{{err: pgx.ErrNoRows}}}
		_, err = ResolveHumanEntitlement(context.Background(), q, admin, BusinessPermissionCatalog(), platform, "")
		capabilityError(t, err, "permission_denied")
	}
	for _, platform := range []string{"", "maibot", "operator_demo"} {
		q := &capabilityQueries{t: t}
		_, err := ResolveHumanEntitlement(context.Background(), q, admin, BusinessPermissionCatalog(), platform, "")
		capabilityError(t, err, "permission_denied")
	}
}

func TestOperatorHumanAdmissionCannotBeUsedByBackgroundSources(t *testing.T) {
	for _, source := range []string{"automatic", "heatbot", "native_debug", "test", ""} {
		q := &capabilityQueries{t: t}
		err := RequireHumanBusinessAdmission(context.Background(), q, Actor{ID: NewID(), Role: "superadmin"},
			BusinessPermissionCatalog(), "wsl", NewID(), "mute_member", source)
		capabilityError(t, err, "invalid_business_source")
	}
}

func TestOperatorConfigToolRejectsClientEntitlementFields(t *testing.T) {
	session := AssistantSession{ID: NewID(), Scope: assistant.Scope{
		Platform: "wsl", Kind: "group", GroupID: NewID(), AccountID: NewID(),
	}}
	for _, field := range []string{"execution_mode", "billing_source", "unlimited", "actor_id"} {
		args, err := json.Marshal(map[string]any{
			"group_id": session.Scope.GroupID, "base_version": 1,
			"changes": map[string]any{"blocked_words": map[string]any{"words": []string{"synthetic"}}},
			field:     "operator_demo",
		})
		if err != nil {
			t.Fatal(err)
		}
		call := assistant.ToolCall{}
		call.Function.Arguments = string(args)
		// Rejection precedes database access, even when the model invents fields.
		if _, err = (&AssistantService{}).propose(context.Background(), SessionRef{}, session, call); err == nil {
			t.Fatal("model selected its execution identity", field)
		}
	}
}

type operatorEntitlementFixture struct {
	base        *assistantBusinessFixture
	admin       Actor
	ref         SessionRef
	manual      *ManualActionService
	writes      atomic.Int32
	beforeWrite func()
	proofChange func(*SettingEvidence)
}

func newOperatorEntitlementFixture(t *testing.T, platform string) *operatorEntitlementFixture {
	t.Helper()
	base := newAssistantBusinessFixture(t, platform)
	f := base.f
	out := &operatorEntitlementFixture{base: base, admin: Actor{ID: NewID(), Role: "superadmin"}}
	f.exec(`INSERT INTO users(id,username,password_hash,role)
		VALUES($1,'synthetic-entitlement-operator','unused-synthetic','superadmin')`, out.admin.ID)
	out.ref = out.session(out.admin, 61)
	gateway := &GatewayService{DB: f.db, Vault: f.service.Vault, Catalog: BusinessPermissionCatalog()}
	if err := gateway.Configure(f.ctx, out.admin, GatewaySettings{Enabled: true, PlatformLimit: 100_000_000},
		ModelRoute{Alias: "default", Enabled: true, Verified: true,
			Rates: aigateway.Rates{Input: 1_000_000, Output: 1_000_000},
			Model: assistant.ModelConfig{BaseURL: "https://synthetic.invalid/v1", Model: "synthetic", APIKey: "synthetic-no-network"}}); err != nil {
		t.Fatal(err)
	}
	base.assistant.Gateway = gateway
	out.manual = &ManualActionService{DB: f.db, Vault: f.service.Vault, Catalog: BusinessPermissionCatalog()}
	out.manual.NativeMembers = func(ctx context.Context, gotPlatform, account, group, search string) ([]AssistantMember, error) {
		if gotPlatform != platform || account != base.scope.AccountID || group != base.nativeID || search != "synthetic member" {
			return nil, Denied(403, "synthetic_scope_mismatch")
		}
		return []AssistantMember{{ID: "30001", Name: "synthetic member", Role: "member"}}, nil
	}
	out.manual.Executor = func(ctx context.Context, ref SessionRef, plan AssistantActionPlan) (SettingOperation, error) {
		change, err := base.native.ManualChange(ctx, ref, out.manual, plan.Scope.GroupID, plan.ID)
		if err != nil {
			return SettingOperation{}, err
		}
		req := SettingRequest{Account: plan.Scope.AccountID, Workspace: platform, Key: plan.ID,
			Epoch: plan.AccountEpoch, Digest: strings.Repeat("c", 64), Change: change}
		if err = f.db.Platform.QueryRow(ctx, `SELECT version FROM accounts WHERE id=$1`, req.Account).Scan(&req.Version); err != nil {
			return SettingOperation{}, err
		}
		return base.native.ExecuteManual(ctx, ref, out.manual, plan.Scope.GroupID, plan.ID, req,
			func(work context.Context, fence SettingFence) error {
				if out.beforeWrite != nil {
					out.beforeWrite()
				}
				proof := base.proof(req)
				if out.proofChange != nil {
					out.proofChange(&proof)
				}
				return fence(work, proof, func(context.Context) error { out.writes.Add(1); return nil })
			})
	}
	return out
}

func (o *operatorEntitlementFixture) session(a Actor, marker byte) SessionRef {
	o.base.f.t.Helper()
	ref := SessionRef{hash: bytes.Repeat([]byte{marker}, 32), platform: o.base.scope.Platform}
	o.base.f.exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,clock_timestamp()+interval '1 hour')`, ref.hash, bytes.Repeat([]byte{marker + 1}, 32), a.ID)
	return ref
}

func (o *operatorEntitlementFixture) gate(source, state string) {
	o.base.f.t.Helper()
	o.base.f.exec(`INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
		VALUES($1,'mute_member',$2,$3,ARRAY['acceptance:SYNTHETIC-OPERATOR'])
		ON CONFLICT(platform,action,source) DO UPDATE SET state=excluded.state`,
		o.base.scope.Platform, source, state)
}

func (o *operatorEntitlementFixture) mutePlan() AssistantActionPlan {
	o.base.f.t.Helper()
	items, err := o.manual.Targets(o.base.f.ctx, o.ref, o.base.scope.GroupID, "mute_member", "synthetic member")
	if err != nil || len(items) != 1 || items[0].Token == "" {
		o.base.f.t.Fatal("synthetic target selection", items, err)
	}
	p, err := o.manual.Create(o.base.f.ctx, o.ref, o.base.scope.GroupID, "mute_member", items[0].Token, "", 1)
	if err != nil {
		o.base.f.t.Fatal(err)
	}
	return p
}

func TestPostgresOperatorEntitlementConfigAndClosedFormalAdmission(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			o := newOperatorEntitlementFixture(t, platform)
			b, f := o.base, o.base.f
			f.exec(`DELETE FROM subscriptions WHERE tenant_id=$1`, f.tenant)
			f.exec(`UPDATE tenant_capability_ceilings SET permissions='{}' WHERE tenant_id=$1`, f.tenant)
			f.exec(`UPDATE group_business_runtime SET enabled=false WHERE group_id=$1`, b.scope.GroupID)
			f.exec(`UPDATE group_bindings SET management_enabled=false,ai_enabled=false WHERE group_id=$1`, b.scope.GroupID)
			f.exec(`INSERT INTO account_groups(account_id,group_id,role,observation_source,observed_at)
				VALUES($1,$2,'admin','native',clock_timestamp())`, b.scope.AccountID, b.scope.GroupID)
			e, err := ResolveHumanEntitlement(f.ctx, f.db.Platform, o.admin, BusinessPermissionCatalog(), platform, b.scope.GroupID)
			if err != nil || !e.OperatorDemo || !e.Active || e.TenantID != f.tenant {
				t.Fatal("operator inherited customer entitlement", e, err)
			}
			session, err := b.assistant.CreateSession(f.ctx, o.ref, b.scope)
			if err != nil {
				t.Fatal("demo session required a customer package", err)
			}
			var version int64
			if err = f.db.Platform.QueryRow(f.ctx, `SELECT version FROM group_bindings WHERE group_id=$1`,
				b.scope.GroupID).Scan(&version); err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal(map[string]any{"group_id": b.scope.GroupID, "base_version": version,
				"changes": map[string]any{"blocked_words": map[string]any{"words": []string{"synthetic-demo-only"}}}})
			call := assistant.ToolCall{}
			call.Function.Arguments = string(args)
			result, err := b.assistant.propose(f.ctx, o.ref, session, call)
			if err != nil {
				t.Fatal("demo suggestion required customer ceiling/runtime", err)
			}
			p, ok := result.Data.(AssistantPlan)
			if !ok || p.ExecutionMode != "operator_demo" || p.Status != "pending_approval" {
				t.Fatal("config plan lost human execution mode", result.Data)
			}
			saved, err := b.assistant.Approve(f.ctx, o.ref, p.ID, p.Digest, p.Version)
			if err != nil || saved.Status != "committed" || saved.Applied || saved.CommittedVersion != version+1 {
				t.Fatal("operator config approval", saved, err)
			}
			var raw []byte
			if err = f.db.Platform.QueryRow(f.ctx, `SELECT config FROM group_bindings WHERE group_id=$1`,
				b.scope.GroupID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var config features.Config
			if err = json.Unmarshal(raw, &config); err != nil || !slices.Equal(config.BlockedWords, []string{"synthetic-demo-only"}) {
				t.Fatal("suggestion did not save its selected field", err)
			}
			if f.count(`SELECT count(*) FROM group_business_runtime WHERE group_id=$1 AND NOT enabled`, b.scope.GroupID) != 1 ||
				f.count(`SELECT count(*) FROM group_bindings WHERE group_id=$1 AND NOT management_enabled AND NOT ai_enabled`, b.scope.GroupID) != 1 ||
				f.count(`SELECT count(*) FROM subscriptions WHERE tenant_id=$1`, f.tenant) != 0 ||
				f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 {
				t.Fatal("human config enabled customer business or manufactured a subscription")
			}
			o.gate("manual", "closed")
			manual := o.mutePlan()
			if manual.ExecutionMode != "operator_demo" || manual.Status != "blocked" ||
				manual.Production || manual.ResultCode != "business_admission_closed" {
				t.Fatal("closed gate did not produce a reviewable blocked plan", manual)
			}
			_, err = o.manual.Approve(f.ctx, o.ref, b.scope.GroupID, manual.ID, manual.Digest, manual.Version, "synthetic-closed-gate")
			capabilityError(t, err, "business_admission_closed")
			if o.writes.Load() != 0 || f.count(`SELECT count(*) FROM native_setting_operations`) != 0 ||
				f.count(`SELECT count(*) FROM manual_action_plans WHERE id=$1 AND idempotency_key IS NULL AND status='blocked'`, manual.ID) != 1 {
				t.Fatal("demo bypassed closed formal admission")
			}
			customer := Actor{ID: f.actor, TenantID: f.tenant, Role: "user"}
			customerEntitlement, err := ResolveHumanEntitlement(f.ctx, f.db.Platform, customer, BusinessPermissionCatalog(), platform, b.scope.GroupID)
			if err != nil || customerEntitlement.Active || customerEntitlement.OperatorDemo ||
				customerEntitlement.Mode != "tenant_business" || customerEntitlement.BillingSource != "tenant_wallet" {
				t.Fatal("customer inherited the operator demo", customerEntitlement, err)
			}
			if _, err = b.assistant.CreateSession(f.ctx, b.ref, b.scope); err == nil {
				t.Fatal("customer without assistant entitlement created a session")
			}
		})
	}
}

func TestPostgresOperatorManualPlanDurabilityAndIsolation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			o := newOperatorEntitlementFixture(t, platform)
			b, f := o.base, o.base.f
			f.exec(`DELETE FROM subscriptions WHERE tenant_id=$1`, f.tenant)
			f.exec(`UPDATE tenant_capability_ceilings SET permissions='{}' WHERE tenant_id=$1`, f.tenant)
			f.exec(`UPDATE group_business_runtime SET enabled=false WHERE group_id=$1`, b.scope.GroupID)
			f.exec(`UPDATE group_bindings SET management_enabled=false WHERE group_id=$1`, b.scope.GroupID)
			o.gate("manual", "open")
			sessions := f.count(`SELECT count(*) FROM assistant_sessions`)
			p := o.mutePlan()
			if !p.Production || p.Status != "pending_approval" || p.SessionID != "" ||
				p.ExecutionMode != "operator_demo" || p.Scope != b.scope {
				t.Fatal("page plan borrowed assistant/test scope", p)
			}
			otherAdmin := Actor{ID: NewID(), Role: "superadmin"}
			f.exec(`INSERT INTO users(id,username,password_hash,role)
				VALUES($1,'synthetic-other-operator','unused-synthetic','superadmin')`, otherAdmin.ID)
			otherRef := o.session(otherAdmin, 63)
			if _, err := o.manual.Approve(f.ctx, otherRef, b.scope.GroupID, p.ID, p.Digest, p.Version, "synthetic-other-person"); err == nil {
				t.Fatal("another operator confirmed someone else's plan")
			}
			otherGroup := f.group(platform, b.scope.AccountID, f.tenant, 2, false).Group
			if _, err := o.manual.Plan(f.ctx, o.ref, otherGroup, p.ID); err == nil {
				t.Fatal("replaced group ID disclosed a plan")
			}
			if _, err := o.manual.Plan(f.ctx, o.ref.InPlatform(otherOperatorPlatform(platform)), b.scope.GroupID, p.ID); err == nil {
				t.Fatal("cross-platform plan was disclosed")
			}
			succeeded, err := o.manual.Approve(f.ctx, o.ref, b.scope.GroupID, p.ID, p.Digest, p.Version, "synthetic-own-confirmation")
			if err != nil || succeeded.Status != "succeeded" || succeeded.OperationID == "" || o.writes.Load() != 1 {
				t.Fatal("manual plan did not use durable native execution", succeeded, err)
			}
			again, err := o.manual.Approve(f.ctx, o.ref, b.scope.GroupID, p.ID, p.Digest, p.Version, "synthetic-own-confirmation")
			if err != nil || again.OperationID != succeeded.OperationID || again.Status != "succeeded" || o.writes.Load() != 1 {
				t.Fatal("duplicate confirmation resent the action", again, err)
			}
			_, err = o.manual.Approve(f.ctx, o.ref, b.scope.GroupID, p.ID, p.Digest, again.Version, "synthetic-different-key")
			capabilityError(t, err, "idempotency_conflict")
			if f.count(`SELECT count(*) FROM assistant_sessions`) != sessions ||
				f.count(`SELECT count(*) FROM assistant_action_plans`) != 0 ||
				f.count(`SELECT count(*) FROM test_windows`) != 0 ||
				f.count(`SELECT count(*) FROM native_setting_operations WHERE id=$1 AND test_window_id IS NULL`, succeeded.OperationID) != 1 {
				t.Fatal("manual execution created assistant or controlled-test authorization")
			}
			p = o.mutePlan()
			f.exec(`UPDATE group_bindings SET version=version+1,epoch=epoch+1 WHERE group_id=$1`, b.scope.GroupID)
			if _, err = o.manual.Approve(f.ctx, o.ref, b.scope.GroupID, p.ID, p.Digest, p.Version, "synthetic-stale-binding"); err == nil {
				t.Fatal("changed binding version accepted a stale plan")
			}
			if o.writes.Load() != 1 || f.count(`SELECT count(*) FROM native_setting_operations`) != 1 {
				t.Fatal("stale binding reached native submission")
			}
		})
	}
}

func otherOperatorPlatform(platform string) string {
	if platform == "wsl" {
		return "lajiaoliao"
	}
	return "wsl"
}

func TestPostgresOperatorManualFinalFenceRechecksLiveScope(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"gate", "actor", "binding"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				o := newOperatorEntitlementFixture(t, platform)
				b, f := o.base, o.base.f
				o.gate("manual", "open")
				p := o.mutePlan()
				o.beforeWrite = func() {
					switch scenario {
					case "gate":
						o.gate("manual", "closed")
					case "actor":
						f.exec(`UPDATE users SET disabled=true WHERE id=$1`, o.admin.ID)
					case "binding":
						f.exec(`UPDATE group_bindings SET version=version+1,epoch=epoch+1 WHERE group_id=$1`, b.scope.GroupID)
					}
				}
				_, _ = o.manual.Approve(f.ctx, o.ref, b.scope.GroupID, p.ID, p.Digest, p.Version, "synthetic-final-fence")
				if o.writes.Load() != 0 ||
					f.count(`SELECT count(*) FROM native_setting_operations WHERE kind='mute_member' AND status='failed'`) != 1 ||
					f.count(`SELECT count(*) FROM manual_action_plans WHERE id=$1 AND status='failed'`, p.ID) != 1 {
					t.Fatal("live revocation crossed the final synthetic write fence", scenario)
				}
			})
		}
	}
}

func TestPostgresOperatorDemoDoesNotEntitleAutomaticBusiness(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			o := newOperatorEntitlementFixture(t, platform)
			b, f := o.base, o.base.f
			group := b.scope.GroupID
			o.gate("manual", "open")
			strict := func() error {
				return RequireBusinessAdmission(f.ctx, f.db.Platform, platform, group, "mute_member", "automatic")
			}
			human := func() {
				t.Helper()
				if err := RequireHumanBusinessAdmission(f.ctx, f.db.Platform, o.admin, BusinessPermissionCatalog(),
					platform, group, "mute_member", "manual"); err != nil {
					t.Fatal("operator demo inherited customer runtime", err)
				}
			}
			if err := strict(); err != nil {
				t.Fatal("synthetic automatic baseline not entitled", err)
			}
			f.exec(`UPDATE group_business_runtime SET enabled=false WHERE group_id=$1`, group)
			human()
			capabilityError(t, strict(), "group_runtime_disabled")
			f.exec(`UPDATE group_business_runtime SET enabled=true WHERE group_id=$1`, group)
			f.exec(`UPDATE subscriptions SET enabled=false WHERE tenant_id=$1`, f.tenant)
			human()
			capabilityError(t, strict(), "active_subscription_required")
			f.exec(`UPDATE subscriptions SET enabled=true WHERE tenant_id=$1`, f.tenant)
			f.exec(`UPDATE tenant_capability_ceilings SET permissions='{}' WHERE tenant_id=$1`, f.tenant)
			human()
			capabilityError(t, strict(), "tenant_capability_denied")
			scope, config, err := f.service.scope(f.ctx, f.db.Platform, group)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.service.authorizeAction(f.ctx, f.db.Platform, scope, config, "blacklist", "mute_member"); err == nil {
				t.Fatal("automatic worker bypassed the strict ceiling")
			}
			if o.writes.Load() != 0 || f.count(`SELECT count(*) FROM native_setting_operations`) != 0 {
				t.Fatal("entitlement resolution executed background work")
			}
		})
	}
}

func TestPostgresOperatorTargetChoiceRemainsScopedAndSealedAcrossTurns(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			o := newOperatorEntitlementFixture(t, platform)
			b, f := o.base, o.base.f
			b.assistant.NativeMembers = func(ctx context.Context, platform, account, group, search string) ([]AssistantMember, error) {
				members, err := o.manual.NativeMembers(ctx, platform, account, group, search)
				if err != nil {
					return nil, err
				}
				return append(members, AssistantMember{ID: "30002", Name: "synthetic member", Role: "member"}), nil
			}
			session, err := b.assistant.CreateSession(f.ctx, o.ref, b.scope)
			if err != nil {
				t.Fatal(err)
			}
			call := assistant.ToolCall{}
			call.Function.Name, call.Function.Arguments = "find_group_members", `{"search":"synthetic member"}`
			result, err := b.assistant.actionTool(f.ctx, o.ref, session, call)
			if err != nil {
				t.Fatal(err)
			}
			members, ok := result.Data.([]AssistantMember)
			if !ok || len(members) != 2 || members[0].Token == "" || members[1].Token == "" {
				t.Fatal("synthetic candidate did not return a scoped choice")
			}
			token := members[0].Token
			otherGroup := f.group(platform, b.scope.AccountID, f.tenant, 2, false)
			otherSession, err := b.assistant.CreateSession(f.ctx, o.ref, assistant.Scope{
				Platform: platform, Kind: "group", AccountID: b.scope.AccountID, GroupID: otherGroup.Group,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, created, err := b.assistant.CreateTurnWithTarget(f.ctx, o.ref, otherSession.ID,
				"synthetic-cross-group-choice", "synthetic request", token)
			if err == nil || created {
				t.Fatal("a target choice crossed group scope")
			}
			sealed, err := base64.RawURLEncoding.DecodeString(token)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := b.assistant.Vault.Open("assistant:target", sealed)
			if err != nil {
				t.Fatal(err)
			}
			var stale assistantTarget
			if err = json.Unmarshal(raw, &stale); err != nil {
				t.Fatal(err)
			}
			clear(raw)
			stale.ExpiresAt = time.Now().Add(-time.Second)
			raw, _ = json.Marshal(stale)
			sealed, err = b.assistant.Vault.Seal("assistant:target", raw)
			clear(raw)
			if err != nil {
				t.Fatal(err)
			}
			_, created, err = b.assistant.CreateTurnWithTarget(f.ctx, o.ref, session.ID,
				"synthetic-expired-choice", "synthetic request", base64.RawURLEncoding.EncodeToString(sealed))
			if err == nil || created || f.count(`SELECT count(*) FROM assistant_turns`) != 0 {
				t.Fatal("expired or foreign choice queued model work")
			}
			chosen, err := b.assistant.ChooseTarget(f.ctx, o.ref, session.ID, token)
			if err != nil || chosen.ID != members[0].ID || chosen.Token != token {
				t.Fatal("human selection did not confirm the exact candidate", chosen, err)
			}
			turn, created, err := b.assistant.CreateTurnWithTarget(f.ctx, o.ref, session.ID,
				"synthetic-valid-choice", "synthetic request", token)
			if err != nil || !created {
				t.Fatal("verified choice did not enter the next turn", turn, err)
			}
			var stored []byte
			if err = f.db.Platform.QueryRow(f.ctx, `SELECT target_sealed FROM assistant_turns WHERE id=$1`, turn.ID).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if len(stored) == 0 || bytes.Contains(stored, []byte(token)) {
				t.Fatal("turn persisted an unsealed selection")
			}
			raw, err = b.assistant.Vault.Open("assistant:turn-target:"+turn.ID, stored)
			if err != nil || string(raw) != token {
				t.Fatal("turn did not seal the exact confirmed token", err)
			}
			clear(raw)
			host, _, history, _, err := b.assistant.Host(f.ctx, o.ref, turn.ID)
			if err != nil || host == nil || host.selectedTarget != token {
				t.Fatal("host lost the selected target", err)
			}
			var choice struct {
				Member AssistantMember `json:"selected_target"`
				Token  string          `json:"target_token"`
			}
			if len(history) == 0 || json.Unmarshal([]byte(history[len(history)-1].Content), &choice) != nil ||
				choice.Member.ID != members[0].ID || choice.Token != token {
				t.Fatal("next-turn context did not carry the structured target")
			}
			for _, replacement := range []string{"synthetic-forged-selection", members[1].Token} {
				args, _ := json.Marshal(map[string]any{
					"action": "mute_member", "minutes": 1, "target_token": replacement,
				})
				call.Function.Name, call.Function.Arguments = "propose_group_action", string(args)
				_, err = host.Call(f.ctx, call)
				capabilityError(t, err, "assistant_target_choice_required")
			}
			if f.count(`SELECT count(*) FROM assistant_action_plans`) != 0 {
				t.Fatal("a substituted selection persisted an action plan")
			}
			for _, args := range []map[string]any{
				{"action": "mute_member", "minutes": 1},
				{"action": "unmute_member"},
				{"action": "kick_member"},
				{"action": "member_nickname", "text": "synthetic-card"},
			} {
				body, _ := json.Marshal(args)
				call.Function.Name, call.Function.Arguments = "propose_group_action", string(body)
				result, err = host.Call(f.ctx, call)
				if err != nil {
					t.Fatal("confirmed target was not injected", args["action"], err)
				}
				plan, ok := result.Data.(AssistantActionPlan)
				if !ok || plan.Action != args["action"] || plan.SessionID != session.ID || plan.Scope != b.scope ||
					plan.Target == nil || plan.Target.ID != chosen.ID || plan.Target.Token != "" || plan.OperationID != "" {
					t.Fatal("action plan lost its confirmed target or scope", args["action"], plan)
				}
				saved, err := b.assistant.ActionPlan(f.ctx, o.ref, plan.ID)
				if err != nil || saved.Target == nil || saved.Target.ID != chosen.ID || saved.Digest != plan.Digest {
					t.Fatal("persisted suggestion changed the confirmed target", saved, err)
				}
			}
			for _, action := range []string{"send_text", "publish_notice"} {
				args, _ := json.Marshal(map[string]any{"action": action, "text": "synthetic target-free text"})
				call.Function.Name, call.Function.Arguments = "propose_group_action", string(args)
				result, err = host.Call(f.ctx, call)
				if err != nil {
					t.Fatal("target-free action inherited the selected member", action, err)
				}
				plan, ok := result.Data.(AssistantActionPlan)
				if !ok || plan.Action != action || plan.Target != nil || plan.Text != "synthetic target-free text" ||
					plan.Scope != b.scope || plan.OperationID != "" || host.selectedTarget != token {
					t.Fatal("target-free action gained a target or changed the selection", plan)
				}
			}
			if err = b.assistant.FinishTurn(f.ctx, turn.ID, nil); err != nil {
				t.Fatal(err)
			}
			late, created, err := b.assistant.CreateTurnWithTarget(f.ctx, o.ref, session.ID,
				"synthetic-late-choice", "synthetic next request", token)
			if err != nil || !created {
				t.Fatal(err)
			}
			f.exec(`UPDATE group_bindings SET version=version+1,epoch=epoch+1 WHERE group_id=$1`, b.scope.GroupID)
			if host, _, _, _, err = b.assistant.Host(f.ctx, o.ref, late.ID); err == nil || host != nil {
				t.Fatal("late selection survived a binding change")
			}
			if f.count(`SELECT count(*) FROM ai_calls`) != 0 ||
				f.count(`SELECT count(*) FROM native_setting_operations`) != 0 ||
				f.count(`SELECT count(*) FROM test_windows`) != 0 {
				t.Fatal("choice validation called a model or native transport")
			}
		})
	}
}

func TestPostgresOperatorMessageChoiceFeedsRecallWithoutModelTarget(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			o := newOperatorEntitlementFixture(t, platform)
			b, f := o.base, o.base.f
			b.assistant.NativeMessages = func(_ context.Context, gotPlatform, account, group, search string) ([]AssistantMessage, error) {
				if gotPlatform != platform || account != b.scope.AccountID || group != b.nativeID || search != "synthetic recall" {
					return nil, Denied(403, "synthetic_scope_mismatch")
				}
				out := make([]AssistantMessage, 0, 2)
				for _, event := range []int64{71, 72} {
					reference := nativeauth.MessageReference{
						Platform: platform, EventID: event, Group: group, Sender: "30001",
						SourceDigest: strings.Repeat("a", 64), CreatedAt: time.Now().Add(-time.Second).UnixMilli(),
					}
					if platform == "wsl" {
						reference.Route = nativewire.ReplyRoute{
							Sender: reference.Sender, Target: group, TimeMS: strconv.FormatInt(reference.CreatedAt, 10),
							ClientID: NewID(), ServerID: strconv.FormatInt(9000+event, 10),
						}
					} else {
						reference.Conversation, reference.Sequence = "sg_"+group, event
					}
					out = append(out, AssistantMessage{
						Member:    AssistantMember{ID: strconv.FormatInt(event, 10), Name: "synthetic message", Role: "message"},
						Reference: reference,
					})
				}
				return out, nil
			}
			session, err := b.assistant.CreateSession(f.ctx, o.ref, b.scope)
			if err != nil {
				t.Fatal(err)
			}
			call := assistant.ToolCall{}
			call.Function.Name, call.Function.Arguments = "find_group_messages", `{"search":"synthetic recall"}`
			result, err := b.assistant.actionTool(f.ctx, o.ref, session, call)
			if err != nil {
				t.Fatal(err)
			}
			messages, ok := result.Data.([]AssistantMember)
			if !ok || len(messages) != 2 || messages[0].Token == "" || messages[1].Token == "" {
				t.Fatal("message candidates were not scoped")
			}
			chosen, err := b.assistant.ChooseTarget(f.ctx, o.ref, session.ID, messages[0].Token)
			if err != nil || chosen.ID != "71" {
				t.Fatal("human message selection", chosen, err)
			}
			turn, created, err := b.assistant.CreateTurnWithTarget(f.ctx, o.ref, session.ID,
				"synthetic-selected-message-turn", "synthetic recall request", messages[0].Token)
			if err != nil || !created {
				t.Fatal("confirmed message did not enter the next turn", turn, err)
			}
			host, _, _, _, err := b.assistant.Host(f.ctx, o.ref, turn.ID)
			if err != nil || host == nil || host.selectedTarget != messages[0].Token {
				t.Fatal("next-turn host lost the selected message", err)
			}
			replaced, _ := json.Marshal(map[string]any{"action": "recall_message", "target_token": messages[1].Token})
			call.Function.Name, call.Function.Arguments = "propose_group_action", string(replaced)
			_, err = host.Call(f.ctx, call)
			capabilityError(t, err, "assistant_target_choice_required")
			foreignGroup := f.group(platform, b.scope.AccountID, f.tenant, 2, false)
			foreignSession, err := b.assistant.CreateSession(f.ctx, o.ref, assistant.Scope{
				Platform: platform, Kind: "group", AccountID: b.scope.AccountID, GroupID: foreignGroup.Group,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = b.assistant.ChooseTarget(f.ctx, o.ref, foreignSession.ID, messages[0].Token)
			// The unclaimed destination is read-only before any candidate can be used.
			capabilityError(t, err, "operator_demo_claim_required")
			if f.count(`SELECT count(*) FROM assistant_action_plans`) != 0 {
				t.Fatal("foreign or substituted message persisted a recall plan")
			}
			call.Function.Arguments = `{"action":"recall_message"}`
			result, err = host.Call(f.ctx, call)
			if err != nil {
				t.Fatal("recall did not use the confirmed message when the model omitted its token", err)
			}
			plan, ok := result.Data.(AssistantActionPlan)
			if !ok || plan.Action != "recall_message" || plan.Scope != b.scope || plan.Target == nil ||
				plan.Target.ID != chosen.ID || plan.Target.Token != "" || plan.message == nil || plan.message.EventID != 71 ||
				plan.message.Platform != platform || plan.message.Group != b.nativeID || plan.OperationID != "" {
				t.Fatal("recall plan changed the confirmed native reference", plan)
			}
			saved, err := b.assistant.ActionPlan(f.ctx, o.ref, plan.ID)
			if err != nil || saved.message == nil || *saved.message != *plan.message || saved.Digest != plan.Digest {
				t.Fatal("persisted recall lost its sealed reference", saved, err)
			}
			if err = b.assistant.FinishTurn(f.ctx, turn.ID, nil); err != nil {
				t.Fatal(err)
			}
			if f.count(`SELECT count(*) FROM ai_calls`) != 0 ||
				f.count(`SELECT count(*) FROM native_setting_operations`) != 0 ||
				f.count(`SELECT count(*) FROM test_windows`) != 0 {
				t.Fatal("synthetic selection regression generated paid or native work")
			}
		})
	}
}
