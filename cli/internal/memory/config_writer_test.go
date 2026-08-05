package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertJSONMcpServer_Added(t *testing.T) {
	existing := make(map[string]any)
	result := upsertJSONMcpServer(existing, "mcpServers", "weknora")
	assert.Equal(t, UpsertAdded, result)

	servers, ok := existing["mcpServers"].(map[string]any)
	require.True(t, ok)
	srv, ok := servers["weknora"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "stdio", srv["type"])
	assert.Equal(t, "weknora", srv["command"])
}

func TestUpsertJSONMcpServer_Updated(t *testing.T) {
	existing := map[string]any{
		"mcpServers": map[string]any{
			"weknora": map[string]any{"command": "old-cmd"},
		},
	}
	result := upsertJSONMcpServer(existing, "mcpServers", "weknora")
	assert.Equal(t, UpsertUpdated, result)
	// Verify entry was overwritten with correct command
	srv := existing["mcpServers"].(map[string]any)["weknora"].(map[string]any)
	assert.Equal(t, "stdio", srv["type"])
	assert.Equal(t, "weknora", srv["command"])
}

func TestUpsertJSONMcpServer_PreservesExisting(t *testing.T) {
	existing := map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "other-tool"},
		},
	}
	result := upsertJSONMcpServer(existing, "mcpServers", "weknora")
	assert.Equal(t, UpsertAdded, result)

	servers := existing["mcpServers"].(map[string]any)
	assert.Len(t, servers, 2)
	assert.Contains(t, servers, "other")
	assert.Contains(t, servers, "weknora")
}

func TestUpsertJSONMcpServer_CopilotServersKey(t *testing.T) {
	existing := make(map[string]any)
	result := upsertJSONMcpServer(existing, "servers", "weknora")
	assert.Equal(t, UpsertAdded, result)

	servers, ok := existing["servers"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, servers, "weknora")
}

func TestUpsertJSONHooks_Added(t *testing.T) {
	existing := make(map[string]any)
	entries := []HookEntry{
		{Event: "SessionStart", Command: "weknora memory hook session-start"},
	}
	result := upsertJSONHooks(existing, entries)
	assert.Equal(t, UpsertAdded, result)

	hooks, ok := existing["hooks"].([]any)
	require.True(t, ok)
	assert.Len(t, hooks, 1)
}

func TestUpsertJSONHooks_Updated(t *testing.T) {
	existing := map[string]any{
		"hooks": []any{
			map[string]any{"event": "SessionStart", "command": "weknora memory hook session-start"},
		},
	}
	entries := []HookEntry{
		{Event: "SessionStart", Command: "weknora memory hook session-start"},
	}
	result := upsertJSONHooks(existing, entries)
	assert.Equal(t, UpsertUpdated, result)
}

func TestUpsertJSONHooks_PreservesOtherHooks(t *testing.T) {
	existing := map[string]any{
		"hooks": []any{
			map[string]any{"event": "OtherEvent", "command": "echo hello"},
		},
	}
	entries := []HookEntry{
		{Event: "SessionStart", Command: "weknora memory hook session-start"},
	}
	result := upsertJSONHooks(existing, entries)
	assert.Equal(t, UpsertAdded, result)

	hooks := existing["hooks"].([]any)
	assert.Len(t, hooks, 2)
}

func TestUpsertPaiCodeHooks_Added(t *testing.T) {
	existing := make(map[string]any)
	entries := []HookEntry{
		{Event: "SessionStart", Command: "weknora memory hook session-start"},
	}
	result := upsertPaiCodeHooks(existing, entries)
	assert.Equal(t, UpsertAdded, result)

	hooks, ok := existing["hooks"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, hooks, "SessionStart")

	// Verify nested structure
	arr, ok := hooks["SessionStart"].([]any)
	require.True(t, ok)
	require.Len(t, arr, 1)

	matcher, ok := arr[0].(map[string]any)
	require.True(t, ok)
	innerHooks, ok := matcher["hooks"].([]any)
	require.True(t, ok)
	require.Len(t, innerHooks, 1)

	cmd, ok := innerHooks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "command", cmd["type"])
	assert.Equal(t, "weknora memory hook session-start", cmd["command"])
}

func TestUpsertPaiCodeHooks_Updated(t *testing.T) {
	existing := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "weknora memory hook session-start",
						},
					},
				},
			},
		},
	}
	entries := []HookEntry{
		{Event: "SessionStart", Command: "weknora memory hook session-start"},
	}
	result := upsertPaiCodeHooks(existing, entries)
	assert.Equal(t, UpsertUpdated, result)
}

