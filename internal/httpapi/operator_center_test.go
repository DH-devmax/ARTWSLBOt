package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"wangshangliao/internal/control"
	"wangshangliao/internal/features"
)

func TestOperatorCenterUnlimitedIsExplicitAndCustomerViewStaysBounded(t *testing.T) {
	demo := entitlementView(control.BusinessEntitlement{
		Mode: "operator_demo", Name: "synthetic demo", BillingSource: "platform_demo", Active: true, OperatorDemo: true,
	})
	if demo["unlimited"] != true || demo["permanent"] != true || demo["billing_source"] != "platform_demo" {
		t.Fatal("demo entitlement lost its explicit meaning", demo)
	}
	for _, field := range []string{"group_limit", "member_limit", "assistant_limit", "credit_limit", "expires_at"} {
		if value, exists := demo[field]; !exists || value != nil {
			t.Fatal("unlimited became a numeric sentinel", field, value)
		}
	}
	customer := entitlementView(control.BusinessEntitlement{Mode: "tenant_business", BillingSource: "tenant_wallet", Active: true})
	if customer["unlimited"] != false || customer["billing_source"] != "tenant_wallet" {
		t.Fatal("customer inherited demo", customer)
	}
	if _, exists := customer["permanent"]; exists {
		t.Fatal("customer received permanent demo entitlement")
	}
}

func operatorCenterExec(t *testing.T, f *commercialFixture, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Platform.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}

