package mcpsetup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDefaultComponentsWritesLegacyFullSetup(t *testing.T) {
	dir := t.TempDir()

	err := Run(dir, []string{"claude-code"}, Options{
		KBIDs:     []string{"kb_abc"},
		ServerURL: "https://kb.example.com",
		APIKey:    "sk-test",
	})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, ".mcp.json"))
	assert.FileExists(t, filepath.Join(dir, ".claude", "settings.json"))
	assert.FileExists(t, filepath.Join(dir, "CLAUDE.md"))

	env := readWeknoraEnv(t, filepath.Join(dir, ".mcp.json"))
	assert.Equal(t, "https://kb.example.com", env["WEKNORA_BASE_URL"])
	assert.Equal(t, "https://kb.example.com", env["WEKNORA_HOST"])
	assert.Equal(t, "sk-test", env["WEKNORA_API_KEY"])
	assert.Equal(t, "kb_abc", env["WEKNORA_KB_ID"])
}

func TestRunMCPOnlyWritesMCPAndEnv(t *testing.T) {
	dir := t.TempDir()

	err := Run(dir, []string{"claude-code"}, Options{
		Components: []Component{ComponentMCP},
		KBIDs:      []string{"kb_abc,kb_def"},
		ServerURL:  "https://kb.example.com",
		APIKey:     "sk-test",
	})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, ".mcp.json"))
	assert.NoFileExists(t, filepath.Join(dir, ".claude", "settings.json"))
	assert.NoFileExists(t, filepath.Join(dir, "CLAUDE.md"))

	env := readWeknoraEnv(t, filepath.Join(dir, ".mcp.json"))
	assert.Equal(t, "https://kb.example.com", env["WEKNORA_BASE_URL"])
	assert.Equal(t, "https://kb.example.com", env["WEKNORA_HOST"])
	assert.Equal(t, "sk-test", env["WEKNORA_API_KEY"])
	assert.Equal(t, "kb_abc", env["WEKNORA_KB_ID"])
}

func TestRunHooksOnlyReadsExistingMCPEnv(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, ".mcp.json")
	writeJSON(t, mcpPath, map[string]any{
		"mcpServers": map[string]any{
			"weknora": map[string]any{
				"command": "weknora",
				"args":    []string{"mcp", "serve"},
				"env": map[string]any{
					"WEKNORA_BASE_URL": "https://existing.example.com",
					"WEKNORA_API_KEY":  "sk-existing",
				},
			},
		},
	})

	err := Run(dir, []string{"claude-code"}, Options{Components: []Component{ComponentMemoryHooks}})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, ".claude", "settings.json"))
	assert.NoFileExists(t, filepath.Join(dir, "CLAUDE.md"))

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "https://existing.example.com")
	assert.Contains(t, string(data), "sk-existing")
}

func TestRunHooksOnlyPrefersExplicitEnv(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, ".mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"weknora": map[string]any{
				"env": map[string]any{"WEKNORA_BASE_URL": "https://existing.example.com"},
			},
		},
	})

	err := Run(dir, []string{"claude-code"}, Options{
		Components: []Component{ComponentMemoryHooks},
		ServerURL:  "https://explicit.example.com",
		APIKey:     "sk-explicit",
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "https://explicit.example.com")
	assert.Contains(t, string(data), "WEKNORA_HOST")
	assert.Contains(t, string(data), "sk-explicit")
	assert.NotContains(t, string(data), "https://existing.example.com")
}

func TestRunRulesOnlyWritesRules(t *testing.T) {
	dir := t.TempDir()

	err := Run(dir, []string{"claude-code"}, Options{
		Components: []Component{ComponentMemoryRules},
		KBIDs:      []string{"kb_abc"},
	})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(dir, ".mcp.json"))
	assert.NoFileExists(t, filepath.Join(dir, ".claude", "settings.json"))
	assert.FileExists(t, filepath.Join(dir, "CLAUDE.md"))
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "WEKNORA_MEMORY_PROTOCOL")
	assert.Contains(t, string(data), "kb_abc")
}

func TestRunDryRunDoesNotWriteFiles(t *testing.T) {
	dir := t.TempDir()

	err := Run(dir, []string{"claude-code"}, Options{DryRun: true})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(dir, ".mcp.json"))
	assert.NoFileExists(t, filepath.Join(dir, ".claude", "settings.json"))
	assert.NoFileExists(t, filepath.Join(dir, "CLAUDE.md"))
}

