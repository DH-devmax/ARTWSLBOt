package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/heatbot"
	"wangshangliao/internal/monitor"
)

type syntheticProvider struct {
	calls atomic.Int32
	fail  bool
}

func TestMetricsEndpointReadsCacheWithoutGeneration(t *testing.T) {
	p := &syntheticProvider{}
	engine := heatbot.New(p)
	defer engine.Close()
	sampler := monitor.NewSampler("heatbot_self", nil)
	h := handler(engine, strings.Repeat("x", 32), false, sampler)
	for range 20 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/internal/v1/metrics", nil))
		var out monitor.Resources
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Available || out.CollectionCount != 0 {
			t.Fatal("metrics endpoint triggered collection", w.Body.String())
		}
	}
	if p.calls.Load() != 0 {
		t.Fatal("metrics invoked model")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/internal/v1/metrics", nil))
	if w.Code != 405 {
		t.Fatal("metrics permits writes")
	}
}

func (p *syntheticProvider) Generate(context.Context, heatbot.Request) (string, error) {
	p.calls.Add(1)
	if p.fail {
		return "", errors.New("synthetic vendor secret that must stay internal")
	}
	return "synthetic reply", nil
}

func TestBasicChatEndpointRequiresScopedGatewayAndNoExtensions(t *testing.T) {
	p := &syntheticProvider{}
	engine := heatbot.New(p)
	defer engine.Close()
	token := strings.Repeat("x", 32)
	h := handler(engine, token, false)
	in := heatbot.Request{ID: "job", Scope: heatbot.Scope{Platform: "wsl", TenantID: "tenant", AccountID: "chat", GroupID: "group"},
		BindingVersion: 1, AccountEpoch: 1, PersonaVersion: 1, Persona: "synthetic",
		Messages: []heatbot.Message{{Role: "user", Content: "hello"}}, ExpiresAt: time.Now().Add(time.Minute),
		ModelAlias: "default", ModelVersion: 1, GatewayToken: strings.Repeat("g", 43)}
	call := func(input heatbot.Request, auth string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", "/internal/v1/chat/generate", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+auth)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out
	}
	if call(in, "wrong").Code != 401 {
		t.Fatal("bridge authorization bypass")
	}
	invalid := in
	invalid.GatewayToken = ""
	if call(invalid, token).Code != 400 {
		t.Fatal("gateway grant omitted")
	}
	invalid = in
	invalid.Context = &heatbot.ContextSnapshot{}
	if call(invalid, token).Code != 400 {
		t.Fatal("extension enabled in basic chat")
	}
	if p.calls.Load() != 0 {
		t.Fatal("invalid requests called provider")
	}
	if out := call(in, token); out.Code != 200 || p.calls.Load() != 1 {
		t.Fatal(out.Code, out.Body.String())
	}
}

func TestPrivateTextEndpointGateUsageAndStrictContract(t *testing.T) {
	p := &syntheticProvider{}
	engine := heatbot.New(p)
	defer engine.Close()
	token := strings.Repeat("synthetic-only-", 3)
	in := heatbot.Request{ID: "generation", Scope: heatbot.Scope{Platform: "lajiaoliao", TenantID: "tenant",
		GroupID: "group", AccountID: "account"}, BindingVersion: 1, AccountEpoch: 1, PersonaVersion: 2,
		Persona: "snapshot", Messages: []heatbot.Message{{Role: "user", Content: "synthetic"}},
		ExpiresAt: time.Now().Add(time.Minute), Settings: heatbot.Settings{Version: 4, MaxOutputTokens: 512}}
	body, _ := json.Marshal(in)
	call := func(h http.Handler, body []byte, credential string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/internal/v1/generate", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+credential)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	closed := handler(engine, token, false)
	if got := call(closed, body, token); got.Code != 409 || p.calls.Load() != 0 {
		t.Fatal("default gate invoked provider")
	}
	open := handler(engine, token, true)
	if got := call(open, body, "wrong"); got.Code != 401 || p.calls.Load() != 0 {
		t.Fatal("unauthorized generation")
	}
	if got := call(open, append(body, []byte("{}")...), token); got.Code != 400 || p.calls.Load() != 0 {
		t.Fatal("trailing JSON accepted")
	}
	var injected map[string]any
	_ = json.Unmarshal(body, &injected)
	injected["tools"] = []string{"send"}
	bad, _ := json.Marshal(injected)
	if got := call(open, bad, token); got.Code != 400 || p.calls.Load() != 0 {
		t.Fatal("unknown native/tool fields accepted")
	}
	got := call(open, body, token)
	var proposal heatbot.Proposal
	if got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &proposal) != nil ||
		proposal.Text != "synthetic reply" || proposal.SettingsVersion != 4 || proposal.Usage.Status != "unknown" ||
		proposal.Scope != in.Scope || proposal.AccountEpoch != 1 {
		t.Fatalf("proposal contract: %d %s", got.Code, got.Body.String())
	}
	p.fail = true
	got = call(open, body, token)
	if got.Code != 409 || !strings.Contains(got.Body.String(), `"status":"unknown"`) ||
		strings.Contains(got.Body.String(), "vendor secret") || p.calls.Load() != 2 {
		t.Fatal("failure lost unknown usage or leaked details/retried")
	}
	req := httptest.NewRequest("GET", "/healthz", nil)
	health := httptest.NewRecorder()
	closed.ServeHTTP(health, req)
	if !strings.Contains(health.Body.String(), `"parity_verified":false`) ||
		!strings.Contains(health.Body.String(), `"native_sender":false`) {
		t.Fatal("health raised acceptance")
	}
}
