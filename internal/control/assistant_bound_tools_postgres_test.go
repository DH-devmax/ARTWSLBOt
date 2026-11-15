package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
)

type assistantBoundConfigModel struct {
	t       *testing.T
	enabled bool
	calls   int
}

func (m *assistantBoundConfigModel) Generate(_ context.Context, messages []assistant.Message, tools []assistant.Tool,
	_ func(string) error) (assistant.Response, error) {
	m.calls++
	for _, tool := range tools {
		props := tool.Function.Parameters["properties"].(map[string]any)
		if props["group_id"] != nil || props["account_id"] != nil {
			m.t.Fatal("single-group model needs to invent a resource selector")
		}
	}
	name, args := "get_group_config", any(map[string]any{})
	if m.calls == 2 {
		last := messages[len(messages)-1]
		var result struct {
			Scope assistant.Scope
			Data  struct {
				Version int64 `json:"version"`
			}
		}
		if last.Role != "tool" || json.Unmarshal([]byte(last.Content), &result) != nil || result.Data.Version < 1 {
			m.t.Fatal("proposal did not use a fresh configuration query")
		}
		name = "propose_group_config_change"
		args = map[string]any{"base_version": result.Data.Version,
			"changes": map[string]any{"keyword_replies": map[string]any{"enabled": m.enabled}}}
	} else if m.calls != 1 {
		m.t.Fatal("model continued after the proposal")
	}
	return assistant.Response{Message: assistant.Message{Role: "assistant",
		ToolCalls: []assistant.ToolCall{assistantOutcomesCall(m.t, name, args)}},
		Usage: assistant.Usage{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12}}, nil
}

