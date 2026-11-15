package httpapi

import (
	"encoding/json"
	"net/url"
	"testing"

	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
)

func TestPostgresOperatorGatewayUsageIsPersonalAndTestRemainsCapped(t *testing.T) {
	f := newCommercialFixture(t)
	customer := f.owner("operator-usage-customer", "PRO")
	group, _ := f.group(customer, "operator-usage-group", false)
	expect(t, f.admin, "PUT", "/ai-gateway", map[string]any{
		"settings": control.GatewaySettings{Enabled: true, PlatformLimit: 100_000_000, TestLimit: 0},
		"route": control.ModelRoute{Alias: "default", Enabled: true, Verified: true,
			Rates: aigateway.Rates{Input: 1_000_000, Output: 1_000_000},
			Model: assistant.ModelConfig{BaseURL: "https://synthetic.invalid/v1", Model: "synthetic", APIKey: "synthetic-no-network"}},
	}, 200)
	expect(t, f.admin, "PUT", "/assistant/settings", control.AssistantSettings{
		Enabled: true, TenantAccess: true, MonthlyTokens: 0, Model: "default",
	}, 200)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.db.Platform.Exec(f.ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var rate string
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT rate_id::text FROM ai_model_routes WHERE alias='default'`).Scan(&rate); err != nil {
		t.Fatal(err)
	}
	foreign := control.NewID()
	exec(`INSERT INTO users(id,username,password_hash,role)
		VALUES($1,'operator-usage-other','unused-synthetic','superadmin')`, foreign)
	demo := func(actor, status string, amount int64) string {
		t.Helper()
		id := control.NewID()
		exec(`INSERT INTO ai_calls(id,actor_id,group_id,purpose,rate_id,external_key,request_hash,
			reserved_micro,charged_micro,status,execution_mode,billing_source)
			VALUES($1,$2,$3,'assistant',$4,$1::uuid::text,$5,$6::bigint,
				CASE WHEN $7='settled' THEN $6::bigint ELSE 0::bigint END,$7,'operator_demo','platform_demo')`,
			id, actor, group, rate, []byte{1}, amount, status)
		return id
	}
	id := demo(f.adminActor.ID, "settled", 1_200)
	demo(f.adminActor.ID, "unknown", 700)
	demo(foreign, "settled", 88_000)
	exec(`INSERT INTO credit_entries(call_id,actor_id,kind,amount_micro,reason,external_key)
		VALUES($1,$2,'adjustment',200,'synthetic verified correction',$3)`, id, f.adminActor.ID, "synthetic-adjustment:"+id)
	// These existing legacy/test records must not appear as operator demo usage.
	for _, purpose := range []string{"assistant", "test"} {
		exec(`INSERT INTO ai_calls(id,actor_id,purpose,rate_id,external_key,request_hash,
			reserved_micro,charged_micro,status) VALUES($1,$2,$3,$4,$1::uuid::text,$5,9000,9000,'settled')`,
			control.NewID(), f.adminActor.ID, purpose, rate, []byte{1})
	}
	exec(`INSERT INTO ai_calls(id,tenant_id,actor_id,group_id,purpose,period_id,rate_id,external_key,request_hash,
		reserved_micro,charged_micro,status,execution_mode,billing_source)
		VALUES($1,$2,$3,$4,'assistant',$5,$6,$1::uuid::text,$7,300,300,'settled','tenant_business','tenant_wallet')`,
		control.NewID(), customer.tenant, customer.id, group, customer.period, rate, []byte{1})
	assertDemo := func(raw json.RawMessage) {
		t.Helper()
		var usage struct {
			Calls     int64  `json:"call_count"`
			Actual    int64  `json:"actual_micro"`
			Held      int64  `json:"held_micro"`
			Unlimited bool   `json:"unlimited"`
			Source    string `json:"billing_source"`
			Currency  string `json:"currency"`
			Scale     int64  `json:"currency_scale"`
			Limit     *int64 `json:"limit"`
		}
		if err := json.Unmarshal(raw, &usage); err != nil || usage.Calls != 2 || usage.Actual != 1_000 ||
			usage.Held != 700 || !usage.Unlimited || usage.Source != "platform_demo" || usage.Limit != nil ||
			usage.Currency != "USDT" || usage.Scale != 1_000_000_000 {
			t.Fatal("personal demo usage mixed with another payer", string(raw), err)
		}
	}
	assertDemo(assistantRequest(t, f.admin, "GET", "/usage", nil, "", 200))
	var gateway struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(expect(t, f.admin, "GET", "/ai-gateway", nil, 200), &gateway); err != nil {
		t.Fatal(err)
	}
	assertDemo(gateway.Usage)
	var customerUsage map[string]any
	if err := json.Unmarshal(assistantRequest(t, customer.client, "GET", "/usage", nil, "", 200), &customerUsage); err != nil {
		t.Fatal(err)
	}
	if customerUsage["actual_micro"] != float64(300) || customerUsage["call_count"] != float64(1) ||
		customerUsage["unlimited"] != false || customerUsage["billing_source"] != "shared_gateway" ||
		customerUsage["charge_source"] != "tenant_wallet" {
		t.Fatal("customer usage semantics changed", customerUsage)
	}
	ref := func(c testClient) control.SessionRef {
		u, _ := url.Parse(c.base)
		for _, cookie := range c.client.Jar.Cookies(u) {
			if cookie.Name == "wsl_session" {
				ref, err := control.WebsiteSession(cookie.Value)
				if err != nil {
					t.Fatal(err)
				}
				return ref
			}
		}
		t.Fatal("missing synthetic website session")
		return control.SessionRef{}
	}
	meter, err := f.s.Gateway.Usage(f.ctx, ref(customer.client))
	if err != nil || meter["billing_source"] != "tenant_wallet" || meter["unlimited"] != false {
		t.Fatal("canonical user-center usage", meter, err)
	}
	expect(t, customer.client, "GET", "/ai-gateway", nil, 403)
	expect(t, customer.client, "POST", "/ai-gateway/test",
		control.GatewayTestInput{RateID: rate, Key: control.NewID(), MaxCost: 10_000_000}, 403)
	calls := 0
	f.s.Gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) {
		calls++
		t.Fatal("exhausted test cap reached the model")
		return nil, nil
	}
	expect(t, f.admin, "POST", "/ai-gateway/test",
		control.GatewayTestInput{RateID: rate, Key: control.NewID(), MaxCost: 10_000_000}, 409)
	expect(t, f.admin, "POST", "/ai-gateway/test", map[string]any{
		"rate_id": rate, "idempotency_key": control.NewID(), "max_cost_micro": 10_000_000,
		"execution_mode": "operator_demo", "billing_source": "platform_demo",
	}, 400)
	if calls != 0 {
		t.Fatal("gateway test bypassed its budget")
	}
	exec(`UPDATE users SET disabled=true WHERE id=$1`, f.adminActor.ID)
	if _, err := f.s.Gateway.Usage(f.ctx, ref(f.admin)); err == nil {
		t.Fatal("revoked website identity could read demo costs")
	}
}
