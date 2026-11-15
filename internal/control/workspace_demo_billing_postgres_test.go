package control

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
)

type workspaceHeatModel struct {
	t      *testing.T
	calls  int
	usage  assistant.Usage
	before func()
}

func (p *workspaceHeatModel) Generate(ctx context.Context, _ []assistant.Message, tools []assistant.Tool,
	_ func(string) error) (assistant.Response, error) {
	if err := ctx.Err(); err != nil {
		return assistant.Response{}, err
	}
	if len(tools) != 0 {
		p.t.Fatal("heat service acquired model tools")
	}
	p.calls++
	if p.before != nil {
		p.before()
	}
	return assistant.Response{Message: assistant.Message{Role: "assistant", Content: "synthetic heat response"},
		Usage: p.usage}, nil
}

type workspaceHeatBillingFixture struct {
	base  *operatorGatewayFixture
	heat  *HeatChatService
	scope HeatScope
	model *workspaceHeatModel
	event int64
}

func newWorkspaceHeatBillingFixture(t *testing.T, platform string, demo bool) *workspaceHeatBillingFixture {
	t.Helper()
	g := newOperatorGatewayFixture(t)
	f := g.f
	tenant, group := f.tenant, g.group[platform]
	if demo {
		space, err := OperatorWorkspace(f.ctx, f.db.Platform, g.admin.ID)
		if err != nil {
			t.Fatal(err)
		}
		tenant = space.ID
		group = f.group(platform, f.account(platform, 3), tenant, 3, false).Group
	}
	account := NewID()
	f.exec(`INSERT INTO accounts(id,name,platform,purpose,workspace,auth_state,credential_sealed,
		native_uid,connection_enabled) VALUES($1,$1::uuid::text,$2,'chat','maibot','authenticated',$3,$1::uuid::text,false)`,
		account, platform, []byte("synthetic-chat-credential"))
	f.exec(`INSERT INTO personas(account_id,nickname,personality) VALUES($1,'synthetic','scope-bound persona')`, account)
	f.exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
		VALUES($1,$2,'member',clock_timestamp(),'native')`, account, group)
	f.exec(`INSERT INTO account_receivers(account_id,enabled,owner,revision,lease_until,observed_at,snapshot)
		VALUES($1,true,'synthetic-heat-owner',1,clock_timestamp()+interval '10 minutes',clock_timestamp(),'{"state":"connected"}')`, account)
	sealed, err := g.gateway.Vault.Seal("maibot:"+account+":chat",
		[]byte(`{"enabled":true,"reply_probability":0,"cooldown_seconds":30,"start_hour":0,"end_hour":24}`))
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO maibot_settings(account_id,section,sealed,version,updated_by)
		VALUES($1,'chat',$2,1,$3)`, account, sealed, g.admin.ID)
	f.exec(`UPDATE group_bindings SET ai_account_id=$2,ai_requested=true,ai_enabled=true WHERE group_id=$1`, group, account)
	f.exec(`UPDATE group_business_runtime SET enabled=true,version=version+1 WHERE group_id=$1`, group)
	f.exec(`UPDATE heat_chat_admission SET enabled=true,version=version+1 WHERE platform=$1`, platform)
	f.exec(`INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
		VALUES($1,'send_text','heatbot','open',ARRAY['acceptance:SYNTHETIC-WORKSPACE-HEAT'])`, platform)
	g.gateway.Scheduler = aigateway.NewScheduler()
	h := &workspaceHeatBillingFixture{base: g, heat: &HeatChatService{DB: f.db, Vault: g.gateway.Vault, Gateway: g.gateway},
		model: &workspaceHeatModel{t: t, usage: assistant.Usage{PromptTokens: 8, CompletionTokens: 1, TotalTokens: 9}}}
	g.gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) { return h.model, nil }
	h.scope, err = h.heat.heatScope(f.ctx, f.db.Platform, group, true)
	if err != nil || h.scope.Tenant != tenant || h.scope.Service.Demo != demo {
		t.Fatal("synthetic service scope unavailable", h.scope, err)
	}
	return h
}

