package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ---- PaiCode platform ----

type paiCodePlatform struct{}

func (p *paiCodePlatform) Name() string             { return "paicode" }
func (p *paiCodePlatform) MCPConfigKey() string      { return "mcpServers" }

// MCPConfigPath returns the project-scoped .mcp.json path.
// PaiCode reads MCP servers from .mcp.json (project scope), same as Claude Code.
func (p *paiCodePlatform) MCPConfigPath(cwd string) string {
	return filepath.Join(cwd, ".mcp.json")
}

// HooksConfigPath returns the project-scoped settings path.
// PaiCode reads hooks from .paicode/settings.json (project scope).
func (p *paiCodePlatform) HooksConfigPath(cwd string) string {
	return filepath.Join(cwd, ".paicode", "settings.json")
}

func (p *paiCodePlatform) RulesFilePath(cwd string) string {
	return filepath.Join(cwd, "AGENTS.md")
}

// PaiCode uses same PascalCase event names as Claude Code.
func (p *paiCodePlatform) GetHookEntries() []HookEntry {
	return []HookEntry{
		{Event: "SessionStart", Command: "weknora memory hook session-start"},
		{Event: "PostToolUse", Command: "weknora memory hook post-tool"},
	}
}

// ---- SetupStrategy implementation ----

// WriteMCPConfig writes the weknora MCP server entry into the project's .mcp.json.
// PaiCode reads MCP servers from .mcp.json (same file and format as Claude Code).
func (p *paiCodePlatform) WriteMCPConfig(cwd string, dryRun bool) error {
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

// WriteHooksConfig writes weknora hooks into the project's .paicode/settings.json.
// PaiCode uses a nested matcher format: hooks.EventName: [{hooks: [{type: "command", command: "..."}]}].
func (p *paiCodePlatform) WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error {
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
	upsertPaiCodeHooks(existing, entries)

	fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.writing_hooks"), path)
	return writeJSONFile(path, existing)
}

// ---- Legacy helper (backward compat) ----

// WritePaiCodeConfig merges weknora MCP and hook config into PaiCode project files.
// Deprecated: use paiCodePlatform.WriteMCPConfig + WriteHooksConfig instead.
func WritePaiCodeConfig(platform Platform, cwd, path string, dryRun bool) error {
	p := &paiCodePlatform{}
	if err := p.WriteMCPConfig(cwd, dryRun); err != nil {
		return err
	}
	return p.WriteHooksConfig(cwd, nil, dryRun)
}
