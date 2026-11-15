package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/security"
)

type workspaceServiceTx struct {
	pgx.Tx
	query Queryer
}

func (tx workspaceServiceTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.query.QueryRow(ctx, sql, args...)
}

func serviceOwnerRow(tenant, owner string, demo bool) capabilityRow {
	if demo {
		return capabilityRow{values: []any{tenant, owner, int64(1), "operator_demo", "superadmin", "", "legacy", true}}
	}
	return capabilityRow{values: []any{tenant, owner, int64(1), "tenant_business", "user", tenant, "owner", false}}
}

func serviceContext(t *testing.T, demo bool) WorkspaceServiceContext {
	t.Helper()
	tenant, owner := NewID(), NewID()
	q := &capabilityQueries{t: t, rows: []capabilityRow{serviceOwnerRow(tenant, owner, demo)}}
	service, err := WorkspaceServiceEntitlement(context.Background(), q, tenant)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestWorkspaceServiceEntitlementUsesActualActiveOwner(t *testing.T) {
	ctx, tenant, owner := context.Background(), NewID(), NewID()
	for _, demo := range []bool{false, true} {
		q := &capabilityQueries{t: t, rows: []capabilityRow{serviceOwnerRow(tenant, owner, demo)}}
		got, err := WorkspaceServiceEntitlement(ctx, q, tenant)
		if err != nil || got.TenantID != tenant || got.OwnerID != owner || got.OwnerVersion != 1 || got.Demo != demo {
			t.Fatal("actual service ownership was lost", got, err)
		}
		mode, source := "tenant_business", "tenant_wallet"
		if demo {
			mode, source = "workspace_demo", "platform_workspace"
		}
		if got.ExecutionMode != mode || got.BillingSource != source {
			t.Fatal("workspace selected another accounting identity", got)
		}
	}
	for _, tc := range []struct {
		name   string
		change func([]any)
	}{
		{"invalid SQL owner", func(v []any) { v[7] = false }},
		{"customer impersonation", func(v []any) { v[4], v[5], v[6] = "user", tenant, "owner" }},
		{"admin with tenant", func(v []any) { v[5] = tenant }},
		{"another workspace", func(v []any) { v[0] = NewID() }},
		{"missing owner", func(v []any) { v[1] = "" }},
		{"missing access version", func(v []any) { v[2] = int64(0) }},
		{"unknown space", func(v []any) { v[3] = "unknown" }},
		{"customer with admin owner", func(v []any) { v[3], v[7] = "tenant_business", false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := serviceOwnerRow(tenant, owner, true)
			tc.change(row.values)
			q := &capabilityQueries{t: t, rows: []capabilityRow{row}}
			_, err := WorkspaceServiceEntitlement(ctx, q, tenant)
			capabilityError(t, err, "workspace_service_unavailable")
		})
	}
	for _, row := range []capabilityRow{
		{err: pgx.ErrNoRows},
		{values: []any{tenant, owner, int64(1), "tenant_business", "user", NewID(), "owner", false}},
		{values: []any{tenant, owner, int64(1), "tenant_business", "user", tenant, "manager", false}},
	} {
		q := &capabilityQueries{t: t, rows: []capabilityRow{row}}
		_, err := WorkspaceServiceEntitlement(ctx, q, tenant)
		capabilityError(t, err, "workspace_service_unavailable")
	}
	q := &capabilityQueries{t: t}
	_, err := WorkspaceServiceEntitlement(ctx, q, "")
	capabilityError(t, err, "workspace_service_unavailable")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = WorkspaceServiceEntitlement(cancelled, q, tenant)
	if !errors.Is(err, context.Canceled) || q.calls != 0 {
		t.Fatal("cancellation acquired a fallback service identity", err, q.calls)
	}
}

func TestWorkspaceServiceRevalidatesOwnerVersionAndGroup(t *testing.T) {
	ctx, service, group := context.Background(), serviceContext(t, true), NewID()
	for _, tc := range []struct {
		name string
		row  capabilityRow
		code string
	}{
		{"current", serviceOwnerRow(service.TenantID, service.OwnerID, true), ""},
		{"owner changed", serviceOwnerRow(service.TenantID, NewID(), true), "workspace_service_changed"},
		{"owner disabled", capabilityRow{err: pgx.ErrNoRows}, "workspace_service_unavailable"},
		{"kind changed", serviceOwnerRow(service.TenantID, service.OwnerID, false), "workspace_service_changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &capabilityQueries{t: t, rows: []capabilityRow{tc.row}}
			err := service.revalidate(ctx, q)
			if tc.code == "" && err != nil {
				t.Fatal(err)
			}
			if tc.code != "" {
				capabilityError(t, err, tc.code)
			}
		})
	}
	row := serviceOwnerRow(service.TenantID, service.OwnerID, true)
	row.values[2] = int64(2)
	q := &capabilityQueries{t: t, rows: []capabilityRow{row}}
	capabilityError(t, service.revalidate(ctx, q), "workspace_service_changed")
	for _, groupRow := range []capabilityRow{
		{values: []any{group, "wsl"}}, {err: pgx.ErrNoRows},
	} {
		q := &capabilityQueries{t: t, rows: []capabilityRow{
			serviceOwnerRow(service.TenantID, service.OwnerID, true), groupRow, ordinaryControlledBindingRow(),
		}}
		err := service.requireGroup(ctx, q, group)
		if groupRow.err == nil && err != nil {
			t.Fatal("owned group denied", err)
		}
		if groupRow.err != nil {
			capabilityError(t, err, "group_not_found")
		}
	}
	q = &capabilityQueries{t: t}
	capabilityError(t, service.requireGroup(ctx, q, ""), "group_not_found")
	if !service.humanOwner(Actor{ID: service.OwnerID, Role: "superadmin"}) {
		t.Fatal("actual human owner denied")
	}
	for _, actor := range []Actor{
		{ID: NewID(), Role: "superadmin"},
		{ID: service.OwnerID, Role: "user", TenantID: service.TenantID},
		{ID: service.OwnerID, Role: "superadmin", TenantID: service.TenantID},
	} {
		if service.humanOwner(actor) {
			t.Fatal("service identity accepted a forged or unrelated website owner", actor)
		}
	}
}

