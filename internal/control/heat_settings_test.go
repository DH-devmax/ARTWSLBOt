package control

import (
	"testing"
	"time"
)

func TestHeatEffectiveGroupOverridesAndMinuteBoundaries(t *testing.T) {
	account := HeatChatSettings{Enabled: true, ReplyProbability: .1, CooldownSeconds: 30, StartHour: 0, EndHour: 24}
	settings := effectiveHeatSettings(account, false, 0, 0, 0, 0, "default")
	if settings.ReplyProbability != .1 || settings.CooldownSeconds != 30 {
		t.Fatal(settings)
	}
	settings = effectiveHeatSettings(account, true, 0, 23*60+58, 1, 60, "group")
	if settings.ReplyProbability != 0 || settings.CooldownSeconds != 60 {
		t.Fatal(settings)
	}
	for _, sample := range []struct {
		hour, minute int
		allowed      bool
	}{
		{23, 57, false}, {23, 58, true}, {23, 59, true}, {0, 0, true}, {0, 1, true}, {0, 2, false},
	} {
		at := time.Date(2026, 11, 4, sample.hour, sample.minute, 59, 0, time.FixedZone("Asia/Shanghai", 28800))
		if settings.allows(at) != sample.allowed {
			t.Fatal(sample, settings)
		}
	}
	settings.AccountStartHour, settings.AccountEndHour = 8, 18
	if settings.allows(time.Date(2026, 11, 4, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 28800))) {
		t.Fatal("account hours bypassed")
	}
	settings = effectiveHeatSettings(account, true, 100, 0, 1439, 0, "group")
	if !settings.allows(time.Date(2026, 11, 4, 23, 59, 59, 0, time.FixedZone("Asia/Shanghai", 28800))) {
		t.Fatal("last minute excluded")
	}
	settings.Enabled = false
	if settings.allows(time.Now()) {
		t.Fatal("account disable bypassed")
	}
}

func TestHeatScopeDigestBindsIdentityAndVersions(t *testing.T) {
	scope := HeatScope{Platform: "wsl", Tenant: "a", Actor: "owner", Group: "g", Account: "c", NativeSelf: "1",
		AccountEpoch: 1, BindingVersion: 1, PersonaVersion: 1, AdmissionVersion: 1, RuntimeVersion: 1,
		Service: WorkspaceServiceContext{TenantID: "a", OwnerID: "owner", OwnerVersion: 1,
			ExecutionMode: "workspace_demo", BillingSource: "platform_workspace", Demo: true}}
	original := string(scope.digest())
	for _, change := range []func(*HeatScope){
		func(s *HeatScope) { s.Platform = "lajiaoliao" }, func(s *HeatScope) { s.Tenant = "b" },
		func(s *HeatScope) { s.Actor = "member" }, func(s *HeatScope) { s.Account = "other" },
		func(s *HeatScope) { s.BindingEpoch++ }, func(s *HeatScope) { s.SettingsVersion++ },
		func(s *HeatScope) { s.PreferenceVersion++ }, func(s *HeatScope) { s.RouteVersion++ },
		func(s *HeatScope) { s.AdmissionVersion++ },
		func(s *HeatScope) { s.RuntimeVersion++ },
		func(s *HeatScope) { s.Service.OwnerVersion++ },
		func(s *HeatScope) { s.Service.OwnerID = "another-owner" },
		func(s *HeatScope) { s.Service.TenantID = "another-space" },
		func(s *HeatScope) { s.Service.ExecutionMode = "operator_demo" },
		func(s *HeatScope) { s.Service.BillingSource = "platform_demo" },
		func(s *HeatScope) { s.Service.Demo = false },
	} {
		copy := scope
		change(&copy)
		if string(copy.digest()) == original {
			t.Fatal("unbound scope")
		}
	}
	scope.Persona = "plain secret"
	scope.ReplyStyle = "private style"
	if string(scope.digest()) != original {
		t.Fatal("plaintext in persistent scope")
	}
}
