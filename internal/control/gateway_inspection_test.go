package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"wangshangliao/internal/assistant"
)

type inspectionTransport func(*http.Request) (*http.Response, error)

func (f inspectionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpstreamInspectionMappingAndProjection(t *testing.T) {
	for _, tc := range []struct {
		name, currency, prices, expected, wallet string
		parity, ceiling                          bool
	}{
		{"exact", "USDT", `"input_price":0.1,"output_price":0.3,"cache_input_price":0,"cache_create_price":0`, "rates_available", "USDT", false, true},
		{"usd_unconfirmed", "USD", `"input_price":0.1,"output_price":0.3,"cache_input_price":0,"cache_create_price":0`, "currency_conversion_required", "USD", false, false},
		{"usd_confirmed", "USD", `"input_price":0.1,"output_price":0.3,"cache_input_price":0,"cache_create_price":0`, "rates_available", "USD", true, true},
		{"cny_even_with_parity", "CNY", `"input_price":0.1,"output_price":0.3,"cache_input_price":0,"cache_create_price":0`, "currency_conversion_required", "CNY", true, false},
		{"missing_cache", "USDT", `"input_price":0.1,"output_price":0.3`, "incomplete_prices", "USDT", false, true},
		{"usd_missing_cache", "USD", `"input_price":0.1,"output_price":0.3`, "incomplete_prices", "USD", true, true},
		{"tiers", "USDT", `"input_price":0.1,"output_price":0.3,"cache_input_price":0,"cache_create_price":0,"cache_create_price_1h":1`, "unsupported_cache_tiers", "USDT", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			client := &http.Client{Transport: inspectionTransport(func(r *http.Request) (*http.Response, error) {
				count++
				body := ""
				switch r.URL.Path {
				case "/v1/usage":
					body = `{"isValid":true,"mode":"unrestricted","unit":"` + tc.wallet + `","remaining":1.2345678919,"balance":2,"api_key":"must-not-project"}`
				case "/v1/models":
					body = `{"data":[{"id":"synthetic"}]}`
				case "/api/provider/pricing":
					if r.Header.Get("Authorization") != "" {
						t.Fatal("price request disclosed key")
					}
					body = `{"schema_version":"1.0","success":true,"data":{"currency":"` + tc.currency + `","price_unit":"per_1m_tokens","models":[{"model_name":"synthetic","enabled":true,` + tc.prices + `}]}}`
				default:
					t.Fatal("unbounded path", r.URL.Path)
				}
				if r.URL.Host != "synthetic.example" {
					t.Fatal("origin changed")
				}
				if r.URL.Path != "/api/provider/pricing" && r.Header.Get("Authorization") != "Bearer synthetic" {
					t.Fatal("auth missing")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			out := (gatewayInspector{client: client, usdUSDTParity: tc.parity}).inspect(context.Background(), "rate", assistant.ModelConfig{BaseURL: "https://synthetic.example/v1", Model: "synthetic", APIKey: "synthetic"})
			if count != 3 || out.MappingStatus != tc.expected || out.USDUSDTParity != tc.parity ||
				(out.CreditCeiling != nil) != tc.ceiling || out.CreditCeiling != nil && *out.CreditCeiling != 1234567891 {
				t.Fatalf("unexpected mapping %+v", out)
			}
			if tc.expected == "rates_available" && (out.MappedRates == nil || out.MappedRates.Input != 100000 || out.MappedRates.Output != 300000) {
				t.Fatal("rate arithmetic")
			}
			if tc.expected != "rates_available" && out.MappedRates != nil {
				t.Fatal("unsafe auto mapping")
			}
		})
	}
}

func TestGatewayProviderPolicyPreservesTransportAndLegacySnapshots(t *testing.T) {
	model := assistant.ModelConfig{BaseURL: "https://synthetic.example/v1", Model: "synthetic", APIKey: "synthetic"}
	legacy, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	var config GatewayProviderConfig
	if err = json.Unmarshal(legacy, &config); err != nil || config.ModelConfig != model || config.USDUSDTParity {
		t.Fatal("legacy snapshots must remain unconfirmed", err)
	}
	config.USDUSDTParity = true
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var transport assistant.ModelConfig
	if err = json.Unmarshal(raw, &transport); err != nil || transport != model {
		t.Fatal("accounting policy changed provider transport", err)
	}
}

func TestGatewayOriginsAndAmounts(t *testing.T) {
	for _, origin := range []string{"https://synthetic.example", "https://synthetic.example/", "https://synthetic.example/v1/"} {
		out, err := normalizedGatewayModel(assistant.ModelConfig{BaseURL: origin, Model: "synthetic", APIKey: "synthetic"})
		if err != nil || out.BaseURL != "https://synthetic.example/v1" {
			t.Fatal(origin, err)
		}
	}
	for _, origin := range []string{"https://synthetic.example/path", "https://synthetic.example/v1?x=y", "https://127.0.0.1", "http://synthetic.example"} {
		if _, err := normalizedGatewayModel(assistant.ModelConfig{BaseURL: origin, Model: "synthetic", APIKey: "synthetic"}); err == nil {
			t.Fatal("unsafe origin", origin)
		}
	}
	for _, value := range []string{"", "-1", "NaN", "1/2", "1e100000", strings.Repeat("9", 1000)} {
		if validAmount(value) {
			t.Fatal("unsafe amount")
		}
	}
	if r, err := rateAmount("0.000000001"); err != nil || r != 1 {
		t.Fatal("price must round up")
	}
}

func TestAssistantTemplatesNeverGrantPlatformOrControlledTestActions(t *testing.T) {
	templates := AssistantPermissionTemplates()
	if len(templates) != 4 {
		t.Fatal("templates missing")
	}
	for _, item := range templates {
		for _, permission := range item.Permissions {
			if strings.HasPrefix(permission, "users:") || strings.HasPrefix(permission, "members:") || strings.HasPrefix(permission, "tests:") ||
				strings.HasPrefix(permission, "ledger:") || permission == "groups:configure" || permission == "operations:create" {
				t.Fatal("template overreach")
			}
			if strings.HasPrefix(permission,"group_action:") && item.Code!="assistant_action_approver"{t.Fatal("configuration template gained actions")}
		}
	}
	templates[0].Permissions[0] = "changed"
	if AssistantPermissionTemplates()[0].Permissions[0] == "changed" {
		t.Fatal("mutable global template")
	}
}
