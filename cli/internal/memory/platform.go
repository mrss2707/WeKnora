package memory

import (
	"errors"
	"fmt"
)

// ErrHooksNotSupported is returned by platforms that do not support file-based hooks.
var ErrHooksNotSupported = errors.New("hooks not supported on this platform")

// HookEntry describes a single hook registration.
type HookEntry struct {
	Event   string            `json:"event"`
	Command string            `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
}

// Platform abstracts per-platform setup (Claude Code, PaiCode, etc.).
type Platform interface {
	// Name returns the platform identifier (e.g. "claude-code", "paicode").
	Name() string

	// MCPConfigPath returns the path to the MCP configuration file.
	MCPConfigPath(cwd string) string

	// MCPConfigKey returns the JSON key for MCP servers (e.g. "mcpServers", "servers").
	MCPConfigKey() string

	// HooksConfigPath returns the path to the hooks configuration file.
	HooksConfigPath(cwd string) string

	// RulesFilePath returns the path to the rules/memory file.
	RulesFilePath(cwd string) string

	// GetHookEntries returns the hook event → command mappings.
	GetHookEntries() []HookEntry
}

// SetupStrategy provides a unified interface for writing platform configuration.
// Each platform implements WriteMCPConfig and WriteHooksConfig using the shared
// config_writer utilities.
type SetupStrategy interface {
	WriteMCPConfig(cwd string, dryRun bool) error
	WriteHooksConfig(cwd string, env map[string]string, dryRun bool) error
}

// ExtraRulesPlatform is an optional interface for platforms that need additional
// rules files beyond the main one (e.g., Gemini CLI writes both GEMINI.md and
// .agents/rules/weknora.md).
type ExtraRulesPlatform interface {
	ExtraRulesPaths(cwd string) []string
}

// NewPlatform returns the platform implementation for the given name.
// Supported: "claude-code", "paicode", "cursor", "copilot", "windsurf",
// "cline", "continue", "gemini", "auto".
func NewPlatform(name string) (Platform, error) {
	switch name {
	case "claude-code":
		return &claudeCodePlatform{}, nil
	case "paicode":
		return &paiCodePlatform{}, nil
	case "cursor":
		return &cursorPlatform{}, nil
	case "copilot":
		return &copilotPlatform{}, nil
	case "windsurf":
		return &windsurfPlatform{}, nil
	case "cline":
		return &clinePlatform{}, nil
	case "continue":
		return &continuePlatform{}, nil
	case "gemini":
		return &geminiPlatform{}, nil
	default:
		return nil, fmt.Errorf(T("setup.unknown_platform"), name)
	}
}
