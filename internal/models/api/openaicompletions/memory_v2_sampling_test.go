package openaicompletions

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/stretchr/testify/require"
)

func TestMemoryV2DeterministicOptionsAreOnTheWire(t *testing.T) {
	client := newClient(t, nil)
	messages := []api.Message{{Role: "user", Content: "Return JSON."}}
	legacy := bodyJSON(t, client, messages, &api.Options{}, false)
	require.NotContains(t, legacy, "temperature", "existing unspecified callers must not change")
	opts := &api.Options{TemperatureSet: true, Format: json.RawMessage(`{"type":"object","required":["actions"]}`)}
	body := bodyJSON(t, client, messages, opts, false)
	require.Equal(t, float64(0), body["temperature"])
	require.Equal(t, map[string]any{"type": "json_object"}, body["response_format"])
	unsupported := newClient(t, func(cfg *Config) { cfg.Settings.SupportsTemperature = false })
	require.NotContains(t, bodyJSON(t, unsupported, messages, opts, false), "temperature")
	fixed := newClient(t, func(cfg *Config) { value := 1.0; cfg.Settings.FixedTemperature = &value })
	require.Equal(t, float64(1), bodyJSON(t, fixed, messages, opts, false)["temperature"])
}
