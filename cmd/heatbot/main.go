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
)

func main() {
	token := os.Getenv("AI_BRIDGE_TOKEN")
	if len(token) < 32 {
		slog.Error("AI_BRIDGE_TOKEN must contain at least 32 bytes")
		os.Exit(1)
	}
	engine := heatbot.New(heatbot.NewHTTPProvider(strings.Fields(os.Getenv("AI_PROVIDER_ORIGINS"))))
	defer engine.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"engine":"go","parity_verified":false,"native_sender":false}`)
	})
	mux.HandleFunc("POST /internal/v1/generate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(token)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		// This service is not a native sender. Production stays gated until parity
		// and every persistent memory/learning scope have passed acceptance.
		if os.Getenv("AI_ALLOW_TEST_GENERATION") != "true" {
			http.Error(w, "engine_acceptance_required", 409)
			return
		}
		var in heatbot.Request
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "invalid_request", 400)
			return
		}
		result, err := engine.Submit(r.Context(), in)
		if err != nil {
			http.Error(w, err.Error(), 429)
			return
		}
		select {
		case out := <-result:
			if out.Err != nil {
				http.Error(w, "generation_not_completed", 409)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out.Proposal)
		case <-r.Context().Done():
		}
	})
	server := &http.Server{Addr: ":8090", Handler: mux, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 100 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
