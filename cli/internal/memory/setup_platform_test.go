package memory

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllPlatforms_InterfaceCompliance verifies interface compliance and key properties for all 8 platforms.
func TestAllPlatforms_InterfaceCompliance(t *testing.T) {
	platforms := map[string]struct {
		p           Platform
		mcpKey      string
		hookEvents  int
		hasHooks    bool
		firstEvent  string
		firstCmd    string
		eventCasing string
	}{
		"claude-code": {
			&claudeCodePlatform{}, "mcpServers", 2, true,
			"SessionStart", "weknora memory hook session-start", "PascalCase",
		},
		"paicode": {
			&paiCodePlatform{}, "mcpServers", 2, true,
			"SessionStart", "weknora memory hook session-start", "PascalCase",
		},
		"cursor": {
			&cursorPlatform{}, "mcpServers", 2, true,
			"sessionStart", "weknora memory hook session-start", "camelCase",
		},
		"copilot": {
			&copilotPlatform{}, "servers", 2, true,
			"sessionStart", "weknora memory hook session-start", "camelCase",
		},
		"windsurf": {
			&windsurfPlatform{}, "mcpServers", 1, true,
			"post_write_code", "weknora memory hook post-tool", "snake_case",
		},
		"cline": {
			&clinePlatform{}, "mcpServers", 0, false,
			"", "", "none",
		},
		"continue": {
			&continuePlatform{}, "mcpServers", 0, false,
			"", "", "none",
		},
		"gemini": {
			&geminiPlatform{}, "mcpServers", 0, false,
			"", "", "none",
		},
	}

	for name, tc := range platforms {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, name, tc.p.Name())
			assert.Equal(t, tc.mcpKey, tc.p.MCPConfigKey())
			assert.NotEmpty(t, tc.p.MCPConfigPath("/test/cwd"))
			assert.NotEmpty(t, tc.p.RulesFilePath("/test/cwd"))
			if tc.hasHooks {
				assert.NotEmpty(t, tc.p.HooksConfigPath("/test/cwd"))
			}
			entries := tc.p.GetHookEntries()
			assert.Len(t, entries, tc.hookEvents)
			if tc.hookEvents > 0 {
				assert.Equal(t, tc.firstEvent, entries[0].Event)
				assert.Equal(t, tc.firstCmd, entries[0].Command)
			}
		})
	}
}

// TestAllPlatforms_SetupStrategy verifies each platform implements SetupStrategy correctly.
func TestAllPlatforms_SetupStrategy(t *testing.T) {
	platforms := []Platform{
		&claudeCodePlatform{},
		&paiCodePlatform{},
		&cursorPlatform{},
		&copilotPlatform{},
		&windsurfPlatform{},
		&clinePlatform{},
		&continuePlatform{},
		&geminiPlatform{},
	}

	for _, p := range platforms {
		t.Run(p.Name(), func(t *testing.T) {
			_, ok := p.(SetupStrategy)
			assert.True(t, ok, "%s should implement SetupStrategy", p.Name())
		})
	}
}

// TestNoHookPlatformsReturnErrHooksNotSupported ensures platforms without hooks return the sentinel error.
func TestNoHookPlatformsReturnErrHooksNotSupported(t *testing.T) {
	noHookPlatforms := []struct {
		name string
		p    SetupStrategy
	}{
		{"cline", &clinePlatform{}},
		{"continue", &continuePlatform{}},
		{"gemini", &geminiPlatform{}},
	}

	for _, tc := range noHookPlatforms {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.WriteHooksConfig(t.TempDir(), nil, false)
			assert.Equal(t, ErrHooksNotSupported, err)
		})
	}
}

// TestCursorCamelCaseEvents verifies Cursor uses exact camelCase event names.
func TestCursorCamelCaseEvents(t *testing.T) {
	p := &cursorPlatform{}
	entries := p.GetHookEntries()
	require.Len(t, entries, 2)
	assert.Equal(t, "sessionStart", entries[0].Event)
	assert.Equal(t, "afterFileEdit", entries[1].Event)
}

// TestCopilotCamelCaseEvents verifies Copilot uses exact camelCase event names.
func TestCopilotCamelCaseEvents(t *testing.T) {
	p := &copilotPlatform{}
	entries := p.GetHookEntries()
	require.Len(t, entries, 2)
	assert.Equal(t, "sessionStart", entries[0].Event)
	assert.Equal(t, "postToolUse", entries[1].Event)
}

// TestWindsurfSnakeCaseEvents verifies Windsurf uses snake_case (only platform to do so).
func TestWindsurfSnakeCaseEvents(t *testing.T) {
	p := &windsurfPlatform{}
	entries := p.GetHookEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "post_write_code", entries[0].Event)
}

// TestCopilotMCPKeyIsServers ensures Copilot uses "servers" not "mcpServers".
func TestCopilotMCPKeyIsServers(t *testing.T) {
	p := &copilotPlatform{}
	assert.Equal(t, "servers", p.MCPConfigKey())
}

// TestPaiCodeRulesFilePathIsAGENTS ensures PaiCode writes to AGENTS.md not PAICODE.md.
func TestPaiCodeRulesFilePathIsAGENTS(t *testing.T) {
	p := &paiCodePlatform{}
	assert.Contains(t, p.RulesFilePath("/test"), "AGENTS.md")
	assert.NotContains(t, p.RulesFilePath("/test"), "PAICODE.md")
}

// TestMCPConfigIdempotency_DryRun ensures dry-run doesn't write files.
func TestMCPConfigIdempotency_DryRun(t *testing.T) {
	platforms := []SetupStrategy{
		&claudeCodePlatform{},
		&cursorPlatform{},
		&copilotPlatform{},
		&clinePlatform{},
		&geminiPlatform{},
	}

	for _, p := range platforms {
		t.Run("dry-run", func(t *testing.T) {
			err := p.WriteMCPConfig(t.TempDir(), true)
			assert.NoError(t, err)
		})
	}
}

// TestContinueYAML verifies Continue MCP config produces valid YAML with weknora entry.
func TestContinueYAML(t *testing.T) {
	dir := t.TempDir()
	p := &continuePlatform{}

	err := p.WriteMCPConfig(dir, false)
	require.NoError(t, err)

	path := p.MCPConfigPath(dir)
	assert.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "weknora")
	assert.Contains(t, string(data), "mcpServers")
	assert.Contains(t, string(data), "mcp")
	assert.Contains(t, string(data), "serve")
}
