package telemetry

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

var runtimeGoroutines = prometheus.NewDesc("weknora_memory_runtime_goroutines", "Current process goroutines for bounded operational soak checks.", nil, nil)
var runtimeHeap = prometheus.NewDesc("weknora_memory_runtime_heap_allocated_bytes", "Current allocated Go heap, bytes.", nil, nil)
var runtimeHeapInuse = prometheus.NewDesc("weknora_memory_runtime_heap_inuse_bytes", "Go heap spans in use, bytes.", nil, nil)
var runtimeAllocations = prometheus.NewDesc("weknora_memory_runtime_allocated_bytes_total", "Cumulative Go allocation volume, bytes.", nil, nil)

type runtimeCollector struct{}

func (runtimeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- runtimeGoroutines
	ch <- runtimeHeap
	ch <- runtimeHeapInuse
	ch <- runtimeAllocations
}

func (runtimeCollector) Collect(ch chan<- prometheus.Metric) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	ch <- prometheus.MustNewConstMetric(runtimeGoroutines, prometheus.GaugeValue, float64(runtime.NumGoroutine()))
	ch <- prometheus.MustNewConstMetric(runtimeHeap, prometheus.GaugeValue, float64(stats.HeapAlloc))
	ch <- prometheus.MustNewConstMetric(runtimeHeapInuse, prometheus.GaugeValue, float64(stats.HeapInuse))
	ch <- prometheus.MustNewConstMetric(runtimeAllocations, prometheus.CounterValue, float64(stats.TotalAlloc))
}

func init() { Registry.MustRegister(runtimeCollector{}) }
