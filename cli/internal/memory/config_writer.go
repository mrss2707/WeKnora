package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---- Shared config writers ----

// writeJSONFile marshals v as indented JSON (with trailing newline) and writes to path.
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// ----- MCP config -----

// UpsertJSONMcpServerResult reports the outcome of an MCP config upsert.
type UpsertJSONMcpServerResult int

const (
	// UpsertMerged means the weknora entry was already present — no changes made.
	UpsertMerged UpsertJSONMcpServerResult = iota
	// UpsertAdded means the weknora entry was newly added.
	UpsertAdded
	// UpsertUpdated means the weknora entry existed but was overwritten with new data.
	UpsertUpdated
)

// upsertJSONMcpServer writes (or overwrites) weknora MCP server entry into
// existing map under key. Always writes to ensure config is correct on re-setup.
// Returns UpsertAdded for new entries, UpsertUpdated when overwriting an existing one.
func upsertJSONMcpServer(existing map[string]any, key, serverName string) UpsertJSONMcpServerResult {
	servers, _ := existing[key].(map[string]any)
	if servers == nil {
		servers = make(map[string]any)
		existing[key] = servers
	}

	// Preserve existing env so re-setup without --server-url doesn't wipe auth.
	var existingEnv map[string]any
	if oldSrv, ok := servers[serverName].(map[string]any); ok {
		existingEnv, _ = oldSrv["env"].(map[string]any)
	}

	entry := map[string]any{
		"type":    "stdio",
		"command": "weknora",
		"args":    []string{"mcp", "serve"},
	}
	if len(existingEnv) > 0 {
		entry["env"] = existingEnv
	}

	_, existed := servers[serverName]
	servers[serverName] = entry
	if existed {
		return UpsertUpdated
	}
	return UpsertAdded
}

// UpsertMcpEnv adds or updates env vars on an existing MCP server entry in the
// file at path. Creates the file if missing. Safe to call after WriteMCPConfig —
// the weknora entry must already exist. Returns nil when entry not found (no-op).
func UpsertMcpEnv(path, serverName string, env map[string]string) error {
	if len(env) == 0 {
		return nil
	}
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}
	servers, _ := existing["mcpServers"].(map[string]any)
	if servers == nil {
		return nil // no mcpServers key
	}
	srv, _ := servers[serverName].(map[string]any)
	if srv == nil {
		return nil // entry not found
	}
	srv["env"] = env
	servers[serverName] = srv
	return writeJSONFile(path, existing)
}

// ReadMcpEnv reads the env vars from an existing MCP server entry.
// Returns nil if the entry or env is not found.
func ReadMcpEnv(path, serverName string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var existing map[string]any
	if err := json.Unmarshal(data, &existing); err != nil {
		return nil
	}
	servers, _ := existing["mcpServers"].(map[string]any)
	if servers == nil {
		return nil
	}
	srv, _ := servers[serverName].(map[string]any)
	if srv == nil {
		return nil
	}
	envRaw, _ := srv["env"].(map[string]any)
	if envRaw == nil {
		return nil
	}
	env := make(map[string]string, len(envRaw))
	for k, v := range envRaw {
		if s, ok := v.(string); ok {
			env[k] = s
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// WriteCustomMcpServer writes a weknora MCP server entry with a custom command
// and args into the project's .mcp.json. Used when the user provides a custom
// MCP server path (e.g. Python main.py) instead of using the Go CLI.
// Idempotent: skips if weknora entry already exists.
func WriteCustomMcpServer(mcpConfigPath, command string, args []string, env map[string]string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), mcpConfigPath)
		return nil
	}

	existing := make(map[string]any)
	if data, err := os.ReadFile(mcpConfigPath); err == nil {
		json.Unmarshal(data, &existing)
	}

	servers, _ := existing["mcpServers"].(map[string]any)
	if servers == nil {
		servers = make(map[string]any)
		existing["mcpServers"] = servers
	}

	entry := map[string]any{
		"type":    "stdio",
		"command": command,
		"args":    args,
	}
	if len(env) > 0 {
		entry["env"] = env
	}
	servers["weknora"] = entry

	fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.writing_mcp"), mcpConfigPath)
	return writeJSONFile(mcpConfigPath, existing)
}

