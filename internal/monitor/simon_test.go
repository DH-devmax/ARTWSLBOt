package monitor

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRestrictedCachedSnapshot(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/g" {
			t.Errorf("unapproved Simon path: %s", r.URL.Path)
		}
		calls.Add(1)
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var out bytes.Buffer
		z := gzip.NewWriter(&out)
		_, _ = z.Write([]byte(`{"cpu":{"count":2,"avg_usage":3},"mem":{"total_mem":100,"used_mem":20},
            "sys":{"uptime":100,"load_avg":[0.1,0.2,0.3],"host_name":"secret-host"},
            "processes":[{"env":"SECRET_TOKEN=private"}]}`))
		_ = z.Close()
		_ = conn.WriteMessage(websocket.BinaryMessage, out.Bytes())
	}))
	defer server.Close()
	client := New(server.URL)
	for range 5 {
		sample := client.Read(context.Background())
		if !sample.Available || sample.CPUCount != 2 || sample.Used != 20 {
			t.Fatalf("invalid projected sample: %#v", sample)
		}
		serialized, _ := json.Marshal(sample)
		if strings.Contains(string(serialized), "secret") || strings.Contains(string(serialized), "private") {
			t.Fatal("unapproved fields escaped the projection")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("monitoring reads were not coalesced")
	}
}

func TestDisabledAndInvalidMonitor(t *testing.T) {
	if New("").Read(context.Background()).Available {
		t.Fatal("missing monitor must not look connected")
	}
	if _, err := New("http://user:pass@localhost:1").fetch(context.Background()); err == nil {
		t.Fatal("credentials in monitoring endpoint must be rejected")
	}
}
