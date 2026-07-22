package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ---- Claude Code platform ----

type claudeCodePlatform struct{}

func (p *claudeCodePlatform) Name() string             { return "claude-code" }
func (p *claudeCodePlatform) MCPConfigKey() string      { return "mcpServers" }
func (p *claudeCodePlatform) MCPConfigPath(cwd string) string {
	return filepath.Join(cwd, ".mcp.json")
}
func (p *claudeCodePlatform) HooksConfigPath(cwd string) string {
	return filepath.Join(cwd, ".claude", "settings.json")
}
func (p *claudeCodePlatform) RulesFilePath(cwd string) string {
	return filepath.Join(cwd, "CLAUDE.md")
}

func (p *claudeCodePlatform) GetHookEntries() []HookEntry {
	return []HookEntry{
		{Event: "SessionStart", Command: "weknora memory hook session-start"},
		{Event: "PostToolUse", Command: "weknora memory hook post-tool"},
	}
}

// ---- SetupStrategy implementation ----

func (p *claudeCodePlatform) WriteMCPConfig(cwd string, dryRun bool) error {
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

func (p *claudeCodePlatform) WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error {
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

// ---- Legacy helpers (kept for backward compat) ----

// WriteClaudeMCPConfig writes or updates the MCP config file at path with the weknora entry.
func WriteClaudeMCPConfig(path string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), path)
		return nil
	}
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}
	upsertJSONMcpServer(existing, "mcpServers", "weknora")
	fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.writing_mcp"), path)
	return writeJSONFile(path, existing)
}

// WriteClaudeHooksConfig writes hooks config at path with weknora hook entries.
func WriteClaudeHooksConfig(platform Platform, path string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), path)
		return nil
	}
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}
	upsertJSONHooks(existing, platform.GetHookEntries())
	fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.writing_hooks"), path)
	return writeJSONFile(path, existing)
}
