package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSelfResourcesUnavailableAndCounterReset(t *testing.T) {
	root := t.TempDir()
	r := &ResourceReader{root: root}
	first := r.Read()
	if first.MemoryBytes != nil || first.CPUQuota != nil || first.CPUPercent != nil || first.Goroutines == 0 {
		t.Fatal("missing cgroup was fabricated")
	}
	put := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("memory.current", "100")
	put("memory.max", "max")
	put("cpu.max", "50000 100000")
	put("cpu.stat", "usage_usec 100\nthrottled_usec 9")
	second := r.Read()
	if second.MemoryBytes == nil || *second.MemoryBytes != 100 || second.MemoryLimit != nil || second.CPUQuota == nil || *second.CPUQuota != 0.5 || second.CPUPercent != nil {
		t.Fatal("invalid cgroup projection")
	}
	time.Sleep(time.Millisecond)
	put("cpu.stat", "usage_usec 200")
	if r.Read().CPUPercent == nil {
		t.Fatal("missing CPU delta")
	}
	put("cpu.stat", "usage_usec 1")
	if r.Read().CPUPercent != nil {
		t.Fatal("counter reset became a usage spike")
	}
}

func TestSnapshotNeverCollects(t *testing.T) {
	c := NewSampler("api_self", nil)
	now := time.Now().UTC()
	c.last = Resources{Available: true, SampledAt: now.Add(-16 * time.Second)}
	start := time.Now()
	if !c.Snapshot().Stale {
		t.Fatal("old sample claimed fresh")
	}
	if time.Since(start) > time.Second {
		t.Fatal("snapshot read waited for network")
	}
}

func TestCgroupV1AndUnlimited(t *testing.T) {
	root := t.TempDir()
	put := func(name, value string) {
		t.Helper()
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("memory/memory.usage_in_bytes", "1024")
	put("memory/memory.limit_in_bytes", "9223372036854771712")
	put("cpuacct/cpuacct.usage", "200000000")
	put("cpu/cpu.cfs_quota_us", "100000")
	put("cpu/cpu.cfs_period_us", "100000")
	r := &ResourceReader{root: root}
	first := r.Read()
	if first.MemoryBytes == nil || *first.MemoryBytes != 1024 || first.MemoryLimit != nil ||
		first.CPUQuota == nil || *first.CPUQuota != 1 || first.CPUPercent != nil {
		t.Fatal(first)
	}
	put("cpuacct/cpuacct.usage", "300000000")
	if r.Read().CPUPercent == nil {
		t.Fatal("v1 delta missing")
	}
	put("cpu/cpu.cfs_quota_us", "-1")
	if r.Read().CPUPercent != nil {
		t.Fatal("unlimited quota reported utilization")
	}
}
