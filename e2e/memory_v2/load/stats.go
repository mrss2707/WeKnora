package load

import (
	"encoding/json"
	"math"
	"sort"
	"sync"
	"time"
)

// Sample is one completed request. Expected policy responses (401/403/404/409/429)
// are classified before measurement and are never reclassified afterwards.
type Sample struct {
	Op       string        `json:"op"`
	Duration time.Duration `json:"duration_ns"`
	Status   int           `json:"status"`
	Expected bool          `json:"expected"`
	Failed   bool          `json:"failed"`
	// Err is a bounded, content-free class of a failure (never request/response text).
	Err string `json:"err,omitempty"`
}

type Recorder struct {
	mu      sync.Mutex
	samples []Sample
}

func (r *Recorder) Add(s Sample) {
	r.mu.Lock()
	r.samples = append(r.samples, s)
	r.mu.Unlock()
}

func (r *Recorder) Snapshot() []Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Sample(nil), r.samples...)
}

type Summary struct {
	Op             string  `json:"op"`
	Count          int     `json:"count"`
	Failures       int     `json:"failures"`
	ExpectedPolicy int     `json:"expected_policy"`
	P50Ms          float64 `json:"p50_ms"`
	P95Ms          float64 `json:"p95_ms"`
	P99Ms          float64 `json:"p99_ms"`
	FailureRate    float64 `json:"failure_rate"`
}

// Percentile uses nearest-rank on a sorted copy; empty input yields NaN so a
// missing measurement can never satisfy a threshold.
func Percentile(durations []time.Duration, p float64) float64 {
	if len(durations) == 0 {
		return math.NaN()
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	rank = max(0, min(rank, len(sorted)-1))
	return float64(sorted[rank]) / float64(time.Millisecond)
}

// Summarize groups by operation. Expected policy responses are excluded from
// latency percentiles and failure rate and reported separately.
func Summarize(samples []Sample) map[string]Summary {
	byOp := map[string][]Sample{}
	for _, s := range samples {
		byOp[s.Op] = append(byOp[s.Op], s)
	}
	out := map[string]Summary{}
	for op, group := range byOp {
		var durations []time.Duration
		summary := Summary{Op: op}
		for _, s := range group {
			switch {
			case s.Expected:
				summary.ExpectedPolicy++
			case s.Failed:
				summary.Failures++
				summary.Count++
			default:
				summary.Count++
				durations = append(durations, s.Duration)
			}
		}
		summary.P50Ms = Percentile(durations, 50)
		summary.P95Ms = Percentile(durations, 95)
		summary.P99Ms = Percentile(durations, 99)
		if summary.Count > 0 {
			summary.FailureRate = float64(summary.Failures) / float64(summary.Count)
		}
		out[op] = summary
	}
	return out
}

// Median of a window; used to compare steady-state resource trends.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

// MarshalJSON writes an unmeasured percentile as null: encoding/json rejects NaN,
// and a silently empty report must never look like a pass.
func (s Summary) MarshalJSON() ([]byte, error) {
	num := func(v float64) *float64 {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return &v
	}
	return json.Marshal(struct {
		Op             string   `json:"op"`
		Count          int      `json:"count"`
		Failures       int      `json:"failures"`
		ExpectedPolicy int      `json:"expected_policy"`
		P50Ms          *float64 `json:"p50_ms"`
		P95Ms          *float64 `json:"p95_ms"`
		P99Ms          *float64 `json:"p99_ms"`
		FailureRate    float64  `json:"failure_rate"`
	}{s.Op, s.Count, s.Failures, s.ExpectedPolicy, num(s.P50Ms), num(s.P95Ms), num(s.P99Ms), s.FailureRate})
}
