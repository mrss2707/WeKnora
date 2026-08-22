package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestApplyCrossSessionMemoryDefaults_DefaultOff(t *testing.T) {
	var cfg Config
	applyCrossSessionMemoryDefaults(&cfg)
	if cfg.CrossSessionMemory {
		t.Fatal("cross_session_memory default = true, want false")
	}
}

func TestApplyCrossSessionMemoryDefaults_EnvOverride(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{"true", true},
		{"1", true},
		{"TRUE", true},
		{" false ", false},
		{"0", false},
		{"garbage", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Setenv("CROSS_SESSION_MEMORY", tt.env)
		var cfg Config
		applyCrossSessionMemoryDefaults(&cfg)
		if cfg.CrossSessionMemory != tt.want {
			t.Fatalf("env %q: got %v, want %v", tt.env, cfg.CrossSessionMemory, tt.want)
		}
	}
}

func TestCrossSessionMemory_YAMLUnmarshal(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("cross_session_memory: true\n"), &cfg); err != nil {
		t.Fatalf("yaml decode: %v", err)
	}
	if !cfg.CrossSessionMemory {
		t.Fatal("yaml cross_session_memory: true not decoded into the flag")
	}
}
