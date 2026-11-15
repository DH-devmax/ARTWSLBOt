package assistant

import (
	"encoding/json"
	"strings"
)

// ToolStarted records only a fixed argument-shape classification, never keys,
// values or the provider's raw argument text.
func ToolStarted(call ToolCall) Event {
	format := "invalid_json"
	raw := strings.TrimSpace(call.Function.Arguments)
	if raw == "" {
		format = "omitted"
	} else if json.Valid([]byte(raw)) {
		var object map[string]json.RawMessage
		if raw[0] == '{' && json.Unmarshal([]byte(raw), &object) == nil {
			format = "object"
			if len(object) == 0 {
				format = "empty_object"
			}
		} else {
			format = "other_json"
		}
	}
	return Event{Kind: "tool_started", Data: map[string]string{
		"name": call.Function.Name, "argument_format": format,
	}}
}