func TestWorkspaceCustomerServiceRetainsPermissionIntersection(t *testing.T) {
	service := serviceContext(t, false)
	grants := []string{"groups:ai", "groups:configure"}
	q := &capabilityQueries{t: t, rows: []capabilityRow{
		{values: []any{grants}}, {values: []any{"owner", true}},
		{values: []any{[]string{"groups:configure:deny"}}},
		{values: []any{[]string{"ai"}, 1}}, {values: []any{grants}},
	}}
	got, err := service.permissions(context.Background(), q, BusinessPermissionCatalog())
	if err != nil || !slices.Equal(got, []string{"groups:ai"}) || q.calls != 5 {
		t.Fatal("customer background escaped its permissions/package/ceiling", got, err, q.calls)
	}
}

func TestWorkspaceHeatAccountingRequiresControlTaskWithoutWebsiteIdentity(t *testing.T) {
	service := serviceContext(t, true)
	task, group := NewID(), NewID()
	valid := gatewayRequest{Service: &service, ServiceTask: task, Group: group, Purpose: "heatbot",
		Key: "heat-job:" + task, HeatGrant: bytes.Repeat([]byte{7}, 32)}
	mode, source, err := gatewayAccounting(valid)
	if err != nil || mode != "workspace_demo" || source != "platform_workspace" {
		t.Fatal("durable demo service did not obtain workspace accounting", mode, source, err)
	}
	tenant, owner := valid.scopeIDs()
	if tenant != service.TenantID || owner != service.OwnerID {
		t.Fatal("service request lost actual workspace owner", tenant, owner)
	}
	for _, tc := range []struct {
		name   string
		change func(*gatewayRequest)
	}{
		{"human owner actor", func(r *gatewayRequest) { r.Actor = Actor{ID: service.OwnerID, Role: "superadmin"} }},
		{"forged customer actor", func(r *gatewayRequest) {
			r.Actor = Actor{ID: service.OwnerID, TenantID: service.TenantID, Role: "user"}
		}},
		{"human session", func(r *gatewayRequest) { r.Session.hash = bytes.Repeat([]byte{8}, 32) }},
		{"human purpose", func(r *gatewayRequest) { r.Purpose = "assistant" }},
		{"test purpose", func(r *gatewayRequest) { r.Purpose = "test" }},
		{"missing task", func(r *gatewayRequest) { r.ServiceTask = "" }},
		{"another task key", func(r *gatewayRequest) { r.Key = "heat-job:" + NewID() }},
		{"missing grant", func(r *gatewayRequest) { r.HeatGrant = nil }},
		{"missing group", func(r *gatewayRequest) { r.Group = "" }},
		{"human billing mode", func(r *gatewayRequest) { r.Service.ExecutionMode = "operator_demo" }},
		{"human billing source", func(r *gatewayRequest) { r.Service.BillingSource = "platform_demo" }},
		{"forged owner role", func(r *gatewayRequest) { r.Service.ownerRole = "user" }},
		{"no service", func(r *gatewayRequest) { r.Service = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, copy := valid, service
			request.Service = &copy
			tc.change(&request)
			_, _, err := gatewayAccounting(request)
			capabilityError(t, err, "heat_gateway_scope_changed")
		})
	}
	customer := serviceContext(t, false)
	valid.Service = &customer
	mode, source, err = gatewayAccounting(valid)
	if err != nil || mode != "tenant_business" || source != "tenant_wallet" {
		t.Fatal("customer background billing changed", mode, source, err)
	}
	valid.ServiceTask = ""
	mode, source, err = gatewayAccounting(valid)
	if err != nil || mode != "tenant_business" || source != "tenant_wallet" {
		t.Fatal("existing standalone customer grant changed billing", mode, source, err)
	}
}

