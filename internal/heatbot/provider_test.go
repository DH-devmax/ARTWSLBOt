package heatbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProviderContractAndOrigin(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer synthetic-secret" {
			t.Error("provider request does not match the explicit contract")
		}
		var payload struct {
			Messages []Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if len(payload.Messages) != 2 || payload.Messages[0].Content != "snapshot" {
			t.Error("persona snapshot missing")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"synthetic reply"}}]}`))
	}))
	defer server.Close()
	p := NewHTTPProvider([]string{server.URL})
	p.Client = server.Client()
	request := fixture("group")
	request.Model = Model{Endpoint: server.URL + "/v1", Name: "synthetic", APIKey: "synthetic-secret"}
	reply, err := p.Generate(context.Background(), request)
	if err != nil || reply != "synthetic reply" {
		t.Fatalf("provider contract failed: %s %v", reply, err)
	}
	request.Model.Endpoint = strings.Replace(server.URL, "https", "http", 1)
	if _, err := p.Generate(context.Background(), request); err == nil {
		t.Fatal("insecure provider accepted")
	}
	request.Model.Endpoint = "https://unapproved.invalid/v1"
	if _, err := p.Generate(context.Background(), request); err == nil || calls.Load() != 1 {
		t.Fatal("unapproved origin reached the network")
	}
}

func TestProviderSnapshotUsageAndTextOnlyContract(t *testing.T) {
	var calls atomic.Int32
	response := `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"reply","reasoning_content":"private reasoning"}}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			Messages    []Message `json:"messages"`
			MaxTokens   int       `json:"max_tokens"`
			Temperature float64   `json:"temperature"`
			Tools       any       `json:"tools"`
			Stream      bool      `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if len(payload.Messages) != 3 || payload.Messages[0].Content != "snapshot\n\nReply style:\nbrief" ||
			payload.Messages[1].Role != "user" || payload.MaxTokens != 512 || payload.Temperature != 0.6 ||
			payload.Tools != nil || payload.Stream {
			t.Errorf("unexpected snapshot request: %#v", payload)
		}
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	p := NewHTTPProvider([]string{server.URL})
	p.Client = server.Client()
	r := contextual("group")
	r.Model = Model{Endpoint: server.URL + "/v1", Name: "synthetic", APIKey: "synthetic-secret"}
	for _, tc := range []struct {
		name, body, status string
		failure            bool
	}{
		{"reported", response, "reported", false},
		{"missing-usage", `{"choices":[{"message":{"role":"assistant","content":"reply"}}]}`, "unknown", false},
		{"bad-usage", `{"choices":[{"message":{"role":"assistant","content":"reply"}}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":12}}`, "unknown", false},
		{"truncated", `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"partial"}}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`, "reported", true},
		{"tool-call", `{"choices":[{"message":{"role":"assistant","content":"reply","tool_calls":[{"function":{"name":"send"}}]}}]}`, "unknown", true},
		{"legacy-function", `{"choices":[{"message":{"role":"assistant","content":"reply","function_call":{"name":"send"}}}]}`, "unknown", true},
		{"wrong-role", `{"choices":[{"message":{"role":"system","content":"reply"}}]}`, "unknown", true},
		{"refusal", `{"choices":[{"message":{"role":"assistant","content":"reply","refusal":"refused"}}]}`, "unknown", true},
		{"multiple-choices", `{"choices":[{"message":{"role":"assistant","content":"a"}},{"message":{"role":"assistant","content":"b"}}]}`, "unknown", true},
		{"oversize", `{"choices":[{"message":{"role":"assistant","content":"` + strings.Repeat("x", 16001) + `"}}]}`, "unknown", true},
		{"invalid-utf8", "{\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":\"\xff\"}}]}", "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response = tc.body
			before := calls.Load()
			got, err := p.GenerateDetailed(context.Background(), r)
			if (err != nil) != tc.failure || got.Usage.Status != tc.status || calls.Load() != before+1 {
				t.Fatalf("text-only/usage/retry contract: %#v %v calls=%d", got, err, calls.Load()-before)
			}
			if tc.failure && got.Text != "" {
				t.Fatal("failed generation leaked partial/tool text")
			}
			if !tc.failure && got.Text != "reply" {
				t.Fatal("reasoning content replaced text")
			}
		})
	}
}

func TestProviderDoesNotRetryPaidFailureOrFollowRedirect(t *testing.T) {
	var calls atomic.Int32
	code := http.StatusTooManyRequests
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "https://unapproved.invalid/steal")
		w.WriteHeader(code)
		_, _ = w.Write([]byte("synthetic-secret vendor prompt"))
	}))
	defer server.Close()
	p := NewHTTPProvider([]string{server.URL})
	redirect := p.Client.CheckRedirect
	p.Client = server.Client()
	p.Client.CheckRedirect = redirect
	r := fixture("group")
	r.Model = Model{Endpoint: server.URL, Name: "synthetic", APIKey: "synthetic-secret"}
	for _, status := range []int{429, 500, 302} {
		code = status
		before := calls.Load()
		got, err := p.GenerateDetailed(context.Background(), r)
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") ||
			got.Usage.Status != "unknown" || got.Text != "" || calls.Load() != before+1 {
			t.Fatalf("failure/redirect/retry contract: %#v %v", got, err)
		}
	}
}
