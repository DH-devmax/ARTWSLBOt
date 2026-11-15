package httpapi

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestDecodeDistributionWindowLimitAndPercentiles(t *testing.T) {
	now := time.Now().UTC()
	points := []decodePoint{
		{At: now.Add(-24 * time.Hour), MS: 9999},
		{At: now.Add(time.Second), MS: 9999},
		{At: now, MS: -1}, {At: now, MS: math.NaN()}, {At: now, MS: math.Inf(1)},
	}
	for i := 0; i < 1100; i++ {
		points = append(points, decodePoint{ID: int64(i), At: now.Add(-time.Duration(i) * time.Second), MS: float64(i)})
	}
	out := decodeDistribution(points, now, true)
	if out.Samples != 1000 || out.SampleLimit != 1000 || out.WindowSeconds != 86400 ||
		out.P95 == nil || math.Abs(*out.P95-949.05) > .0001 ||
		out.From == nil || !out.From.Equal(now.Add(-999*time.Second)) ||
		out.To == nil || !out.To.Equal(now) {
		t.Fatalf("wrong bounded distribution: %+v", out)
	}
	total := 0
	for _, bucket := range out.Histogram {
		total += bucket
	}
	if total != 1000 {
		t.Fatal("histogram does not describe quantile sample population")
	}
	empty := decodeDistribution(nil, now, false)
	if empty.P50 != nil || empty.P95 != nil || empty.P99 != nil || empty.Samples != 0 || empty.Available {
		t.Fatal("missing observations invented latency")
	}
}

func TestWorkspaceP95MergesSamplesAndFencesWorkspace(t *testing.T) {
	now := time.Now().UTC()
	low, high := []decodePoint{}, []decodePoint{}
	for i := 0; i < 990; i++ {
		low = append(low, decodePoint{ID: int64(i), At: now, MS: 1})
	}
	for i := 0; i < 10; i++ {
		high = append(high, decodePoint{ID: int64(i + 1000), At: now, MS: 100})
	}
	server := &Server{}
	server.monitoring.last = systemMonitor{DatabaseAvailable: true, DatabaseSampledAt: now, Accounts: []accountMonitor{
		{ID: "low", Workspace: "wsl", decodePoints: low},
		{ID: "high", Workspace: "wsl", decodePoints: high},
		{ID: "other", Workspace: "lajiaoliao", decodePoints: []decodePoint{{At: now, MS: 99999}}},
	}}
	out := server.workspaceDecodeDistribution("wsl")
	if out.Samples != 1000 || out.P95 == nil || *out.P95 != 1 {
		t.Fatalf("averaged account P95s or crossed workspace: %+v", out)
	}
	raw, err := json.Marshal(out)
	if err != nil || strings.Contains(string(raw), `"id"`) || strings.Contains(string(raw), `"points"`) {
		t.Fatal("raw event observations leaked into API summary")
	}
	server.monitoring.last.DatabaseAvailable = false
	if server.workspaceDecodeDistribution("wsl").Available {
		t.Fatal("database failure reported a fresh measurement")
	}
}

func TestMonitorDecodeDistributionUsesDatabaseClock(t *testing.T) {
	now := time.Now().UTC()
	stale := now.Add(-time.Minute)
	snapshot := systemMonitor{DatabaseAvailable: true, DatabaseSampledAt: stale, Accounts: []accountMonitor{
		{Workspace: "wsl", SampledAt: now, decodePoints: []decodePoint{{At: stale, MS: 2}}},
		{Workspace: "lajiaoliao", SampledAt: now.Add(time.Hour), decodePoints: []decodePoint{{At: now, MS: 9999}}},
	}}
	out := monitorDecodeDistribution(snapshot, "wsl")
	if out.Available || !out.SampledAt.Equal(stale) || out.Samples != 1 || *out.P95 != 2 {
		t.Fatalf("a newer account clock masked stale database observations: %+v", out)
	}
	snapshot.DatabaseSampledAt = now.Add(time.Hour)
	if monitorDecodeDistribution(snapshot, "wsl").Available {
		t.Fatal("future sampling time was treated as fresh")
	}
}
