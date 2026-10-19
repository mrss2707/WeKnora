package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeMetricsAreBoundedAndLabelFree(t *testing.T) {
	families, err := Registry.Gather()
	require.NoError(t, err)
	wanted := map[string]bool{
		"weknora_memory_runtime_goroutines":            false,
		"weknora_memory_runtime_heap_allocated_bytes":  false,
		"weknora_memory_runtime_heap_inuse_bytes":      false,
		"weknora_memory_runtime_allocated_bytes_total": false,
	}
	for _, family := range families {
		if _, ok := wanted[family.GetName()]; !ok {
			continue
		}
		require.Len(t, family.Metric, 1)
		require.Empty(t, family.Metric[0].Label)
		wanted[family.GetName()] = true
	}
	for name, found := range wanted {
		require.True(t, found, name)
	}
}
