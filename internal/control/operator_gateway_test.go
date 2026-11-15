package control

import (
	"bytes"
	"testing"

	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
)

func TestOperatorGatewayAccountingIsServerDerived(t *testing.T) {
	admin := Actor{ID: NewID(), Role: "superadmin"}
	customer := Actor{ID: NewID(), TenantID: NewID(), Role: "user"}
	ref := SessionRef{hash: bytes.Repeat([]byte{31}, 32)}
	for _, row := range []struct {
		name, purpose, mode, source string
		actor                       Actor
		session                     SessionRef
		group                       string
	}{
		{"human demo", "assistant", "operator_demo", "platform_demo", admin, ref, NewID()},
		{"customer", "assistant", "tenant_business", "tenant_wallet", customer, ref, NewID()},
		{"heat requires service context", "heatbot", "", "", customer, SessionRef{}, NewID()},
		{"gateway test", "test", "legacy", "platform_test", admin, ref, ""},
		{"old assistant", "platform_assistant", "legacy", "legacy", admin, ref, ""},
		{"background cannot demo", "assistant", "", "", admin, SessionRef{}, NewID()},
		{"demo requires group", "assistant", "", "", admin, ref, ""},
		{"heat cannot demo", "heatbot", "", "", admin, ref, NewID()},
		{"customer cannot test", "test", "", "", customer, ref, ""},
		{"empty customer scope", "assistant", "", "", Actor{ID: NewID(), Role: "user"}, ref, NewID()},
	} {
		t.Run(row.name, func(t *testing.T) {
			mode, source, err := gatewayAccounting(gatewayRequest{
				Actor: row.actor, Session: row.session, Group: row.group, Purpose: row.purpose,
			})
			if row.mode == "" {
				if err == nil {
					t.Fatal("invalid billing identity was admitted")
				}
			} else if err != nil || mode != row.mode || source != row.source {
				t.Fatal("accounting identity", mode, source, err)
			}
		})
	}
}

func TestOperatorGatewayUsageCurrencyMatchesCreditArithmetic(t *testing.T) {
	// One million input tokens at one USDT per million costs one USDT,
	// represented as 1000 credits rather than one million micro-credits.
	cost, err := aigateway.Cost(aigateway.Usage{Input: 1_000_000}, aigateway.Rates{Input: 1_000_000})
	if err != nil || cost != 1_000_000_000 || cost/aigateway.MicroPerCredit != 1000 {
		t.Fatal("currency scale does not match the immutable rate arithmetic", cost, err)
	}
	if cost != gatewayUsageCurrencyScale {
		t.Fatal("usage currency scale does not represent one USDT", gatewayUsageCurrencyScale, cost)
	}
}

type operatorGatewayFixture struct {
	f        *businessFixture
	gateway  *GatewayService
	admin    Actor
	adminRef SessionRef
	user     Actor
	userRef  SessionRef
	group    map[string]string
	period   string
}

