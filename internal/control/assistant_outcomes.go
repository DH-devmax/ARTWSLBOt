package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"wangshangliao/internal/assistant"
)

// Outcomes are current projections of durable plans, never model-authored receipts.
type AssistantOutcome struct {
	PlanID        string `json:"plan_id"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Summary       string `json:"summary"`
	OperationID   string `json:"operation_id,omitempty"`
	EvidenceState string `json:"evidence_state,omitempty"`
}

func assistantDisplay(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}

func assistantConfigSummary(plan AssistantPlan) string {
	features := map[string]string{
		"blocked_words": "拦截词", "blacklist": "业务黑名单",
		"exemptions": "豁免名单", "keyword_replies": "关键词回复",
	}
	fields := map[string]string{
		"enabled": "功能开关", "words": "词语", "members": "成员",
		"rules": "回复规则", "admin_only": "仅管理员触发",
	}
	var changes []string
	for _, feature := range sortedAssistantKeys(plan.Changes) {
		var parts []string
		for _, field := range sortedAssistantKeys(plan.Changes[feature]) {
			var value any
			if json.Unmarshal(plan.Changes[feature][field], &value) != nil {
				continue
			}
			label := fields[field]
			if label == "" {
				continue
			}
			if enabled, ok := value.(bool); ok {
				state := "关闭"
				if enabled {
					state = "开启"
				}
				parts = append(parts, label+"设为"+state)
			} else {
				raw, err := json.Marshal(value)
				var items []json.RawMessage
				if err == nil && json.Unmarshal(raw, &items) == nil {
					part := fmt.Sprintf("%s设为%d条", label, len(items))
					if field == "words" || field == "members" {
						var examples []string
						for _, item := range items[:min(3, len(items))] {
							var text string
							if json.Unmarshal(item, &text) == nil {
								examples = append(examples, "「"+assistantDisplay(text, 32)+"」")
							}
						}
						if len(examples) > 0 {
							part += "（" + strings.Join(examples, "、") + "）"
						}
					}
					if field == "rules" {
						var examples []string
						for _, item := range items[:min(3, len(items))] {
							var rule struct {
								Keyword, Reply, Match string
								Disabled              bool
							}
							if json.Unmarshal(item, &rule) == nil {
								detail := "「" + assistantDisplay(rule.Keyword, 40) + "」→「" + assistantDisplay(rule.Reply, 100) + "」"
								if rule.Match == "exact" {
									detail += "（完全匹配）"
								}
								if rule.Disabled {
									detail += "（已停用）"
								}
								examples = append(examples, detail)
							}
						}
						if len(examples) > 0 {
							part += "（" + strings.Join(examples, "、") + "）"
						}
					}
					parts = append(parts, part)
				}
			}
		}
		if name := features[feature]; name != "" && len(parts) > 0 {
			changes = append(changes, name+"："+strings.Join(parts, "，"))
		}
	}
	detail := strings.Join(changes, "；")
	if detail == "" {
		detail = "所选配置"
	}
	group := "本群"
	if plan.GroupName != "" {
		group = "「" + assistantDisplay(plan.GroupName, 80) + "」"
	}
	switch plan.Status {
	case "committed":
		return fmt.Sprintf("已保存%s的配置：%s。配置版本为%d；实际运行效果尚未核验。", group, detail, plan.CommittedVersion)
	case "cancelled":
		return "已取消" + group + "的配置建议，本次建议没有保存。"
	case "expired":
		return group + "的配置建议已过期，没有保存；需要修改时请重新提出要求。"
	case "invalidated":
		return group + "的配置建议因模块开放策略或权限变化而失效，没有保存；请重新提出需求。"
	case "failed":
		return group + "的配置未能保存，请查看建议详情核对原因。"
	case "unknown":
		return group + "的配置保存结果尚未确认，请核对原建议；不会自动重复保存。"
	default:
		return "已整理好" + group + "的配置建议：" + detail + "。请查看差异并确认保存，当前尚未修改配置。"
	}
}

func sortedAssistantKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assistantActionSummary(plan AssistantActionPlan) string {
	platform := "旺商聊"
	if plan.Scope.Platform == "lajiaoliao" {
		platform = "辣椒聊"
	}
	group := "本群"
	if plan.GroupName != "" {
		group = "「" + assistantDisplay(plan.GroupName, 80) + "」"
	}
	target := "所选成员"
	if plan.Target != nil {
		target = "「" + assistantDisplay(plan.Target.Name, 60) + "」"
		if plan.Target.ID != "" {
			target += "（" + assistantDisplay(plan.Target.ID, 80) + "）"
		}
	}
	action := map[string]string{
		"group_info":      "将群名称修改为「" + assistantDisplay(plan.Text, 80) + "」",
		"publish_notice":  "发布群公告",
		"member_nickname": "将" + target + "的群名片修改为「" + assistantDisplay(plan.Text, 40) + "」",
		"mute_member":     fmt.Sprintf("禁言%s%d分钟", target, plan.Minutes),
		"unmute_member":   "解除" + target + "的禁言",
		"mute_group":      "开启全群禁言",
		"unmute_group":    "解除全群禁言",
		"kick_member":     "移出成员" + target,
		"send_text":       fmt.Sprintf("发送1条群文本（%d字）", len([]rune(plan.Text))),
		"recall_message":  "撤回所选消息",
	}[plan.Action]
	if action == "" {
		action = "执行所选群管操作"
	}
	prefix := platform + group + "："
	if plan.RobotNickname != "" {
		prefix = platform + group + " · 机器人「" + assistantDisplay(plan.RobotNickname, 60) + "」："
	}
	detail := ""
	if plan.Action == "send_text" || plan.Action == "publish_notice" {
		detail = " 内容：「" + assistantDisplay(plan.Text, 2000) + "」。"
	}
	if plan.OperationID != "" {
		detail += " 原操作编号：" + plan.OperationID + "。"
	}
	switch plan.Status {
	case "succeeded":
		evidence := "接收侧尚未核对到对应消息，不代表用户已读。"
		if plan.Action != "send_text" {
			evidence = "实际效果需结合群内观测核对。"
		}
		if plan.EvidenceState == "receive_observed" {
			evidence = "接收侧已观测到同一原生消息，不代表用户已读。"
		}
		return prefix + "已" + action + "，平台已返回成功回执。" + detail + evidence
	case "failed":
		return prefix + action + "未完成，本次操作已失败。" + detail + "请查看动作详情核对原因。"
	case "unknown":
		return prefix + action + "的提交结果尚未确认。" + detail + "请核对原操作，不会自动重发或再次执行。"
	case "approved", "submitting":
		return prefix + "你已确认" + action + "，正在核对本次提交结果。"
	case "cancelled":
		return prefix + "已取消" + action + "的建议，没有提交新的原生操作。"
	case "expired":
		return prefix + action + "的建议已过期，没有执行。"
	case "invalidated":
		return prefix + action + "的建议因模块开放策略或权限变化而失效，没有执行；请重新提出需求。"
	case "blocked":
		return prefix + "已整理" + action + "的建议，但该动作当前尚未开放，没有执行。"
	default:
		return prefix + "准备" + action + "。" + detail + "请查看动作与影响后确认，当前尚未执行。"
	}
}

// A stored tool card supplies only a plan reference. Its old payload is never
// used as evidence that an operation executed or that access is still granted.
func (s *AssistantService) projectAssistantOutcome(ctx context.Context, ref SessionRef, session AssistantSession, body []byte) (AssistantOutcome, []byte, bool, error) {
	var result struct {
		Type  string          `json:"type"`
		Scope assistant.Scope `json:"scope"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return AssistantOutcome{}, nil, false, Denied(503, "assistant_invalid_event")
	}
	if result.Type != "propose_group_config_change" && result.Type != "propose_group_action" {
		return AssistantOutcome{}, nil, false, nil
	}
	if result.Scope.Platform == "" {
		result.Scope.Platform = "wsl"
	}
	var planRef struct{ ID string }
	if json.Unmarshal(result.Data, &planRef) != nil || !ValidID(planRef.ID) || result.Scope != session.Scope {
		return AssistantOutcome{}, nil, true, Denied(503, "assistant_invalid_event")
	}
	var data any
	var outcome AssistantOutcome
	var err error
	if result.Type == "propose_group_config_change" {
		var plan AssistantPlan
		plan, err = s.Plan(ctx, ref, planRef.ID)
		if err == nil && (plan.SessionID != session.ID || plan.GroupID != session.Scope.GroupID ||
			plan.AccountID != session.Scope.AccountID || plan.Platform != session.Scope.Platform) {
			err = Denied(403, "assistant_scope_mismatch")
		}
		data = plan
		outcome = AssistantOutcome{PlanID: plan.ID, Kind: "configuration", Status: plan.Status, Summary: plan.Summary}
	} else {
		var plan AssistantActionPlan
		plan, err = s.ActionPlan(ctx, ref, planRef.ID)
		if err == nil && (plan.SessionID != session.ID || plan.Scope != session.Scope) {
			err = Denied(403, "assistant_scope_mismatch")
		}
		data = plan
		outcome = AssistantOutcome{PlanID: plan.ID, Kind: "action", Status: plan.Status, Summary: plan.Summary, OperationID: plan.OperationID, EvidenceState: plan.EvidenceState}
	}
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) && (failure.Status == 403 || failure.Status == 404 || failure.Status == 409) {
			return AssistantOutcome{}, nil, true, nil
		}
		return AssistantOutcome{}, nil, true, err
	}
	var card map[string]json.RawMessage
	if err = json.Unmarshal(body, &card); err != nil {
		return AssistantOutcome{}, nil, true, err
	}
	card["data"], err = json.Marshal(data)
	if err != nil {
		return AssistantOutcome{}, nil, true, err
	}
	current, err := json.Marshal(card)
	return outcome, current, true, err
}
