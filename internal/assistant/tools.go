package assistant

func Tools(propose bool) []Tool {
	id := map[string]any{"type": "string", "format": "uuid"}
	spec := func(name, description string, props map[string]any, required ...string) Tool {
		return Tool{Type: "function", Function: ToolFunction{Name: name, Description: description,
			Parameters: map[string]any{"type": "object", "properties": props, "required": required,
				"additionalProperties": false}}}
	}
	tools := []Tool{
		spec("list_authorized_bots", "List permitted management bots in the bound scope.", map[string]any{}),
		spec("get_bot_status", "Read a permitted bot's sampled status; missing telemetry is unknown.", map[string]any{"account_id": id}, "account_id"),
		spec("list_authorized_groups", "List active authorized groups in the bound scope.", map[string]any{}),
		spec("get_group_config", "Read permitted rule configuration, not runtime activation.", map[string]any{"group_id": id}, "group_id"),
		spec("list_group_operations", "Read safe operation metadata for a permitted group.", map[string]any{"group_id": id}, "group_id"),
	}
	if propose {
		tools = append(tools, spec("propose_group_config_change",
			"Propose a bounded configuration patch in the bound group. Saving requires human confirmation and does not establish runtime activation.",
			map[string]any{"group_id": id, "base_version": map[string]any{"type": "integer", "minimum": 1},
				"changes": map[string]any{"type": "object",
					"description": "Only blocked_words.words/enabled, blacklist.members/enabled, exemptions.members/enabled, keyword_replies.rules/admin_only/enabled."}},
			"group_id", "base_version", "changes"))
	}
	return tools
}

// GroupTools omits resource selectors because control already binds one group.
func GroupTools(propose bool) []Tool {
	tools := Tools(propose)
	for i := range tools {
		f := &tools[i].Function
		switch f.Name {
		case "get_bot_status", "get_group_config", "list_group_operations":
			f.Parameters["properties"] = map[string]any{}
			f.Parameters["required"] = []string{}
		case "propose_group_config_change":
			delete(f.Parameters["properties"].(map[string]any), "group_id")
			f.Parameters["required"] = []string{"base_version", "changes"}
		}
		f.Description += " The current platform, bot and single group are already bound by the server. Do not pass account_id, group_id, platform, user or tenant selectors."
	}
	return tools
}

func Allowed(name string, propose bool) bool {
	return ContainsTool(Tools(propose), name)
}

func ContainsTool(tools []Tool, name string) bool {
	for _, tool := range tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}

// ScopedTools is implemented by trusted control, not model output.
type ScopedTools interface{ AssistantTools() []Tool }

func ToolsFor(host Host, propose bool) []Tool {
	if scoped, ok := host.(ScopedTools); ok {
		return scoped.AssistantTools()
	}
	return Tools(propose)
}

func ActionTools(actions []string, members bool) []Tool {
	spec := func(name, description string, props map[string]any, required ...string) Tool {
		return Tool{Type: "function", Function: ToolFunction{Name: name, Description: description, Parameters: map[string]any{
			"type": "object", "properties": props, "required": required, "additionalProperties": false}}}
	}
	out := []Tool{spec("get_group_capabilities", "Read this bound group's supported actions and production blockers.", map[string]any{})}
	if len(actions) == 0 {
		return out
	}
	if members {
		out = append(out, spec("find_group_members", "Find at most 20 scoped member candidates. Never guess ambiguous names; ask the human to choose the displayed candidate.", map[string]any{
			"search": map[string]any{"type": "string", "minLength": 1, "maxLength": 80}}, "search"))
	}
	for _, action := range actions {
		if action == "recall_message" {
			out = append(out, spec("find_group_messages",
				"Find up to 20 recent message excerpts from this group only. The human must choose a candidate before a recall suggestion. No full history is returned.",
				map[string]any{"search": map[string]any{"type": "string", "minLength": 1, "maxLength": 80}}, "search"))
			break
		}
	}
	return append(out, spec("propose_group_action", "Create an immutable suggestion for this single group. This never executes an action. A human must review the plan; blocked production actions cannot be confirmed.",
		map[string]any{"action": map[string]any{"type": "string", "enum": actions},
			"target_token": map[string]any{"type": "string", "maxLength": 4096, "description": "Exact server-issued candidate token; never a guessed member ID."},
			"text":         map[string]any{"type": "string", "maxLength": 8000},
			"minutes":      map[string]any{"type": "integer", "minimum": 1, "maximum": 1440}}, "action"))
}
