package control

// Templates are drafts, never role assignments. Existing roles and new catalog
// permissions do not inherit them automatically.
type AssistantPermissionTemplate struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Version     int      `json:"version"`
	Permissions []string `json:"permissions"`
}

func AssistantPermissionTemplates() []AssistantPermissionTemplate {
	base := []string{"overview:read", "accounts:read", "groups:read", "operations:read", "assistant:use"}
	makeTemplate := func(code, name string, extra ...string) AssistantPermissionTemplate {
		grants := append([]string{}, base...)
		return AssistantPermissionTemplate{Code: code, Name: name, Version: 3, Permissions: append(grants, extra...)}
	}
	return []AssistantPermissionTemplate{
		makeTemplate("assistant_reader", "助手只读"),
		makeTemplate("assistant_editor", "配置建议", "assistant:propose", "feature:keyword_replies:rules"),
		makeTemplate("assistant_self_approver", "配置本人确认", "assistant:propose", "assistant:approve", "feature:keyword_replies:rules"),
		makeTemplate("assistant_action_approver", "群管本人确认",
			"assistant:action_propose", "assistant:action_approve"),
	}
}
