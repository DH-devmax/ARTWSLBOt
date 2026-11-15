package control

import (
	"strings"
	"testing"
	"wangshangliao/internal/assistant"
)

func TestPostgresAssistantModulesDefaultClosedAndIndependent(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newAssistantOutcomesFixture(t, platform)
			f.f.exec(`UPDATE assistant_module_exposure SET capabilities='{}' WHERE platform=$1`, platform)
			session, err := f.assistant.Session(f.f.ctx, f.ref, f.session, true)
			if err != nil {
				t.Fatal(err)
			}
			tools, version, err := f.assistant.effectiveAssistantTools(f.f.ctx, session.Actor, f.scope)
			if err != nil || len(tools) != 0 || version != 1 {
				t.Fatal("closed policy registered tools", tools, version, err)
			}
			f.f.exec(`UPDATE assistant_module_exposure SET capabilities=ARRAY['blocked_words.query'] WHERE platform=$1`, platform)
			tools, _, err = f.assistant.effectiveAssistantTools(f.f.ctx, session.Actor, f.scope)
			if err != nil || len(tools) != 1 || !assistant.ContainsTool(tools, "get_group_config") {
				t.Fatal("query-only policy leaked suggestions", tools, err)
			}
			result, err := f.assistant.queryTool(f.f.ctx, f.ref, session, assistantOutcomesCall(t, "get_group_config", map[string]string{"group_id": f.scope.GroupID}))
			if err != nil {
				t.Fatal(err)
			}
			values := result.Data.(map[string]any)["values"].(map[string]any)
			if len(values) != 1 || values["blocked_words"] == nil {
				t.Fatal("closed feature data leaked", values)
			}
			f.f.exec(`UPDATE user_access SET permissions=array_remove(permissions,'assistant_module:blocked_words:query') WHERE user_id=$1`, f.f.actor)
			tools, _, err = f.assistant.effectiveAssistantTools(f.f.ctx, session.Actor, f.scope)
			if err != nil || len(tools) != 0 {
				t.Fatal("module grant inherited", tools, err)
			}
			other := "wsl"
			if platform == other {
				other = "lajiaoliao"
			}
			policy, err := assistantExposure(f.f.ctx, f.f.db.Platform, other)
			if err != nil || len(policy.Capabilities) != 0 {
				t.Fatal("cross-platform policy inherited", policy, err)
			}
		})
	}
}

func TestPostgresAssistantModulesClosingInvalidatesPlansAndLateTurns(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newAssistantOutcomesFixture(t, platform)
			call := assistantOutcomesConfigCall(t, f)
			turn, _ := runAssistantOutcomesProposal(t, f, call, "合成修改拦截词", "")
			row := readAssistantOutcomesTurn(t, f, f.ref, f.session, turn.ID)
			if len(row.Outcomes) != 1 {
				t.Fatal("missing configuration proposal")
			}
			plan, err := f.assistant.Plan(f.f.ctx, f.ref, row.Outcomes[0].PlanID)
			if err != nil || plan.PolicyVersion != 1 {
				t.Fatal(plan, err)
			}
			next, _, err := f.assistant.CreateTurn(f.f.ctx, f.ref, f.session, NewID(), "合成下一回合")
			if err != nil {
				t.Fatal(err)
			}
			host, _, _, _, err := f.assistant.Host(f.f.ctx, f.ref, next.ID)
			if err != nil {
				t.Fatal(err)
			}
			f.f.exec(`UPDATE assistant_module_exposure SET capabilities='{}',version=version+1 WHERE platform=$1`, platform)
			_, err = f.assistant.Approve(f.f.ctx, f.ref, plan.ID, plan.Digest, plan.Version)
			capabilityError(t, err, "assistant_module_policy_changed")
			capabilityError(t, host.Check(f.f.ctx), "assistant_module_policy_changed")
			_, _, err = f.assistant.Turn(f.f.ctx, f.ref, turn.ID)
			capabilityError(t, err, "assistant_history_scope_changed")
			_, _, err = f.assistant.Events(f.f.ctx, f.ref, turn.ID, 0)
			capabilityError(t, err, "assistant_history_scope_changed")
			if f.f.count(`SELECT count(*) FROM assistant_plan_executions`) != 0 {
				t.Fatal("closed plan committed")
			}
			if err = f.assistant.FinishTurn(f.f.ctx, next.ID, assistant.ErrInvalid); err != nil {
				t.Fatal(err)
			}
			rows, err := f.assistant.Transcript(f.f.ctx, f.ref, f.session)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if !r.Unavailable || len(r.Cards) != 0 || strings.Contains(r.Answer, "synthetic") {
					t.Fatal("late protected content survived policy change")
				}
			}
		})
	}
}

func TestPostgresAssistantModulesNoLegacyPendingApproval(t *testing.T) {
	f := newAssistantOutcomesFixture(t, "wsl")
	session, err := f.assistant.Session(f.f.ctx, f.ref, f.session, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.assistant.propose(f.f.ctx, f.ref, session, assistantOutcomesConfigCall(t, f))
	if err != nil {
		t.Fatal(err)
	}
	p := result.Data.(AssistantPlan)
	raw, err := f.assistant.Vault.Seal("assistant:plan:"+p.ID, []byte(`{"changes":{"blocked_words":{"words":["synthetic"]}},"before":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	f.f.exec(`UPDATE assistant_plans SET payload_sealed=$2 WHERE id=$1`, p.ID, raw)
	_, err = f.assistant.Approve(f.f.ctx, f.ref, p.ID, p.Digest, p.Version)
	capabilityError(t, err, "assistant_module_policy_changed")
}

func TestPostgresAssistantQueryFactsRemainScopedInNextTurn(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newAssistantOutcomesFixture(t, platform)
			turn, _, err := f.assistant.CreateTurn(f.f.ctx, f.ref, f.session, NewID(), "先查询本群配置")
			if err != nil {
				t.Fatal(err)
			}
			host, _, _, _, err := f.assistant.Host(f.f.ctx, f.ref, turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			result, err := host.Call(f.f.ctx, assistantOutcomesCall(t, "get_group_config", map[string]string{"group_id": f.scope.GroupID}))
			if err != nil {
				t.Fatal(err)
			}
			if err = host.Emit(f.f.ctx, assistant.Event{Kind: "tool_result", Data: result}); err != nil {
				t.Fatal(err)
			}
			if err = f.assistant.FinishTurn(f.f.ctx, turn.ID, nil); err != nil {
				t.Fatal(err)
			}
			history := assistantOutcomesHistory(t, f, f.ref, f.session)
			if !strings.Contains(history, "Historical query snapshot") || !strings.Contains(history, "get_group_config") || !strings.Contains(history, "sampled_at") {
				t.Fatal("query facts were discarded")
			}
			f.f.exec(`UPDATE user_access SET permissions=array_remove(permissions,'assistant_module:blocked_words:query') WHERE user_id=$1`, f.f.actor)
			if strings.Contains(assistantOutcomesHistory(t, f, f.ref, f.session), "get_group_config") {
				t.Fatal("revoked query survived in model context")
			}
			if f.f.count(`SELECT count(*) FROM assistant_usage`) != 0 || f.f.count(`SELECT count(*) FROM native_setting_operations`) != 0 {
				t.Fatal("history generated paid or native work")
			}
		})
	}
}
