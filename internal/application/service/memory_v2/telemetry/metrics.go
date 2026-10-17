// Package telemetry owns bounded, content-free Memory V2 operational metrics.
package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var Registry = prometheus.NewRegistry()
var runs = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "weknora_memory_worker_runs_total", Help: "Memory worker passes by outcome."}, []string{"worker", "outcome"})
var actions = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "weknora_memory_worker_actions_total", Help: "Memory worker actions by outcome."}, []string{"worker", "outcome"})
var tokens = prometheus.NewCounter(prometheus.CounterOpts{Name: "weknora_memory_dreamer_estimated_tokens_total", Help: "Estimated prompt and output tokens used by dreamer calls."})
var latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "weknora_memory_operation_seconds", Help: "Memory operation latency split into database and model phases.", Buckets: prometheus.DefBuckets}, []string{"operation", "phase"})
var lastRun = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "weknora_memory_worker_last_run_timestamp_seconds", Help: "Last completed worker pass, Unix time."}, []string{"worker"})

func init() { Registry.MustRegister(runs, actions, tokens, latency, lastRun) }

func WorkerRun(worker, outcome string, started time.Time) {
	runs.WithLabelValues(worker, outcome).Inc()
	latency.WithLabelValues(worker, "total").Observe(time.Since(started).Seconds())
	if outcome == "completed" {
		lastRun.WithLabelValues(worker).Set(float64(time.Now().Unix()))
	}
}
func WorkerActions(worker, outcome string, count int) {
	if count > 0 {
		actions.WithLabelValues(worker, outcome).Add(float64(count))
	}
}
func DreamTokens(count int) {
	if count > 0 {
		tokens.Add(float64(count))
	}
}
func Observe(operation, phase string, started time.Time) {
	latency.WithLabelValues(operation, phase).Observe(time.Since(started).Seconds())
}
