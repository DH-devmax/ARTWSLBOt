package httpapi

import (
	"encoding/json"
	"testing"

	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
)

func TestPostgresGatewayUnpricedDraftCannotActivate(t *testing.T) {
	f := newCommercialFixture(t)
	input := map[string]any{
		"settings": control.GatewaySettings{Version: 0},
		"route": control.ModelRoute{Alias: "default", Model: assistant.ModelConfig{
			BaseURL: "https://synthetic.example/v1", Model: "synthetic-model", APIKey: "synthetic-business-key",
		}},
	}
	expect(t, f.admin, "PUT", "/ai-gateway", input, 200)
	var data map[string]any
	if err := json.Unmarshal(expect(t, f.admin, "GET", "/ai-gateway", nil, 200), &data); err != nil {
		t.Fatal(err)
	}
	provider := data["provider"].(map[string]any)
	if provider["model"] != "synthetic-model" || provider["has_key"] != true || provider["api_key"] != nil {
		t.Fatal("draft projection leaked key", data)
	}
	input["settings"] = control.GatewaySettings{Version: 1, Enabled: true, PlatformLimit: 1000000}
	route := input["route"].(control.ModelRoute)
	route.Enabled = true
	input["route"] = route
	expect(t, f.admin, "PUT", "/ai-gateway", input, 409)
	route.Verified = true
	input["route"] = route
	expect(t, f.admin, "PUT", "/ai-gateway", input, 400)
	var calls int
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT count(*) FROM ai_calls`).Scan(&calls); err != nil || calls != 0 {
		t.Fatal("draft invoked model", err)
	}
}
