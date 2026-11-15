package features

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestNativeDetailsAreIndependentAndScoped(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao", "", "unknown"} {
		parents := map[string]NativeCapability{}
		for _, item := range NativeCatalogForPlatform(platform) {
			if _, exists := parents[item.ID]; exists || item.ID == "" || item.Evidence == "" ||
				!slices.Contains([]string{"review", "confirmed"}, item.Support) ||
				!slices.Contains([]string{"pending", "partial", "implemented", "accepted", "deferred"}, item.Integration) {
				t.Fatal("invalid native catalog", platform, item.ID)
			}
			parents[item.ID] = item
		}
		seen := map[string]bool{}
		for _, item := range NativeDetailsForPlatform(platform) {
			parent, ok := parents[item.ParentID]
			if !ok || seen[item.ID] || parents[item.ID].ID != "" || item.Category != parent.Category ||
				item.Kind != parent.Kind || item.Role == "" || item.Summary == "" || item.Evidence == "" ||
				!slices.Contains([]string{"review", "confirmed"}, item.Support) ||
				!slices.Contains([]string{"pending", "partial", "implemented", "accepted", "deferred"}, item.Integration) {
				t.Fatal("invalid or ambiguous child capability", platform, item.ID)
			}
			seen[item.ID] = true
			if item.Integration == "accepted" || item.Acceptance != nil && item.Acceptance.Evidence == "" {
				t.Fatal("child acceptance invented", platform, item.ID)
			}
			if platform == "lajiaoliao" && item.Acceptance != nil {
				expected, exists := nativeDetailAcceptanceForPlatform(platform)[item.ID]
				if !exists || *item.Acceptance != expected ||
					!strings.HasPrefix(item.Acceptance.Evidence, "docs/test-report.md#辣椒聊") ||
					*item.Acceptance == nativeDetailAcceptanceForPlatform("wsl")[item.ID] {
					t.Fatal("WSL acceptance leaked into Chili", item.ID)
				}
			}
		}
		if len(parents) == 0 && len(seen) != 0 {
			t.Fatal("empty platform fallback")
		}
		for _, definition := range CatalogForPlatform(platform) {
			for _, id := range definition.NativeDependencies {
				if parents[id].ID == "" {
					t.Fatal("business dependency missing from its platform", platform, definition.ID, id)
				}
			}
		}
	}
	seen := map[string]NativeDetail{}
	for _, item := range NativeDetailsForPlatform("wsl") {
		seen[item.ID] = item
	}
	for _, id := range []string{"signup_sms", "signup_submit", "account_nickname", "account_avatar",
		"privacy_search", "privacy_friend", "privacy_invitation", "privacy_calls", "change_password",
		"change_phone", "secondary_security_code", "legal_identity", "send_voice", "send_video",
		"send_file", "invite_group_request", "accept_group_invitation", "set_self_card", "set_other_card",
		"enable_group_mute", "disable_group_mute", "accept_friend", "reject_friend"} {
		if seen[id].ID == "" {
			t.Fatal("missing separately verifiable scope", id)
		}
	}
	for _, id := range []string{"set_self_card", "invite_group_request", "accept_group_invitation",
		"enable_group_mute", "disable_group_mute", "privacy_calls", "set_other_card",
		"account_nickname", "account_avatar"} {
		if seen[id].Acceptance == nil {
			t.Fatal("reviewed scope hidden", id)
		}
	}
	for _, id := range []string{"privacy_search", "privacy_friend",
		"privacy_invitation", "change_password", "change_phone", "secondary_security_code", "legal_identity"} {
		if seen[id].Acceptance != nil {
			t.Fatal("call-setting result inherited by another setting", id)
		}
	}
	if seen["set_self_card"].Acceptance.Result == seen["set_other_card"].Acceptance.Result ||
		seen["account_nickname"].Acceptance.Result == seen["account_avatar"].Acceptance.Result ||
		seen["privacy_calls"].Acceptance.VerifiedAt == seen["account_nickname"].Acceptance.VerifiedAt {
		t.Fatal("different field or target evidence collapsed into its parent")
	}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		roles := 0
		for _, item := range NativeCatalogForPlatform(platform) {
			if platform == "lajiaoliao" && (item.ID == "grant_admin" || item.ID == "revoke_admin") &&
				(item.Support != "confirmed" || item.Integration != "partial") {
				t.Fatal("independently adapted role write missing", platform, item.ID)
			}
			if platform == "wsl" && (item.ID == "grant_admin" || item.ID == "revoke_admin") &&
				(item.Support != "confirmed" || item.Integration != "partial") {
				t.Fatal("durable controlled role write missing", item.ID)
			}
			if item.ID == "grant_admin" || item.ID == "revoke_admin" {
				roles++
			}
		}
		if roles != 2 {
			t.Fatal("role write gap hidden", platform)
		}
	}
	raw, _ := json.Marshal(seen)
	for _, forbidden := range []string{`"account_id"`, `"native_uid"`, `"token"`, `"password"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("runtime identity or secret in static directory")
		}
	}
	seen["set_self_card"].Acceptance.Result = "mutated"
	for _, item := range NativeDetailsForPlatform("wsl") {
		if item.Acceptance != nil && item.Acceptance.Result == "mutated" {
			t.Fatal("caller mutated shared acceptance")
		}
	}
}
