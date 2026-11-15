package features

import "testing"

func TestMobileSamplesDoNotAdmitWebsiteExecution(t *testing.T) {
	samples := NativeDemonstrationsForPlatform("wsl")
	if len(samples) != 9 || len(NativeDemonstrationsForPlatform("lajiaoliao")) != 4 ||
		len(NativeDemonstrationsForPlatform("unknown")) != 0 {
		t.Fatal("platform evidence inherited or missing")
	}
	controlled := NativeAcceptanceForPlatform("wsl")
	for id, sample := range samples {
		if sample.Scope == "" || sample.Result == "" || sample.Remaining == "" || sample.Evidence == "" {
			t.Fatal("incomplete sample boundary")
		}
		if execution, exists := controlled[id]; exists &&
			(execution.Evidence == sample.Evidence || execution.Scope == sample.Scope) {
			t.Fatalf("website execution inherited mobile evidence: %s", id)
		}
		if len(NativeBlockersForPlatform("wsl", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatalf("mobile sample admitted automatic execution: %s", id)
		}
	}
	for _, id := range []string{"send_media", "send_file"} {
		if _, exists := samples[id]; exists {
			t.Fatalf("unsampled capability inherited a mobile record: %s", id)
		}
	}
	for _, id := range []string{"send_private_text", "send_private_image"} {
		if samples[id].Scope == "" || samples[id].Scope == samples["send_text"].Scope {
			t.Fatal("private sample inherited group scope")
		}
		execution, exists := controlled[id]
		if !exists || execution.Scope == controlled["send_text"].Scope ||
			execution.Scope == samples[id].Scope ||
			execution.Evidence != "docs/test-report.md#旺商聊本站私聊发送验收2026-11-03" {
			t.Fatal("private website execution lost independent evidence")
		}
	}
	if _, exists := controlled["read_quote"]; !exists {
		t.Fatal("authenticated quote reading was not registered separately")
	}
	for _, action := range AssistantActions("wsl") {
		if action.Production {
			t.Fatal("sample inspection raised assistant production admission")
		}
	}
}

func TestChiliMobileGroupSamplesCannotApproveNativeWrites(t *testing.T) {
	samples := NativeDemonstrationsForPlatform("lajiaoliao")
	for id, sample := range samples {
		if sample.Scope == "" || sample.Result == "" || sample.Remaining == "" ||
			sample.Evidence != "docs/lajiaoliao-membership-20261104.md" ||
			len(NativeBlockersForPlatform("lajiaoliao", Definition{NativeDependencies: []string{id}})) != 1 {
			t.Fatal("mobile group sample admitted execution or lost evidence", id)
		}
	}
	for _, id := range []string{"leave_group", "invite_group", "remove_group_ban", "kick_member_permanent"} {
		if _, ok := samples[id]; ok {
			t.Fatal("unobserved native action inherited mobile evidence", id)
		}
	}
	for _, action := range AssistantActions("lajiaoliao") {
		if action.ID == "kick_member_ban" || action.ID == "remove_group_ban" || action.Production {
			t.Fatal("controlled ban tool widened assistant admission")
		}
	}
}