func TestWorkspaceDemoBackgroundAndHumanCallsShareOnlyGlobalBudget(t *testing.T) {
	service := serviceContext(t, true)
	background := gatewayRequest{Service: &service, Purpose: "heatbot"}
	human := gatewayRequest{Actor: Actor{ID: service.OwnerID, Role: "superadmin"}, Purpose: "assistant"}
	customer := gatewayRequest{Service: &WorkspaceServiceContext{TenantID: NewID()}, Purpose: "heatbot"}
	test := gatewayRequest{Actor: human.Actor, Purpose: "test"}
	for _, tc := range []struct {
		name, source, code string
		request            gatewayRequest
		remaining, test    int64
	}{
		{"background no test cap", "platform_workspace", "", background, 10, 0},
		{"background global cap", "platform_workspace", "platform_ai_budget_exhausted", background, 9, 100},
		{"unknown global hold", "platform_workspace", "platform_ai_budget_exhausted", background, -1, 100},
		{"human historical cap", "platform_demo", "", human, 10, 0},
		{"human global cap", "platform_demo", "platform_ai_budget_exhausted", human, 9, 100},
		{"customer separate wallet", "tenant_wallet", "", customer, 10, 0},
		{"customer global cap", "tenant_wallet", "platform_ai_budget_exhausted", customer, 9, 100},
		{"test cap closed", "platform_test", "test_ai_budget_exhausted", test, 10, 0},
		{"test cap boundary", "platform_test", "", test, 10, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := gatewayBudgetAdmission(tc.request, tc.source, 10, tc.remaining, tc.test)
			if tc.code == "" && err != nil {
				t.Fatal(err)
			}
			if tc.code != "" {
				capabilityError(t, err, tc.code)
			}
		})
	}
}

