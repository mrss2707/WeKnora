package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ---- Cursor platform ----

type cursorPlatform struct{}

func (p *cursorPlatform) Name() string             { return "cursor" }
func (p *cursorPlatform) MCPConfigKey() string      { return "mcpServers" }

func (p *cursorPlatform) MCPConfigPath(cwd string) string {
	return filepath.Join(cwd, ".cursor", "mcp.json")
}

func (p *cursorPlatform) HooksConfigPath(cwd string) string {
	return filepath.Join(cwd, ".cursor", "hooks.json")
}

func (p *cursorPlatform) RulesFilePath(cwd string) string {
	return filepath.Join(cwd, ".cursorrules")
}

// Cursor uses camelCase hook event names (2 events, no PreCompact).
func (p *cursorPlatform) GetHookEntries() []HookEntry {
	return []HookEntry{
		{Event: "sessionStart", Command: "weknora memory hook session-start"},
		{Event: "afterFileEdit", Command: "weknora memory hook post-tool"},
	}
}

// ---- SetupStrategy implementation ----

func (p *cursorPlatform) WriteMCPConfig(cwd string, dryRun bool) error {
	path := p.MCPConfigPath(cwd)
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), path)
		return nil
	}

	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}

	upsertJSONMcpServer(existing, p.MCPConfigKey(), "weknora")

	fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.writing_mcp"), path)
	return writeJSONFile(path, existing)
}

func (p *cursorPlatform) WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error {
	path := p.HooksConfigPath(cwd)
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), path)
		return nil
	}

	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}

	entries := p.GetHookEntries()
	if len(env) > 0 {
		for i := range entries {
			entries[i].Env = env
		}
	}
	upsertJSONHooks(existing, entries)

	fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.writing_hooks"), path)
	return writeJSONFile(path, existing)
}
