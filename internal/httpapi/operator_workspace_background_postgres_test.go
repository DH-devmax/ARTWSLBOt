package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
)

func bindBackgroundDemoGroup(t *testing.T, f *onboardingFixture, platform string) (testClient, string, string) {
	t.Helper()
	client := f.admin
	client.workspace = platform
	c := f.createChallenge(t, client, platform, "background-own-space-"+platform)
	c = f.prove(t, client, platform, c, f.native.owner, "background-native-"+platform)
	onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm",
		"background-confirm-"+platform, map[string]any{"version": c.Version}, 200)
	space, err := control.OperatorWorkspace(context.Background(), f.db.Platform, f.adminID)
	if err != nil {
		t.Fatal(err)
	}
	return client, c.Group.ID, space.ID
}

func backgroundRuntime(t *testing.T, client testClient, group string) control.BusinessRuntime {
	t.Helper()
	var out control.BusinessRuntime
	if err := json.Unmarshal(expect(t, client, "GET", "/groups/"+group+"/business-runtime", nil, 200), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertDemoHasNoCommercialState(t *testing.T, f *onboardingFixture, tenant string) {
	t.Helper()
	var clean bool
	err := f.db.Platform.QueryRow(context.Background(), `SELECT u.role='superadmin' AND u.tenant_id IS NULL
		AND t.owner_id=u.id AND t.operator_owner_id=u.id
		AND NOT EXISTS(SELECT 1 FROM users WHERE tenant_id=t.id)
		AND NOT EXISTS(SELECT 1 FROM subscriptions WHERE tenant_id=t.id)
		AND NOT EXISTS(SELECT 1 FROM subscription_orders WHERE tenant_id=t.id)
		AND NOT EXISTS(SELECT 1 FROM subscription_periods WHERE tenant_id=t.id)
		AND NOT EXISTS(SELECT 1 FROM ai_wallets WHERE tenant_id=t.id)
		FROM tenants t JOIN users u ON u.id=t.owner_id WHERE t.id=$1`, tenant).Scan(&clean)
	if err != nil || !clean {
		t.Fatal("demo control operation manufactured customer identity or commercial state", err)
	}
}

func TestPostgresOperatorWorkspaceRuntimeStartStopKeepsSourceGatesClosed(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			client, group, tenant := bindBackgroundDemoGroup(t, f, platform)
			path := "/groups/" + group + "/business-runtime"
			before := backgroundRuntime(t, client, group)
			if before.Enabled || before.ManagementEnabled || !before.CanManage {
				t.Fatal("onboarding enabled execution or hid the demo owner's runtime switch", before)
			}
			var running control.BusinessRuntime
			if err := json.Unmarshal(expect(t, client, "PUT", path,
				map[string]any{"enabled": true, "version": before.Version}, 200), &running); err != nil ||
				!running.Enabled || !running.ManagementEnabled || running.Version <= before.Version {
				t.Fatal("demo owner could not explicitly enable its proven runtime", running, err)
			}
			if err := control.RequireBusinessAdmission(context.Background(), f.db.Platform,
				platform, group, "mute_member", "automatic"); err == nil {
				t.Fatal("runtime toggle opened the automatic source gate")
			}
			expect(t, client, "PUT", path, map[string]any{"enabled": false, "version": before.Version}, 409)
			var stopped control.BusinessRuntime
			if err := json.Unmarshal(expect(t, client, "PUT", path,
				map[string]any{"enabled": false, "version": running.Version}, 200), &stopped); err != nil ||
				stopped.Enabled || stopped.ManagementEnabled {
				t.Fatal("demo runtime did not stop", stopped, err)
			}
			other := control.NewID()
			if _, err := f.db.Platform.Exec(context.Background(), `INSERT INTO users(id,username,password_hash,role)
				VALUES($1,$1::uuid::text,'unused-synthetic','superadmin')`, other); err != nil {
				t.Fatal(err)
			}
			foreign := f.client(t, client.base, other)
			foreign.workspace = platform
			expect(t, foreign, "PUT", path, map[string]any{"enabled": true, "version": stopped.Version}, 404)
			customer := f.alice
			customer.workspace = platform
			expect(t, customer, "PUT", path, map[string]any{"enabled": true, "version": stopped.Version}, 404)
			assertDemoHasNoCommercialState(t, f, tenant)
			if f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
				t.Fatal("runtime toggle dispatched a native mutation")
			}
		})
	}
}

