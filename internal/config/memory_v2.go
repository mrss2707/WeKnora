package config

import (
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// newMemoryV2ConfigBase returns the Memory V2 defaults that the YAML
// `memory_v2:` block is decoded on top of, so a partial block only overrides
// the keys it names instead of zeroing every other threshold.
func newMemoryV2ConfigBase() *types.MemoryV2Config {
	defaults := types.DefaultMemoryV2Config()
	return &defaults
}

// applyMemoryV2Defaults initializes the Memory V2 config and applies env
// overrides:
//   - MEMORY_V2_ENABLED=false|0 disables the module (default on).
//   - MEMORY_V2_WORKER_MUTATIONS=false|0 is the kill switch for every
//     background worker that modifies stored memories (default on).
//   - MEMORY_V2_DREAMER_ENABLED=false|0 turns the dreamer alone off.
func applyMemoryV2Defaults(cfg *Config) {
	if cfg.MemoryV2 == nil {
		cfg.MemoryV2 = newMemoryV2ConfigBase()
	}
	if v := envFlag("MEMORY_V2_ENABLED"); v != nil && !*v {
		cfg.MemoryV2.Enabled = false
	}
	if v := envFlag("MEMORY_V2_WORKER_MUTATIONS"); v != nil {
		cfg.MemoryV2.WorkerMutations = *v
	}
	if v := envFlag("MEMORY_V2_DREAMER_ENABLED"); v != nil {
		cfg.MemoryV2.Dreamer.Enabled = *v
	}
}

// envFlag parses a boolean env var; nil when unset or unrecognized.
func envFlag(name string) *bool {
	t, f := true, false
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "true", "1", "on", "yes":
		return &t
	case "false", "0", "off", "no":
		return &f
	}
	return nil
}
