//go:build linux

package metricsproc

import (
	"strings"
	"testing"
	"time"
)

func TestOwnProcess(t *testing.T) {
	t.Setenv("HOST_PROC", t.TempDir())
	sample, err := (Reader{}).Read(t.Context())
	if err != nil || sample.RSSBytes == 0 || sample.StartedAt.IsZero() || sample.StartedAt.After(time.Now()) || sample.CPUSeconds < 0 {
		t.Fatal("self process read", err, sample)
	}
}

func TestOwnStatParsing(t *testing.T) {
	fields := strings.Fields("S 1 2 3 4 5 6 7 8 9 10 250 50 13 14 15 16 17 18 1000")
	raw := "42 (name ) with spaces) " + strings.Join(fields, " ")
	now := time.Unix(1000, 0)
	cpu, started, err := parseOwnStat([]byte(raw), 42, 100, 20*time.Second, now)
	if err != nil || cpu != 3 || !started.Equal(now.Add(-10*time.Second)) {
		t.Fatal(cpu, started, err)
	}
	for _, broken := range []string{
		"42 missing", "41 (wrong pid) " + strings.Join(fields, " "),
		"42 (short) S", strings.Replace(raw, "250", "-1", 1),
		strings.Replace(raw, "1000", "3000", 1),
	} {
		if _, _, err := parseOwnStat([]byte(broken), 42, 100, 20*time.Second, now); err == nil {
			t.Fatal("accepted invalid stat")
		}
	}
}

func BenchmarkOwnProcess(b *testing.B) {
	for range b.N {
		if _, err := (Reader{}).Read(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