// These helpers exercise real grant/task and billing transactions. The model
// provider is in-process synthetic code; no native send port is installed.
func (h *workspaceHeatBillingFixture) job(t *testing.T) (string, string) {
	t.Helper()
	f := h.base.f
	f.exec(`UPDATE heat_chat_jobs SET status='failed',result_code='synthetic_generation_only'
		WHERE group_id=$1 AND status='generating'`, h.scope.Group)
	f.exec(`UPDATE account_receivers SET observed_at=clock_timestamp() WHERE account_id=$1`, h.scope.Account)
	current, err := h.heat.heatScope(f.ctx, f.db.Platform, h.scope.Group, true)
	if err != nil {
		t.Fatal(err)
	}
	h.scope = current
	id := NewID()
	raw, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := h.heat.Vault.Seal("heat-chat-scope:"+id, raw)
	if err != nil {
		t.Fatal(err)
	}
	h.event++
	f.exec(`INSERT INTO heat_chat_jobs(id,platform,tenant_id,actor_id,account_id,group_id,event_id,directed,
		scope_digest,scope_sealed,status,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,true,$8,$9,'generating',clock_timestamp()+interval '120 seconds')`,
		id, current.Platform, current.Tenant, current.Actor, current.Account, current.Group, h.event, current.digest(), sealed)
	token, err := h.heat.mintJobGrant(f.ctx, id, current, time.Now().Add(90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return id, token
}

func (h *workspaceHeatBillingFixture) generate(token string) (assistant.Response, error) {
	ctx, cancel := context.WithTimeout(h.base.f.ctx, 10*time.Second)
	defer cancel()
	return h.base.gateway.HeatGenerate(ctx, token, HeatGatewayInput{ModelAlias: "default",
		ModelVersion: h.scope.RouteVersion, Messages: []assistant.Message{{Role: "user", Content: "synthetic bounded input"}}})
}

func TestPostgresWorkspaceHeatGenerateDemoBillingAndUnknownHolds(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			h := newWorkspaceHeatBillingFixture(t, platform, true)
			g, f := h.base, h.base.f
			job, token := h.job(t)
			if _, err := h.generate(token); err != nil {
				t.Fatal("demo HeatGenerate required customer credits or test cap", err)
			}
			if f.count(`SELECT count(*) FROM ai_calls WHERE external_key=$1 AND tenant_id=$2 AND actor_id=$3
				AND group_id=$4 AND purpose='heatbot' AND execution_mode='workspace_demo'
				AND billing_source='platform_workspace' AND service_task_id=$5 AND period_id IS NULL
				AND status='settled' AND charged_micro=9000`, "heat-job:"+job, h.scope.Tenant, g.admin.ID, h.scope.Group, job) != 1 {
				t.Fatal("demo charge lost its actual workspace/group/task attribution")
			}
			if _, err := h.generate(token); err == nil || h.model.calls != 1 {
				t.Fatal("single-use service grant replayed generation", err, h.model.calls)
			}
			if spent, held := g.wallet(); spent != 0 || held != 0 {
				t.Fatal("demo HeatGenerate debited the customer wallet", spent, held)
			}
			h.model.usage = assistant.Usage{}
			unknownJob, unknownToken := h.job(t)
			if _, err := h.generate(unknownToken); err != nil {
				t.Fatal(err)
			}
			var hold int64
			if err := f.db.Platform.QueryRow(f.ctx, `SELECT reserved_micro FROM ai_calls
				WHERE external_key=$1 AND status='unknown' AND charged_micro=0 AND period_id IS NULL`,
				"heat-job:"+unknownJob).Scan(&hold); err != nil || hold < 1 {
				t.Fatal("unknown workspace usage did not retain its platform reservation", hold, err)
			}
			if err := g.gateway.Recover(f.ctx); err != nil {
				t.Fatal(err)
			}
			if f.count(`SELECT count(*) FROM ai_calls WHERE external_key=$1 AND status='unknown'
				AND reserved_micro=$2 AND service_task_id=$3`, "heat-job:"+unknownJob, hold, unknownJob) != 1 {
				t.Fatal("recovery released or rewrote unknown workspace cost")
			}
			human := g.request(platform)
			reserved := g.reserve(human)
			if err := g.gateway.submit(f.ctx, human, reserved); err != nil {
				t.Fatal(err)
			}
			if err := g.gateway.settle(f.ctx, reserved.ID,
				assistant.Usage{PromptTokens: 8, CompletionTokens: 1, TotalTokens: 9}, true, false, ""); err != nil {
				t.Fatal(err)
			}
			for _, change := range []struct {
				statement, value string
			}{
				{`UPDATE ai_calls SET tenant_id=$2 WHERE id=$1`, h.scope.Tenant},
				{`UPDATE ai_calls SET service_task_id=$2 WHERE id=$1`, job},
			} {
				if _, err := f.db.Platform.Exec(f.ctx, change.statement, reserved.ID, change.value); err == nil {
					t.Fatal("historical human attribution was mutable")
				}
			}
			if f.count(`SELECT count(*) FROM ai_calls WHERE id=$1 AND tenant_id IS NULL AND period_id IS NULL
				AND service_task_id IS NULL AND execution_mode='operator_demo' AND billing_source='platform_demo'`, reserved.ID) != 1 {
				t.Fatal("workspace background changed historical human billing")
			}
			usage, err := g.gateway.UsageForCenter(f.ctx, g.adminRef)
			if err != nil {
				t.Fatal(err)
			}
			background, ok := usage["background_usage"].(map[string]any)
			if !ok || background["call_count"] != int64(2) || background["actual_micro"] != int64(9000) ||
				background["held_micro"] != hold || usage["call_count"] != int64(1) || usage["actual_micro"] != int64(9000) {
				t.Fatal("human and background usage were combined", usage)
			}
			nextJob, nextToken := h.job(t)
			f.exec(`UPDATE ai_gateway_settings SET platform_limit_micro=(SELECT coalesce(sum(
				CASE WHEN status='settled' THEN effective_charged_micro WHEN status<>'released' THEN reserved_micro ELSE 0 END),0)
				FROM ai_call_costs)+$1-1`, hold)
			_, err = h.generate(nextToken)
			capabilityError(t, err, "platform_ai_budget_exhausted")
			if h.model.calls != 2 || f.count(`SELECT count(*) FROM ai_calls WHERE external_key=$1`, "heat-job:"+nextJob) != 0 {
				t.Fatal("unknown workspace reservation escaped the shared global budget")
			}
			if spent, held := g.wallet(); spent != 0 || held != 0 {
				t.Fatal("workspace accounting changed customer money", spent, held)
			}
			if f.count(`SELECT count(*) FROM subscriptions WHERE tenant_id=$1`, h.scope.Tenant) != 0 ||
				f.count(`SELECT count(*) FROM ai_wallets WHERE tenant_id=$1`, h.scope.Tenant) != 0 ||
				f.count(`SELECT count(*) FROM heat_chat_send_attempts`) != 0 {
				t.Fatal("model accounting fabricated a wallet or native send")
			}
		})
	}
}

