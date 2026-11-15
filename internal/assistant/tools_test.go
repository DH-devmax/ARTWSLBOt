package assistant

import "testing"

func TestGroupToolsUseBoundResourcesWithoutChangingLegacyContracts(t *testing.T) {
	for _, propose := range []bool{false, true} {
		for _, tool := range GroupTools(propose) {
			params := tool.Function.Parameters
			props := params["properties"].(map[string]any)
			if props["account_id"] != nil || props["group_id"] != nil || params["additionalProperties"] != false {
				t.Fatal("bound tool exposes a resource selector", tool.Function.Name, params)
			}
			required := params["required"].([]string)
			if tool.Function.Name == "propose_group_config_change" {
				if len(required) != 2 || required[0] != "base_version" || required[1] != "changes" {
					t.Fatal("proposal lost its version fence", params)
				}
			} else if len(props) != 0 || len(required) != 0 {
				t.Fatal("bound query requires model-selected resources", tool.Function.Name, params)
			}
		}
	}
	for _, tool := range Tools(true) {
		props := tool.Function.Parameters["properties"].(map[string]any)
		switch tool.Function.Name {
		case "get_bot_status":
			if props["account_id"] == nil {
				t.Fatal("legacy account selector was mutated")
			}
		case "get_group_config", "list_group_operations", "propose_group_config_change":
			if props["group_id"] == nil {
				t.Fatal("legacy group selector was mutated")
			}
		}
	}
}