func TestUpsertPaiCodeHooks_WithEnv(t *testing.T) {
	existing := make(map[string]any)
	entries := []HookEntry{
		{
			Event:   "SessionStart",
			Command: "weknora memory hook session-start",
			Env: map[string]string{
				"WEKNORA_BASE_URL": "https://example.com/api/v1",
				"WEKNORA_API_KEY":  "sk-test",
			},
		},
	}
	result := upsertPaiCodeHooks(existing, entries)
	assert.Equal(t, UpsertAdded, result)

	hooks, ok := existing["hooks"].(map[string]any)
	require.True(t, ok)

	arr, ok := hooks["SessionStart"].([]any)
	require.True(t, ok)
	require.Len(t, arr, 1)

	matcher, ok := arr[0].(map[string]any)
	require.True(t, ok)
	innerHooks, ok := matcher["hooks"].([]any)
	require.True(t, ok)
	require.Len(t, innerHooks, 1)

	cmd, ok := innerHooks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "command", cmd["type"])
	assert.Equal(t, `env WEKNORA_API_KEY="sk-test" WEKNORA_BASE_URL="https://example.com/api/v1" weknora memory hook session-start`, cmd["command"])
	assert.NotContains(t, cmd, "env")
}

func TestUpsertJSONHooks_WithEnv(t *testing.T) {
	existing := make(map[string]any)
	entries := []HookEntry{
		{
			Event:   "SessionStart",
			Command: "weknora memory hook session-start",
			Env: map[string]string{
				"WEKNORA_BASE_URL": "https://example.com/api/v1",
			},
		},
	}
	result := upsertJSONHooks(existing, entries)
	assert.Equal(t, UpsertAdded, result)

	hooks := existing["hooks"].([]any)
	require.Len(t, hooks, 1)

	hook := hooks[0].(map[string]any)
	assert.Equal(t, "SessionStart", hook["event"])
	assert.Equal(t, "weknora memory hook session-start", hook["command"])

	env, ok := hook["env"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/api/v1", env["WEKNORA_BASE_URL"])
}

func TestWriteRulesFile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// First write
	err := WriteRulesFile(path, []string{"kb_test"}, false)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "WEKNORA_MEMORY_PROTOCOL")

	// Second write should be idempotent (no change)
	firstContent := string(data)
	err = WriteRulesFile(path, []string{"kb_test"}, false)
	require.NoError(t, err)
	data2, _ := os.ReadFile(path)
	assert.Equal(t, firstContent, string(data2))
}

func TestWriteRulesFile_DryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	err := WriteRulesFile(path, []string{"kb_test"}, true)
	require.NoError(t, err)

	// Dry run should not create file
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestWriteJSONFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	err := writeJSONFile(path, map[string]any{"key": "value"})
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)
	assert.Equal(t, "value", result["key"])
}

func TestWriteJSONFile_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "test.json")

	err := writeJSONFile(path, map[string]any{"key": "value"})
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err)
}

// ---- UpsertMcpEnv tests ----

func TestUpsertMcpEnv_AddsEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	initial := `{"mcpServers":{"weknora":{"type":"stdio","command":"weknora","args":["mcp","serve"]}}}`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0644))

	err := UpsertMcpEnv(path, "weknora", map[string]string{
		"WEKNORA_BASE_URL": "https://example.com/api/v1",
		"WEKNORA_API_KEY":  "sk-test",
	})
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	servers := result["mcpServers"].(map[string]any)
	srv := servers["weknora"].(map[string]any)
	assert.Equal(t, "stdio", srv["type"])
	assert.Equal(t, "weknora", srv["command"])
	env := srv["env"].(map[string]any)
	assert.Equal(t, "https://example.com/api/v1", env["WEKNORA_BASE_URL"])
	assert.Equal(t, "sk-test", env["WEKNORA_API_KEY"])
}

func TestUpsertMcpEnv_EmptyEnvNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	initial := `{"mcpServers":{"weknora":{"type":"stdio","command":"weknora","args":["mcp","serve"]}}}`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0644))

	err := UpsertMcpEnv(path, "weknora", nil)
	require.NoError(t, err)

	data, _ := os.ReadFile(path)
	assert.Equal(t, initial, string(data))
}

func TestUpsertMcpEnv_NoEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	initial := `{"mcpServers":{"other":{"command":"echo"}}}`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0644))

	// Should be no-op, no error
	err := UpsertMcpEnv(path, "weknora", map[string]string{"KEY": "val"})
	require.NoError(t, err)

	data, _ := os.ReadFile(path)
	assert.Equal(t, initial, string(data))
}

// ---- WriteCustomMcpServer tests ----