func TestPostgresWorkspaceHeatGenerateCustomerWalletAndCapsUnchanged(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			h := newWorkspaceHeatBillingFixture(t, platform, false)
			g, f := h.base, h.base.f
			job, token := h.job(t)
			if _, err := h.generate(token); err != nil {
				t.Fatal(err)
			}
			if spent, held := g.wallet(); spent != 9000 || held != 0 {
				t.Fatal("customer heat did not debit its existing wallet", spent, held)
			}
			if f.count(`SELECT count(*) FROM ai_calls WHERE external_key=$1 AND tenant_id=$2 AND actor_id=$3
				AND period_id=$4 AND service_task_id=$5 AND execution_mode='tenant_business'
				AND billing_source='tenant_wallet' AND charged_micro=9000`, "heat-job:"+job, f.tenant, f.actor, g.period, job) != 1 {
				t.Fatal("customer background acquired workspace billing")
			}
			h.model.usage = assistant.Usage{}
			unknownJob, unknownToken := h.job(t)
			if _, err := h.generate(unknownToken); err != nil {
				t.Fatal(err)
			}
			var hold int64
			if err := f.db.Platform.QueryRow(f.ctx, `SELECT reserved_micro FROM ai_calls
				WHERE external_key=$1 AND status='unknown' AND period_id=$2`, "heat-job:"+unknownJob, g.period).Scan(&hold); err != nil {
				t.Fatal(err)
			}
			if err := g.gateway.Recover(f.ctx); err != nil {
				t.Fatal(err)
			}
			if spent, held := g.wallet(); spent != 9000 || held != hold || hold < 1 {
				t.Fatal("unknown customer cost released its wallet reservation", spent, held, hold)
			}
			f.exec(`INSERT INTO ai_credit_limits(tenant_id,kind,scope,limit_micro) VALUES($1,'group',$2,0)`,
				f.tenant, h.scope.Group)
			cappedJob, cappedToken := h.job(t)
			_, err := h.generate(cappedToken)
			capabilityError(t, err, "ai_credit_sub_limit")
			if h.model.calls != 2 || f.count(`SELECT count(*) FROM ai_calls WHERE external_key=$1`, "heat-job:"+cappedJob) != 0 {
				t.Fatal("customer background bypassed its configured group spending cap")
			}
			_, expiredToken := h.job(t)
			f.exec(`UPDATE subscriptions SET enabled=false WHERE tenant_id=$1`, f.tenant)
			if _, err = h.generate(expiredToken); err == nil || h.model.calls != 2 {
				t.Fatal("inactive customer subscription reached a paid model", err, h.model.calls)
			}
			if spent, held := g.wallet(); spent != 9000 || held != hold {
				t.Fatal("admission failure changed settled or unknown customer money", spent, held)
			}
			if f.count(`SELECT count(*) FROM ai_calls WHERE execution_mode='workspace_demo' OR billing_source='platform_workspace'`) != 0 {
				t.Fatal("customer background inherited demo accounting")
			}
		})
	}
}

