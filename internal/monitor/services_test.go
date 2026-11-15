package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFixedServiceProbes(t *testing.T) {
	mode, requests := "healthy", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch mode {
		case "healthy":
			if r.Method == http.MethodHead {
				if r.URL.Path != "/" {
					t.Error("web probed API health")
				}
				return
			}
			_ = json.NewEncoder(w).Encode(Resources{Source: "heatbot_self", Available: true, SampledAt: time.Now().UTC()})
		case "large":
			_, _ = w.Write([]byte(strings.Repeat("x", 16*1024+1)))
		case "redirect":
			http.Redirect(w, r, serverURLNotFollowed, http.StatusFound)
		case "old":
			_ = json.NewEncoder(w).Encode(Resources{Source: "heatbot_self", Available: true, SampledAt: time.Now().Add(-16 * time.Second)})
		default:
			w.WriteHeader(503)
		}
	}))
	defer server.Close()
	reader := NewServiceReader()
	first := reader.heatbot(t.Context(), Service{}, server.URL)
	if !first.Available {
		t.Fatal(first)
	}
	if !reader.web(t.Context(), Service{}, server.URL).Available {
		t.Fatal("HEAD failed")
	}
	for _, value := range []string{"large", "redirect", "old", "error"} {
		mode = value
		if reader.heatbot(t.Context(), first, server.URL).Available {
			t.Fatal("accepted invalid service", value)
		}
	}
	mode = "error"
	failed := reader.web(t.Context(), first, server.URL)
	if failed.Available || failed.SampledAt != first.SampledAt {
		t.Fatal("lost last observation")
	}
	if requests != 7 {
		t.Fatal("redirect followed or retry performed", requests)
	}
}

const serverURLNotFollowed = "http://127.0.0.1:1/"

func TestServiceTimeoutIsBoundedAndKeepsTimestamp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	reader := NewServiceReader()
	old := Service{Available: true, SampledAt: time.Now().Add(-20 * time.Second)}
	start := time.Now()
	got := reader.heatbot(t.Context(), old, server.URL)
	if got.Available || !got.Stale || got.SampledAt != old.SampledAt || time.Since(start) > 2*time.Second {
		t.Fatal("timeout lost bounds or provenance", got)
	}
}
