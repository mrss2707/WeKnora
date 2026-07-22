package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ---- Cline platform ----

type clinePlatform struct{}

func (p *clinePlatform) Name() string             { return "cline" }
func (p *clinePlatform) MCPConfigKey() string      { return "mcpServers" }

// Cline MCP path is OS-dependent (VS Code extension globalStorage).
func (p *clinePlatform) MCPConfigPath(cwd string) string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
	default: // linux and others
		return filepath.Join(home, ".config", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
	}
}

// Cline has no file-based hooks (SDK-based integration only).
func (p *clinePlatform) HooksConfigPath(cwd string) string {
	return ""
}

func (p *clinePlatform) RulesFilePath(cwd string) string {
	return filepath.Join(cwd, ".clinerules")
}

// Cline does not support file-based hooks.
func (p *clinePlatform) GetHookEntries() []HookEntry {
	return nil
}

// ---- SetupStrategy implementation ----

func (p *clinePlatform) WriteMCPConfig(cwd string, dryRun bool) error {
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

func (p *clinePlatform) WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), "(hooks not supported)")
		return nil
	}
	return ErrHooksNotSupported
}
