package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/monitor"
)

func TestProjectMonitorIndependentServicesAndRetiredSource(t *testing.T) {
	now := time.Now()
	snapshot := systemMonitor{DatabaseAvailable: true, Services: map[string]monitor.Service{
		"api": {Available: true}, "web": {Available: false}, "heatbot": {Available: false},
	}}
	states := monitorStates(snapshot)
	if states["|web"] == "" || states["|heatbot"] == "" || states["|http"] != "" || states["|database"] != "" {
		t.Fatal(states)
	}
	changes := monitorChanges(map[string]string{"|simon": "simon_unavailable"}, states, now)
	for _, change := range changes {
		if change.Key == "|simon" {
			t.Fatal("retirement recorded as recovery")
		}
	}
	raw, _ := json.Marshal(snapshot)
	for _, forbidden := range []string{`"environment"`, `"cpu_count"`, `"cpu_usage"`, `"scope":"container_visible_system"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("retired environment response", forbidden)
		}
	}
}

func TestPostgresProjectMonitorDatabaseAndIncidents(t *testing.T) {
	db := integrationDB(t)
	sample := sampleDatabaseStats(t.Context(), db.Platform, databaseStats{})
	if !sample.Available || sample.Connections < 1 || sample.SizeBytes < 1 || sample.QueryMS < 0 || sample.SampledAt.IsZero() {
		t.Fatal(sample)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	failed := sampleDatabaseStats(ctx, db.Platform, sample)
	if failed.Available || failed.SampledAt != sample.SampledAt || failed.ErrorCode != "database_stats_unavailable" {
		t.Fatal("failed database observation lost provenance", failed)
	}
	now := time.Now()
	changes := []monitorTransition{
		{Key: "|simon", Reason: "simon_unavailable", State: "opened", At: now},
		{Key: "|web", Reason: "web_monitor_unavailable", State: "opened", At: now},
		{Key: "|heatbot", Reason: "heatbot_monitor_unavailable", State: "opened", At: now},
	}
	if err := flushMonitorTransitions(t.Context(), db.Platform, changes, nil); err != nil {
		t.Fatal(err)
	}
	states, err := loadMonitorStates(t.Context(), db.Platform)
	if err != nil || states["|simon"] != "" || states["|web"] == "" || states["|heatbot"] == "" {
		t.Fatal(states, err)
	}
}