func TestRunCustomMCPPathOnlyWhenMCPSelected(t *testing.T) {
	dir := t.TempDir()
	mainPy := filepath.Join(dir, "main.py")
	require.NoError(t, os.WriteFile(mainPy, []byte("print('mcp')\n"), 0644))

	err := Run(dir, []string{"claude-code"}, Options{
		Components:    []Component{ComponentMCP},
		McpServerPath: mainPy,
		ServerURL:     "https://kb.example.com",
		APIKey:        "sk-test",
	})
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, ".mcp.json"))
	assert.NoFileExists(t, filepath.Join(dir, ".claude", "settings.json"))
	assert.NoFileExists(t, filepath.Join(dir, "CLAUDE.md"))
	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), mainPy)
	assert.Contains(t, string(data), "WEKNORA_BASE_URL")
}

func TestRunCustomMCPPathIgnoredWithoutMCPComponent(t *testing.T) {
	dir := t.TempDir()
	mainPy := filepath.Join(dir, "main.py")
	require.NoError(t, os.WriteFile(mainPy, []byte("print('mcp')\n"), 0644))

	err := Run(dir, []string{"claude-code"}, Options{
		Components:    []Component{ComponentMemoryRules},
		McpServerPath: mainPy,
	})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(dir, ".mcp.json"))
	assert.FileExists(t, filepath.Join(dir, "CLAUDE.md"))
}

func TestRunUnsupportedHooksWarnsButSucceeds(t *testing.T) {
	dir := t.TempDir()

	err := Run(dir, []string{"cline"}, Options{Components: []Component{ComponentMemoryHooks}})
	require.NoError(t, err)
}

func TestRunFallsBackToEnvironment(t *testing.T) {
	t.Setenv("WEKNORA_KB_ID", "kb_env_a,kb_env_b")
	t.Setenv("WEKNORA_BASE_URL", "https://env.example.com")
	t.Setenv("WEKNORA_API_KEY", "sk-env")
	dir := t.TempDir()

	err := Run(dir, []string{"claude-code"}, Options{Components: []Component{ComponentMCP}})
	require.NoError(t, err)

	env := readWeknoraEnv(t, filepath.Join(dir, ".mcp.json"))
	assert.Equal(t, "https://env.example.com", env["WEKNORA_BASE_URL"])
	assert.Equal(t, "https://env.example.com", env["WEKNORA_HOST"])
	assert.Equal(t, "sk-env", env["WEKNORA_API_KEY"])
	assert.Equal(t, "kb_env_a", env["WEKNORA_KB_ID"])
}

func TestRunFallsBackToWeknoraHost(t *testing.T) {
	t.Setenv("WEKNORA_HOST", "https://host.example.com")
	t.Setenv("WEKNORA_API_KEY", "sk-host")
	dir := t.TempDir()

	err := Run(dir, []string{"claude-code"}, Options{Components: []Component{ComponentMCP}})
	require.NoError(t, err)

	env := readWeknoraEnv(t, filepath.Join(dir, ".mcp.json"))
	assert.Equal(t, "https://host.example.com", env["WEKNORA_BASE_URL"])
	assert.Equal(t, "https://host.example.com", env["WEKNORA_HOST"])
	assert.Equal(t, "sk-host", env["WEKNORA_API_KEY"])
}

func TestNormalizeComponents(t *testing.T) {
	assert.Equal(t, DefaultComponents(), NormalizeComponents(nil))
	assert.Equal(t,
		[]Component{ComponentMemoryHooks, ComponentMCP},
		NormalizeComponents([]Component{ComponentMemoryHooks, ComponentMCP, ComponentMemoryHooks}),
	)
}

func TestFlattenKBIDs(t *testing.T) {
	assert.Equal(t, []string{"kb_abc", "kb_def", "kb_ghi"}, FlattenKBIDs([]string{" kb_abc,kb_def ", "kb_abc", "kb_ghi"}))
	assert.Nil(t, FlattenKBIDs(nil))
}

func TestSplitCommaSeparated(t *testing.T) {
	assert.Equal(t, []string{"kb_abc", "kb_def"}, SplitCommaSeparated(" kb_abc , kb_def "))
	assert.Nil(t, SplitCommaSeparated(""))
}

func readWeknoraEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var cfg struct {
		MCPServers map[string]struct {
			Env map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	return cfg.MCPServers["weknora"].Env
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	data, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, '\n'), 0644))
}