// ----- Hooks config (flat array, used by Claude Code, Cursor, Copilot) -----

// upsertJSONHooks removes existing weknora hooks, then appends fresh entries.
// This ensures re-setup always has the correct hooks.
// Returns UpsertAdded when hooks were written, UpsertUpdated when old hooks
// were replaced.
func upsertJSONHooks(existing map[string]any, entries []HookEntry) UpsertJSONMcpServerResult {
	hooks, _ := existing["hooks"].([]any)

	// Filter out old weknora hooks
	filtered := make([]any, 0, len(hooks))
	hadWeknora := false
	for _, h := range hooks {
		if hm, ok := h.(map[string]any); ok {
			if cmd, ok := hm["command"].(string); ok && len(cmd) >= 7 && cmd[:7] == "weknora" {
				hadWeknora = true
				continue
			}
		}
		filtered = append(filtered, h)
	}
	hooks = filtered

	for _, entry := range entries {
		hook := map[string]any{
			"event":   entry.Event,
			"command": entry.Command,
		}
		if len(entry.Env) > 0 {
			hook["env"] = entry.Env
		}
		hooks = append(hooks, hook)
	}
	existing["hooks"] = hooks
	if hadWeknora {
		return UpsertUpdated
	}
	return UpsertAdded
}

// ----- PaiCode hooks (nested matcher format) -----

// upsertPaiCodeHooks writes weknora hooks into existing["hooks"] using PaiCode's
// nested matcher format: hooks.EventName: [{hooks: [{type: "command", command: "..."}]}].
// Always overwrites weknora events to ensure config is correct on re-setup.
func upsertPaiCodeHooks(existing map[string]any, entries []HookEntry) UpsertJSONMcpServerResult {
	hooks, _ := existing["hooks"].(map[string]any)
	if hooks == nil {
		hooks = make(map[string]any)
	}

	hadWeknora := hasWeknoraInNestedHooks(hooks)

	for _, entry := range entries {
		innerHook := map[string]any{
			"type":    "command",
			"command": entry.Command,
		}
		if len(entry.Env) > 0 {
			innerHook["env"] = entry.Env
		}
		matcher := map[string]any{
			"hooks": []any{innerHook},
		}
		hooks[entry.Event] = []any{matcher}
	}
	existing["hooks"] = hooks
	if hadWeknora {
		return UpsertUpdated
	}
	return UpsertAdded
}

// hasWeknoraInNestedHooks checks PaiCode-style nested hooks for any weknora command.
func hasWeknoraInNestedHooks(hooks map[string]any) bool {
	for _, entries := range hooks {
		arr, _ := entries.([]any)
		for _, matcher := range arr {
			m, _ := matcher.(map[string]any)
			innerHooks, _ := m["hooks"].([]any)
			for _, h := range innerHooks {
				hm, _ := h.(map[string]any)
				if cmd, ok := hm["command"].(string); ok && len(cmd) >= 7 && cmd[:7] == "weknora" {
					return true
				}
			}
		}
	}
	return false
}

// ----- Rules file -----

// WriteRulesFile writes the memory protocol rules block to rulesPath.
// Idempotent: skips if the WEKNORA_MEMORY_PROTOCOL marker is already present.
func WriteRulesFile(rulesPath string, kbIDs []string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s %s\n", T("setup.dry_run"), rulesPath)
		return nil
	}

	existing, _ := os.ReadFile(rulesPath)
	if HasMemoryProtocolRules(string(existing)) {
		fmt.Fprintf(os.Stderr, "%s\n", T("setup.idempotent_rules"))
		return nil
	}

	fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(T("setup.writing_rules"), rulesPath))
	newContent := InjectRules(string(existing), kbIDs)
	if err := os.MkdirAll(filepath.Dir(rulesPath), 0755); err != nil {
		return fmt.Errorf("create rules dir %s: %w", filepath.Dir(rulesPath), err)
	}
	if err := os.WriteFile(rulesPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("write rules file: %w", err)
	}
	return nil
}

// expandPath resolves ~ to the user's home directory.
func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