func TestPostgresAssistantBoundToolsConfigurationRoundTrip(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, mode := range []string{"tenant", "operator_demo"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				f := newAssistantOutcomesFixture(t, platform)
				ref := f.ref
				claim := ""
				if mode == "operator_demo" {
					actor, demo := groupBusinessAccessOperator(t, f, 97)
					ref = demo
					groupBusinessAccessSetOwnership(t, f, "unverified")
					claim = groupBusinessAccessClaim(t, f, actor, false)
				}
				f.f.exec(`UPDATE group_bindings SET management_enabled=false,ai_enabled=false,
					config='{"feature_options":{"keyword_replies":{"enabled":false}},"keyword_replies":[]}'
					WHERE group_id=$1`, f.scope.GroupID)
				session, err := f.assistant.CreateSession(f.f.ctx, ref, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				for _, enabled := range []bool{true, false} {
					turn, created, err := f.assistant.CreateTurn(f.f.ctx, ref, session.ID, NewID(), "合成配置开关往返")
					if err != nil || !created {
						t.Fatal(err)
					}
					host, _, history, propose, err := f.assistant.Host(f.f.ctx, ref, turn.ID)
					if err != nil {
						t.Fatal(err)
					}
					if !enabled {
						body, _ := json.Marshal(history)
						if !strings.Contains(string(body), `"status"`) && !strings.Contains(string(body), `\"status\"`) {
							t.Fatal("follow-up lost its committed result")
						}
					}
					model := &assistantBoundConfigModel{t: t, enabled: enabled}
					if err = (assistant.Runner{}).Run(f.f.ctx, model, host, history, propose); err != nil {
						t.Fatal("bound model/control contract failed", err)
					}
					if err = f.assistant.FinishTurn(f.f.ctx, turn.ID, nil); err != nil {
						t.Fatal(err)
					}
					rows, err := f.assistant.Transcript(f.f.ctx, ref, session.ID)
					if err != nil || len(rows) == 0 {
						t.Fatal(err)
					}
					latest := rows[len(rows)-1]
					if latest.ID != turn.ID || len(latest.Outcomes) != 1 || latest.Outcomes[0].Status != "pending_approval" {
						t.Fatal("model did not stop at human confirmation", latest)
					}
					plan, err := f.assistant.Plan(f.f.ctx, ref, latest.Outcomes[0].PlanID)
					if err != nil {
						t.Fatal(err)
					}
					committed, err := f.assistant.Approve(f.f.ctx, ref, plan.ID, plan.Digest, plan.Version)
					if err != nil || committed.Status != "committed" || model.calls != 2 {
						t.Fatal("human confirmation failed or called the model again", err)
					}
					var raw []byte
					if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT config FROM group_bindings WHERE group_id=$1`,
						f.scope.GroupID).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					var config features.Config
					if json.Unmarshal(raw, &config) != nil || features.Values(config, "keyword_replies")["enabled"] != enabled ||
						len(config.KeywordReplies) != 0 {
						t.Fatal("saved configuration does not match the bounded request")
					}
				}
				if f.f.count(`SELECT count(*) FROM assistant_plan_executions`) != 2 ||
					f.f.count(`SELECT count(*) FROM assistant_usage`) != 4 ||
					f.f.count(`SELECT count(*) FROM native_setting_operations`) != 0 ||
					f.f.count(`SELECT count(*) FROM business_rule_jobs`) != 0 ||
					f.f.count(`SELECT count(*) FROM heat_chat_jobs`) != 0 ||
					f.f.count(`SELECT count(*) FROM group_bindings WHERE group_id=$1 AND NOT management_enabled AND NOT ai_enabled`, f.scope.GroupID) != 1 {
					t.Fatal("configuration round trip created extra work or activated business")
				}
				if claim != "" && f.f.count(`SELECT count(*) FROM operator_demo_group_claims current
					JOIN operator_demo_group_claims original ON original.id=$1
					WHERE current.actor_id=original.actor_id AND current.group_id=original.group_id
					AND current.state='active' AND current.expires_at=original.expires_at
					AND current.binding_version=original.binding_version+2`, claim) != 1 {
					t.Fatal("round trip extended or lost the personal claim")
				}
			})
		}
	}
}

func TestPostgresAssistantBoundQueriesRejectSelectorsAndRevoke(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newAssistantOutcomesFixture(t, platform)
			session, err := f.assistant.Session(f.f.ctx, f.ref, f.session, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"list_authorized_bots", "list_authorized_groups", "get_bot_status", "get_group_config", "list_group_operations"} {
				for _, raw := range []string{"{}", "", " \n "} {
					call := assistantOutcomesCall(t, name, map[string]any{})
					call.Function.Arguments = raw
					result, err := f.assistant.queryTool(f.f.ctx, f.ref, session, call)
					if err != nil || result.Scope != session.Scope {
						t.Fatal("empty arguments escaped the trusted scope", name, err)
					}
				}
			}
			for _, raw := range []string{`"{}"`, `null`, `[]`, `{} {}`, `{"feature":"keyword_replies"}`, `{"role":"superadmin"}`} {
				call := assistantOutcomesCall(t, "get_group_config", map[string]any{})
				call.Function.Arguments = raw
				if _, err := f.assistant.queryTool(f.f.ctx, f.ref, session, call); assistantCode(err) != "assistant_invalid_tool_arguments" {
					t.Fatal("unexpected parameters were not rejected with a safe diagnosis", err)
				}
			}
			for _, test := range []struct {
				name string
				args map[string]any
			}{
				{"list_authorized_groups", map[string]any{"group_id": f.scope.GroupID}},
				{"list_authorized_groups", map[string]any{"search": "测试群"}},
				{"list_authorized_bots", map[string]any{"account_id": f.scope.AccountID}},
				{"get_group_config", map[string]any{"group_id": NewID()}},
				{"get_bot_status", map[string]any{"account_id": NewID()}},
				{"get_group_config", map[string]any{"platform": "other"}},
				{"get_group_config", map[string]any{"actor_id": NewID(), "role": "superadmin"}},
			} {
				if _, err := f.assistant.queryTool(f.f.ctx, f.ref, session, assistantOutcomesCall(t, test.name, test.args)); err == nil {
					t.Fatal("forged selector or unknown field accepted", test.name)
				}
			}
			f.f.exec(`DELETE FROM sessions WHERE token_hash=$1`, f.ref.hash)
			if _, err := f.assistant.queryTool(f.f.ctx, f.ref, session, assistantOutcomesCall(t, "get_group_config", map[string]any{})); err == nil {
				t.Fatal("bound scope replaced live authorization")
			}
		})
	}
}
