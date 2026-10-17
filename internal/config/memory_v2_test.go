package config

import (
	"strings"
	"testing"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// decodeMemoryV2YAML decodes a config snippet the same way LoadConfig does.
func decodeMemoryV2YAML(t *testing.T, raw string) *Config {
	t.Helper()
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(raw)); err != nil {
		t.Fatalf("read yaml: %v", err)
	}
	var cfg Config
	cfg.MemoryV2 = newMemoryV2ConfigBase()
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) { dc.TagName = "yaml" }); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return &cfg
}

func TestMemoryV2Config_PartialYAMLKeepsDefaults(t *testing.T) {
	cfg := decodeMemoryV2YAML(t, `
memory_v2:
  max_search_results: 7
  worker_mutations: false
  dreamer:
    interval: 2h
  semantic_dedup:
    near_threshold: 0.9
`)
	m := cfg.MemoryV2
	if m.MaxSearchResults != 7 || m.WorkerMutations || m.Dreamer.Interval != "2h" || m.SemanticDedup.NearThreshold != 0.9 {
		t.Fatalf("snake_case keys not applied: %+v", m)
	}
	if !m.Enabled || m.MinScoreThreshold != 0.4 || m.Dreamer.MaxActions != 5 || !m.Dreamer.Enabled || m.SemanticDedup.ExactThreshold != 0.97 {
		t.Fatalf("unspecified keys lost their defaults: %+v", m)
	}
}

func TestMemoryV2Config_NoBlockUsesDefaults(t *testing.T) {
	cfg := decodeMemoryV2YAML(t, "server:\n  port: 8080\n")
	if cfg.MemoryV2 == nil || !cfg.MemoryV2.Enabled || cfg.MemoryV2.MaxSearchResults != 20 || !cfg.MemoryV2.WorkerMutations || !cfg.MemoryV2.Dreamer.Enabled {
		t.Fatalf("defaults not applied: %+v", cfg.MemoryV2)
	}
}

func TestApplyMemoryV2Defaults_EnvOverrides(t *testing.T) {
	tests := []struct {
		enabled, mutations, dreamer       string
		wantEnabled, wantMut, wantDreamer bool
	}{
		{"", "", "", true, true, true},
		{"false", "", "", false, true, true},
		{"0", "false", "", false, false, true},
		{"garbage", "1", "off", true, true, false},
		{"true", "off", "yes", true, false, true},
	}
	for _, tt := range tests {
		t.Setenv("MEMORY_V2_ENABLED", tt.enabled)
		t.Setenv("MEMORY_V2_WORKER_MUTATIONS", tt.mutations)
		t.Setenv("MEMORY_V2_DREAMER_ENABLED", tt.dreamer)
		var cfg Config
		applyMemoryV2Defaults(&cfg)
		m := cfg.MemoryV2
		if m.Enabled != tt.wantEnabled || m.WorkerMutations != tt.wantMut || m.Dreamer.Enabled != tt.wantDreamer {
			t.Fatalf("env enabled=%q mutations=%q dreamer=%q: got enabled=%v mutations=%v dreamer=%v",
				tt.enabled, tt.mutations, tt.dreamer, m.Enabled, m.WorkerMutations, m.Dreamer.Enabled)
		}
	}
}