func backgroundChatAccount(t *testing.T, f *onboardingFixture, platform, group string, index int) string {
	t.Helper()
	ctx, account := context.Background(), control.NewID()
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO accounts(id,name,platform,purpose,workspace,
		auth_state,credential_sealed,native_uid,connection_enabled)
		VALUES($1,$2,$3,'chat','maibot','authenticated',$4,$2,false)`,
		account, fmt.Sprintf("synthetic-demo-chat-%s-%d", platform, index), platform, []byte("synthetic-credential")); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO personas(account_id,nickname,personality) VALUES($1,'synthetic','scoped synthetic persona')`,
		`INSERT INTO account_receivers(account_id,enabled,owner,revision,lease_until,observed_at,snapshot)
			VALUES($1,true,'synthetic-chat-receiver',1,clock_timestamp()+interval '10 minutes',clock_timestamp(),
				jsonb_build_object('state','connected','synced_at',clock_timestamp(),'heartbeat_at',clock_timestamp()))`,
	} {
		if _, err := f.db.Platform.Exec(ctx, statement, account); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
		VALUES($1,$2,'member',clock_timestamp(),'native')`, account, group); err != nil {
		t.Fatal(err)
	}
	return account
}

func TestPostgresOperatorWorkspaceChatAssignmentUsesServiceEntitlement(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingFixture(t)
			ctx := context.Background()
			client, group, tenant := bindBackgroundDemoGroup(t, f, platform)
			runtime := backgroundRuntime(t, client, group)
			expect(t, client, "PUT", "/groups/"+group+"/business-runtime",
				map[string]any{"enabled": true, "version": runtime.Version}, 200)
			if err := f.app.Gateway.Configure(ctx, control.Actor{ID: f.adminID, Role: "superadmin"},
				control.GatewaySettings{Enabled: true, PlatformLimit: 100_000_000},
				control.ModelRoute{Alias: "default", Enabled: true, Verified: true,
					Rates: aigateway.Rates{Input: 1_000_000, Output: 1_000_000},
					Model: assistant.ModelConfig{BaseURL: "https://synthetic.invalid/v1", Model: "synthetic", APIKey: "synthetic"}}); err != nil {
				t.Fatal(err)
			}
			account := backgroundChatAccount(t, f, platform, group, 1)
			var version int64
			if err := f.db.Platform.QueryRow(ctx, `SELECT version FROM group_bindings WHERE group_id=$1`, group).Scan(&version); err != nil {
				t.Fatal(err)
			}
			type result struct {
				Version   int64  `json:"version"`
				Requested bool   `json:"requested"`
				Enabled   bool   `json:"enabled"`
				Reason    string `json:"reason"`
			}
			path := "/groups/" + group + "/ai-service"
			toggle := func(enabled bool) result {
				t.Helper()
				var out result
				if err := json.Unmarshal(expect(t, client, "PUT", path,
					map[string]any{"enabled": enabled, "version": version}, 200), &out); err != nil {
					t.Fatal(err)
				}
				version = out.Version
				return out
			}
			if out := toggle(true); !out.Requested || out.Enabled || out.Reason != "heat_chat_account_disabled" {
				t.Fatal("demo assignment activated default-off account settings", out)
			}
			sealed, err := f.vault.Seal("maibot:"+account+":chat",
				[]byte(`{"enabled":true,"reply_probability":0,"cooldown_seconds":30,"start_hour":0,"end_hour":24}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Platform.Exec(ctx, `INSERT INTO maibot_settings(account_id,section,sealed,version,updated_by)
				VALUES($1,'chat',$2,1,$3)`, account, sealed, f.adminID); err != nil {
				t.Fatal(err)
			}
			if out := toggle(true); out.Enabled || out.Reason != "business_admission_closed" {
				t.Fatal("demo settings opened a source gate", out)
			}
			expect(t, client, "PUT", "/capabilities/gates/send_text/heatbot", map[string]any{
				"state": "pilot", "version": 0, "pilot_group_ids": []string{group},
				"evidence_refs": []string{"acceptance:SYNTHETIC-DEMO-BACKGROUND"},
			}, 200)
			if _, err = f.app.HeatChat.SetHeatAdmission(ctx, onboardingRef(t, client, platform),
				platform, true, 1, strings.Repeat("b", 64)); err != nil {
				t.Fatal(err)
			}
			if out := toggle(true); !out.Enabled || !out.Requested {
				t.Fatal("explicitly admitted demo chat still required a customer subscription", out)
			}
			second := backgroundChatAccount(t, f, platform, group, 2)
			chatClient := client
			chatClient.workspace = "maibot"
			var preview control.AssignmentPreview
			assignmentPath := "/groups/" + group + "/assignment"
			if err = json.Unmarshal(expect(t, chatClient, "GET", assignmentPath, nil, 200), &preview); err != nil ||
				preview.Source != account {
				t.Fatal("demo chat assignment lost its source", preview, err)
			}
			var targetEpoch int64
			for _, candidate := range preview.Candidates {
				if candidate.ID == second && candidate.Eligible {
					targetEpoch = candidate.Epoch
				}
			}
			if targetEpoch == 0 {
				t.Fatal("verified chat target unavailable", preview)
			}
			input := map[string]any{"target_account_id": second, "source_account_id": account,
				"source_epoch": preview.SourceEpoch, "target_epoch": targetEpoch,
				"version": preview.Version, "binding_epoch": preview.Epoch}
			raw := onboardingAPI(t, chatClient, "POST", assignmentPath, "demo-chat-assignment-"+platform, input, 200)
			var assigned struct{ Version int64 }
			if json.Unmarshal(raw, &assigned) != nil || assigned.Version <= preview.Version {
				t.Fatal("demo chat assignment was not persisted", string(raw))
			}
			version = assigned.Version
			other := control.NewID()
			if _, err = f.db.Platform.Exec(ctx, `INSERT INTO users(id,username,password_hash,role)
				VALUES($1,$1::uuid::text,'unused-synthetic','superadmin')`, other); err != nil {
				t.Fatal(err)
			}
			foreign := f.client(t, client.base, other)
			foreign.workspace = "maibot"
			expect(t, foreign, "GET", assignmentPath, nil, 404)
			onboardingAPI(t, foreign, "POST", assignmentPath, "foreign-demo-chat-"+platform, input, 404)
			foreign.workspace = platform
			expect(t, foreign, "PUT", path, map[string]any{"enabled": true, "version": version}, 404)
			if out := toggle(false); out.Enabled || out.Requested {
				t.Fatal("demo chat could not be stopped", out)
			}
			var dormant bool
			if err = f.db.Platform.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM ai_calls)
				AND NOT EXISTS(SELECT 1 FROM heat_chat_jobs) AND NOT EXISTS(SELECT 1 FROM operations)`).Scan(&dormant); err != nil || !dormant {
				t.Fatal("assignment or toggles generated model/native work", err)
			}
			assertDemoHasNoCommercialState(t, f, tenant)
		})
	}
}
