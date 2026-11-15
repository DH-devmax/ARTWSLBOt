package features

import "testing"

func TestManualCapabilitiesCannotBecomeBusinessTools(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		usage := NativeUsageForPlatform(platform)
		for _, id := range []string{"account_signup", "signup_sms", "signup_submit", "account_login",
			"account_settings", "account_nickname", "account_avatar", "privacy_search", "privacy_friend",
			"privacy_invitation", "change_password", "change_phone", "approve_friend", "onboarding_friend_request",
			"accept_friend", "reject_friend", "read_voice", "read_file"} {
			if usage[id] != NativeManual || NativeBusinessAllowed(platform, id) {
				t.Fatalf("%s/%s escaped the manual boundary", platform, id)
			}
			if len(NativeBlockersForPlatform(platform, Definition{NativeDependencies: []string{id}})) != 1 {
				t.Fatalf("%s/%s admitted a manual dependency", platform, id)
			}
		}
		for _, id := range []string{"receive_message", "session_restore", "send_text", "read_quote",
			"read_recalls", "mute_member", "set_self_card", "send_voice", "send_video"} {
			if !NativeBusinessAllowed(platform, id) {
				t.Fatalf("%s/%s lost its foundation classification", platform, id)
			}
		}
		if usage["send_file"] != NativeExcluded || NativeBusinessAllowed(platform, "send_file") {
			t.Fatal("previously excluded file sending returned to the foundation")
		}
		for _, item := range CatalogForPlatform(platform) {
			if item.ID == "friend_approval" {
				t.Fatal("retired automatic approval remains in the product catalog")
			}
			for _, id := range item.NativeDependencies {
				if !NativeBusinessAllowed(platform, id) {
					t.Fatalf("%s/%s depends on manual capability %s", platform, item.ID, id)
				}
			}
		}
		for _, item := range AssistantActions(platform) {
			if !NativeBusinessAllowed(platform, item.NativeID) {
				t.Fatalf("%s/%s exposes a manual tool", platform, item.ID)
			}
		}
	}
	if NativeBusinessAllowed("unknown", "send_text") || NativeBusinessAllowed("wsl", "unknown") {
		t.Fatal("unknown capability acquired a consumer")
	}
	legacy, ok := Find("friend_approval")
	if !ok || legacy.Status != "manual_only" || len(legacy.Fields) != 0 {
		t.Fatal("legacy approval ID lost its non-executable compatibility entry")
	}
	var config Config
	if Apply(&config, "friend_approval", "enabled", []byte("true")) == nil {
		t.Fatal("retired approval accepted an enabling configuration")
	}
}
