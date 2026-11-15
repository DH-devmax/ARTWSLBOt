package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type ModelConfig struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
}

func (c ModelConfig) Validate() error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || len(c.BaseURL) > 500 ||
		len(c.Model) < 1 || len(c.Model) > 100 || strings.TrimSpace(c.Model) != c.Model ||
		len(c.APIKey) < 1 || len(c.APIKey) > 4096 || strings.ContainsAny(c.APIKey, "\r\n") {
		return ErrInvalid
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicAddress(ip) {
		return ErrInvalid
	}
	if u.Port() != "" {
		n, err := strconv.Atoi(u.Port())
		if err != nil || n < 1 || n > 65535 {
			return ErrInvalid
		}
	}
	return nil
}

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32",
		"64:ff9b::/96", "64:ff9b:1::/48", "2001::/32", "2002::/16", "192.88.99.0/24"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}

type HTTPProvider struct {
	config      ModelConfig
	client      *http.Client
	outputLimit int
}

// LimitOutput can only reduce this request-local provider's output budget.
func (p *HTTPProvider) LimitOutput(limit int) {
	if limit > 0 && limit <= MaxOutputTokens && (p.outputLimit == 0 || limit < p.outputLimit) {
		p.outputLimit = limit
	}
}

func NewHTTPProvider(c ModelConfig) (*HTTPProvider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &HTTPProvider{config: c, client: NewPublicHTTPClient(TurnTimeout)}, nil
}

// NewPublicHTTPClient pins each dial to validated public addresses and never
// follows redirects or uses a host proxy, including for control-owned probes.
func NewPublicHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{Proxy: nil, MaxConnsPerHost: 2, MaxIdleConnsPerHost: 2,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout: 30 * time.Second, ForceAttemptHTTP2: true}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrModel
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, ErrModel
		}
		for _, ip := range ips {
			if !publicAddress(ip) {
				return nil, ErrModel
			}
		}
		var dialer net.Dialer
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, ErrModel
	}
	return &http.Client{Transport: transport, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrModel }}
}

func (p *HTTPProvider) Generate(ctx context.Context, messages []Message, tools []Tool, emit func(string) error) (out Response, err error) {
	limit := p.outputLimit
	if limit == 0 {
		limit = MaxOutputTokens
	}
	body, err := json.Marshal(map[string]any{"model": p.config.Model, "messages": messages,
		"tools": tools, "stream": true, "stream_options": map[string]bool{"include_usage": true},
		"max_tokens": limit, "parallel_tool_calls": false})
	if err != nil || len(body) > 128*1024 {
		return out, ErrLimit
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return out, ErrInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	res, err := p.client.Do(req)
	if err != nil {
		return out, Error("assistant_model_unknown")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests {
		// Never replay an ambiguous paid request automatically.
		return out, Error("assistant_model_rate_limited")
	}
	if res.StatusCode != http.StatusOK {
		return out, ErrModel
	}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		var result struct {
			Choices []struct{ Message Message } `json:"choices"`
			Usage   Usage                       `json:"usage"`
		}
		blob, e := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
		if e != nil || len(blob) > 1024*1024 || json.Unmarshal(blob, &result) != nil || len(result.Choices) != 1 {
			return out, ErrModel
		}
		out = Response{Message: result.Choices[0].Message, Usage: result.Usage}
		if len(out.Message.Content) > MaxResultBytes {
			return out, ErrLimit
		}
		if out.Message.Content != "" {
			err = emit(out.Message.Content)
		}
		return out, err
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		return out, ErrModel
	}
	out.Message.Role = "assistant"
	reader := bufio.NewScanner(io.LimitReader(res.Body, 1024*1024+1))
	reader.Buffer(make([]byte, 4096), 65536)
	pending, data := "", ""
	last := time.Now()
	flush := func() error {
		if pending == "" {
			return nil
		}
		if e := emit(pending); e != nil {
			return e
		}
		pending, last = "", time.Now()
		return nil
	}
	done := false
	process := func() error {
		if data == "" {
			return nil
		}
		frame := strings.TrimSpace(data)
		data = ""
		if frame == "[DONE]" {
			done = true
			return flush()
		}
		var chunk struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				Finish string `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage          `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if !utf8.ValidString(frame) || json.Unmarshal([]byte(frame), &chunk) != nil || len(chunk.Error) > 0 && string(chunk.Error) != "null" || len(chunk.Choices) > 1 {
			return ErrModel
		}
		if chunk.Usage != nil {
			out.Usage = *chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 || choice.Finish == "length" {
				return ErrLimit
			}
			out.Message.Content += choice.Delta.Content
			pending += choice.Delta.Content
			if len(out.Message.Content) > MaxResultBytes {
				return ErrLimit
			}
			for _, part := range choice.Delta.ToolCalls {
				if part.Index < 0 || part.Index >= MaxTools {
					return ErrInvalid
				}
				for len(out.Message.ToolCalls) <= part.Index {
					out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{Type: "function"})
				}
				call := &out.Message.ToolCalls[part.Index]
				call.ID += part.ID
				call.Function.Name += part.Function.Name
				call.Function.Arguments += part.Function.Arguments
				if part.Type != "" && part.Type != "function" || len(call.ID) > 128 ||
					len(call.Function.Name) > 80 || len(call.Function.Arguments) > MaxResultBytes {
					return ErrInvalid
				}
			}
		}
		if len(pending) >= 512 || time.Since(last) >= 250*time.Millisecond {
			return flush()
		}
		return nil
	}
	for reader.Scan() {
		line := reader.Text()
		if line == "" {
			if err := process(); err != nil {
				return out, err
			}
			if done {
				break
			}
		} else if strings.HasPrefix(line, "data:") {
			data += strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ") + "\n"
			if len(data) > 65536 {
				return out, ErrLimit
			}
		}
	}
	if e := reader.Err(); e != nil && !errors.Is(e, io.EOF) {
		return out, Error("assistant_model_unknown")
	}
	if !done {
		return out, Error("assistant_model_unknown")
	}
	return out, nil
}
