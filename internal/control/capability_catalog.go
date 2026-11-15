package control

import "wangshangliao/internal/features"

// BusinessPermissionCatalog is shared by website grants, tenant ceilings and
// action admission. Native action modules are never maintained separately.
func BusinessPermissionCatalog() []PermissionSpec {
	out := []PermissionSpec{
		{Code: "overview:read", Label: "查看工作台"},
		{Code: "accounts:read", Label: "查看管理机器人状态"},
		{Code: "groups:read", Label: "查看已授权群"},
		{Code: "groups:configure", Label: "配置群管与自动化", Module: "management"},
		{Code: "groups:moderation", Label: "编辑拦截词与名单", Module: "moderation"},
		{Code: "groups:replies", Label: "编辑关键词回复", Module: "replies"},
		{Code: "groups:revoke", Label: "撤销本租户群授权"},
		{Code: "groups:ai", Label: "申请 AI 入群与启停", Module: "ai"},
		{Code: "claims:read", Label: "查看群授权申请"},
		{Code: "claims:create", Label: "申请认领群组"},
		{Code: "operations:read", Label: "查看操作记录"},
		{Code: "operations:create", Label: "提交群管操作", Module: "moderation"},
		{Code: "operations:cancel", Label: "取消排队操作"},
		{Code: "ledger:read", Label: "查看历史台账"},
		{Code: "ledger:create", Label: "新增财务记录", Module: "finance"},
		{Code: "activities:read", Label: "查看活动"},
		{Code: "activities:create", Label: "创建活动", Module: "activities"},
		{Code: "activities:draw", Label: "活动开奖", Module: "activities"},
		{Code: "audit:read", Label: "查看本租户审计"},
		{Code: "members:manage", Label: "管理租户成员（仅主账号）"},
		{Code: "billing:read", Label: "查看租户账单"},
		{Code: "assistant:use", Label: "使用群管助手", Module: "management_assistant"},
		{Code: "assistant:propose", Label: "生成群管配置建议", Module: "management_assistant"},
		{Code: "assistant:approve", Label: "审批本人群管配置计划", Module: "management_assistant"},
		{Code: "assistant:action_propose", Label: "生成群管动作建议", Module: "management_assistant"},
		{Code: "assistant:action_approve", Label: "确认本人群管动作", Module: "management_assistant"},
	}
	for _, action := range features.BusinessActions("wsl") {
		out = append(out, PermissionSpec{Code: action.Permission, Label: action.Name, Module: action.Module})
	}
	for _, definition := range features.Catalog() {
		for _, field := range definition.Fields {
			out = append(out, PermissionSpec{Code: features.Permission(definition.ID, field.Key),
				Label: definition.Name + " · " + field.Label, Module: definition.Module})
		}
	}
	seen := map[string]bool{}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, d := range features.AssistantCapabilities(platform) {
			if !seen[d.Permission] {
				out = append(out, PermissionSpec{Code: d.Permission, Label: d.Name + " · 助手" + d.Category, Module: "management_assistant"})
				seen[d.Permission] = true
			}
		}
	}
	return out
}