func TestWorkspaceDemoAutomaticAndHeatKeepRuntimeAndSourceGates(t *testing.T) {
	ctx, group := context.Background(), NewID()
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, source := range []string{"automatic", "heatbot"} {
			for _, tc := range []struct {
				name, code string
				rows       []capabilityRow
			}{
				{"closed", "business_admission_closed", []capabilityRow{{values: []any{"closed", false}}}},
				{"other pilot", "business_admission_closed", []capabilityRow{{values: []any{"pilot", false}}}},
				{"runtime off", "group_runtime_disabled", []capabilityRow{
					{values: []any{"open", false}}, {values: []any{false, platform}},
				}},
				{"explicit demo", "", []capabilityRow{
					{values: []any{"pilot", true}}, {values: []any{true, platform}}, ordinaryControlledBindingRow(),
					{values: []any{true, true, true}},
				}},
				{"customer no subscription", "active_subscription_required", []capabilityRow{
					{values: []any{"open", false}}, {values: []any{true, platform}}, ordinaryControlledBindingRow(),
					{values: []any{false, true, true}}, {err: pgx.ErrNoRows},
				}},
			} {
				t.Run(platform+"/"+source+"/"+tc.name, func(t *testing.T) {
					q := &capabilityQueries{t: t, rows: tc.rows}
					err := RequireBusinessAdmission(ctx, q, platform, group, "send_text", source)
					if tc.code == "" && err != nil {
						t.Fatal(err)
					}
					if tc.code != "" {
						capabilityError(t, err, tc.code)
					}
					if q.calls != len(tc.rows) {
						t.Fatal("admission skipped a required fence", q.calls)
					}
				})
			}
			q := &capabilityQueries{t: t, rows: []capabilityRow{
				{values: []any{"open", false}}, {values: []any{true, platform}}, ordinaryControlledBindingRow(),
				{values: []any{true, false, false}},
			}}
			code := "group_management_disabled"
			if source == "heatbot" {
				code = "group_chat_disabled"
			}
			capabilityError(t, RequireBusinessAdmission(ctx, q, platform, group, "send_text", source), code)
		}
	}
}

func TestWorkspaceDemoAssignmentRetainsPhysicalLimitsAndCustomerPlan(t *testing.T) {
	ctx, tenant, owner, group := context.Background(), NewID(), NewID(), NewID()
	for _, workspace := range []string{"wsl", "lajiaoliao", "maibot"} {
		for _, total := range []int{299, 300} {
			q := &capabilityQueries{t: t, rows: []capabilityRow{
				{values: []any{true}}, serviceOwnerRow(tenant, owner, true), {values: []any{total}},
			}}
			err := CheckAssignmentEntitlement(ctx, workspaceServiceTx{query: q}, tenant, group, workspace)
			if total < MaxAuthorizedGroups && err != nil {
				t.Fatal("demo required a commercial plan", workspace, err)
			}
			if total >= MaxAuthorizedGroups {
				capabilityError(t, err, "group_quota_exceeded")
			}
			if q.calls != 3 {
				t.Fatal("demo assignment lost physical pool check")
			}
		}
		q := &capabilityQueries{t: t, rows: []capabilityRow{{values: []any{false}}, {err: pgx.ErrNoRows}}}
		capabilityError(t, CheckAssignmentEntitlement(ctx, workspaceServiceTx{query: q}, tenant, group, workspace),
			"active_subscription_required")
	}
	for _, tc := range []struct {
		used     int
		retained bool
		code     string
	}{
		{29, false, ""}, {30, false, "account_group_limit_reached"}, {30, true, ""},
	} {
		q := &capabilityQueries{t: t, rows: []capabilityRow{{values: []any{tc.used, tc.retained}}}}
		err := CheckAccountCapacity(ctx, workspaceServiceTx{query: q}, NewID(), group)
		if tc.code == "" && err != nil {
			t.Fatal(err)
		}
		if tc.code != "" {
			capabilityError(t, err, tc.code)
		}
	}
	for _, tc := range []struct {
		modules []string
		used    int
		code    string
	}{
		{[]string{"moderation"}, 0, "module_not_in_plan"},
		{[]string{"ai"}, 1, "group_quota_exceeded"},
		{[]string{"ai"}, 0, ""},
	} {
		rows := []capabilityRow{{values: []any{false}}, {values: []any{1, tc.modules}}}
		if slices.Contains(tc.modules, "ai") {
			rows = append(rows, capabilityRow{values: []any{tc.used, 1}})
		}
		q := &capabilityQueries{t: t, rows: rows}
		err := CheckAssignmentEntitlement(ctx, workspaceServiceTx{query: q}, tenant, group, "maibot")
		if tc.code == "" && err != nil {
			t.Fatal(err)
		}
		if tc.code != "" {
			capabilityError(t, err, tc.code)
		}
	}
}

