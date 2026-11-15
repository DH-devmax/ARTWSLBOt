package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"wangshangliao/internal/control"
)

func TestPostgresOperatorWorkspaceOwnBindingAndSharedCenter(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	space, err := control.OperatorWorkspace(ctx, f.db.Platform, f.adminID)
	if err != nil || space.Kind != "operator_demo" || space.OwnerID != f.adminID {
		t.Fatal("operator creation did not atomically provision a real space", space, err)
	}
	var userTenant *string
	if err = f.db.Platform.QueryRow(ctx, `SELECT tenant_id::text FROM users WHERE id=$1`, f.adminID).Scan(&userTenant); err != nil || userTenant != nil {
		t.Fatal("operator became an ordinary customer", err)
	}
	counts := func() string {
		var result string
		if err := f.db.Platform.QueryRow(ctx, `SELECT jsonb_build_array((SELECT count(*) FROM tenants),
			(SELECT count(*) FROM subscriptions),(SELECT count(*) FROM subscription_orders),
			(SELECT count(*) FROM users))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := counts()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		client := f.admin
		client.workspace = platform
		c := f.createChallenge(t, client, platform, "own-space-"+platform)
		if c.Customer.TenantID != space.ID || c.Scope.TenantID != space.ID {
			t.Fatal("default onboarding chose a customer instead of the actual operator space", c)
		}
		c = f.prove(t, client, platform, c, f.native.owner, "workspace-native-"+platform)
		onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm",
			"own-confirm-"+platform, map[string]any{"version": c.Version}, 200)
		var safe bool
		err = f.db.Platform.QueryRow(ctx, `SELECT b.tenant_id=$2 AND NOT b.management_enabled
			AND NOT b.ai_enabled AND NOT b.ai_requested AND NOT r.enabled
			AND o.tenant_id=$2 AND o.state='active'
			FROM group_bindings b JOIN group_business_runtime r ON r.group_id=b.group_id
			JOIN group_binding_ownership o ON o.group_id=b.group_id WHERE b.group_id=$1`, c.Group.ID, space.ID).Scan(&safe)
		if err != nil || !safe {
			t.Fatal("own space binding lost its native proof or enabled a runtime", err)
		}
		for _, path := range []string{"/user-center", "/user-center/groups?platform=" + platform, "/user-center/usage", "/billing"} {
			var response map[string]json.RawMessage
			raw := onboardingAPI(t, client, "GET", path, "", nil, 200)
			if err = json.Unmarshal(raw, &response); err != nil {
				t.Fatal(err)
			}
			var current control.ResourceWorkspace
			var plan map[string]any
			if json.Unmarshal(response["workspace"], &current) != nil || current.ID != space.ID ||
				json.Unmarshal(response["effective_plan"], &plan) != nil ||
				plan["id"] != "operator_unlimited" || plan["unlimited"] != true || plan["permanent"] != true ||
				plan["system_only"] != true || plan["group_limit"] != nil || plan["credit_limit"] != nil {
				t.Fatal("shared center omitted explicit workspace/system entitlement", path, string(raw))
			}
		}
		var hidden struct{ Items []any }
		raw := onboardingAPI(t, f.alice, "GET", "/user-center/groups?platform="+platform, "", nil, 200)
		if json.Unmarshal(raw, &hidden) != nil || len(hidden.Items) != 0 {
			t.Fatal("customer could see operator's groups", string(raw))
		}
		onboardingAPI(t, f.alice, "GET", "/user-center/groups?tenant_id="+space.ID, "", nil, 403)
	}
	if after := counts(); after != before {
		t.Fatal("onboarding or GET fabricated a customer/order/subscription", before, after)
	}
	other := control.NewID()
	if _, err = f.db.Platform.Exec(ctx, `INSERT INTO users(id,username,password_hash,role)
		VALUES($1,$2,'unused-synthetic-password','superadmin')`, other, "other-space-"+other); err != nil {
		t.Fatal(err)
	}
	otherSpace, err := control.OperatorWorkspace(ctx, f.db.Platform, other)
	if err != nil || otherSpace.ID == space.ID {
		t.Fatal("operators share a demo workspace", otherSpace, err)
	}
	for _, statement := range []string{
		`UPDATE tenants SET operator_owner_id='` + other + `'::uuid,owner_id='` + other + `'::uuid WHERE id=$1`,
		`INSERT INTO users(username,password_hash,role,tenant_id) VALUES('not-a-demo-member','synthetic','user',$1)`,
		`UPDATE tenants SET space_kind='tenant_business',operator_owner_id=NULL WHERE id=$1`,
	} {
		if _, err = f.db.Platform.Exec(ctx, statement, space.ID); err == nil {
			t.Fatal("workspace owner-only constraint could be changed", statement)
		}
	}
}

func TestPostgresOperatorWorkspaceDefaultListAndCrossOperatorIsolation(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	own := f.createChallenge(t, f.admin, "wsl", "own-empty-default")
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+own.ID+"/cancel", "",
		map[string]any{"version": own.Version}, 200)
	other := control.NewID()
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO users(id,username,password_hash,role)
		VALUES($1,$2,'synthetic-password-unused','superadmin')`, other, fmt.Sprint("separate-", other)); err != nil {
		t.Fatal(err)
	}
	client := f.client(t, f.admin.base, other)
	client.workspace = "wsl"
	c := f.createChallenge(t, client, "wsl", "other-own-group")
	c = f.prove(t, client, "wsl", c, f.native.owner, "other-demo-native")
	onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm",
		"other-confirm", map[string]any{"version": c.Version}, 200)
	onboardingAPI(t, f.admin, "GET", "/group-binding/challenges/"+c.ID, "", nil, 404)
	if err := control.RequireGroup(ctx, f.db.Platform, control.Actor{ID: f.adminID, Role: "superadmin"}, c.Group.ID); err == nil {
		t.Fatal("another operator inherited a private workspace's group")
	}
	for _, actor := range []control.Actor{
		{ID: f.adminID, Role: "superadmin"},
		{ID: f.aliceID, TenantID: f.tenantA, Role: "user"},
	} {
		raw, err := control.SafeBusinessOperations(ctx, f.db.Platform, actor, "wsl", "", 20)
		if err != nil || strings.Contains(string(raw), c.Group.ID) {
			t.Fatal("operation history leaked another private workspace", string(raw), err)
		}
	}
	var page struct{ Total int }
	for _, path := range []string{"/user-center/groups", "/user-center/groups?workspace=all"} {
		raw := onboardingAPI(t, f.admin, "GET", path, "", nil, 200)
		if json.Unmarshal(raw, &page) != nil || page.Total != 0 {
			t.Fatal("operator listed another private demo workspace", string(raw))
		}
	}
	onboardingAPI(t, f.alice, "GET", "/user-center/groups?workspace=all", "", nil, 403)
}

