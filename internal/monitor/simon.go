package monitor

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Snapshot deliberately excludes process commands/environment, files, network
// addresses, container logs and every mutating Simon API.
type Snapshot struct {
	Available bool      `json:"available"`
	Stale     bool      `json:"stale"`
	SampledAt time.Time `json:"sampled_at,omitempty"`
	Scope     string    `json:"scope"`
	CPUCount  int       `json:"cpu_count"`
	CPUUsage  float64   `json:"cpu_usage"`
	Memory    uint64    `json:"memory_bytes"`
	Used      uint64    `json:"used_memory_bytes"`
	Uptime    uint64    `json:"uptime_seconds"`
	Load      []float64 `json:"load_average"`
}

type Client struct {
	base  string
	mu    sync.Mutex
	until time.Time
	last  Snapshot
}

func New(base string) *Client {
	return &Client{base: base, last: Snapshot{Scope: "container_visible_system"}}
}

func (c *Client) Read(ctx context.Context) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.until) || c.base == "" {
		return c.last
	}
	c.until = time.Now().Add(5 * time.Second)
	sample, err := c.fetch(ctx)
	if err != nil {
		c.last.Stale = true
		return c.last
	}
	c.last = sample
	return c.last
}

func (c *Client) fetch(ctx context.Context) (Snapshot, error) {
	u, err := url.Parse(c.base)
	if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") {
		return Snapshot{}, errors.New("invalid monitoring endpoint")
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws/g"
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second, Proxy: nil}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, response, err := dialer.DialContext(ctx, u.String(), http.Header{})
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()
	conn.SetReadLimit(256 * 1024)
	deadline, _ := ctx.Deadline()
	_ = conn.SetReadDeadline(deadline)
	kind, payload, err := conn.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		return Snapshot{}, errors.New("invalid monitoring frame")
	}
	z, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return Snapshot{}, err
	}
	defer z.Close()
	plain, err := io.ReadAll(io.LimitReader(z, 1024*1024+1))
	if err != nil || len(plain) > 1024*1024 {
		return Snapshot{}, errors.New("monitoring frame too large")
	}
	var raw struct {
		T   int64 `json:"t"`
		CPU struct {
			Count int     `json:"count"`
			Usage float64 `json:"avg_usage"`
		} `json:"cpu"`
		Mem struct {
			Total uint64 `json:"total_mem"`
			Used  uint64 `json:"used_mem"`
		} `json:"mem"`
		Sys struct {
			Uptime uint64    `json:"uptime"`
			Load   []float64 `json:"load_avg"`
		} `json:"sys"`
	}
	if err := json.Unmarshal(plain, &raw); err != nil || raw.CPU.Count < 1 || raw.CPU.Usage < 0 ||
		raw.CPU.Usage > 100 || raw.Mem.Total == 0 || raw.Mem.Used > raw.Mem.Total || len(raw.Sys.Load) > 3 {
		return Snapshot{}, errors.New("invalid monitoring sample")
	}
	return Snapshot{Available: true, SampledAt: time.Now().UTC(), Scope: "container_visible_system",
		CPUCount: raw.CPU.Count, CPUUsage: raw.CPU.Usage, Memory: raw.Mem.Total,
		Used: raw.Mem.Used, Uptime: raw.Sys.Uptime, Load: raw.Sys.Load}, nil
}
