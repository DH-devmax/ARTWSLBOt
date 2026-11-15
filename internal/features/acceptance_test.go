package features

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestControlledAcceptanceDoesNotChangeProductionAdmission(t *testing.T) {
	checks := NativeAcceptanceForPlatform("wsl")
	if len(checks) != 36 {
		t.Fatal("unexpected controlled acceptance count")
	}
	for id, check := range checks {
		capability, found := FindNative(id)
		if !found || capability.Integration == "accepted" {
			t.Fatal("controlled result changed production admission", id)
		}
		if _, err := time.Parse(time.RFC3339, check.VerifiedAt); err != nil ||
			check.Scope == "" || check.Result == "" || check.Remaining == "" ||
			!strings.HasPrefix(check.Evidence, "docs/test-report.md#") {
			t.Fatal("controlled result lost scope, limitations or evidence", id)
		}
		definition := Definition{NativeDependencies: []string{id}}
		if len(NativeBlockers(definition)) != 1 || len(NativeBlockersForPlatform("wsl", definition)) != 1 {
			t.Fatal("historical test activated native execution", id)
		}
	}
	delete(checks, "mute_member")
	if len(NativeAcceptanceForPlatform("wsl")) != 36 {
		t.Fatal("caller mutated shared acceptance metadata")
	}
	for _, platform := range []string{"", "unknown"} {
		if len(NativeAcceptanceForPlatform(platform)) != 0 {
			t.Fatal("acceptance leaked into another platform", platform)
		}
	}
	raw, _ := json.Marshal(NativeAcceptanceForPlatform("wsl"))
	for _, forbidden := range []string{"password", "token", "native_uid", "account_id", "group_id"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("native runtime identity or secrets in public metadata")
		}
	}
}