func operatorCenterCount(t *testing.T, f *commercialFixture, sql string, args ...any) int {
	t.Helper()
	var count int
	if err := f.db.Platform.QueryRow(f.ctx, sql, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func operatorCenterGroup(t *testing.T, f *commercialFixture, owner commercialOwner, platform string, index int) (string, string) {
	t.Helper()
	account, group := control.NewID(), control.NewID()
	operatorCenterExec(t, f, `INSERT INTO accounts(id,name,platform,purpose,workspace,native_uid,native_nickname,
		auth_state,connection_enabled,credential_sealed)
		VALUES($1,$2,$3,'management',$3,$4,$5,'authenticated',false,$6)`,
		account, "synthetic robot", platform, fmt.Sprint(710000+index), "native-robot-"+platform, []byte("synthetic-sealed"))
	operatorCenterExec(t, f, `INSERT INTO native_groups(id,platform,native_id,name)
		VALUES($1,$2,$3,'synthetic same-name group')`, group, platform, fmt.Sprint(730000+index))
	operatorCenterExec(t, f, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled)
		VALUES($1,$2,$3,true)`, group, owner.tenant, account)
	return group, account
}

func TestPostgresOperatorCenterGroupsAndEntitlementAreServerScoped(t *testing.T) {
	f := newCommercialFixture(t)
	created := expect(t, f.admin, "POST", "/users", map[string]string{
		"username": "center-unsubscribed", "password": "synthetic-commercial-password", "tenant_name": "synthetic no package",
	}, 201)
	unsubscribed := commercialOwner{id: field(t, created, "id"), tenant: field(t, created, "tenant_id"),
		client: f.login("center-unsubscribed")}
	customer := f.owner("center-paid-customer", "PRO")
	wsl, _ := operatorCenterGroup(t, f, unsubscribed, "wsl", 1)
	chili, _ := operatorCenterGroup(t, f, unsubscribed, "lajiaoliao", 2)
	own, _ := operatorCenterGroup(t, f, customer, "wsl", 3)
	if operatorCenterCount(t, f, `SELECT count(*) FROM subscriptions WHERE tenant_id=$1`, unsubscribed.tenant) != 0 {
		t.Fatal("no-package fixture unexpectedly has a subscription")
	}
	var center struct {
		Admin        bool                        `json:"platform_admin"`
		Subscription json.RawMessage             `json:"subscription"`
		Wallet       json.RawMessage             `json:"credit_wallet"`
		Groups       []map[string]any            `json:"groups"`
		Entitlement  control.BusinessEntitlement `json:"entitlement"`
		Usage        map[string]any              `json:"usage"`
		NativeReady  bool                        `json:"native_ready"`
		AIReady      bool                        `json:"ai_ready"`
	}
	if err := json.Unmarshal(expect(t, f.admin, "GET", "/user-center?workspace=all", nil, 200), &center); err != nil {
		t.Fatal(err)
	}
	if !center.Admin || !center.Entitlement.OperatorDemo || !center.Entitlement.Active ||
		center.Entitlement.Mode != "operator_demo" || center.Entitlement.BillingSource != "platform_demo" ||
		len(center.Groups) != 3 || string(center.Subscription) != "null" || string(center.Wallet) != "null" ||
		center.NativeReady || center.AIReady || center.Usage["billing_source"] != "platform_demo" {
		t.Fatal("operator center mixed customer subscription with demo", center)
	}
	type groupPage struct {
		Items []struct {
			ID       string `json:"group_id"`
			Platform string `json:"platform"`
			Tenant   string `json:"tenant_id"`
			Robot    string `json:"robot_nickname"`
			Blocked  string `json:"customer_blocked_reason"`
		} `json:"items"`
		Total, Limit, Offset int
	}
	page := func(client testClient, query string) groupPage {
		t.Helper()
		if client.client == f.admin.client {
			if query == "" {
				query = "?workspace=all"
			} else {
				query += "&workspace=all"
			}
		}
		var result groupPage
		if err := json.Unmarshal(expect(t, client, "GET", "/user-center/groups"+query, nil, 200), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first, second := page(f.admin, "?limit=1"), page(f.admin, "?limit=1&offset=1")
	if first.Total != 3 || second.Total != 3 || first.Limit != 1 || second.Offset != 1 ||
		len(first.Items) != 1 || len(second.Items) != 1 || first.Items[0].ID == second.Items[0].ID {
		t.Fatal("group pagination lost scope or bounds", first, second)
	}
	chiliPage := page(f.admin, "?platform=lajiaoliao&tenant_id="+unsubscribed.tenant+"&search=synthetic")
	if chiliPage.Total != 1 || len(chiliPage.Items) != 1 || chiliPage.Items[0].ID != chili ||
		chiliPage.Items[0].Robot != "native-robot-lajiaoliao" || chiliPage.Items[0].Blocked != "group_runtime_disabled" {
		t.Fatal("same-name cross-platform groups were conflated", chiliPage)
	}
	byCustomer := page(f.admin, "?tenant_id="+unsubscribed.tenant+"&platform=wsl")
	if byCustomer.Total != 1 || len(byCustomer.Items) != 1 || byCustomer.Items[0].ID != wsl {
		t.Fatal("operator customer filter", byCustomer)
	}
	customerPage := page(customer.client, "")
	if customerPage.Total != 1 || len(customerPage.Items) != 1 || customerPage.Items[0].ID != own {
		t.Fatal("customer group listing crossed tenants", customerPage)
	}
	if empty := page(f.admin, "?offset=99"); len(empty.Items) != 0 || empty.Total != 3 {
		t.Fatal("empty page changed the total", empty)
	}
	expect(t, customer.client, "GET", "/user-center/groups?tenant_id="+unsubscribed.tenant, nil, 403)
	expect(t, customer.client, "GET", "/user-center?tenant_id="+unsubscribed.tenant, nil, 403)
	for _, query := range []string{"?limit=51", "?limit=0", "?offset=-1", "?platform=operator_demo"} {
		expect(t, f.admin, "GET", "/user-center/groups"+query, nil, 400)
	}
	var customerCenter struct {
		Entitlement control.BusinessEntitlement `json:"entitlement"`
	}
	if err := json.Unmarshal(expect(t, unsubscribed.client, "GET", "/user-center", nil, 200), &customerCenter); err != nil ||
		customerCenter.Entitlement.OperatorDemo || customerCenter.Entitlement.Active ||
		customerCenter.Entitlement.Mode != "tenant_business" {
		t.Fatal("unsubscribed customer received demo benefits", customerCenter, err)
	}
	var usage map[string]any
	if err := json.Unmarshal(expect(t, f.admin, "GET", "/user-center/usage", nil, 200), &usage); err != nil ||
		usage["unlimited"] != true || usage["billing_source"] != "platform_demo" {
		t.Fatal("operator usage did not use personal demo accounting", usage, err)
	}
	for _, row := range []struct {
		client testClient
		admin  bool
	}{
		{f.admin, true}, {customer.client, false},
	} {
		for _, path := range []string{"/me", "/workspaces"} {
			var navigation struct {
				Default  string `json:"default_console"`
				Consoles []struct {
					ID string `json:"id"`
				} `json:"consoles"`
			}
			if err := json.Unmarshal(expect(t, row.client, "GET", path, nil, 200), &navigation); err != nil {
				t.Fatal(err)
			}
			want := "user"
			if row.admin {
				want = "admin"
			}
			if navigation.Default != want {
				t.Fatal("wrong default console", path, navigation)
			}
			seenAdmin := false
			for _, console := range navigation.Consoles {
				seenAdmin = seenAdmin || console.ID == "admin"
			}
			if seenAdmin != row.admin {
				t.Fatal("client-selected console changed system role", path, navigation)
			}
		}
	}
	forgedWorkspace := customer.client
	forgedWorkspace.workspace = "maibot"
	expect(t, forgedWorkspace, "GET", "/user-center", nil, 403)
	expect(t, customer.client, "GET", "/ai-gateway", nil, 403)
	if operatorCenterCount(t, f, `SELECT count(*) FROM subscriptions WHERE tenant_id=$1`, unsubscribed.tenant) != 0 ||
		operatorCenterCount(t, f, `SELECT count(*) FROM ai_calls`) != 0 ||
		operatorCenterCount(t, f, `SELECT count(*) FROM test_windows`) != 0 {
		t.Fatal("viewing the center activated a package, model or test grant")
	}
}

func TestPostgresOperatorCenterManualHTTPPlansCannotDelegateIdentity(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newCommercialFixture(t)
			a, b := f.owner("manual-center-a", "PRO"), f.owner("manual-center-b", "PRO")
			group, account := operatorCenterGroup(t, f, a, platform, 1)
			otherGroup, _ := operatorCenterGroup(t, f, b, platform, 2)
			var permissions []string
			for _, permission := range control.BusinessPermissionCatalog() {
				permissions = append(permissions, permission.Code)
			}
			operatorCenterExec(t, f, `INSERT INTO user_access(user_id,role_code,permissions) VALUES($1,'manager',$2)
				ON CONFLICT(user_id) DO UPDATE SET permissions=excluded.permissions`, a.id, permissions)
			admin, client, other := f.admin, a.client, b.client
			admin.workspace, client.workspace, other.workspace = platform, platform, platform
			path := "/groups/" + group + "/action-plans"
			sessions := operatorCenterCount(t, f, `SELECT count(*) FROM assistant_sessions`)
			var calls atomic.Int32
			f.s.ManualActions.Executor = func(ctx context.Context, _ control.SessionRef, p control.AssistantActionPlan) (control.SettingOperation, error) {
				calls.Add(1)
				id := control.NewID()
				_, err := f.db.Platform.Exec(ctx, `INSERT INTO native_setting_operations(id,actor_id,account_id,group_id,kind,
					idempotency_key,request_hash,payload_sealed,account_epoch,status,expires_at)
					VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,'succeeded',clock_timestamp()+interval '15 seconds')`,
					id, f.adminActor.ID, p.Scope.AccountID, p.Scope.GroupID, p.Action, p.ID,
					bytes.Repeat([]byte{1}, 32), []byte("synthetic-http-result"))
				return control.SettingOperation{ID: id, Status: "succeeded", Kind: p.Action}, err
			}
			for _, field := range []string{"execution_mode", "billing_source", "actor_id", "tenant_id", "account_id", "test_window_id", "session_id"} {
				expect(t, client, "POST", path, map[string]any{"action": "mute_group", field: "operator_demo"}, 400)
			}
			if operatorCenterCount(t, f, `SELECT count(*) FROM manual_action_plans`) != 0 {
				t.Fatal("forged fields persisted a page plan")
			}
			expect(t, client, "POST", "/groups/"+otherGroup+"/action-plans", map[string]any{"action": "mute_group"}, 404)
			var p control.AssistantActionPlan
			if err := json.Unmarshal(expect(t, admin, "POST", path, map[string]any{"action": "mute_group"}, 201), &p); err != nil ||
				p.ExecutionMode != "operator_demo" || p.SessionID != "" || p.Status != "blocked" || p.Production {
				t.Fatal("closed page plan", p, err)
			}
			confirmation := map[string]any{"digest": p.Digest, "version": p.Version, "key": "synthetic-http-confirm"}
			planPath := path + "/" + p.ID
			expect(t, client, "GET", planPath, nil, 404)
			expect(t, client, "POST", planPath+"/approve", confirmation, 404)
			expect(t, admin, "GET", "/groups/"+otherGroup+"/action-plans/"+p.ID, nil, 404)
			expect(t, admin, "POST", planPath+"/approve", confirmation, 409)
			for _, field := range []string{"execution_mode", "billing_source", "actor_id", "test_window_id"} {
				forged := map[string]any{"digest": p.Digest, "version": p.Version, "key": "synthetic-http-confirm", field: "operator_demo"}
				expect(t, admin, "POST", planPath+"/approve", forged, 400)
			}
			if calls.Load() != 0 || operatorCenterCount(t, f, `SELECT count(*) FROM native_setting_operations`) != 0 {
				t.Fatal("closed/forged approval reached the executor")
			}
			// Demo opens no customer runtime or subscription: only this formal gate changes.
			operatorCenterExec(t, f, `INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
				VALUES($1,'mute_group','manual','open',ARRAY['acceptance:SYNTHETIC-HTTP-MANUAL'])`, platform)
			// This test replaces the executor; native evidence acquisition is
			// exercised independently by the dual-platform demo-claim tests.
			operatorCenterExec(t, f, `INSERT INTO operator_demo_group_claims(id,actor_id,actor_version,tenant_id,
				group_id,platform,account_id,account_epoch,account_version,binding_epoch,binding_version,
				create_key,state,robot_role,owner_identity_hash)
				SELECT $1,u.id,u.access_version,b.tenant_id,b.group_id,a.platform,a.id,a.epoch,a.version,b.epoch,b.version,
				'synthetic-http-claim','active','admin',$4 FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
				JOIN users u ON u.id=$2 WHERE b.group_id=$3`, control.NewID(), f.adminActor.ID, group, bytes.Repeat([]byte{1}, 32))
			var approved control.AssistantActionPlan
			if err := json.Unmarshal(expect(t, admin, "POST", planPath+"/approve", confirmation, 200), &approved); err != nil ||
				approved.Status != "succeeded" || approved.OperationID == "" || calls.Load() != 1 {
				t.Fatal("HTTP manual confirmation", approved, err)
			}
			var again control.AssistantActionPlan
			if err := json.Unmarshal(expect(t, admin, "POST", planPath+"/approve", confirmation, 200), &again); err != nil ||
				again.OperationID != approved.OperationID || calls.Load() != 1 {
				t.Fatal("HTTP duplicate confirmation replayed", again, err)
			}
			if operatorCenterCount(t, f, `SELECT count(*) FROM native_setting_operations WHERE id=$1 AND test_window_id IS NULL`, approved.OperationID) != 1 ||
				operatorCenterCount(t, f, `SELECT count(*) FROM test_windows`) != 0 ||
				operatorCenterCount(t, f, `SELECT count(*) FROM assistant_sessions`) != sessions ||
				operatorCenterCount(t, f, `SELECT count(*) FROM assistant_action_plans`) != 0 ||
				operatorCenterCount(t, f, `SELECT count(*) FROM group_business_runtime WHERE group_id=$1 AND NOT enabled`, group) != 1 {
				t.Fatal("page operation borrowed an assistant/test grant or enabled automatic business")
			}
			if err := json.Unmarshal(expect(t, admin, "POST", path, map[string]any{"action": "mute_group"}, 201), &p); err != nil {
				t.Fatal(err)
			}
			operatorCenterExec(t, f, `UPDATE group_bindings SET version=version+1,epoch=epoch+1 WHERE group_id=$1`, group)
			status, _, _ := admin.request("POST", path+"/"+p.ID+"/approve",
				map[string]any{"digest": p.Digest, "version": p.Version, "key": "synthetic-changed-binding"})
			if status != 403 && status != 409 || calls.Load() != 1 {
				t.Fatal("changed binding reached HTTP executor", status, calls.Load())
			}
			wrongPlatform := admin
			wrongPlatform.workspace = "wsl"
			if platform == "wsl" {
				wrongPlatform.workspace = "lajiaoliao"
			}
			expect(t, wrongPlatform, "GET", path+"/"+p.ID, nil, 404)
			if operatorCenterCount(t, f, `SELECT count(*) FROM accounts WHERE id=$1 AND NOT testing_enabled AND NOT connection_enabled`, account) != 1 {
				t.Fatal("page actions toggled controlled-test connection flags")
			}
		})
	}
}

func TestPostgresOperatorCenterPLUSUsageDoesNotGrantAssistant(t *testing.T) {
	f := newCommercialFixture(t)
	plus := f.owner("center-plus-usage-reader", "PLUS")
	operatorCenterExec(t, f, `INSERT INTO user_access(user_id,role_code,permissions) VALUES($1,'custom',ARRAY['billing:read'])
		ON CONFLICT(user_id) DO UPDATE SET permissions=excluded.permissions`, plus.id)
	plus.client = f.login("center-plus-usage-reader")
	var usage struct {
		Calls     int64  `json:"call_count"`
		Actual    int64  `json:"actual_micro"`
		Held      int64  `json:"held_micro"`
		Unit      string `json:"unit"`
		Currency  string `json:"currency"`
		Scale     int64  `json:"currency_scale"`
		Source    string `json:"billing_source"`
		Unlimited bool   `json:"unlimited"`
	}
	if err := json.Unmarshal(expect(t, plus.client, "GET", "/user-center/usage", nil, 200), &usage); err != nil ||
		usage.Calls != 0 || usage.Actual != 0 || usage.Held != 0 || usage.Unlimited ||
		usage.Unit != "micro_credit" || usage.Currency != "USDT" || usage.Scale != 1_000_000_000 ||
		usage.Source != "tenant_wallet" {
		t.Fatal("PLUS billing reader could not view accurate personal usage", usage, err)
	}
	assistantRequest(t, plus.client, "GET", "/usage", nil, "", 403)
	expect(t, plus.client, "GET", "/ai-gateway", nil, 403)
	if operatorCenterCount(t, f, `SELECT count(*) FROM ai_calls`) != 0 ||
		operatorCenterCount(t, f, `SELECT count(*) FROM ai_wallets WHERE tenant_id=$1 AND (spent_micro<>0 OR held_micro<>0)`, plus.tenant) != 0 {
		t.Fatal("viewing usage invoked the model or charged the PLUS wallet")
	}
}

func operatorCenterFeatureAccess(t *testing.T, client testClient, group string, want control.HumanGroupAccess) {
	t.Helper()
	var view struct {
		Group       string                   `json:"group_id"`
		Platform    string                   `json:"platform"`
		Access      control.HumanGroupAccess `json:"group_access"`
		NativeReady bool                     `json:"native_ready"`
		Items       []struct {
			Definition features.Definition `json:"definition"`
			Editable   []string            `json:"editable_fields"`
			Effective  bool                `json:"effective_enabled"`
		} `json:"items"`
	}
	if err := json.Unmarshal(expect(t, client, "GET", "/groups/"+group+"/features", nil, 200), &view); err != nil {
		t.Fatal(err)
	}
	if view.Group != group || view.Platform != client.workspace || view.Access != want || view.NativeReady || len(view.Items) == 0 {
		t.Fatal("incorrect feature ownership projection", view)
	}
	editableWords := false
	for _, item := range view.Items {
		if item.Effective || want.AccessMode == "read_only" && len(item.Editable) != 0 {
			t.Fatal("read-only configuration exposed a write or runtime activation", item)
		}
		if item.Definition.ID == "blocked_words" {
			for _, field := range item.Editable {
				editableWords = editableWords || field == "words"
			}
		}
	}
	if editableWords != (want.AccessMode == "manage") {
		t.Fatal("feature editability did not follow current ownership", view.Access, editableWords)
	}
}

type operatorCenterFeatureState struct {
	Version, Epoch int64
	Config         string
	Management, AI bool
	Audits         int
}

func operatorCenterFeatureSnapshot(t *testing.T, f *commercialFixture, group string) operatorCenterFeatureState {
	t.Helper()
	var state operatorCenterFeatureState
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT version,epoch,config::text,management_enabled,ai_enabled,
		(SELECT count(*) FROM audit_events WHERE resource_id=$1::uuid::text AND action IN ('group_features_updated','group_updated'))
		FROM group_bindings WHERE group_id=$1`, group).
		Scan(&state.Version, &state.Epoch, &state.Config, &state.Management, &state.AI, &state.Audits); err != nil {
		t.Fatal(err)
	}
	return state
}

