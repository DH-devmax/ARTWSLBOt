package monitor

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Resources struct {
	Available         bool       `json:"available"`
	Stale             bool       `json:"stale"`
	ErrorCode         string     `json:"error_code,omitempty"`
	ProcessRSS        *uint64    `json:"process_rss_bytes"`
	ProcessCPUSeconds *float64   `json:"process_cpu_seconds"`
	ProcessStartedAt  *time.Time `json:"process_started_at"`
	CollectionMS      float64    `json:"collection_ms"`
	CollectionCount   uint64     `json:"collection_count"`
	Source            string     `json:"source"`
	SampledAt         time.Time  `json:"sampled_at"`
	MemoryBytes       *uint64    `json:"memory_bytes"`
	MemoryLimit       *uint64    `json:"memory_limit_bytes"`
	CPUQuota          *float64   `json:"cpu_quota_cores"`
	CPUPercent        *float64   `json:"cpu_quota_percent"`
	ThrottledUsec     *uint64    `json:"cpu_throttled_usec"`
	OOMKills          *uint64    `json:"oom_kills"`
	HeapBytes         uint64     `json:"go_heap_bytes"`
	Goroutines        int        `json:"go_goroutines"`
	GCCycles          uint32     `json:"go_gc_cycles"`
	GCPauseTotalNS    uint64     `json:"go_gc_pause_total_ns"`
}

// ResourceReader reads only the current container's cgroup, never host mounts.
type ResourceReader struct {
	mu      sync.Mutex
	root    string
	lastAt  time.Time
	lastCPU uint64
}

func NewResourceReader() *ResourceReader { return &ResourceReader{root: "/sys/fs/cgroup"} }

func (r *ResourceReader) Read() Resources {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out := Resources{Source: "api_self", SampledAt: now, HeapBytes: mem.HeapAlloc,
		Goroutines: runtime.NumGoroutine(), GCCycles: mem.NumGC, GCPauseTotalNS: mem.PauseTotalNs}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(r.root, name))
		return strings.TrimSpace(string(b))
	}
	out.MemoryBytes = unsigned(read("memory.current"))
	out.MemoryLimit = unsigned(read("memory.max"))
	cpu := keyValues(read("cpu.stat"))
	usage := cpu["usage_usec"]
	out.ThrottledUsec = cpu["throttled_usec"]
	out.OOMKills = keyValues(read("memory.events"))["oom_kill"]
	parts := strings.Fields(read("cpu.max"))
	if len(parts) == 2 {
		quota, qerr := strconv.ParseFloat(parts[0], 64)
		period, perr := strconv.ParseFloat(parts[1], 64)
		if qerr == nil && perr == nil && quota > 0 && period > 0 {
			value := quota / period
			out.CPUQuota = &value
		}
	}
	// cgroup v1 is optional; unavailable/unlimited values remain null.
	if out.MemoryBytes == nil {
		out.MemoryBytes = unsigned(read("memory/memory.usage_in_bytes"))
		out.MemoryLimit = unsigned(read("memory/memory.limit_in_bytes"))
		if out.MemoryLimit != nil && *out.MemoryLimit > 1<<60 {
			out.MemoryLimit = nil
		}
		ns := unsigned(read("cpuacct/cpuacct.usage"))
		if ns != nil {
			value := *ns / 1000
			usage = &value
		}
		q, _ := strconv.ParseFloat(read("cpu/cpu.cfs_quota_us"), 64)
		p, _ := strconv.ParseFloat(read("cpu/cpu.cfs_period_us"), 64)
		if q > 0 && p > 0 {
			value := q / p
			out.CPUQuota = &value
		}
	}
	if usage != nil {
		if !r.lastAt.IsZero() && *usage >= r.lastCPU && out.CPUQuota != nil {
			value := float64(*usage-r.lastCPU) / (now.Sub(r.lastAt).Seconds() * 1e6) / *out.CPUQuota * 100
			out.CPUPercent = &value
		}
		r.lastAt, r.lastCPU = now, *usage
	} else {
		r.lastAt = time.Time{}
	}
	return out
}

func unsigned(raw string) *uint64 {
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

func keyValues(raw string) map[string]*uint64 {
	out := map[string]*uint64{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			out[fields[0]] = unsigned(fields[1])
		}
	}
	return out
}
