package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
)

// UpstreamSnapshot contains only the selected model's safe metadata. Raw
// responses, key identifiers and upstream request history are never persisted.
type UpstreamSnapshot struct {
	RateID          string           `json:"rate_id"`
	CheckedAt       time.Time        `json:"checked_at"`
	UsageStatus     string           `json:"usage_status"`
	BudgetKind      string           `json:"budget_kind,omitempty"`
	Currency        string           `json:"currency,omitempty"`
	Remaining       string           `json:"remaining,omitempty"`
	CreditCeiling   *int64           `json:"credit_ceiling_micro,omitempty"`
	USDUSDTParity   bool             `json:"usd_usdt_parity"`
	ModelsStatus    string           `json:"models_status"`
	ModelAvailable  bool             `json:"model_available"`
	PricingStatus   string           `json:"pricing_status"`
	PricingCurrency string           `json:"pricing_currency,omitempty"`
	PricingUnit     string           `json:"pricing_unit,omitempty"`
	MappingStatus   string           `json:"mapping_status"`
	MappedRates     *aigateway.Rates `json:"mapped_rates,omitempty"`
}

type gatewayInspector struct {
	client        *http.Client
	usdUSDTParity bool
}

func (p gatewayInspector) creditCurrency(currency string) bool {
	return currency == "USDT" || currency == "USD" && p.usdUSDTParity
}

func normalizedGatewayModel(c assistant.ModelConfig) (assistant.ModelConfig, error) {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Path != "" && u.Path != "/v1") || u.RawPath != "" {
		return c, Denied(400, "gateway_origin_or_v1_required")
	}
	u.Path = "/v1"
	c.BaseURL = u.String()
	if c.Validate() != nil {
		return c, Denied(400, "invalid_gateway_model")
	}
	return c, nil
}

func (s *GatewayService) routeModel(ctx context.Context) (string, GatewayProviderConfig, error) {
	var id string
	var sealed []byte
	err := s.DB.Platform.QueryRow(ctx, `SELECT m.id::text,m.provider_sealed FROM ai_model_routes r
		JOIN ai_model_rates m ON m.id=r.rate_id WHERE r.alias='default'`).Scan(&id, &sealed)
	if err == pgx.ErrNoRows {
		return "", GatewayProviderConfig{}, Denied(409, "gateway_not_configured")
	}
	if err != nil {
		return "", GatewayProviderConfig{}, err
	}
	raw, err := s.Vault.Open("ai-gateway:"+id, sealed)
	if err != nil {
		return "", GatewayProviderConfig{}, err
	}
	defer clear(raw)
	var model GatewayProviderConfig
	if json.Unmarshal(raw, &model) != nil {
		return "", model, Denied(503, "model_unavailable")
	}
	model.ModelConfig, err = normalizedGatewayModel(model.ModelConfig)
	return id, model, err
}