func operatorCenterFeatureWrite(method string, version int64) (string, map[string]any) {
	if method == "PUT" {
		return "/features", map[string]any{"version": version, "changes": map[string]any{
			"blocked_words": map[string]any{"words": []string{"synthetic-http-feature"}}}}
	}
	return "", map[string]any{"version": version, "management_enabled": false, "ai_enabled": false,
		"config": features.Config{BlockedWords: []string{"synthetic-http-legacy"}}}
}

func operatorCenterDenyFeatureWrites(t *testing.T, f *commercialFixture, client testClient, group, code string) {
	t.Helper()
	before := operatorCenterFeatureSnapshot(t, f, group)
	for _, method := range []string{"PUT", "PATCH"} {
		suffix, body := operatorCenterFeatureWrite(method, before.Version)
		status, _, got := client.request(method, "/groups/"+group+suffix, body)
		if status != 409 || got != code {
			t.Fatalf("%s without current ownership = %d %s; want 409 %s", method, status, got, code)
		}
	}
	if after := operatorCenterFeatureSnapshot(t, f, group); after != before {
		t.Fatal("rejected HTTP save changed configuration, fences or audit", before, after)
	}
}

func operatorCenterSaveFeature(t *testing.T, f *commercialFixture, client testClient, group, method string) {
	t.Helper()
	before := operatorCenterFeatureSnapshot(t, f, group)
	suffix, body := operatorCenterFeatureWrite(method, before.Version)
	var result struct {
		Version int64 `json:"version"`
		Applied bool  `json:"applied"`
	}
	if err := json.Unmarshal(expect(t, client, method, "/groups/"+group+suffix, body, 200), &result); err != nil {
		t.Fatal(err)
	}
	after := operatorCenterFeatureSnapshot(t, f, group)
	if result.Version != before.Version+1 || result.Applied || after.Version != result.Version ||
		after.Epoch != before.Epoch+1 || after.Audits != before.Audits+1 {
		t.Fatal("configuration save lost its version, epoch or audit fence", before, after, result)
	}
	var config features.Config
	if err := json.Unmarshal([]byte(after.Config), &config); err != nil || len(config.BlockedWords) != 1 {
		t.Fatal("configuration was not saved", config, err)
	}
	want := "synthetic-http-feature"
	if method == "PATCH" {
		want = "synthetic-http-legacy"
	}
	if config.BlockedWords[0] != want {
		t.Fatal("wrong configuration saved", config)
	}
}

