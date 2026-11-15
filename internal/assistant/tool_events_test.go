package assistant

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolStartedKeepsOnlyFixedArgumentMetadata(t *testing.T) {
	for _, test := range []struct{ raw, format string }{
		{"", "omitted"}, {" \n ", "omitted"}, {"{}", "empty_object"}, {" { } ", "empty_object"},
		{`{"private_field":"private-value"}`, "object"}, {`"private-value"`, "other_json"},
		{"null", "other_json"}, {"[]", "other_json"}, {"{private-value", "invalid_json"},
	} {
		call := ToolCall{}
		call.Function.Name, call.Function.Arguments = "get_group_config", test.raw
		event := ToolStarted(call)
		fields := event.Data.(map[string]string)
		if event.Kind != "tool_started" || len(fields) != 2 ||
			fields["name"] != call.Function.Name || fields["argument_format"] != test.format {
			t.Fatal("incorrect bounded metadata", fields)
		}
		encoded, err := json.Marshal(event)
		if err != nil || strings.Contains(string(encoded), "private") {
			t.Fatal("tool argument content entered event metadata")
		}
	}
}
