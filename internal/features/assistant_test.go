package features

import "testing"

func TestAssistantActionsSeparatePlatformAndAdmission(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		seen := map[string]bool{}
		for _, d := range AssistantActions(platform) {
			if seen[d.ID] || d.Permission != "group_action:"+d.ID || d.Module == "" || d.NativeID == "" {
				t.Fatal(platform, d)
			}
			seen[d.ID] = true
			if d.Production {
				t.Fatal("controlled acceptance cannot enable production", platform, d.ID)
			}
			if d.BlockReason == "" {
				t.Fatal("missing blocker", platform, d.ID)
			}
			switch d.ID {
			case "grant_admin", "revoke_admin", "group_invite", "group_leave", "nickname", "account_settings":
				t.Fatal("excluded assistant action", d.ID)
			}
		}
	}
	if len(AssistantActions("maibot")) != 0 {
		t.Fatal("chat workspace gained assistant actions")
	}
}