func newOperatorGatewayFixture(t *testing.T) *operatorGatewayFixture {
	t.Helper()
	f := newBusinessFixture(t)
	g := &operatorGatewayFixture{
		f: f, admin: Actor{ID: NewID(), Role: "superadmin"},
		user:  Actor{ID: f.actor, TenantID: f.tenant, Role: "user"},
		group: map[string]string{},
	}
	f.exec(`INSERT INTO users(id,username,password_hash,role)
		VALUES($1,'synthetic-gateway-operator','unused-synthetic','superadmin')`, g.admin.ID)
	g.adminRef = g.session(g.admin, 41)
	g.userRef = g.session(g.user, 42)
	g.gateway = &GatewayService{DB: f.db, Vault: f.service.Vault, Catalog: BusinessPermissionCatalog()}
	if err := g.gateway.Configure(f.ctx, g.admin, GatewaySettings{Enabled: true, PlatformLimit: 100_000_000},
		ModelRoute{Alias: "default", Enabled: true, Verified: true,
			Rates: aigateway.Rates{Input: 1_000_000, Output: 1_000_000},
			Model: assistant.ModelConfig{BaseURL: "https://synthetic.invalid/v1", Model: "synthetic", APIKey: "synthetic-no-network"}}); err != nil {
		t.Fatal(err)
	}
	f.exec(`DELETE FROM subscriptions WHERE tenant_id=$1`, f.tenant)
	var pro string
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM commercial_plans WHERE code='PRO' AND version=1`).Scan(&pro); err != nil {
		t.Fatal(err)
	}
	period, err := (&BillingService{DB: f.db}).Confirm(f.ctx, g.admin, PaymentConfirmation{
		TenantID: f.tenant, PlanVersionID: pro, IdempotencyKey: "operator-gateway-period",
		PaymentReference: "synthetic-operator-gateway", AmountMicroUSDT: 49_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	g.period = period.ID
	for i, platform := range []string{"wsl", "lajiaoliao"} {
		account := f.account(platform, i+1)
		g.group[platform] = f.group(platform, account, f.tenant, i+1, false).Group
	}
	return g
}

func (g *operatorGatewayFixture) session(a Actor, marker byte) SessionRef {
	g.f.t.Helper()
	ref := SessionRef{hash: bytes.Repeat([]byte{marker}, 32)}
	g.f.exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at)
		VALUES($1,$2,$3,clock_timestamp()+interval '1 hour')`, ref.hash, bytes.Repeat([]byte{marker + 1}, 32), a.ID)
	return ref
}

func (g *operatorGatewayFixture) request(platform string) gatewayRequest {
	return gatewayRequest{Actor: g.admin, Session: g.adminRef.InPlatform(platform), Group: g.group[platform],
		Purpose: "assistant", Key: "synthetic-demo:" + NewID()}
}

func (g *operatorGatewayFixture) reserve(r gatewayRequest) gatewayReservation {
	g.f.t.Helper()
	reservation, err := g.gateway.reserve(g.f.ctx, r, []byte("synthetic bounded request"))
	if err != nil {
		g.f.t.Fatal(err)
	}
	return reservation
}

func (g *operatorGatewayFixture) wallet() (int64, int64) {
	g.f.t.Helper()
	var spent, held int64
	if err := g.f.db.Platform.QueryRow(g.f.ctx, `SELECT spent_micro,held_micro FROM ai_wallets WHERE period_id=$1`,
		g.period).Scan(&spent, &held); err != nil {
		g.f.t.Fatal(err)
	}
	return spent, held
}