func operatorCenterNoFeatureExecution(t *testing.T, f *commercialFixture) {
	t.Helper()
	for _, table := range []string{"native_setting_operations", "ai_calls", "test_windows"} {
		if n := operatorCenterCount(t, f, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("configuration HTTP regression created %d rows in %s", n, table)
		}
	}
}

func TestPostgresOperatorCenterFeatureOwnershipHTTP(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newCommercialFixture(t)
			owner := f.owner("http-feature-owner", "PRO")
			group, _ := operatorCenterGroup(t, f, owner, platform, 1)
			admin, customer := f.admin, owner.client
			admin.workspace, customer.workspace = platform, platform
			t.Cleanup(func() { operatorCenterNoFeatureExecution(t, f) })
			readOnly := control.HumanGroupAccess{OwnershipState: "unverified", AccessMode: "read_only", BlockedReason: "operator_demo_claim_required"}
			operatorCenterFeatureAccess(t, admin, group, readOnly)
			status, _, code := customer.request("GET", "/groups/"+group+"/features", nil)
			if status != 409 || code != "group_ownership_proof_required" {
				t.Fatal("unclaimed customer read configuration", status, code)
			}
			operatorCenterDenyFeatureWrites(t, f, admin, group, "operator_demo_claim_required")
			operatorCenterDenyFeatureWrites(t, f, customer, group, "group_ownership_proof_required")

			seedSyntheticGroupOwnership(t, f.db, f.s.Vault, group)
			manage := control.HumanGroupAccess{OwnershipState: "active", AccessMode: "manage"}
			for _, client := range []testClient{admin, customer} {
				operatorCenterFeatureAccess(t, client, group, manage)
				for _, method := range []string{"PUT", "PATCH"} {
					operatorCenterSaveFeature(t, f, client, group, method)
				}
			}
			// Revoke only the ownership anchor so version conflicts cannot mask
			// the admission checks in either configuration endpoint.
			before := operatorCenterFeatureSnapshot(t, f, group)
			operatorCenterExec(t, f, `UPDATE group_binding_ownership SET state='paused',version=version+1 WHERE group_id=$1`, group)
			if after := operatorCenterFeatureSnapshot(t, f, group); after != before {
				t.Fatal("synthetic ownership revocation changed the config fence", before, after)
			}
			readOnly.OwnershipState = "paused"
			operatorCenterFeatureAccess(t, admin, group, readOnly)
			status, _, code = customer.request("GET", "/groups/"+group+"/features", nil)
			if status != 409 || code != "group_ownership_reproof_required" {
				t.Fatal("paused customer read configuration", status, code)
			}
			operatorCenterDenyFeatureWrites(t, f, admin, group, "operator_demo_claim_required")
			operatorCenterDenyFeatureWrites(t, f, customer, group, "group_ownership_proof_required")
		})
	}
}

