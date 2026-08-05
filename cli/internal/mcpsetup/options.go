package mcpsetup

import "strings"

// Component identifies one independently-writable part of the WeKnora MCP setup.
type Component string

const (
	// ComponentMCP writes MCP server configuration and environment variables.
	ComponentMCP Component = "mcp"
	// ComponentMemoryHooks writes lifecycle hook configuration for memory capture.
	ComponentMemoryHooks Component = "memory-hooks"
	// ComponentMemoryRules writes agent instruction rules for memory usage.
	ComponentMemoryRules Component = "memory-rules"
)

// Options carries the shared setup inputs used by both `mcp setup` and the
// legacy `memory setup` wrapper.
type Options struct {
	Platform      string
	DryRun        bool
	KBIDs         []string
	ServerURL     string
	APIKey        string
	McpServerPath string
	Components    []Component
}

// DefaultComponents preserves the historical setup behavior for non-interactive
// and flag-driven runs.
func DefaultComponents() []Component {
	return []Component{ComponentMCP, ComponentMemoryHooks, ComponentMemoryRules}
}

// NormalizeComponents de-duplicates valid components while preserving order.
// An empty selection means the historical all-components setup.
func NormalizeComponents(raw []Component) []Component {
	if len(raw) == 0 {
		return DefaultComponents()
	}
	valid := map[Component]bool{
		ComponentMCP:         true,
		ComponentMemoryHooks: true,
		ComponentMemoryRules: true,
	}
	seen := make(map[Component]bool)
	out := make([]Component, 0, len(raw))
	for _, c := range raw {
		if !valid[c] || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return DefaultComponents()
	}
	return out
}

func hasComponent(components []Component, want Component) bool {
	for _, c := range NormalizeComponents(components) {
		if c == want {
			return true
		}
	}
	return false
}

// FlattenKBIDs flattens repeated --kb flags and comma-separated values into a
// de-duplicated list.
func FlattenKBIDs(raw []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, k := range raw {
		for _, part := range strings.Split(k, ",") {
			part = strings.TrimSpace(part)
			if part != "" && !seen[part] {
				seen[part] = true
				result = append(result, part)
			}
		}
	}
	return result
}

// SplitCommaSeparated splits a comma-separated string into trimmed non-empty parts.
func SplitCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
