package features

// AssistantCapability is an explicit compile-time opt-in contract. Catalog
// entries without an adapter never become model tools. Exposure is separate.
type AssistantCapability struct {
	ID                 string `json:"id"`
	Module             string `json:"module"`
	Name               string `json:"name"`
	Category           string `json:"category"`
	Tool               string `json:"tool"`
	Permission         string `json:"permission"`
	BusinessPermission string `json:"business_permission"`
	Feature            string `json:"feature,omitempty"`
	Action             string `json:"action,omitempty"`
	InputContract      string `json:"input_contract"`
	OutputContract     string `json:"output_contract"`
	Implemented        bool   `json:"implemented"`
}

const AssistantCatalogVersion = "business-agent-v1"

func AssistantCapabilities(platform string) []AssistantCapability {
	if platform != "wsl" && platform != "lajiaoliao" {
		return nil
	}
	out := []AssistantCapability{}
	add := func(module, name, kind, tool, business, feature, action string, implemented bool) {
		id := module + "." + kind
		if action != "" {
			id += "." + action
		}
		out = append(out, AssistantCapability{ID: id, Module: module, Name: name, Category: kind, Tool: tool,
			Permission: "assistant_module:" + module + ":" + kind, BusinessPermission: business, Feature: feature, Action: action,
			InputContract: tool + ":v1", OutputContract: "scoped-business-result:v1", Implemented: implemented})
	}
	for _, q := range []struct{ tool, permission string }{
		{"list_authorized_bots", "accounts:read"}, {"get_bot_status", "accounts:read"},
		{"list_authorized_groups", "groups:read"}, {"list_group_operations", "operations:read"},
		{"get_group_capabilities", "groups:read"},
	} {
		// Each query has a stable identifier while sharing one explicit module grant.
		add("workspace", "群与机器人查询", "query", q.tool, q.permission, "", "", true)
		out[len(out)-1].ID = "workspace.query." + q.tool
	}
	for _, d := range CatalogForPlatform(platform) {
		// These adapters exist today. Other business catalog entries stay unexposed.
		if d.ID != "blocked_words" && d.ID != "blacklist" && d.ID != "exemptions" && d.ID != "keyword_replies" {
			continue
		}
		add(d.ID, d.Name, "query", "get_group_config", "groups:read", d.ID, "", true)
		add(d.ID, d.Name, "config_propose", "propose_group_config_change", "assistant:propose", d.ID, "", true)
	}
	for _, d := range AssistantActions(platform) {
		add(d.Module, d.Name, "action_propose", "propose_group_action", d.Permission, "", d.ID, d.Implemented)
	}
	return out
}

func FindAssistantCapability(platform, id string) (AssistantCapability, bool) {
	for _, d := range AssistantCapabilities(platform) {
		if d.ID == id {
			return d, true
		}
	}
	return AssistantCapability{}, false
}