func TestPostgresOperatorCenterPersonalClaimFeatureHTTP(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, method := range []string{"PUT", "PATCH"} {
			t.Run(platform+"/"+method, func(t *testing.T) {
				f := newCommercialFixture(t)
				owner := f.owner("http-personal-claim-owner", "PRO")
				group, _ := operatorCenterGroup(t, f, owner, platform, 1)
				admin, customer := f.admin, owner.client
				admin.workspace, customer.workspace = platform, platform
				t.Cleanup(func() { operatorCenterNoFeatureExecution(t, f) })
				claim := seedSyntheticPersonalGroupClaim(t, f.db, f.adminActor.ID, group)
				manage := control.HumanGroupAccess{OwnershipState: "unverified", AccessMode: "manage"}
				readOnly := control.HumanGroupAccess{OwnershipState: "unverified", AccessMode: "read_only", BlockedReason: "operator_demo_claim_required"}
				operatorCenterFeatureAccess(t, admin, group, manage)
				operatorCenterDenyFeatureWrites(t, f, customer, group, "group_ownership_proof_required")
				status, _, code := customer.request("GET", "/groups/"+group+"/features", nil)
				if status != 409 || code != "group_ownership_proof_required" {
					t.Fatal("personal claim became customer ownership", status, code)
				}
				operatorCenterExec(t, f, `INSERT INTO users(username,password_hash,role)
					SELECT 'http-other-operator',password_hash,'superadmin' FROM users WHERE id=$1`, f.adminActor.ID)
				other := f.login("http-other-operator")
				other.workspace = platform
				operatorCenterFeatureAccess(t, other, group, readOnly)
				operatorCenterDenyFeatureWrites(t, f, other, group, "operator_demo_claim_required")
				claimSnapshot := func() string {
					t.Helper()
					var snapshot string
					if err := f.db.Platform.QueryRow(f.ctx, `SELECT jsonb_build_array(binding_version,binding_epoch,
						account_version,account_epoch,actor_version,expires_at)::text
						FROM operator_demo_group_claims WHERE id=$1`, claim).Scan(&snapshot); err != nil {
						t.Fatal(err)
					}
					return snapshot
				}
				path := "/user-center/groups/" + group + "/demo-claim"
				readClaim := func() control.OperatorDemoClaim {
					t.Helper()
					var out control.OperatorDemoClaim
					if err := json.Unmarshal(expect(t, admin, "GET", path, nil, 200), &out); err != nil {
						t.Fatal(err)
					}
					return out
				}
				initial := readClaim()
				before := claimSnapshot()
				operatorCenterSaveFeature(t, f, admin, group, method)
				if after := claimSnapshot(); after != before {
					t.Fatal("saving configuration rewrote immutable claim evidence", before, after)
				}
				current := readClaim()
				if current.ID == initial.ID || current.State != "active" ||
					current.ExpiresAt == nil || initial.ExpiresAt == nil || !current.ExpiresAt.Equal(*initial.ExpiresAt) {
					t.Fatal("configuration did not continue the same fixed test deadline", initial, current)
				}
				if n := operatorCenterCount(t, f, `SELECT count(*) FROM operator_demo_group_claims
					WHERE id=$1 AND state='cancelled'`, claim); n != 1 {
					t.Fatal("original claim was not cancelled", n)
				}
				operatorCenterFeatureAccess(t, admin, group, manage)
				operatorCenterDenyFeatureWrites(t, f, customer, group, "group_ownership_proof_required")
				operatorCenterDenyFeatureWrites(t, f, other, group, "operator_demo_claim_required")
				status, _, code = admin.request("POST", path+"/"+initial.ID+"/cancel", map[string]any{"version": initial.Version})
				if status != 409 || code != "operator_demo_claim_scope_changed" {
					t.Fatal("cancelling an old claim falsely cancelled its successor", status, code)
				}
				nextMethod := "PUT"
				if method == "PUT" {
					nextMethod = "PATCH"
				}
				operatorCenterSaveFeature(t, f, admin, group, nextMethod)
				next := readClaim()
				if next.ID == current.ID || next.State != "active" || next.ExpiresAt == nil ||
					!next.ExpiresAt.Equal(*initial.ExpiresAt) {
					t.Fatal("second HTTP save renewed or lost the personal claim", initial, next)
				}
				binding := operatorCenterFeatureSnapshot(t, f, group)
				expect(t, admin, "POST", path+"/"+next.ID+"/cancel", map[string]any{"version": next.Version}, 200)
				if after := operatorCenterFeatureSnapshot(t, f, group); after != binding {
					t.Fatal("claim cancellation changed the config fence", binding, after)
				}
				operatorCenterFeatureAccess(t, admin, group, readOnly)
				operatorCenterDenyFeatureWrites(t, f, admin, group, "operator_demo_claim_required")
				if n := operatorCenterCount(t, f, `SELECT count(*) FROM group_binding_ownership WHERE group_id=$1`, group); n != 0 {
					t.Fatal("personal claim manufactured a customer ownership anchor", n)
				}
			})
		}
	}
}