func TestWorkspaceDemoOwnerCanToggleWithoutCustomerImpersonation(t *testing.T) {
	ctx, service, group, account := context.Background(), serviceContext(t, true), NewID(), NewID()
	admin := Actor{ID: service.OwnerID, Role: "superadmin"}
	for _, other := range []bool{false, true} {
		actor := admin
		if other {
			actor.ID = NewID()
		}
		q := &capabilityQueries{t: t, rows: []capabilityRow{
			{values: []any{actor.ID}}, {values: []any{true}}, {values: []any{"wsl"}}, ordinaryControlledBindingRow(),
			{values: []any{service.TenantID}},
			serviceOwnerRow(service.TenantID, service.OwnerID, true),
		}}
		got, err := HeatToggleEntitlement(ctx, workspaceServiceTx{query: q}, actor,
			BusinessPermissionCatalog(), "wsl", group)
		if other {
			capabilityError(t, err, "workspace_owner_required")
		} else if err != nil || got != service || actor.TenantID != "" {
			t.Fatal("demo heat toggle needed a customer identity or plan", got, err)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, other := range []bool{false, true} {
			actor := admin
			if other {
				actor.ID = NewID()
			}
			q := &capabilityQueries{t: t, rows: []capabilityRow{
				{values: []any{actor.ID, "", "superadmin"}}, {values: []any{true}},
				{values: []any{"wsl"}}, ordinaryControlledBindingRow(),
				{values: []any{actor.ID}}, {values: []any{account}},
				{values: []any{group, service.TenantID, false, int64(1), int64(1),
					account, int64(1), int64(1), "synthetic-group", int64(1)}},
				{values: []any{"authenticated_message"}},
				serviceOwnerRow(service.TenantID, service.OwnerID, true),
			}}
			s := &GroupBindingService{Catalog: BusinessPermissionCatalog()}
			got, scope, err := s.businessRuntimeScope(ctx, workspaceServiceTx{query: q},
				SessionRef{hash: bytes.Repeat([]byte{9}, 32)}, group, enabled)
			if other {
				capabilityError(t, err, "workspace_owner_required")
			} else if err != nil || got != admin || scope.Tenant != service.TenantID || q.calls != 9 {
				t.Fatal("runtime toggle required a forged user owner or subscription", got, scope, err, q.calls)
			}
		}
	}
}

func serviceHeatScopeRow(scope HeatScope, admitted bool) capabilityRow {
	return capabilityRow{values: []any{
		scope.Platform, scope.Tenant, scope.Actor, scope.Group, scope.NativeGroup, scope.Account, scope.NativeSelf,
		scope.BindingVersion, scope.BindingEpoch, scope.AccountEpoch, scope.AccountVersion,
		scope.SettingsVersion, scope.PreferenceVersion, scope.PersonaVersion, scope.PersonaAccount, scope.RouteVersion,
		scope.ReceiverVersion, scope.ReceiverOwner, scope.AdmissionVersion, admitted, scope.RuntimeVersion,
		false, 0, 0, 1439, 0, false, "synthetic persona", "synthetic style",
	}}
}

func TestWorkspaceDemoHeatDefaultsRemainOff(t *testing.T) {
	service := serviceContext(t, true)
	scope := HeatScope{Service: service, Platform: "wsl", Tenant: service.TenantID, Actor: service.OwnerID,
		Group: NewID(), Account: NewID(), RuntimeVersion: 1}
	scope.PersonaAccount = scope.Account
	q := &capabilityQueries{t: t, rows: []capabilityRow{
		serviceHeatScopeRow(scope, true), {values: []any{"authenticated_message"}}, serviceOwnerRow(service.TenantID, service.OwnerID, true),
		{values: []any{"open", false}}, {values: []any{true, "wsl"}}, ordinaryControlledBindingRow(), {values: []any{true, true, true}},
	}}
	got, err := (&HeatChatService{}).heatScope(context.Background(), q, scope.Group, true)
	if err != nil || got.Effective.Enabled || got.Service != service || q.calls != 7 {
		t.Fatal("demo ownership enabled an unconfigured heat account", got.Effective, err, q.calls)
	}
}

func TestWorkspaceHeatServiceAdmissionRechecksDurableScopeAndNativeRole(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	service := serviceContext(t, true)
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{21}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		scope := HeatScope{Service: service, Platform: platform, Tenant: service.TenantID, Actor: service.OwnerID,
			Group: NewID(), Account: NewID(), NativeGroup: "synthetic-group", NativeSelf: "synthetic-self",
			BindingVersion: 1, BindingEpoch: 2, AccountEpoch: 3, AccountVersion: 4, SettingsVersion: 1,
			PersonaVersion: 1, RouteVersion: 1, ReceiverVersion: 1, ReceiverOwner: "synthetic-receiver",
			AdmissionVersion: 1, RuntimeVersion: 1}
		scope.PersonaAccount = scope.Account
		settings := HeatChatSettings{Enabled: true, ReplyProbability: .1, CooldownSeconds: 30, EndHour: 24}
		scope.Effective = effectiveHeatSettings(settings, false, 0, 0, 1439, 0, scope.Account)
		raw, marshalErr := json.Marshal(settings)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		sealed, sealErr := vault.Seal("maibot:"+scope.Account+":chat", raw)
		if sealErr != nil {
			t.Fatal(sealErr)
		}
		task := NewID()
		request := gatewayRequest{Service: &service, ServiceTask: task, HeatGrant: bytes.Repeat([]byte{22}, 32),
			Group: scope.Group, Purpose: "heatbot", Key: "heat-job:" + task, ExpectedRoute: scope.RouteVersion}
		gateway := &GatewayService{Vault: vault, Catalog: BusinessPermissionCatalog()}
		for _, tc := range []struct {
			name, code string
			change     func([]capabilityRow) []capabilityRow
		}{
			{"current", "", func(rows []capabilityRow) []capabilityRow { return rows }},
			{"disabled owner", "workspace_service_unavailable", func(rows []capabilityRow) []capabilityRow {
				rows[0] = capabilityRow{err: pgx.ErrNoRows}
				return rows[:1]
			}},
			{"changed owner version", "workspace_service_changed", func(rows []capabilityRow) []capabilityRow {
				rows[0].values[2] = int64(2)
				return rows[:1]
			}},
			{"another group", "group_not_found", func(rows []capabilityRow) []capabilityRow {
				rows[1] = capabilityRow{err: pgx.ErrNoRows}
				return rows[:2]
			}},
			{"platform closed", "heat_chat_platform_not_admitted", func(rows []capabilityRow) []capabilityRow {
				rows[3] = serviceHeatScopeRow(scope, false)
				return rows[:6]
			}},
			{"source closed", "business_admission_closed", func(rows []capabilityRow) []capabilityRow {
				rows[6] = capabilityRow{values: []any{"closed", false}}
				return rows[:7]
			}},
			{"runtime off", "group_runtime_disabled", func(rows []capabilityRow) []capabilityRow {
				rows[7] = capabilityRow{values: []any{false, platform}}
				return rows[:8]
			}},
			{"job no longer live", "heat_chat_job_changed", func(rows []capabilityRow) []capabilityRow {
				rows[11] = capabilityRow{err: pgx.ErrNoRows}
				return rows[:12]
			}},
			{"changed job snapshot", "heat_chat_scope_changed", func(rows []capabilityRow) []capabilityRow {
				rows[11].values[0] = bytes.Repeat([]byte{23}, 32)
				return rows[:12]
			}},
			{"stale native role", "heat_gateway_scope_changed", func(rows []capabilityRow) []capabilityRow {
				rows[12] = capabilityRow{err: pgx.ErrNoRows}
				return rows
			}},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				rows := tc.change([]capabilityRow{
					serviceOwnerRow(service.TenantID, service.OwnerID, true), {values: []any{scope.Group, platform}},
					ordinaryControlledBindingRow(),
					serviceHeatScopeRow(scope, true), {values: []any{"authenticated_message"}}, serviceOwnerRow(service.TenantID, service.OwnerID, true),
					{values: []any{"open", false}}, {values: []any{true, platform}}, ordinaryControlledBindingRow(), {values: []any{true, true, true}},
					{values: []any{sealed}},
					{values: []any{scope.digest(), scope.Account, scope.Group}}, {values: []any{"member"}},
				})
				q := &capabilityQueries{t: t, rows: rows}
				err := gateway.heatServiceAdmission(ctx, q, request)
				if tc.code == "" && err != nil {
					t.Fatal("live control-owned service scope rejected", err)
				}
				if tc.code != "" {
					capabilityError(t, err, tc.code)
				}
				if q.calls != len(rows) {
					t.Fatal("service skipped a durable scope or native role fence", q.calls, len(rows))
				}
			})
		}
	}
}
