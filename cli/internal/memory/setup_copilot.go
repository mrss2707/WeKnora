package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ---- VS Code Copilot platform ----

type copilotPlatform struct{}

func (p *copilotPlatform) Name() string             { return "copilot" }
func (p *copilotPlatform) MCPConfigKey() string      { return "servers" } // ⚠️ NOT "mcpServers"

func (p *copilotPlatform) MCPConfigPath(cwd string) string {
	return filepath.Join(cwd, ".vscode", "mcp.json")
}

func (p *copilotPlatform) HooksConfigPath(cwd string) string {
	return filepath.Join(cwd, ".github", "hooks", "weknora.json")
}

func (p *copilotPlatform) RulesFilePath(cwd string) string {
	return filepath.Join(cwd, ".github", "copilot-instructions.md")
}

// Copilot uses camelCase hook events (2 events, no PreCompact).
func (p *copilotPlatform) GetHookEntries() []HookEntry {
	return []HookEntry{
		{Event: "sessionStart", Command: "weknora memory hook session-start"},
		{Event: "postToolUse", Command: "weknora memory hook post-tool"},
	}
}

// ---- SetupStrategy implementation ----

func (p *copilotPlatform) WriteMCPConfig(cwd string, dryRun bool) error {
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

func (p *copilotPlatform) WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error {
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