func TestPostgresOperatorGatewayDemoWalletAndBudgetSeparation(t *testing.T) {
	g := newOperatorGatewayFixture(t)
	f := g.f
	var first gatewayReservation
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		r := g.request(platform)
		out := g.reserve(r)
		first = out
		if out.Period != "" || out.ExecutionMode != "operator_demo" || out.BillingSource != "platform_demo" {
			t.Fatal("demo used customer accounting", out.Period, out.ExecutionMode, out.BillingSource)
		}
		if err := g.gateway.submit(f.ctx, r, out); err != nil {
			t.Fatal(err)
		}
		usage := assistant.Usage{PromptTokens: 8, CompletionTokens: 1, TotalTokens: 9}
		if platform == "lajiaoliao" {
			usage = assistant.Usage{} // Missing usage keeps the platform hold.
		}
		if err := g.gateway.settle(f.ctx, out.ID, usage, true, false, ""); err != nil {
			t.Fatal(err)
		}
		if spent, held := g.wallet(); spent != 0 || held != 0 {
			t.Fatal("demo changed customer wallet", spent, held)
		}
	}
	if f.count(`SELECT count(*) FROM ai_calls WHERE execution_mode='operator_demo' AND billing_source='platform_demo'
		AND tenant_id IS NULL AND period_id IS NULL`) != 2 ||
		f.count(`SELECT count(*) FROM ai_calls WHERE execution_mode='operator_demo' AND status='unknown'`) != 1 {
		t.Fatal("demo identity or unknown reservation was lost")
	}
	r := gatewayRequest{Actor: g.user, Session: g.userRef, Group: g.group["wsl"], Purpose: "assistant",
		Key: "synthetic-customer:" + NewID()}
	customer := g.reserve(r)
	if customer.Period != g.period || customer.ExecutionMode != "tenant_business" || customer.BillingSource != "tenant_wallet" {
		t.Fatal("customer did not reserve its own wallet")
	}
	if spent, held := g.wallet(); spent != 0 || held != customer.Amount {
		t.Fatal("customer reservation missing", spent, held)
	}
	if err := g.gateway.submit(f.ctx, r, customer); err != nil {
		t.Fatal(err)
	}
	if err := g.gateway.settle(f.ctx, customer.ID, assistant.Usage{PromptTokens: 8, CompletionTokens: 1, TotalTokens: 9}, true, false, ""); err != nil {
		t.Fatal(err)
	}
	if spent, held := g.wallet(); spent != 9_000 || held != 0 {
		t.Fatal("customer charge changed", spent, held)
	}
	// A legacy null-tenant assistant call still consumes the gateway test cap.
	f.exec(`INSERT INTO ai_calls(id,actor_id,purpose,rate_id,external_key,request_hash,
		reserved_micro,charged_micro,status)
		VALUES($1,$2,'assistant',$3,$1::uuid::text,$4,700,700,'settled')`,
		NewID(), g.admin.ID, first.RateID, []byte{1})
	test := gatewayRequest{Actor: g.admin, Session: g.adminRef, Purpose: "test", Key: "synthetic-test:" + NewID()}
	_, err := g.gateway.reserve(f.ctx, test, []byte("synthetic bounded request"))
	capabilityError(t, err, "test_ai_budget_exhausted")
	f.exec(`UPDATE ai_gateway_settings SET test_limit_micro=$1`, first.Amount+699)
	_, err = g.gateway.reserve(f.ctx, test, []byte("synthetic bounded request"))
	capabilityError(t, err, "test_ai_budget_exhausted")
	f.exec(`UPDATE ai_gateway_settings SET test_limit_micro=$1`, first.Amount+700)
	accepted := g.reserve(test)
	if accepted.ExecutionMode != "legacy" || accepted.BillingSource != "platform_test" {
		t.Fatal("gateway test inherited demo billing")
	}
	if err = g.gateway.settle(f.ctx, accepted.ID, assistant.Usage{}, false, false, ""); err != nil {
		t.Fatal(err)
	}
	// Unknown demo cost remains included in the shared platform cap.
	f.exec(`UPDATE ai_gateway_settings SET platform_limit_micro=(SELECT coalesce(sum(
		CASE WHEN status='settled' THEN effective_charged_micro WHEN status<>'released' THEN reserved_micro ELSE 0 END),0)
		FROM ai_call_costs)+$1-1`, first.Amount)
	_, err = g.gateway.reserve(f.ctx, g.request("wsl"), []byte("synthetic bounded request"))
	capabilityError(t, err, "platform_ai_budget_exhausted")
	f.exec(`UPDATE ai_gateway_settings SET platform_limit_micro=100000000,test_limit_micro=0`)
	f.exec(`UPDATE subscriptions SET enabled=false WHERE tenant_id=$1`, f.tenant)
	demo := g.reserve(g.request("wsl"))
	if demo.Period != "" {
		t.Fatal("expired customer blocked or funded demo")
	}
	if err = g.gateway.Recover(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.count(`SELECT count(*) FROM ai_calls WHERE execution_mode='operator_demo' AND status='unknown'`) != 1 {
		t.Fatal("recovery released unknown demo cost")
	}
}

