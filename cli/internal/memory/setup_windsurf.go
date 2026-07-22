package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ---- Windsurf platform ----

type windsurfPlatform struct{}

func (p *windsurfPlatform) Name() string             { return "windsurf" }
func (p *windsurfPlatform) MCPConfigKey() string      { return "mcpServers" }

// Windsurf MCP config is user-level (not project-level).
func (p *windsurfPlatform) MCPConfigPath(cwd string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
}

func (p *windsurfPlatform) HooksConfigPath(cwd string) string {
	return filepath.Join(cwd, ".windsurf", "hooks.json")
}

func (p *windsurfPlatform) RulesFilePath(cwd string) string {
	return filepath.Join(cwd, ".windsurfrules")
}

// Windsurf has the ONLY snake_case hook event names (1 event).
func (p *windsurfPlatform) GetHookEntries() []HookEntry {
	return []HookEntry{
		{Event: "post_write_code", Command: "weknora memory hook post-tool"},
	}
}

// ---- SetupStrategy implementation ----

func (p *windsurfPlatform) WriteMCPConfig(cwd string, dryRun bool) error {
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

func (p *windsurfPlatform) WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error {
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
