package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryV2DreamerIntervalOverride(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"5s", "5s"}, {"2m", "2m0s"}, {"0s", "1h"}, {"-1s", "1h"}, {"broken", "1h"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("MEMORY_V2_DREAMER_INTERVAL", tc.value)
			cfg := &Config{MemoryV2: newMemoryV2ConfigBase()}
			cfg.MemoryV2.Dreamer.Interval = "1h"
			applyMemoryV2Defaults(cfg)
			require.Equal(t, tc.want, cfg.MemoryV2.Dreamer.Interval)
		})
	}
}
