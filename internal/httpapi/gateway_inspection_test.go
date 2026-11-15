package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
)

type gatewayProbeTransport func(*http.Request) (*http.Response, error)

func (f gatewayProbeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type gatewayTestProvider struct{ calls *atomic.Int32 }
type gatewayUnknownProvider struct{ calls *atomic.Int32 }

func (p gatewayUnknownProvider) Generate(context.Context, []assistant.Message, []assistant.Tool, func(string) error) (assistant.Response, error) {
	p.calls.Add(1)
	return assistant.Response{}, assistant.ErrModel
}

func (p gatewayTestProvider) Generate(ctx context.Context, messages []assistant.Message, tools []assistant.Tool, emit func(string) error) (assistant.Response, error) {
	p.calls.Add(1)
	if len(messages) != 1 || len(tools) != 0 || messages[0].Content != "Reply with OK only." {
		panic("unbounded test request")
	}
	return assistant.Response{Message: assistant.Message{Role: "assistant", Content: "OK"}, Usage: assistant.Usage{PromptTokens: 8, CompletionTokens: 1, TotalTokens: 9}}, nil
}

func TestPostgresGatewayInspectionAndBoundedTest(t *testing.T) {
	f := newCommercialFixture(t)
	owner := f.owner("inspection-owner", "PRO")
	cfg := map[string]any{"settings": control.GatewaySettings{}, "route": control.ModelRoute{Alias: "default",
		Model: assistant.ModelConfig{BaseURL: "https://synthetic.example", Model: "synthetic", APIKey: "synthetic-business-key"}}}
	expect(t, f.admin, "PUT", "/ai-gateway", cfg, 200)
	current := func() string {
		var data struct {
			Runtime struct {
				Routes []struct {
					ID string `json:"rate_id"`
				} `json:"routes"`
			} `json:"runtime"`
		}
		if err := json.Unmarshal(expect(t, f.admin, "GET", "/ai-gateway", nil, 200), &data); err != nil {
			t.Fatal(err)
		}
		return data.Runtime.Routes[0].ID
	}
	rate := current()
	var probes atomic.Int32
	f.s.Gateway.InspectionClient = &http.Client{Transport: gatewayProbeTransport(func(r *http.Request) (*http.Response, error) {
		probes.Add(1)
		body := `{"schema_version":"1.0","success":true,"data":{"currency":"CNY","price_unit":"per_1m_tokens","models":[]}}`
		if r.URL.Path == "/v1/usage" {
			body = `{"isValid":true,"mode":"unrestricted","unit":"USD","balance":58.2,"remaining":58.2,"secret":"never project"}`
		}
		if r.URL.Path == "/v1/models" {
			body = `{"data":[{"id":"synthetic"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	expect(t, owner.client, "POST", "/ai-gateway/inspect", map[string]string{"rate_id": rate}, 403)
	expect(t, f.admin, "POST", "/ai-gateway/inspect", map[string]string{"rate_id": control.NewID()}, 409)
	if probes.Load() != 0 {
		t.Fatal("unauthorized or stale inspection reached provider")
	}
	data := expect(t, f.admin, "POST", "/ai-gateway/inspect", map[string]string{"rate_id": rate}, 200)
	if strings.Contains(string(data), "never project") || !strings.Contains(string(data), "model_price_missing") || probes.Load() != 3 {
		t.Fatal("projection", string(data))
	}
	var calls atomic.Int32
	f.s.Gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) { return gatewayTestProvider{&calls}, nil }
	in := control.GatewayTestInput{RateID: rate, Key: control.NewID(), MaxCost: 1_000_000}
	expect(t, f.admin, "POST", "/ai-gateway/test", in, 409)
	if calls.Load() != 0 {
		t.Fatal("draft reached model")
	}
	cfg["settings"] = control.GatewaySettings{Version: 1, Enabled: true, PlatformLimit: 100_000_000, TestLimit: 10_000_000}
	route := cfg["route"].(control.ModelRoute)
	route.Enabled, route.Verified = true, true
	route.Rates = aigateway.Rates{Input: 1_000_000, Output: 1_000_000}
	cfg["route"] = route
	expect(t, f.admin, "PUT", "/ai-gateway", cfg, 200)
	in.RateID = current()
	in.MaxCost = 1
	expect(t, f.admin, "POST", "/ai-gateway/test", in, 409)
	in.MaxCost = 10_000_000
	expect(t, owner.client, "POST", "/ai-gateway/test", in, 403)
	in.RateID = rate
	expect(t, f.admin, "POST", "/ai-gateway/test", in, 409)
	in.RateID = current()
	first := expect(t, f.admin, "POST", "/ai-gateway/test", in, 200)
	second := expect(t, f.admin, "POST", "/ai-gateway/test", in, 200)
	if field(t, first, "id") != field(t, second, "id") || calls.Load() != 1 || field(t, first, "status") != "settled" {
		t.Fatal("test replay or settlement", calls.Load(), string(first), string(second))
	}
	var snapshots, recorded int
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM ai_upstream_snapshots),(SELECT count(*) FROM ai_calls WHERE purpose='test')`).Scan(&snapshots, &recorded); err != nil || snapshots != 1 || recorded != 1 {
		t.Fatal("persistence", err)
	}
	roles := expect(t, owner.client, "GET", "/member-roles", nil, 200)
	if !strings.Contains(string(roles), "assistant_reader") {
		t.Fatal("templates missing")
	}
	f.s.Gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) {
		return gatewayUnknownProvider{&calls}, nil
	}
	in.Key = control.NewID()
	unknown := expect(t, f.admin, "POST", "/ai-gateway/test", in, 200)
	again := expect(t, f.admin, "POST", "/ai-gateway/test", in, 200)
	if field(t, unknown, "status") != "unknown" || field(t, unknown, "id") != field(t, again, "id") || calls.Load() != 2 {
		t.Fatal("unknown call replayed or released")
	}
	in.Key = control.NewID()
	expect(t, f.admin, "POST", "/ai-gateway/test", in, 409)
	if calls.Load() != 2 {
		t.Fatal("new key bypassed unresolved test")
	}
	route.Model.APIKey = ""
	route.Model.BaseURL = "https://another-synthetic.example/v1"
	cfg["settings"] = control.GatewaySettings{Version: 2}
	cfg["route"] = route
	expect(t, f.admin, "PUT", "/ai-gateway", cfg, 409)
}

func TestPostgresGatewayCurrencyPolicyIsVersionedAndBounded(t *testing.T) {
	f := newCommercialFixture(t)
	owner := f.owner("currency-owner", "PRO")
	route := control.ModelRoute{Alias: "default", Model: assistant.ModelConfig{
		BaseURL: "https://synthetic.example/v1", Model: "synthetic", APIKey: "synthetic-business-key"}}
	configure := func(version int64) map[string]any {
		return map[string]any{"settings": control.GatewaySettings{Version: version}, "route": route}
	}
	expect(t, f.admin, "PUT", "/ai-gateway", configure(0), 200)
	var probes atomic.Int32
	f.s.Gateway.InspectionClient = &http.Client{Transport: gatewayProbeTransport(func(r *http.Request) (*http.Response, error) {
		probes.Add(1)
		body := `{"schema_version":"1.0","success":true,"data":{"currency":"USD","price_unit":"per_1m_tokens","models":[{"model_name":"synthetic","enabled":true,"input_price":0.1,"output_price":0.3,"cache_input_price":0,"cache_create_price":0}]}}`
		switch r.URL.Path {
		case "/v1/usage":
			body = `{"isValid":true,"mode":"unrestricted","unit":"USD","balance":2,"remaining":1.2345678919}`
		case "/v1/models":
			body = `{"data":[{"id":"synthetic"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	current := func() (string, bool) {
		var data struct {
			Provider struct {
				Parity bool `json:"usd_usdt_parity"`
			} `json:"provider"`
			Runtime struct {
				Routes []struct {
					ID string `json:"rate_id"`
				} `json:"routes"`
			} `json:"runtime"`
		}
		raw := expect(t, f.admin, "GET", "/ai-gateway", nil, 200)
		if json.Unmarshal(raw, &data) != nil || strings.Contains(string(raw), "synthetic-business-key") {
			t.Fatal("unsafe provider projection")
		}
		return data.Runtime.Routes[0].ID, data.Provider.Parity
	}
	rate, parity := current()
	if parity {
		t.Fatal("currency policy defaulted to approved")
	}
	inspect := func(rate string) control.UpstreamSnapshot {
		var out control.UpstreamSnapshot
		if json.Unmarshal(expect(t, f.admin, "POST", "/ai-gateway/inspect", map[string]string{"rate_id": rate}, 200), &out) != nil {
			t.Fatal("invalid inspection")
		}
		return out
	}
	before := inspect(rate)
	if before.CreditCeiling != nil || before.MappedRates != nil || before.MappingStatus != "currency_conversion_required" {
		t.Fatal("unconfirmed policy used")
	}
	expect(t, f.admin, "POST", "/ai-gateway/inspect", map[string]any{"rate_id": rate, "usd_usdt_parity": true}, 400)
	route.USDUSDTParity, route.Model.APIKey = true, ""
	expect(t, owner.client, "PUT", "/ai-gateway", configure(1), 403)
	expect(t, f.admin, "PUT", "/ai-gateway", configure(1), 200)
	next, parity := current()
	if next == rate || !parity {
		t.Fatal("policy did not receive a new immutable rate snapshot")
	}
	expect(t, f.admin, "POST", "/ai-gateway/inspect", map[string]string{"rate_id": rate}, 409)
	after := inspect(next)
	if !after.USDUSDTParity || after.CreditCeiling == nil || *after.CreditCeiling != 1234567891 ||
		after.MappedRates == nil || after.MappedRates.Input != 100000 || after.MappingStatus != "rates_available" || probes.Load() != 6 {
		t.Fatal("confirmed mapping", after)
	}
	expect(t, f.admin, "POST", "/ai-gateway/test", control.GatewayTestInput{RateID: next, Key: control.NewID(), MaxCost: 1_000_000}, 409)
	var calls, audit int
	var enabled, verified bool
	var platformLimit, testLimit int64
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM ai_calls),
		(SELECT count(*) FROM audit_events WHERE action='ai_gateway_configured' AND resource_id=$1::text AND detail->>'usd_usdt_parity'='true'),
		s.enabled,s.platform_limit_micro,s.test_limit_micro,m.verified
		FROM ai_gateway_settings s CROSS JOIN ai_model_rates m WHERE m.id=$1::uuid`, next).Scan(
		&calls, &audit, &enabled, &platformLimit, &testLimit, &verified); err != nil ||
		calls != 0 || audit != 1 || enabled || verified || platformLimit != 0 || testLimit != 0 {
		t.Fatal("currency confirmation bypassed financial gates", err)
	}
}