func TestChiliControlledAcceptanceIsIndependentAndCannotEnableProduction(t *testing.T) {
	checks := NativeAcceptanceForPlatform("lajiaoliao")
	if len(checks) != 38 {
		t.Fatal("unexpected Chili controlled acceptance count")
	}
	for _, id := range []string{"list_members", "read_roles", "grant_admin", "revoke_admin"} {
		check, ok := checks[id]
		if !ok || !strings.Contains(check.Scope, "辣椒聊") || check.Result == "" || check.Remaining == "" ||
			check.Evidence != "docs/test-report.md#辣椒聊隔离测试绑定与角色验收2026-11-03" {
			t.Fatal("missing platform-specific scope or evidence", id)
		}
		if _, err := time.Parse(time.RFC3339, check.VerifiedAt); err != nil {
			t.Fatal("invalid evidence time", id)
		}
		if len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("controlled result activated production", id)
		}
		if check == NativeAcceptanceForPlatform("wsl")[id] {
			t.Fatal("another platform's acceptance was inherited", id)
		}
	}
	delete(checks, "grant_admin")
	if len(NativeAcceptanceForPlatform("lajiaoliao")) != 38 {
		t.Fatal("caller mutated shared Chili evidence")
	}
	for _, id := range []string{"mute_member", "unmute_member", "read_mutes"} {
		if check, ok := checks[id]; !ok || check.Evidence != "docs/test-report.md#辣椒聊头像与群管实操验收2026-11-04" ||
			check.Result == "" || check.Remaining == "" ||
			len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("moderation evidence missing or activated production", id)
		}
	}
	for _, id := range []string{"recall_message", "receive_message", "list_groups",
		"read_quote", "read_mentions", "read_forward", "read_images", "read_avatars", "read_recalls"} {
		check, ok := checks[id]
		if !ok || check.Evidence != "docs/test-report.md#辣椒聊解包补齐与真实发送撤回验收2026-11-03" ||
			check.Result == "" || check.Remaining == "" {
			t.Fatal("Chili completion evidence absent", id)
		}
		if _, err := time.Parse(time.RFC3339, check.VerifiedAt); err != nil {
			t.Fatal("invalid completion evidence time", id)
		}
		if len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("controlled completion opened production", id)
		}
	}
	for _, id := range []string{"read_contact_cards", "read_video"} {
		check, ok := checks[id]
		if !ok || check.Evidence != "docs/test-report.md#辣椒聊基础资料与新增媒体验收2026-11-04" ||
			check.Result == "" || check.Remaining == "" {
			t.Fatal("new read evidence absent", id)
		}
		if len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("media read enabled production", id)
		}
	}
	for _, id := range []string{"send_text", "quote_reply", "mention_member", "send_image",
		"send_media", "send_private_text", "send_private_image"} {
		check, ok := checks[id]
		if !ok || check.Evidence != "docs/test-report.md#辣椒聊本站头像与八种发送验收2026-11-04" ||
			check.Result == "" || check.Remaining == "" ||
			len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("independent Chili send evidence absent or enabled production", id)
		}
		if _, err := time.Parse(time.RFC3339, check.VerifiedAt); err != nil {
			t.Fatal("invalid send evidence time", id)
		}
		if check == NativeAcceptanceForPlatform("wsl")[id] {
			t.Fatal("WSL send acceptance inherited", id)
		}
	}
	for _, id := range []string{"read_group_events"} {
		check, ok := checks[id]
		if !ok || check.Evidence != "docs/test-report.md#辣椒聊本站本人退群验收2026-11-04" ||
			check.Result == "" || check.Remaining == "" ||
			len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("native group reads missing independent evidence or admitted production", id)
		}
	}
	details := nativeDetailAcceptanceForPlatform("lajiaoliao")
	if len(details) != 11 {
		t.Fatal("unexecuted fields inherited avatar or media acceptance")
	}
	for _, id := range []string{"account_avatar", "send_voice", "send_video"} {
		check, ok := details[id]
		if !ok || check.Evidence != "docs/test-report.md#辣椒聊本站头像与八种发送验收2026-11-04" ||
			check.Result == "" || check.Remaining == "" || check == nativeDetailAcceptanceForPlatform("wsl")[id] {
			t.Fatal("missing independent atomic evidence", id)
		}
		if _, err := time.Parse(time.RFC3339, check.VerifiedAt); err != nil {
			t.Fatal("invalid atomic evidence time", id)
		}
	}
	const partialEvidence = "docs/test-report.md#辣椒聊剩余原生能力受控验收2026-11-04"
	for _, id := range []string{"session_restore", "edit_group_info", "set_member_card", "mute_group",
		"publish_notice", "list_friends", "kick_member"} {
		check, ok := checks[id]
		if !ok || check.Evidence != partialEvidence || check.Remaining == "" ||
			len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("partial acceptance lost scope or enabled production", id)
		}
		if _, err := time.Parse(time.RFC3339, check.VerifiedAt); err != nil {
			t.Fatal("invalid partial evidence time", id)
		}
	}
	for _, id := range []string{"set_self_card", "set_other_card", "enable_group_mute",
		"disable_group_mute", "kick_member_once"} {
		check, ok := details[id]
		if !ok || check.Evidence != partialEvidence || check.Remaining == "" {
			t.Fatal("atomic result was not recorded independently", id)
		}
	}
	const followupEvidence = "docs/test-report.md#辣椒聊邀请恢复与永久禁入受控验收2026-11-04"
	for _, id := range []string{"read_group_bans", "remove_group_ban"} {
		check, ok := checks[id]
		if !ok || check.Evidence != followupEvidence || check.Result == "" || check.Remaining == "" ||
			check == NativeAcceptanceForPlatform("wsl")[id] ||
			len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("membership evidence missing or enabled production", id)
		}
		if _, err := time.Parse(time.RFC3339, check.VerifiedAt); err != nil {
			t.Fatal("invalid membership evidence time", id)
		}
	}
	for _, id := range []string{"kick_member_permanent", "invite_group_request"} {
		check, ok := details[id]
		if !ok || check.Evidence != followupEvidence || check.Result == "" || check.Remaining == "" ||
			check == nativeDetailAcceptanceForPlatform("wsl")[id] {
			t.Fatal("membership child lost independent evidence", id)
		}
		if _, err := time.Parse(time.RFC3339, check.VerifiedAt); err != nil {
			t.Fatal("invalid membership child evidence time", id)
		}
	}
	const selfEvidence = "docs/test-report.md#辣椒聊本站本人接受邀请2026-11-04"
	for _, id := range []string{"read_invitations", "invite_group"} {
		check, ok := checks[id]
		if !ok || check.Evidence != selfEvidence || check.Result == "" || check.Remaining == "" ||
			len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("website self acceptance lost independent scope or enabled production", id)
		}
	}
	self, ok := details["accept_group_invitation"]
	if !ok || self.Evidence != selfEvidence || self.Result == "" || self.Remaining == "" ||
		self == nativeDetailAcceptanceForPlatform("wsl")["accept_group_invitation"] ||
		self == details["invite_group_request"] {
		t.Fatal("website self acceptance inherited another action or platform")
	}
	if _, err := time.Parse(time.RFC3339, self.VerifiedAt); err != nil {
		t.Fatal("invalid website self acceptance time")
	}
	leave, ok := checks["leave_group"]
	if !ok || leave.Evidence != "docs/test-report.md#辣椒聊本站本人退群验收2026-11-04" ||
		leave.Result == "" || leave.Remaining == "" ||
		leave == NativeAcceptanceForPlatform("wsl")["leave_group"] ||
		leave == checks["invite_group"] ||
		len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{"leave_group"}})) != 1 {
		t.Fatal("website self leave lost independent evidence or enabled production")
	}
	if _, err := time.Parse(time.RFC3339, leave.VerifiedAt); err != nil {
		t.Fatal("invalid website self leave time")
	}
	raw, _ := json.Marshal(NativeAcceptanceForPlatform("lajiaoliao"))
	for _, forbidden := range []string{"password", "token", "native_uid", "account_id", "group_id"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("native runtime identity or secrets in public metadata")
		}
	}
}