// Inspect makes fixed, read-only calls; it cannot generate text, grant credits,
// enable consumers, or change the administrator's monthly budgets.
func (s *GatewayService) Inspect(ctx context.Context, ref SessionRef, expected string) (UpstreamSnapshot, error) {
	var out UpstreamSnapshot
	a, err := sessionActor(ctx, s.DB.Platform, ref, false)
	if err != nil {
		return out, err
	}
	if a.Role != "superadmin" || a.TenantID != "" {
		return out, Denied(403, "superadmin_required")
	}
	id, model, err := s.routeModel(ctx)
	if err != nil {
		return out, err
	}
	if id != expected {
		return out, Denied(409, "model_route_changed")
	}
	release, err := s.Scheduler.Acquire(ctx, "platform-inspection")
	if err != nil {
		return out, Denied(429, "ai_gateway_busy")
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	client := s.InspectionClient
	if client == nil {
		client = assistant.NewPublicHTTPClient(4 * time.Second)
		defer client.CloseIdleConnections()
	}
	out = (gatewayInspector{client: client, usdUSDTParity: model.USDUSDTParity}).inspect(ctx, id, model.ModelConfig)
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err = sessionActor(ctx, tx, ref, true)
	if err != nil || a.Role != "superadmin" || a.TenantID != "" {
		return out, Denied(403, "superadmin_required")
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT rate_id::text FROM ai_model_routes WHERE alias='default' FOR SHARE`).Scan(&current); err != nil {
		return out, err
	}
	if current != id {
		return out, Denied(409, "model_route_changed")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ai_upstream_snapshots(rate_id,snapshot,checked_by)
		VALUES($1,$2,$3) ON CONFLICT(rate_id) DO UPDATE
		SET snapshot=excluded.snapshot,checked_by=excluded.checked_by,checked_at=clock_timestamp()`, id, out, a.ID); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
		VALUES($1,'ai_upstream_inspected',$2,jsonb_build_object('mapping_status',$3::text,'usd_usdt_parity',$4::boolean))`,
		a.ID, id, out.MappingStatus, out.USDUSDTParity); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (p gatewayInspector) get(ctx context.Context, model assistant.ModelConfig, path string, authenticated bool, value any) string {
	u, _ := url.Parse(model.BaseURL)
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "invalid_response"
	}
	req.Header.Set("Accept", "application/json")
	if authenticated {
		req.Header.Set("Authorization", "Bearer "+model.APIKey)
	}
	res, err := p.client.Do(req)
	if err != nil {
		return "unavailable"
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return "access_denied"
	}
	if res.StatusCode != 200 {
		return "unsupported"
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 256*1024+1))
	if err != nil || len(raw) > 256*1024 || json.Unmarshal(raw, value) != nil {
		return "invalid_response"
	}
	return "ok"
}

func (p gatewayInspector) inspect(ctx context.Context, id string, model assistant.ModelConfig) UpstreamSnapshot {
	out := UpstreamSnapshot{RateID: id, CheckedAt: time.Now().UTC(), MappingStatus: "unavailable", USDUSDTParity: p.usdUSDTParity}
	var usage struct {
		Valid     *bool       `json:"isValid"`
		Mode      string      `json:"mode"`
		Unit      string      `json:"unit"`
		Balance   json.Number `json:"balance"`
		Remaining json.Number `json:"remaining"`
	}
	out.UsageStatus = p.get(ctx, model, "/v1/usage", true, &usage)
	if out.UsageStatus == "ok" {
		// Only the verified wallet contract is mapped. Subscription quotas and
		// rolling windows need their own semantics, never a guessed reset time.
		if usage.Valid == nil || !*usage.Valid || usage.Mode != "unrestricted" ||
			!validAmount(usage.Remaining.String()) || !validAmount(usage.Balance.String()) ||
			(usage.Unit != "USD" && usage.Unit != "USDT" && usage.Unit != "CNY") {
			out.UsageStatus = "unsupported_schema"
		} else {
			out.BudgetKind, out.Currency, out.Remaining = "wallet", usage.Unit, usage.Remaining.String()
			if p.creditCurrency(usage.Unit) {
				// One credit is .001 USDT. Round down a budget, never up.
				r, _ := new(big.Rat).SetString(out.Remaining)
				r.Mul(r, big.NewRat(1_000_000_000, 1))
				n := new(big.Int).Quo(r.Num(), r.Denom())
				if n.IsInt64() {
					v := n.Int64()
					out.CreditCeiling = &v
				}
			}
		}
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	out.ModelsStatus = p.get(ctx, model, "/v1/models", true, &models)
	if out.ModelsStatus == "ok" {
		if models.Data == nil {
			out.ModelsStatus = "invalid_response"
		}
		for _, candidate := range models.Data {
			out.ModelAvailable = out.ModelAvailable || candidate.ID == model.Model
		}
	}
	var pricing struct {
		Schema  string `json:"schema_version"`
		Success bool   `json:"success"`
		Data    struct {
			Currency string `json:"currency"`
			Unit     string `json:"price_unit"`
			Models   []struct {
				Model   string      `json:"model_name"`
				Enabled bool        `json:"enabled"`
				Input   json.Number `json:"input_price"`
				Output  json.Number `json:"output_price"`
				Read    json.Number `json:"cache_input_price"`
				Write   json.Number `json:"cache_create_price"`
				Write1H json.Number `json:"cache_create_price_1h"`
			} `json:"models"`
		} `json:"data"`
	}
	out.PricingStatus = p.get(ctx, model, "/api/provider/pricing", false, &pricing)
	if out.PricingStatus != "ok" {
		return out
	}
	if !pricing.Success || pricing.Schema != "1.0" || pricing.Data.Unit != "per_1m_tokens" ||
		(pricing.Data.Currency != "USD" && pricing.Data.Currency != "USDT" && pricing.Data.Currency != "CNY") {
		out.PricingStatus = "unsupported_schema"
		return out
	}
	out.PricingCurrency, out.PricingUnit = pricing.Data.Currency, pricing.Data.Unit
	out.MappingStatus = "model_price_missing"
	matches := 0
	var mapped aigateway.Rates
	for _, m := range pricing.Data.Models {
		if m.Model != model.Model || !m.Enabled {
			continue
		}
		matches++
		var err error
		mapped.Input, err = rateAmount(m.Input.String())
		if err == nil {
			mapped.Output, err = rateAmount(m.Output.String())
		}
		if err == nil {
			mapped.CacheRead, err = rateAmount(m.Read.String())
		}
		if err == nil {
			mapped.CacheWrite, err = rateAmount(m.Write.String())
		}
		out.MappingStatus = "rates_available"
		if err != nil || mapped.Input+mapped.Output == 0 {
			out.MappingStatus = "incomplete_prices"
		} else if m.Write1H != "" && m.Write1H != m.Write {
			out.MappingStatus = "unsupported_cache_tiers"
		} else if !p.creditCurrency(pricing.Data.Currency) {
			out.MappingStatus = "currency_conversion_required"
		}
	}
	if matches > 1 {
		out.MappingStatus = "ambiguous_model_prices"
	}
	if out.MappingStatus == "rates_available" {
		if out.ModelsStatus != "ok" || !out.ModelAvailable {
			out.MappingStatus = "model_not_available"
		} else {
			out.MappedRates = &mapped
		}
	}
	return out
}

func validAmount(value string) bool {
	if value == "" || len(value) > 32 || strings.ContainsAny(value, "/eE") {
		return false
	}
	r, ok := new(big.Rat).SetString(value)
	return ok && r.Sign() >= 0 && r.Cmp(big.NewRat(1_000_000_000, 1)) <= 0
}

func rateAmount(value string) (int64, error) {
	if !validAmount(value) {
		return 0, errors.New("invalid_upstream_price")
	}
	r, _ := new(big.Rat).SetString(value)
	r.Mul(r, big.NewRat(1_000_000, 1))
	// Price snapshots round up to the smallest representable price unit.
	n := new(big.Int).Quo(new(big.Int).Add(r.Num(), new(big.Int).Sub(r.Denom(), big.NewInt(1))), r.Denom())
	if !n.IsInt64() || n.Int64() > 1_000_000_000_000 {
		return 0, errors.New("invalid_upstream_price")
	}
	return n.Int64(), nil
}
