package features

import "slices"

// AssistantAction is a product contract. Integration and controlled acceptance
// never imply production admission. Admission is deliberately explicit.
type AssistantAction struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	NativeID     string   `json:"native_id"`
	Module       string   `json:"module"`
	Permission   string   `json:"permission"`
	ProductScope string   `json:"product_scope"`
	Role         string   `json:"native_role"`
	Implemented  bool     `json:"implemented"`
	Production   bool     `json:"production_admitted"`
	BlockReason  string   `json:"block_reason,omitempty"`
	Sources      []string `json:"sources"`
}

func AssistantActions(platform string) []AssistantAction {
	if platform != "wsl" && platform != "lajiaoliao" {
		return nil
	}
	defs := []AssistantAction{
		{ID: "group_info", Name: "修改群名称", NativeID: "edit_group_info", Module: "moderation", Role: "admin"},
		{ID: "publish_notice", Name: "发布公告", NativeID: "publish_notice", Module: "moderation", Role: "admin"},
		{ID: "member_nickname", Name: "成员群名片", NativeID: "set_member_card", Module: "cards", Role: "admin"},
		{ID: "mute_member", Name: "成员禁言", NativeID: "mute_member", Module: "moderation", Role: "admin"},
		{ID: "unmute_member", Name: "成员解禁", NativeID: "unmute_member", Module: "moderation", Role: "admin"},
		{ID: "mute_group", Name: "全群禁言", NativeID: "mute_group", Module: "moderation", Role: "admin"},
		{ID: "unmute_group", Name: "解除全群禁言", NativeID: "mute_group", Module: "moderation", Role: "admin"},
		{ID: "kick_member", Name: "移出成员", NativeID: "kick_member", Module: "moderation", Role: "admin"},
		{ID: "send_text", Name: "发送群文本", NativeID: "send_text", Module: "moderation", Role: "member"},
		{ID: "recall_message", Name: "撤回消息", NativeID: "recall_message", Module: "moderation", Role: "admin"},
	}
	native := NativeCatalogForPlatform(platform)
	for i := range defs {
		d := &defs[i]
		d.Permission = "group_action:" + d.ID
		d.ProductScope = "included"
		d.Sources = []string{"assistant", "manual"}
		if slices.Contains([]string{"send_text", "recall_message", "mute_member"}, d.ID) {
			d.Sources = append(d.Sources, "automatic")
		}
		if d.ID == "send_text" {
			d.Sources = append(d.Sources, "heatbot")
		}
		for _, n := range native {
			if n.ID == d.NativeID && NativeBusinessAllowed(platform, n.ID) &&
				n.Support == "confirmed" && (n.Integration == "implemented" || n.Integration == "partial" || n.Integration == "accepted") {
				d.Implemented = true
			}
		}
		d.BlockReason = "production_acceptance_required"
		if !d.Implemented {
			d.BlockReason = "durable_action_not_implemented"
		}
	}
	return defs
}

// BusinessActions is the common permission/admission catalog. Onboarding is a
// separate source and never becomes an assistant tool.
func BusinessActions(platform string) []AssistantAction {
	out := AssistantActions(platform)
	if out == nil {
		return nil
	}
	accept := AssistantAction{ID: "group_accept", Name: "接受本账号群邀请", NativeID: "accept_group_invitation",
		Permission: "group_action:group_accept", Module: "management", Role: "self",
		ProductScope: "included", Sources: []string{"onboarding"}, BlockReason: "production_acceptance_required"}
	for _, n := range NativeDetailsForPlatform(platform) {
		if n.ID == accept.NativeID && n.Support == "confirmed" &&
			(n.Integration == "implemented" || n.Integration == "partial" || n.Integration == "accepted") {
			accept.Implemented = true
		}
	}
	if !accept.Implemented {
		accept.BlockReason = "durable_action_not_implemented"
	}
	return append(out, accept)
}

func FindBusinessAction(platform, id, source string) (AssistantAction, bool) {
	for _, d := range BusinessActions(platform) {
		if d.ID == id && slices.Contains(d.Sources, source) {
			return d, true
		}
	}
	return AssistantAction{}, false
}

func FindAssistantAction(platform, id string) (AssistantAction, bool) {
	for _, d := range AssistantActions(platform) {
		if d.ID == id {
			return d, true
		}
	}
	return AssistantAction{}, false
}
