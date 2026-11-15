package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"wangshangliao/internal/heatbot"
	"wangshangliao/internal/metricsproc"
	"wangshangliao/internal/monitor"
)

func main() {
	token := os.Getenv("AI_BRIDGE_TOKEN")
	if len(token) < 32 {
		slog.Error("AI_BRIDGE_TOKEN must contain at least 32 bytes")
		os.Exit(1)
	}
	engine := heatbot.New(heatbot.NewGatewayProvider())
	defer engine.Close()
	sampler := monitor.NewSampler("heatbot_self", metricsproc.Reader{})
	mux := handler(engine, token, os.Getenv("AI_ALLOW_TEST_GENERATION") == "true", sampler)
	server := &http.Server{Addr: ":8090", Handler: mux, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 100 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	samplingDone := make(chan struct{})
	go func() { defer close(samplingDone); sampler.Run(ctx) }()
	defer func() { stop(); <-samplingDone }()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("Go heat-group engine listening", "parity_verified", false, "native_sender", false)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("chat engine stopped", "code", "http_server_error")
		os.Exit(1)
	}
}

func handler(engine *heatbot.Engine, token string, testGeneration bool, samplers ...*monitor.Sampler) http.Handler {
	mux := http.NewServeMux()
	if len(samplers) > 0 {
		mux.HandleFunc("GET /internal/v1/metrics", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(samplers[0].Snapshot())
		})
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"engine":"go","parity_verified":false,"native_sender":false}`)
	})
	generate := func(basic bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !basic && !testGeneration {
				http.Error(w, "engine_acceptance_required", http.StatusConflict)
				return
			}
			var in heatbot.Request
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
				http.Error(w, "invalid_request", 400)
				return
			}
			// Basic chat has no plugins, memory or direct vendor configuration.
			// The shared gateway redeems and validates its durable, single-use job.
			if basic && (len(in.GatewayToken) != 43 || in.ModelAlias != "default" || in.Context != nil) {
				http.Error(w, "basic_chat_scope_required", 400)
				return
			}
			result, err := engine.Submit(r.Context(), in)
			if err != nil {
				http.Error(w, err.Error(), http.StatusTooManyRequests)
				return
			}
			select {
			case out := <-result:
				if out.Err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(struct {
						Error string        `json:"error"`
						Usage heatbot.Usage `json:"usage"`
					}{"generation_not_completed", out.Usage})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(out.Proposal)
			case <-r.Context().Done():
			}
		}
	}
	mux.HandleFunc("POST /internal/v1/generate", generate(false))
	mux.HandleFunc("POST /internal/v1/chat/generate", generate(true))
	return mux
}