func TestPostgresOnboardingRobotSearchAccountComesFromSealedSession(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	for _, tc := range []struct{ platform, nativeID, adding, domain, payload string }{
		{"wsl", "23691273", "340738649", "native-session:",
			`{"session":{"uid":23691273,"account_id":"340738649","token":"never-output-token"},"password":"never-output-password"}`},
		{"lajiaoliao", "9680209", "9680209", "lajiaoliao-session:",
			`{"user_id":"9680209","chat_token":"never-output-token","password":"never-output-password"}`},
		{"lajiaoliao", "self-string", "self-string", "lajiaoliao-session:",
			`{"user_id":"self-string","chat_token":"never-output-token","password":"never-output-password"}`},
	} {
		platform, nativeID, adding, domain := tc.platform, tc.nativeID, tc.adding, tc.domain
		account, raw := f.accounts[platform], []byte(tc.payload)
		sealed, err := f.vault.Seal(domain+account, raw)
		clear(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Platform.Exec(ctx, `UPDATE accounts SET native_uid=$2,credential_sealed=$3 WHERE id=$1`,
			account, nativeID, sealed); err != nil {
			t.Fatal(err)
		}
		client := f.admin
		client.workspace = platform
		c := f.createChallenge(t, client, platform, "identity-from-session-"+platform+"-"+nativeID)
		if !c.Robot.AddAvailable || c.Robot.AddAccount != adding || c.Robot.NativeID != nativeID {
			t.Fatal("search account was replaced by an internal UID or account label", c.Robot)
		}
		bodies := [][]byte{onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID, "", nil, 200)}
		for _, body := range bodies {
			if strings.Contains(string(body), "never-output-") || strings.Contains(string(body), `"credential`) ||
				strings.Contains(string(body), `"password"`) {
				t.Fatal("session secrets escaped the robot adding projection")
			}
		}
		if f.native.roles.Load() != 0 || f.native.invites.Load() != 0 || f.native.sends.Load() != 0 {
			t.Fatal("robot adding identity or progress performed native work")
		}
		onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/cancel", "",
			map[string]any{"version": c.Version}, 200)
	}
}
