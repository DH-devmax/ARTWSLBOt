package heatbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

type Model struct {
	Endpoint string `json:"endpoint"`
	Name     string `json:"name"`
	APIKey   string `json:"api_key"`
}

type HTTPProvider struct {
	Client         *http.Client
	AllowedOrigins []string
}

func NewHTTPProvider(origins []string) *HTTPProvider {
	return &HTTPProvider{AllowedOrigins: append([]string(nil), origins...), Client: &http.Client{
		Timeout: 90 * time.Second,
		Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 60 * time.Second, MaxConnsPerHost: 4},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("provider_redirect_rejected") },
	}}
}

func (p *HTTPProvider) Generate(ctx context.Context, r Request) (string, error) {
	result, err := p.GenerateDetailed(ctx, r)
	return result.Text, err
}

func (p *HTTPProvider) GenerateDetailed(ctx context.Context, r Request) (out Generation, err error) {
	out.Usage = unknownUsage()
	r, err = snapshot(r)
	if err != nil {
		return out, err
	}
	if ctx.Err() != nil {
		return out, contextError(ctx)
	}
	u, err := url.Parse(r.Model.Endpoint)
	if err != nil || u.Host == "" || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return out, errors.New("provider_endpoint_rejected")
	}
	origin := u.Scheme + "://" + u.Host
	allowed := false
	for _, entry := range p.AllowedOrigins {
		allowed = allowed || entry == origin
	}
	if !allowed || r.Model.Name == "" || len(r.Model.Name) > 200 || r.Model.APIKey == "" ||
		len(r.Model.APIKey) > 4096 || strings.ContainsAny(r.Model.APIKey, "\r\n") {
		return out, errors.New("provider_configuration_rejected")
	}
	payload := map[string]any{"model": r.Model.Name, "messages": prompt(r), "stream": false,
		"max_tokens": r.Settings.MaxOutputTokens}
	if r.Settings.Temperature != nil {
		payload["temperature"] = *r.Settings.Temperature
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return out, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return out, errors.New("provider_request_invalid")
	}
	req.Header.Set("Authorization", "Bearer "+r.Model.APIKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := p.Client.Do(req)
	if err != nil {
		return out, errors.New("provider_transport_failed")
	}
	defer response.Body.Close()
	// Never return vendor bodies; they can contain prompts or credentials.
	if response.StatusCode != http.StatusOK {
		return out, errors.New("provider_request_failed")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return out, errors.New("provider_response_too_large")
	}
	var result struct {
		Choices []struct {
			Message struct {
				Message
				ToolCalls    json.RawMessage `json:"tool_calls"`
				FunctionCall json.RawMessage `json:"function_call"`
				Refusal      string          `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if !utf8.Valid(data) || json.Unmarshal(data, &result) != nil {
		return out, errors.New("provider_response_invalid")
	}
	out.Usage = reportedUsage(result.Usage)
	if len(result.Choices) != 1 {
		return out, errors.New("provider_response_invalid")
	}
	choice := result.Choices[0]
	if strings.TrimSpace(choice.Message.Content) == "" || len(choice.Message.Content) > 16000 ||
		choice.Message.Role != "assistant" || choice.Message.Refusal != "" ||
		nonemptyJSON(choice.Message.ToolCalls) || nonemptyJSON(choice.Message.FunctionCall) {
		return out, errors.New("provider_response_invalid")
	}
	if choice.FinishReason != "" && choice.FinishReason != "stop" {
		return out, errors.New("provider_response_incomplete")
	}
	out.Text = choice.Message.Content
	return out, nil
}

func nonemptyJSON(value json.RawMessage) bool {
	return len(value) != 0 && string(value) != "null" && string(value) != "[]"
}
