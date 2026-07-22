package memory

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ---- Continue platform ----

type continuePlatform struct{}

func (p *continuePlatform) Name() string             { return "continue" }
func (p *continuePlatform) MCPConfigKey() string      { return "mcpServers" }
func (p *continuePlatform) MCPConfigPath(cwd string) string {
	return filepath.Join(cwd, ".continue", "config.yaml")
}
func (p *continuePlatform) HooksConfigPath(cwd string) string {
	return ""
}
func (p *continuePlatform) RulesFilePath(cwd string) string {
	return filepath.Join(cwd, ".continue", "rules", "weknora.md")
}
func (p *continuePlatform) GetHookEntries() []HookEntry {
	return nil
}

// ---- SetupStrategy implementation ----

// WriteMCPConfig writes the weknora MCP server entry into .continue/config.yaml.
// Continue uses YAML format where mcpServers is an array of {name, command, args} objects.
func (p *continuePlatform) WriteMCPConfig(cwd string, dryRun bool) error {
	path := p.MCPConfigPath(cwd)
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), path)
		return nil
	}

	// Read existing YAML into a generic node tree
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		yaml.Unmarshal(data, &existing)
	}

	// Check idempotency: look for weknora in the mcpServers array
	servers, _ := existing["mcpServers"].([]any)
	for _, s := range servers {
		if sm, ok := s.(map[string]any); ok {
			if name, ok := sm["name"].(string); ok && name == "weknora" {
				fmt.Fprintf(os.Stderr, "%s\n", T("setup.idempotent_mcp"))
				return nil
			}
		}
	}

	// Append weknora entry
	servers = append(servers, map[string]any{
		"name":    "weknora",
		"command": "weknora",
		"args":    []string{"mcp", "serve"},
	})
	existing["mcpServers"] = servers

	fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.writing_mcp"), path)

	// Marshal back to YAML
	data, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal YAML: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create .continue dir: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (p *continuePlatform) WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), "(hooks not supported)")
		return nil
	}
	return ErrHooksNotSupported
}
