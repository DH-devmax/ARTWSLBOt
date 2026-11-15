package httpapi

import (
	"math"
	"slices"
	"time"
)

const performanceSampleLimit = 1000
const performanceWindowSeconds = 24 * 60 * 60

type decodePoint struct {
	ID int64     `json:"id"`
	At time.Time `json:"at"`
	MS float64   `json:"ms"`
}

type latencyDistribution struct {
	Available     bool       `json:"available"`
	Samples       int        `json:"samples"`
	SampleLimit   int        `json:"sample_limit"`
	WindowSeconds int        `json:"window_seconds"`
	From          *time.Time `json:"from"`
	To            *time.Time `json:"to"`
	SampledAt     time.Time  `json:"sampled_at"`
	P50           *float64   `json:"p50_ms"`
	P95           *float64   `json:"p95_ms"`
	P99           *float64   `json:"p99_ms"`
	Histogram     []int      `json:"histogram"`
}

// Merge the latest raw observations, never account percentiles or padded samples.
func decodeDistribution(points []decodePoint, at time.Time, available bool) latencyDistribution {
	out := latencyDistribution{Available: available, SampleLimit: performanceSampleLimit,
		WindowSeconds: performanceWindowSeconds, SampledAt: at, Histogram: make([]int, 6)}
	valid := make([]decodePoint, 0, len(points))
	for _, point := range points {
		if point.MS >= 0 && !math.IsNaN(point.MS) && !math.IsInf(point.MS, 0) &&
			!point.At.After(at) && point.At.After(at.Add(-24*time.Hour)) {
			valid = append(valid, point)
		}
	}
	slices.SortFunc(valid, func(a, b decodePoint) int {
		if order := b.At.Compare(a.At); order != 0 {
			return order
		}
		if a.ID > b.ID {
			return -1
		}
		if a.ID < b.ID {
			return 1
		}
		return 0
	})
	valid = valid[:min(len(valid), performanceSampleLimit)]
	out.Samples = len(valid)
	if len(valid) == 0 {
		return out
	}
	from, to := valid[len(valid)-1].At, valid[0].At
	out.From, out.To = &from, &to
	values := make([]float64, len(valid))
	for i, point := range valid {
		values[i] = point.MS
		bucket := 5
		for j, upper := range []float64{0.5, 1, 2, 5, 10} {
			if point.MS <= upper {
				bucket = j
				break
			}
		}
		out.Histogram[bucket]++
	}
	slices.Sort(values)
	quantile := func(q float64) *float64 {
		index := q * float64(len(values)-1)
		lower := int(index)
		value := values[lower] + (values[int(math.Ceil(index))]-values[lower])*(index-float64(lower))
		return &value
	}
	out.P50, out.P95, out.P99 = quantile(.5), quantile(.95), quantile(.99)
	return out
}

func (s *Server) workspaceDecodeDistribution(workspace string) latencyDistribution {
	s.monitoring.mu.RLock()
	snapshot := s.monitoring.last
	s.monitoring.mu.RUnlock()
	return monitorDecodeDistribution(snapshot, workspace)
}

func monitorDecodeDistribution(snapshot systemMonitor, workspace string) latencyDistribution {
	points := []decodePoint{}
	for _, account := range snapshot.Accounts {
		if account.Workspace == workspace {
			points = append(points, account.decodePoints...)
		}
	}
	at := snapshot.DatabaseSampledAt
	age := time.Since(at)
	return decodeDistribution(points, at, snapshot.DatabaseAvailable && !at.IsZero() && age >= 0 && age < 45*time.Second)
}
