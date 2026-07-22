package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ---- Gemini CLI platform ----

type geminiPlatform struct{}

func (p *geminiPlatform) Name() string             { return "gemini" }
func (p *geminiPlatform) MCPConfigKey() string      { return "mcpServers" }

func (p *geminiPlatform) MCPConfigPath(cwd string) string {
	return filepath.Join(cwd, ".gemini", "settings.json")
}

// Gemini CLI has no file-based hooks.
func (p *geminiPlatform) HooksConfigPath(cwd string) string {
	return ""
}

// Gemini CLI uses two rules files: GEMINI.md (root) + .agents/rules/weknora.md (agent-level).
func (p *geminiPlatform) RulesFilePath(cwd string) string {
	return filepath.Join(cwd, "GEMINI.md")
}

// AgentRulesFilePath returns the path for the agent-specific rules file.
func (p *geminiPlatform) AgentRulesFilePath(cwd string) string {
	return filepath.Join(cwd, ".agents", "rules", "weknora.md")
}

func (p *geminiPlatform) GetHookEntries() []HookEntry {
	return nil
}

// ---- SetupStrategy implementation ----

func (p *geminiPlatform) WriteMCPConfig(cwd string, dryRun bool) error {
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

func (p *geminiPlatform) WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), "(hooks not supported)")
		return nil
	}
	return ErrHooksNotSupported
}

// ExtraRulesPaths returns additional rules files for Gemini CLI.
func (p *geminiPlatform) ExtraRulesPaths(cwd string) []string {
	return []string{p.AgentRulesFilePath(cwd)}
}