func TestWriteCustomMcpServer_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")

	err := WriteCustomMcpServer(path, "/venv/bin/python", []string{"/path/to/main.py"}, nil, false)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	servers := result["mcpServers"].(map[string]any)
	srv := servers["weknora"].(map[string]any)
	assert.Equal(t, "/venv/bin/python", srv["command"])
	args := srv["args"].([]any)
	assert.Equal(t, "/path/to/main.py", args[0])
	_, hasEnv := srv["env"]
	assert.False(t, hasEnv, "env should be omitted when empty")
}

func TestWriteCustomMcpServer_WithEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")

	err := WriteCustomMcpServer(path, "python3", []string{"/app/main.py"}, map[string]string{
		"WEKNORA_BASE_URL": "https://know.supermeo.com/api/v1",
		"WEKNORA_API_KEY":  "sk-test",
	}, false)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	srv := result["mcpServers"].(map[string]any)["weknora"].(map[string]any)
	env := srv["env"].(map[string]any)
	assert.Equal(t, "https://know.supermeo.com/api/v1", env["WEKNORA_BASE_URL"])
	assert.Equal(t, "sk-test", env["WEKNORA_API_KEY"])
}

func TestWriteCustomMcpServer_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")

	// First write
	err := WriteCustomMcpServer(path, "python3", []string{"/app/main.py"}, nil, false)
	require.NoError(t, err)

	// Second write — should overwrite with new data
	err = WriteCustomMcpServer(path, "python3", []string{"/other/main.py"}, nil, false)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	srv := result["mcpServers"].(map[string]any)["weknora"].(map[string]any)
	// Should have the NEW args (overwritten on re-setup)
	args := srv["args"].([]any)
	assert.Equal(t, "/other/main.py", args[0])
}

func TestWriteCustomMcpServer_DryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")

	err := WriteCustomMcpServer(path, "python3", []string{"/app/main.py"}, nil, true)
	require.NoError(t, err)

	// Dry run should not create file
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

// ---- upsertJSONMcpServer env preservation ----

func TestUpsertJSONMcpServer_PreservesEnv(t *testing.T) {
	// Simulate an existing MCP entry with env vars from a previous setup.
	existing := map[string]any{
		"mcpServers": map[string]any{
			"weknora": map[string]any{
				"type":    "stdio",
				"command": "weknora",
				"args":    []any{"mcp", "serve"},
				"env": map[string]any{
					"WEKNORA_BASE_URL": "https://example.com/api/v1",
					"WEKNORA_API_KEY":  "sk-old",
				},
			},
		},
	}
	result := upsertJSONMcpServer(existing, "mcpServers", "weknora")
	assert.Equal(t, UpsertUpdated, result)

	servers := existing["mcpServers"].(map[string]any)
	srv := servers["weknora"].(map[string]any)
	assert.Equal(t, "stdio", srv["type"])
	assert.Equal(t, "weknora", srv["command"])

	// Env should be preserved from the old entry.
	env := srv["env"].(map[string]any)
	assert.Equal(t, "https://example.com/api/v1", env["WEKNORA_BASE_URL"])
	assert.Equal(t, "sk-old", env["WEKNORA_API_KEY"])
}

func TestUpsertJSONMcpServer_NoEnvWhenNew(t *testing.T) {
	// Brand new entry should NOT have an env field.
	existing := make(map[string]any)
	result := upsertJSONMcpServer(existing, "mcpServers", "weknora")
	assert.Equal(t, UpsertAdded, result)

	servers := existing["mcpServers"].(map[string]any)
	srv := servers["weknora"].(map[string]any)
	_, hasEnv := srv["env"]
	assert.False(t, hasEnv, "new entry should not have env")
}

// ---- ReadMcpEnv tests ----

func TestReadMcpEnv_ReadsEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	initial := `{"mcpServers":{"weknora":{"type":"stdio","command":"weknora","args":["mcp","serve"],"env":{"WEKNORA_BASE_URL":"https://example.com/api/v1","WEKNORA_API_KEY":"sk-test"}}}}`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0644))

	env := ReadMcpEnv(path, "weknora")
	require.NotNil(t, env)
	assert.Equal(t, "https://example.com/api/v1", env["WEKNORA_BASE_URL"])
	assert.Equal(t, "sk-test", env["WEKNORA_API_KEY"])
}

func TestReadMcpEnv_NoEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	initial := `{"mcpServers":{}}`
	require.NoError(t, os.WriteFile(path, []byte(initial), 0644))

	env := ReadMcpEnv(path, "weknora")
	assert.Nil(t, env)
}

func TestReadMcpEnv_NoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")

	env := ReadMcpEnv(path, "weknora")
	assert.Nil(t, env)
}
