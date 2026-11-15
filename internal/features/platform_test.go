package features

import "testing"

func TestPlatformCapabilitiesFailClosed(t *testing.T) {
	for _, p := range []string{"wsl", "lajiaoliao", "unknown", ""} {
		for _, d := range CatalogForPlatform(p) {
			if len(d.NativeDependencies) > 0 && len(NativeBlockersForPlatform(p, d)) == 0 {
				t.Fatal("unaccepted platform activated", p, d.ID)
			}
			if p == "lajiaoliao" && (d.Module == "finance" || d.Module == "activities") {
				t.Fatal("out-of-scope module exposed")
			}
		}
	}
	if len(CatalogForPlatform("")) != 0 || len(NativeCatalogForPlatform("")) != 0 {
		t.Fatal("empty platform fallback")
	}
	found := false
	for _, c := range NativeCatalogForPlatform("lajiaoliao") {
		if c.ID == "account_signup" {
			found = c.Support == "confirmed" && c.Integration == "partial"
		}
	}
	if !found {
		t.Fatal("implemented Chili signup routes missing from directory")
	}
	for _, id := range []string{"quote_reply", "mention_member", "recall_message", "read_quote", "read_mentions", "read_recalls", "read_forward"} {
		found = false
		for _, c := range NativeCatalogForPlatform("lajiaoliao") {
			if c.ID == id {
				found = c.Support == "confirmed" && c.Integration == "partial"
			}
		}
		if !found {
			t.Fatal("observed app capability missing or fabricated as integrated", id)
		}
	}
}

func TestSecurityAndRecallCatalogDoesNotClaimProductionAcceptance(t *testing.T) {
	for _, detail := range NativeDetailsForPlatform("wsl") {
		if detail.ID == "secondary_security_code" || detail.ID == "legal_identity" {
			if detail.Support != "confirmed" || detail.Integration != "deferred" || detail.Acceptance != nil {
				t.Fatal("deferred security capability treated as active", detail.ID)
			}
		}
	}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, item := range NativeCatalogForPlatform(platform) {
			if item.ID == "read_recalls" && (item.Support != "confirmed" || item.Integration != "partial") {
				t.Fatal("recall read missing or falsely accepted", platform)
			}
		}
	}
}

func TestLajiaoliaoKeepsIndependentContractsAndDeferredIDs(t *testing.T) {
	items := map[string]NativeCapability{}
	for _, item := range NativeCatalogForPlatform("lajiaoliao") {
		items[item.ID] = item
	}
	for _, id := range []string{"mute_member", "unmute_member"} {
		item := items[id]
		if item.Role != "管理员操作普通成员；群主可操作普通成员或管理员，不能操作本人或群主" {
			t.Fatal("Chili role contract lost", id)
		}
	}
	for _, id := range []string{"send_media", "send_image", "send_private_text", "send_private_image"} {
		item := items[id]
		if item.Support != "confirmed" || item.Integration != "partial" {
			t.Fatal("Chili send implementation or pending real acceptance misreported", id)
		}
	}
	for _, id := range []string{"read_images", "read_video", "read_contact_cards", "read_avatars"} {
		item := items[id]
		if item.Support != "confirmed" || item.Integration != "partial" {
			t.Fatal("unpacked Chili read contract missing", id)
		}
	}
	for _, id := range []string{"invite_group", "account_settings", "grant_admin", "revoke_admin", "mute_group",
		"kick_member", "publish_notice", "set_member_card", "list_friends", "approve_friend", "leave_group"} {
		if item := items[id]; item.Support != "confirmed" || item.Integration != "partial" {
			t.Fatal("independently adapted Chili primitive missing", id)
		}
	}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		deferred := 0
		for _, item := range NativeDetailsForPlatform(platform) {
			if item.ID == "secondary_security_code" || item.ID == "legal_identity" {
				if item.Integration != "deferred" || item.Acceptance != nil {
					t.Fatal("deferred ID removed or activated", platform, item.ID)
				}
				if platform == "lajiaoliao" && item.Support != "review" {
					t.Fatal("WSL security support inherited by Chili", item.ID)
				}
				deferred++
			}
		}
		if deferred != 2 {
			t.Fatal("deferred IDs missing", platform)
		}
	}
}