func TestWSLControlledMatrixCoversEveryNativeItem(t *testing.T) {
	raw, err := os.ReadFile("../../docs/wsl-native-acceptance-20261102.md")
	if err != nil {
		t.Fatal(err)
	}
	report := string(raw)
	pending := NativePendingAcceptanceForPlatform("wsl")
	currentReports := map[string]string{}
	for id, requirement := range pending {
		if requirement.Status != "not_run" || requirement.Scope == "" || requirement.Remaining == "" {
			t.Fatal("pending matrix item lost its unverified scope", id)
		}
		if _, ok := NativeAcceptanceForPlatform("wsl")[id]; ok {
			t.Fatal("unexecuted matrix item acquired historical acceptance", id)
		}
		path, section, ok := strings.Cut(requirement.Evidence, "#")
		if !ok || path != "docs/native-social.md" || section != "删除好友" {
			t.Fatal("pending matrix item lost its current scope document", id)
		}
		raw, err := os.ReadFile("../../" + path)
		if err != nil || !strings.Contains(string(raw), "## "+section) ||
			!strings.Contains(string(raw), "`"+id+"`") {
			t.Fatal("pending matrix item missing from its current scope document", id, err)
		}
		currentReports[id] = string(raw)
	}
	covered := func(id string) bool {
		if current, ok := currentReports[id]; ok {
			return strings.Contains(current, "`"+id+"`")
		}
		return strings.Contains(report, "`"+id+"`")
	}
	for _, item := range NativeCatalogForPlatform("wsl") {
		if !covered(item.ID) {
			t.Fatal("native capability missing from controlled acceptance matrix", item.ID)
		}
	}
	for _, item := range NativeDetailsForPlatform("wsl") {
		if !covered(item.ID) {
			t.Fatal("atomic capability missing from controlled acceptance matrix", item.ID)
		}
	}
}

func TestFriendDeleteControlledAcceptanceRemainsUnverifiedAndIndependent(t *testing.T) {
	for platform, name := range map[string]string{"wsl": "旺商聊", "lajiaoliao": "辣椒聊"} {
		t.Run(platform, func(t *testing.T) {
			pending := NativePendingAcceptanceForPlatform(platform)
			requirement, ok := pending["friend_delete"]
			if !ok || len(pending) != 1 || requirement.Status != "not_run" ||
				!strings.Contains(requirement.Scope, name) || !strings.Contains(requirement.Scope, "native_debug") ||
				!strings.Contains(requirement.Scope, "一次预算") || !strings.Contains(requirement.Remaining, "未验收") ||
				!strings.Contains(requirement.Remaining, "不继承") || requirement.Evidence != "docs/native-social.md#删除好友" {
				t.Fatal("deletion lost independent unverified acceptance metadata", requirement)
			}
			for _, other := range []string{"wsl", "lajiaoliao"} {
				if other != platform && requirement == NativePendingAcceptanceForPlatform(other)["friend_delete"] {
					t.Fatal("another platform's deletion scope was inherited")
				}
			}
			if _, ok := NativeAcceptanceForPlatform(platform)["friend_delete"]; ok {
				t.Fatal("unexecuted deletion acquired historical acceptance")
			}
			if _, ok := nativeDetailAcceptanceForPlatform(platform)["friend_delete"]; ok {
				t.Fatal("deletion acquired another action's atomic acceptance")
			}
			if _, ok := NativeDemonstrationsForPlatform(platform)["friend_delete"]; ok {
				t.Fatal("deletion acquired an unrelated client demonstration")
			}
			found := false
			for _, item := range NativeCatalogForPlatform(platform) {
				if item.ID != "friend_delete" {
					continue
				}
				found = true
				if item.Kind != "action" || item.Support != "confirmed" || item.Integration != "partial" ||
					!strings.Contains(item.Evidence, "docs/native-social.md") {
					t.Fatal("implemented deletion claimed complete acceptance", item)
				}
			}
			if !found || NativeUsageForPlatform(platform)["friend_delete"] != NativeManual ||
				NativeBusinessAllowed(platform, "friend_delete") ||
				len(NativeBlockersForPlatform(platform, Definition{NativeDependencies: []string{"friend_delete"}})) != 1 {
				t.Fatal("unverified deletion lost its manual and production boundaries")
			}
			for _, action := range AssistantActions(platform) {
				if action.NativeID == "friend_delete" {
					t.Fatal("manual deletion leaked into assistant actions")
				}
			}
			for _, definition := range CatalogForPlatform(platform) {
				for _, id := range definition.NativeDependencies {
					if id == "friend_delete" {
						t.Fatal("manual deletion leaked into business dependencies", definition.ID)
					}
				}
			}
			delete(pending, "friend_delete")
			if len(NativePendingAcceptanceForPlatform(platform)) != 1 {
				t.Fatal("caller mutated shared pending acceptance metadata")
			}
		})
	}
	for _, platform := range []string{"", "unknown"} {
		if len(NativePendingAcceptanceForPlatform(platform)) != 0 {
			t.Fatal("pending acceptance leaked into another platform")
		}
	}
}