func TestPostgresOperatorGatewaySubmissionRevalidatesLiveIdentity(t *testing.T) {
	for _, row := range []struct {
		name, change, restore, code string
	}{
		{"disabled", `UPDATE users SET disabled=true WHERE id=$1`,
			`UPDATE users SET disabled=false WHERE id=$1`, "permission_denied"},
		{"downgraded", `UPDATE users SET role='user',tenant_id=$2 WHERE id=$1`,
			`UPDATE users SET role='superadmin',tenant_id=NULL WHERE id=$1`, "permission_denied"},
		{"website session", `UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE token_hash=$1`,
			`UPDATE sessions SET expires_at=clock_timestamp()+interval '1 hour' WHERE token_hash=$1`,
			"authentication_required"},
		{"binding revoked", `UPDATE group_bindings SET active=false WHERE group_id=$1`,
			`UPDATE group_bindings SET active=true WHERE group_id=$1`, "group_not_found"},
	} {
		t.Run(row.name, func(t *testing.T) {
			// User-state triggers revoke sessions permanently; each case needs
			// an independent authenticated fixture before reserving any cost.
			g := newOperatorGatewayFixture(t)
			f := g.f
			args := []any{g.admin.ID}
			switch row.name {
			case "downgraded":
				args = append(args, f.tenant)
			case "website session":
				args = []any{g.adminRef.hash}
			case "binding revoked":
				args = []any{g.group["wsl"]}
			}
			r := g.request("wsl")
			out := g.reserve(r)
			f.exec(row.change, args...)
			// The downgrade restore query deliberately needs only the actor ID.
			restoreArgs := args
			if row.name == "downgraded" {
				restoreArgs = args[:1]
			}
			t.Cleanup(func() { f.exec(row.restore, restoreArgs...) })
			capabilityError(t, g.gateway.submit(f.ctx, r, out), row.code)
			if f.count(`SELECT count(*) FROM ai_calls WHERE id=$1 AND status='reserved'`, out.ID) != 1 {
				t.Fatal("revoked request crossed the submission boundary")
			}
			if err := g.gateway.settle(f.ctx, out.ID, assistant.Usage{}, false, false, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
	g := newOperatorGatewayFixture(t)
	f := g.f
	r := g.request("wsl")
	r.Session = SessionRef{}
	_, err := g.gateway.reserve(f.ctx, r, []byte("synthetic"))
	capabilityError(t, err, "assistant_scope_changed")
	r = g.request("wsl")
	r.Actor = g.user
	r.Actor.Role = "superadmin"
	r.Actor.TenantID = ""
	_, err = g.gateway.reserve(f.ctx, r, []byte("synthetic"))
	capabilityError(t, err, "permission_denied")
	r = g.request("wsl")
	out := g.reserve(r)
	r.Purpose = "test"
	capabilityError(t, g.gateway.submit(f.ctx, r, out), "ai_call_scope_changed")
}

func TestPostgresOperatorGatewayCenterUsageNeedsReadPermissionNotAssistant(t *testing.T) {
	g := newOperatorGatewayFixture(t)
	f := g.f
	plusTenant, plusUser := f.customer()
	f.exec(`DELETE FROM subscriptions WHERE tenant_id=$1`, plusTenant)
	var plusPlan string
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM commercial_plans WHERE code='PLUS' AND version=1`).Scan(&plusPlan); err != nil {
		t.Fatal(err)
	}
	plusPeriod, err := (&BillingService{DB: f.db}).Confirm(f.ctx, g.admin, PaymentConfirmation{
		TenantID: plusTenant, PlanVersionID: plusPlan, IdempotencyKey: "operator-center-plus-period",
		PaymentReference: "synthetic-operator-center-plus", AmountMicroUSDT: 29_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE user_access SET permissions=ARRAY['billing:read'] WHERE user_id=$1`, plusUser)
	plusRef := g.session(Actor{ID: plusUser, TenantID: plusTenant, Role: "user"}, 71)
	_, err = g.gateway.Usage(f.ctx, plusRef)
	capabilityError(t, err, "permission_denied")
	plusUsage, err := g.gateway.UsageForCenter(f.ctx, plusRef)
	if err != nil || plusUsage["call_count"] != int64(0) || plusUsage["actual_micro"] != int64(0) ||
		plusUsage["held_micro"] != int64(0) || plusUsage["unlimited"] != false ||
		plusUsage["currency"] != "USDT" || plusUsage["currency_scale"] != int64(1_000_000_000) {
		t.Fatal("PLUS could not read its own zero usage without assistant", plusUsage, err)
	}
	var rate string
	if err = f.db.Platform.QueryRow(f.ctx, `SELECT rate_id::text FROM ai_model_routes WHERE alias='default'`).Scan(&rate); err != nil {
		t.Fatal(err)
	}
	member := Actor{ID: NewID(), TenantID: f.tenant, Role: "user"}
	f.exec(`INSERT INTO users(id,tenant_id,username,password_hash,role,tenant_role)
		VALUES($1,$2,'synthetic-usage-reader','unused-synthetic','user','viewer')`, member.ID, member.TenantID)
	f.exec(`INSERT INTO user_access(user_id,role_code,permissions) VALUES($1,'viewer',ARRAY['operations:read'])`, member.ID)
	f.exec(`INSERT INTO member_groups(user_id,tenant_id,group_id) VALUES($1,$2,$3)`, member.ID, member.TenantID, g.group["wsl"])
	ref := g.session(member, 73)
	record := func(actor, tenant, group, period, status string, amount int64) {
		t.Helper()
		f.exec(`INSERT INTO ai_calls(id,tenant_id,actor_id,group_id,purpose,period_id,rate_id,
			external_key,request_hash,reserved_micro,charged_micro,status,execution_mode,billing_source)
			VALUES($1,$2,$3,$4,'assistant',$5,$6,$1::uuid::text,$7,$8::bigint,
				CASE WHEN $9='settled' THEN $8::bigint ELSE 0::bigint END,$9,'tenant_business','tenant_wallet')`,
			NewID(), tenant, actor, group, period, rate, []byte{1}, amount, status)
	}
	record(member.ID, f.tenant, g.group["wsl"], g.period, "settled", 1000)
	record(member.ID, f.tenant, g.group["wsl"], g.period, "unknown", 500)
	record(member.ID, f.tenant, g.group["lajiaoliao"], g.period, "settled", 7000)
	record(f.actor, f.tenant, g.group["wsl"], g.period, "settled", 9000)
	plusAccount := f.account("wsl", 3)
	plusGroup := f.group("wsl", plusAccount, plusTenant, 3, false)
	record(member.ID, plusTenant, plusGroup.Group, plusPeriod.ID, "settled", 11000)
	// Viewing usage must not depend on the gateway being enabled or create a call.
	f.exec(`UPDATE ai_gateway_settings SET enabled=false`)
	f.exec(`UPDATE ai_model_routes SET enabled=false`)
	memberUsage, err := g.gateway.UsageForCenter(f.ctx, ref)
	if err != nil || memberUsage["call_count"] != int64(2) || memberUsage["actual_micro"] != int64(1000) ||
		memberUsage["held_micro"] != int64(500) || memberUsage["billing_source"] != "tenant_wallet" ||
		memberUsage["unit"] != "micro_credit" || memberUsage["currency"] != "USDT" {
		t.Fatal("center usage crossed actor, tenant or member group scope", memberUsage, err)
	}
	_, err = g.gateway.Usage(f.ctx, ref)
	capabilityError(t, err, "permission_denied")
	f.exec(`DELETE FROM member_groups WHERE user_id=$1`, member.ID)
	ref = g.session(member, 75)
	memberUsage, err = g.gateway.UsageForCenter(f.ctx, ref)
	if err != nil || memberUsage["call_count"] != int64(0) || memberUsage["actual_micro"] != int64(0) ||
		memberUsage["held_micro"] != int64(0) {
		t.Fatal("withdrawn group assignment still delivered usage", memberUsage, err)
	}
	f.exec(`UPDATE user_access SET permissions=ARRAY['groups:read'] WHERE user_id=$1`, member.ID)
	ref = g.session(member, 77)
	_, err = g.gateway.UsageForCenter(f.ctx, ref)
	capabilityError(t, err, "permission_denied")
	if f.count(`SELECT count(*) FROM ai_calls`) != 5 ||
		f.count(`SELECT count(*) FROM ai_calls WHERE execution_mode='operator_demo'`) != 0 ||
		f.count(`SELECT count(*) FROM credit_entries WHERE kind IN('charge','release')`) != 0 {
		t.Fatal("read-only center usage created model work or charged a wallet")
	}
}
