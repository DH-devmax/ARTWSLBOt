package heatbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// GatewayProvider has one fixed Compose destination. Native chat requests carry
// a single-use control credential and model alias, never a vendor key or URL.
type GatewayProvider struct{ client *http.Client }

func NewGatewayProvider() *GatewayProvider {
	return &GatewayProvider{client: &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, MaxConnsPerHost: 4,
		ResponseHeaderTimeout: 85 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("gateway_redirect_rejected") }}}
}
func (p *GatewayProvider) Generate(ctx context.Context, r Request) (string, error) {
	out, err := p.GenerateDetailed(ctx, r)
	return out.Text, err
}
func (p *GatewayProvider) GenerateDetailed(ctx context.Context, r Request) (Generation, error) {
	out := Generation{Usage: unknownUsage()}
	if r.Model.Endpoint != "" || r.Model.APIKey != "" || r.Model.Name != "" || len(r.GatewayToken) != 43 || r.ModelAlias != "default" || r.ModelVersion < 1 {
		return out, errors.New("gateway_credentials_required")
	}
	body, err := json.Marshal(map[string]any{"model_alias": r.ModelAlias, "model_version": r.ModelVersion, "messages": prompt(r)})
	if err != nil || len(body) > 64*1024 {
		return out, ErrScope
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://api:8080/internal/v1/ai/generate", bytes.NewReader(body))
	if err != nil {
		return out, ErrScope
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.GatewayToken)
	res, err := p.client.Do(req)
	if err != nil {
		return out, errors.New("gateway_result_unknown")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return out, errors.New("gateway_request_rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32*1024+1))
	if err != nil || len(raw) > 32*1024 {
		return out, errors.New("gateway_result_unknown")
	}
	var response struct {
		Text  string `json:"text"`
		Usage struct {
			Input  int64 `json:"prompt_tokens"`
			Output int64 `json:"completion_tokens"`
			Total  int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Text == "" || len(response.Text) > 16000 {
		return out, errors.New("gateway_result_unknown")
	}
	out.Text = response.Text
	if response.Usage.Total > 0 && response.Usage.Total == response.Usage.Input+response.Usage.Output {
		out.Usage = Usage{Status: "reported", InputTokens: &response.Usage.Input, OutputTokens: &response.Usage.Output, TotalTokens: &response.Usage.Total}
	}
	return out, nil
}