func TestPostgresWorkspaceHeatGenerateRevalidatesServiceBeforeAndAfterSubmission(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, scenario := range []string{"runtime_off", "source_closed", "native_role_missing", "owner_version",
			"owner_before_submit", "owner_after_usage"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				h := newWorkspaceHeatBillingFixture(t, platform, true)
				g, f := h.base, h.base.f
				job, token := h.job(t)
				revoke := func() { f.exec(`UPDATE users SET access_version=access_version+1 WHERE id=$1`, g.admin.ID) }
				switch scenario {
				case "runtime_off":
					f.exec(`UPDATE group_business_runtime SET enabled=false,version=version+1 WHERE group_id=$1`, h.scope.Group)
				case "source_closed":
					f.exec(`UPDATE business_capability_gates SET state='closed',version=version+1
						WHERE platform=$1 AND action='send_text' AND source='heatbot'`, platform)
				case "native_role_missing":
					f.exec(`DELETE FROM account_groups WHERE account_id=$1 AND group_id=$2`, h.scope.Account, h.scope.Group)
				case "owner_version":
					revoke()
				case "owner_before_submit":
					g.gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) {
						revoke()
						return h.model, nil
					}
				case "owner_after_usage":
					h.model.before = revoke
				}
				out, err := h.generate(token)
				if err == nil || out.Message.Content != "" {
					t.Fatal("revoked service scope delivered a model response", out, err)
				}
				switch scenario {
				case "owner_before_submit":
					if h.model.calls != 0 || f.count(`SELECT count(*) FROM ai_calls WHERE external_key=$1
						AND status='released' AND charged_micro=0 AND execution_mode='workspace_demo'
						AND billing_source='platform_workspace' AND service_task_id=$2 AND period_id IS NULL`,
						"heat-job:"+job, job) != 1 {
						t.Fatal("late pre-submission revocation invoked a model or retained its hold")
					}
				case "owner_after_usage":
					if h.model.calls != 1 || f.count(`SELECT count(*) FROM ai_calls WHERE external_key=$1
						AND status='settled' AND charged_micro=9000 AND actor_id=$2 AND tenant_id=$3
						AND execution_mode='workspace_demo' AND billing_source='platform_workspace'
						AND service_task_id=$4 AND period_id IS NULL`, "heat-job:"+job, g.admin.ID, h.scope.Tenant, job) != 1 {
						t.Fatal("incurred model cost lost immutable attribution after owner revocation")
					}
				default:
					if h.model.calls != 0 || f.count(`SELECT count(*) FROM ai_calls WHERE external_key=$1`, "heat-job:"+job) != 0 {
						t.Fatal("invalid task/service/native scope reserved a paid model call")
					}
				}
				if spent, held := g.wallet(); spent != 0 || held != 0 {
					t.Fatal("demo revocation changed customer credits", spent, held)
				}
				if f.count(`SELECT count(*) FROM heat_chat_send_attempts`) != 0 {
					t.Fatal("generation acquired a native send")
				}
			})
		}
	}
}
