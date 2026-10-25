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
