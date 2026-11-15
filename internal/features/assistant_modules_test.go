package features

import "testing"

func TestAssistantModuleCatalogIsExplicitAndPlatformBound(t *testing.T) {
	if len(AssistantCapabilities("")) != 0 || len(AssistantCapabilities("maibot")) != 0 {
		t.Fatal("empty or foreign scope fallback")
	}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		seen := map[string]bool{}
		for _, d := range AssistantCapabilities(platform) {
			if d.ID == "" || d.Module == "" || d.Permission == "" || d.InputContract == "" || d.OutputContract == "" || seen[d.ID] {
				t.Fatal("invalid registration", d)
			}
			seen[d.ID] = true
			if d.Tool != "propose_group_action" && d.Action != "" {
				t.Fatal("action outside proposal adapter")
			}
			if d.Category != "query" && d.Category != "config_propose" && d.Category != "action_propose" {
				t.Fatal("executable tool category")
			}
			if d.Feature == "ledger" || d.Feature == "lottery" || d.Feature == "group_ai" {
				t.Fatal("unadapted module exposed")
			}
			if got, ok := FindAssistantCapability(platform, d.ID); !ok || got != d {
				t.Fatal("unstable ID")
			}
		}
	}
}
