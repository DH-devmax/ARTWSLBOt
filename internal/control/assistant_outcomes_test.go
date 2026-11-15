package control

import (
	"encoding/json"
	"strings"
	"testing"

	"wangshangliao/internal/assistant"
)

func TestAssistantConfigurationSummariesDistinguishSavingAndRuntime(t *testing.T) {
	plan := AssistantPlan{
		GroupName: "测试群", CommittedVersion: 9,
		Changes: FeatureChanges{
			"keyword_replies": {"rules": json.RawMessage(`[{"keyword":"验收","reply":"合成回复"}]`), "enabled": json.RawMessage(`false`)},
			"blocked_words":   {"words": json.RawMessage(`["合成词"]`)},
		},
	}
	for _, row := range []struct {
		status string
		want   []string
		absent string
	}{
		{"pending_approval", []string{"测试群", "拦截词", "合成词", "回复规则设为1条", "功能开关设为关闭", "尚未修改"}, "已保存"},
		{"committed", []string{"已保存", "配置版本为9", "实际运行效果尚未核验"}, "已经生效"},
		{"cancelled", []string{"已取消", "没有保存"}, "已保存"},
		{"expired", []string{"已过期", "没有保存"}, "已保存"},
		{"unknown", []string{"尚未确认", "不会自动重复保存"}, "已保存"},
		{"failed", []string{"未能保存"}, "已保存"},
	} {
		t.Run(row.status, func(t *testing.T) {
			plan.Status = row.status
			text := assistantConfigSummary(plan)
			for _, word := range row.want {
				if !strings.Contains(text, word) {
					t.Fatalf("summary missing %q: %s", word, text)
				}
			}
			if strings.Contains(text, row.absent) {
				t.Fatal("summary overstated the outcome", text)
			}
		})
	}
}

func TestAssistantRuleSummaryExplainsMatchingAndDisabledState(t *testing.T) {
	text := assistantConfigSummary(AssistantPlan{Changes: FeatureChanges{
		"keyword_replies": {"rules": json.RawMessage(`[{"keyword":"验收","reply":"合成回复","match":"exact","disabled":true}]`)},
	}})
	for _, want := range []string{"验收", "合成回复", "完全匹配", "已停用", "尚未修改配置"} {
		if !strings.Contains(text, want) {
			t.Fatal("confirmation summary hid rule behavior", text)
		}
	}
}

func TestAssistantActionSummariesReflectDurableOutcome(t *testing.T) {
	for _, platform := range []struct{ id, name string }{{"wsl", "旺商聊"}, {"lajiaoliao", "辣椒聊"}} {
		plan := AssistantActionPlan{
			Scope: assistant.Scope{Platform: platform.id}, GroupName: "测试群",
			Action: "send_text", Text: "合成消息",
		}
		for _, row := range []struct{ status, want, absent string }{
			{"pending_approval", "尚未执行", "成功回执"},
			{"blocked", "尚未开放", "成功回执"},
			{"succeeded", "发送1条群文本（4字），平台已返回成功回执", "对方已阅读"},
			{"unknown", "不会自动重发", "成功回执"},
			{"approved", "正在核对", "成功回执"},
			{"submitting", "正在核对", "成功回执"},
			{"failed", "本次操作已失败", "成功回执"},
			{"cancelled", "没有提交新的原生操作", "成功回执"},
			{"expired", "没有执行", "成功回执"},
		} {
			t.Run(platform.id+"/"+row.status, func(t *testing.T) {
				plan.Status = row.status
				text := assistantActionSummary(plan)
				if !strings.Contains(text, platform.name+"「测试群」") || !strings.Contains(text, row.want) || strings.Contains(text, row.absent) {
					t.Fatal("summary lost scope or overstated success", text)
				}
			})
		}
	}
}

func TestAssistantSummariesDescribeTargetsAndBoundUntrustedLabels(t *testing.T) {
	plan := AssistantActionPlan{
		Action: "mute_member", Status: "pending_approval", Minutes: 10,
		Target: &AssistantMember{ID: "string-identity", Name: "同名成员"},
	}
	text := assistantActionSummary(plan)
	if !strings.Contains(text, "同名成员") || !strings.Contains(text, "string-identity") || !strings.Contains(text, "10分钟") {
		t.Fatal("moderation summary lost the selected target", text)
	}
	plan.GroupName = strings.Repeat("长群名", 10000) + "\n\x00"
	plan.Target.Name = strings.Repeat("长名", 10000) + "\r\x00"
	if text = assistantActionSummary(plan); len([]rune(text)) > 300 || strings.ContainsAny(text, "\r\n\x00") {
		t.Fatal("summary retained an unbounded label or control characters")
	}
	plan.Action, plan.Text = "group_info", "新群名"
	if text = assistantActionSummary(plan); !strings.Contains(text, "新群名") || strings.Contains(text, "已将群名称") {
		t.Fatal("pending rename was presented as complete", text)
	}
}
