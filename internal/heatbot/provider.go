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
	u, err := url.Parse(r.Model.Endpoint)
	if err != nil || u.Host == "" || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("provider_endpoint_rejected")
	}
	origin := u.Scheme + "://" + u.Host
	allowed := false
	for _, entry := range p.AllowedOrigins {
		allowed = allowed || entry == origin
	}
	if !allowed || r.Model.Name == "" || len(r.Model.Name) > 200 || r.Model.APIKey == "" ||
		len(r.Model.APIKey) > 4096 || strings.ContainsAny(r.Model.APIKey, "\r\n") {
		return "", errors.New("provider_configuration_rejected")
	}
	messages := append([]Message{{Role: "system", Content: r.Persona}}, r.Messages...)
	body, err := json.Marshal(map[string]any{"model": r.Model.Name, "messages": messages, "stream": false})
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return "", errors.New("provider_request_invalid")
	}
	req.Header.Set("Authorization", "Bearer "+r.Model.APIKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := p.Client.Do(req)
	if err != nil {
		return "", errors.New("provider_transport_failed")
	}
	defer response.Body.Close()
	// Never return vendor bodies; they can contain prompts or credentials.
	if response.StatusCode != http.StatusOK {
		return "", errors.New("provider_request_failed")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return "", errors.New("provider_response_too_large")
	}
	var result struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Choices) != 1 ||
		strings.TrimSpace(result.Choices[0].Message.Content) == "" || len(result.Choices[0].Message.Content) > 16000 {
		return "", errors.New("provider_response_invalid")
	}
	return result.Choices[0].Message.Content, nil
}
