package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestModelOrigins(t *testing.T) {
	for _, origin := range []string{"http://example.com/v1", "https://127.0.0.1", "https://[::1]", "https://169.254.169.254",
		"https://10.0.0.1", "https://100.64.0.1", "https://example.com?key=secret", "https://user:pass@example.com", "https://example.com/#x"} {
		if (ModelConfig{BaseURL: origin, Model: "test", APIKey: "test"}).Validate() == nil {
			t.Errorf("accepted %s", origin)
		}
	}
	for _, address := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "192.168.1.1", "198.18.1.1", "2001:db8::1"} {
		if publicAddress(netip.MustParseAddr(address)) {
			t.Errorf("public %s", address)
		}
	}
}
func TestProviderSyntheticStreamAndUnknown(t *testing.T) {
	for _, fixture := range []struct {
		name, body string
		code       int
		unknown    bool
	}{
		{"utf8", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\ndata: [DONE]\n\n", 200, false},
		{"missing_done", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n", 200, true},
		{"rate", "secret response must not be exposed", 429, true},
		{"oversize", "data: " + strings.Repeat("x", 70000) + "\n\n", 200, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer synthetic" {
					t.Error("contract")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(fixture.code)
				// Split inside UTF-8 sequences as well as SSE framing.
				for _, b := range []byte(fixture.body) {
					w.Write([]byte{b})
				}
			}))
			defer server.Close()
			p := &HTTPProvider{config: ModelConfig{BaseURL: server.URL + "/v1", Model: "synthetic", APIKey: "synthetic"}, client: server.Client()}
			text := ""
			res, err := p.Generate(context.Background(), nil, nil, func(v string) error { text += v; return nil })
			if (err != nil) != fixture.unknown || requests != 1 {
				t.Fatalf("err=%v requests=%d", err, requests)
			}
			if !fixture.unknown && (text != "你好" || !res.Usage.Valid()) {
				t.Fatalf("decode: %+v %s", res, text)
			}
		})
	}
}
func TestProviderCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer server.Close()
	p := &HTTPProvider{config: ModelConfig{BaseURL: server.URL, Model: "test", APIKey: "test"}, client: server.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Generate(ctx, nil, nil, func(string) error { return nil })
	if err == nil || strings.Contains(fmt.Sprint(err), "test") {
		t.Fatal("cancellation not sanitized")
	}
}

func TestProviderOutputLimitOnlyNarrows(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Max int `json:"max_tokens"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Max != 32 {
			t.Error("test output budget not honored")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	defer server.Close()
	p := &HTTPProvider{config: ModelConfig{BaseURL: server.URL + "/v1", Model: "synthetic", APIKey: "synthetic"}, client: server.Client()}
	p.LimitOutput(32)
	p.LimitOutput(1024)
	p.LimitOutput(MaxOutputTokens + 1)
	p.LimitOutput(-1)
	if _, err := p.Generate(context.Background(), nil, nil, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
