package memorycmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
	memoryinternal "github.com/Tencent/WeKnora/cli/internal/memory"
)

// NewCmdSetup builds `weknora memory setup [--platform <name>]`.
// Without --platform, launches an interactive TUI form if stdin is a terminal.
func NewCmdSetup(f *cmdutil.Factory) *cobra.Command {
	opts := &setupOptions{}

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure agent platform for Memory V2 (MCP + hooks + rules)",
		Long: `Set up weknora memory integration for an AI agent platform.
Writes MCP server config, lifecycle hooks, and memory protocol rules
to the platform's configuration files.

Run without --platform to pick platforms interactively (TUI).

Supported platforms:
  claude-code  — .mcp.json + .claude/settings.json + CLAUDE.md
  paicode      — ~/.paicode.json + AGENTS.md
  cursor       — .cursor/mcp.json + .cursor/hooks.json + .cursorrules
  copilot      — .vscode/mcp.json + .github/hooks/weknora.json + .github/copilot-instructions.md
  windsurf     — ~/.codeium/windsurf/mcp_config.json + .windsurf/hooks.json + .windsurfrules
  cline        — OS-dependent MCP config + .clinerules (no hooks)
  continue     — .continue/config.yaml + .continue/rules/weknora.md (no hooks)
  gemini       — .gemini/settings.json + GEMINI.md + .agents/rules/weknora.md (no hooks)
  auto         — detect platform from project markers

The command is idempotent: running it twice does not duplicate entries.
Use --dry-run to preview changes without writing files.`,
		Example: `  weknora memory setup                       # interactive TUI
  weknora memory setup --platform claude-code
  weknora memory setup --platform cursor --dry-run
  weknora memory setup --platform auto`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "get working directory")
			}

			// Determine platform list
			var platforms []string
			if opts.Platform != "" {
				platforms = []string{opts.Platform}
			} else if isInteractive() && !isAccessible() {
				// Launch interactive TUI
				selected, tuiKBs, tuiURL, tuiKey, tuiMcpPath, err := runInteractiveSetup()
				if err != nil {
					return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "interactive setup")
				}
				if len(selected) == 0 {
					return nil // user cancelled
				}
				platforms = selected
				if len(tuiKBs) > 0 && len(opts.KBIDs) == 0 {
					opts.KBIDs = tuiKBs
				}
				if tuiURL != "" && opts.ServerURL == "" {
					opts.ServerURL = tuiURL
				}
				if tuiKey != "" && opts.APIKey == "" {
					opts.APIKey = tuiKey
				}
				if tuiMcpPath != "" && opts.McpServerPath == "" {
					opts.McpServerPath = tuiMcpPath
				}
			} else {
				return cmdutil.NewFlagError(fmt.Errorf(
					"required flag \"--platform\" not set; run without flags for interactive mode"))
			}

			dryRun := opts.DryRun

			// Flatten comma-separated --kb values
			kbIDs := flattenKBIDs(opts.KBIDs)

			for _, platformName := range platforms {
				// Handle --platform auto via detection
				name := platformName
				if name == "auto" {
					detected := memoryinternal.DetectPlatform(cwd)
					if detected == "" {
						fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.no_platform_detected"))
						continue
					}
					name = detected
					fmt.Fprintf(os.Stderr, "%s %s\n", memoryinternal.T("setup.detected_platform"), name)
				}

				if err := setupPlatform(cwd, name, kbIDs, opts.ServerURL, opts.APIKey, opts.McpServerPath, dryRun); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Platform, "platform", "", "Target platform: claude-code | paicode | cursor | copilot | windsurf | cline | continue | gemini | auto")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Preview changes without writing files")
	cmd.Flags().StringSliceVar(&opts.KBIDs, "kb", nil, "Knowledge base ID (supports --kb a,b and --kb a --kb b; overrides WEKNORA_KB_ID env var)")
	cmd.Flags().StringVar(&opts.ServerURL, "server-url", "", "WeKnora server base URL (overrides WEKNORA_BASE_URL env var)")
	cmd.Flags().StringVar(&opts.APIKey, "api-key", "", "WeKnora API key (overrides WEKNORA_API_KEY env var)")

	cmdutil.SetAgentHelp(cmd, cmdutil.AgentHelp{
		UsedFor: "configure an AI agent platform to use weknora memory hooks and MCP tools",
		Output:  "stdout: none (all output goes to stderr); exit 0 on success",
		RequiredFlags: []string{"--platform"},
		Warnings: []string{
			"idempotent — safe to run multiple times",
			"use --dry-run to preview changes first",
			"omit --platform to run interactive TUI form",
		},
	})
	return cmd
}

// setupPlatform configures a single platform: MCP + hooks + rules.
func setupPlatform(cwd, platformName string, kbIDs []string, serverURL, apiKey, mcpServerPath string, dryRun bool) error {
	platform, err := memoryinternal.NewPlatform(platformName)
	if err != nil {
		return cmdutil.NewFlagError(err)
	}

	if len(kbIDs) == 0 {
		kbIDs = splitCommaSeparated(os.Getenv("WEKNORA_KB_ID"))
	}

	// Env var fallback: WEKNORA_BASE_URL / WEKNORA_API_KEY serve as defaults
	// when --server-url / --api-key flags are not set.
	if serverURL == "" {
		serverURL = os.Getenv("WEKNORA_BASE_URL")
	}
	if apiKey == "" {
		apiKey = os.Getenv("WEKNORA_API_KEY")
	}

	fmt.Fprintf(os.Stderr, "\n── %s ──\n", platform.Name())
	fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.scanning"))
	if len(kbIDs) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(memoryinternal.T("setup.kb_detected"), strings.Join(kbIDs, ", ")))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.no_kb"))
	}

	// 1. Write MCP config
	if mcpServerPath != "" {
		// Custom MCP server (e.g. Python main.py)
		if err := writeCustomMcpConfig(cwd, mcpServerPath, serverURL, apiKey, dryRun); err != nil {
			return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write MCP config")
		}
	} else if strat, ok := platform.(memoryinternal.SetupStrategy); ok {
		// Default Go CLI MCP server
		if err := strat.WriteMCPConfig(cwd, dryRun); err != nil {
			return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write MCP config")
		}
		// If server URL + API key provided, inject env into the MCP entry
		if !dryRun {
			mcpConfigPath := platform.MCPConfigPath(cwd)
			mcpEnv := memoryinternal.ReadMcpEnv(mcpConfigPath, "weknora")
			if mcpEnv == nil {
				mcpEnv = make(map[string]string)
			}
			if serverURL != "" {
				mcpEnv["WEKNORA_BASE_URL"] = serverURL
				if apiKey != "" {
					mcpEnv["WEKNORA_API_KEY"] = apiKey
				}
			}
			// Write the first KB ID as the default write target
			if len(kbIDs) > 0 {
				mcpEnv["WEKNORA_KB_ID"] = kbIDs[0]
			}
			if len(mcpEnv) > 0 {
				if err := memoryinternal.UpsertMcpEnv(mcpConfigPath, "weknora", mcpEnv); err != nil {
					return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write MCP env")
				}
			}
		}
	}

	// 2. Write hooks config via SetupStrategy
	if strat, ok := platform.(memoryinternal.SetupStrategy); ok {
		hookEnv := make(map[string]string)
		if serverURL != "" {
			hookEnv["WEKNORA_BASE_URL"] = serverURL
			if apiKey != "" {
				hookEnv["WEKNORA_API_KEY"] = apiKey
			}
		} else if mcpServerPath == "" {
			hookEnv = memoryinternal.ReadMcpEnv(platform.MCPConfigPath(cwd), "weknora")
		}
		if err := strat.WriteHooksConfig(cwd, hookEnv, dryRun); err != nil {
			if err == memoryinternal.ErrHooksNotSupported {
				fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.no_hooks"))
			} else {
				return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write hooks config")
			}
		}
	}

	// 3. Write rules file
	if err := memoryinternal.WriteRulesFile(platform.RulesFilePath(cwd), kbIDs, dryRun); err != nil {
		return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write rules file")
	}

	// 3b. Write extra rules files (e.g. Gemini .agents/rules/weknora.md)
	if extra, ok := platform.(memoryinternal.ExtraRulesPlatform); ok {
		for _, p := range extra.ExtraRulesPaths(cwd) {
			if err := memoryinternal.WriteRulesFile(p, kbIDs, dryRun); err != nil {
				return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write extra rules file")
			}
		}
	}

	if !dryRun {
		fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.done"))
	}
	return nil
}

// splitCommaSeparated splits a comma-separated string into trimmed non-empty parts.
func splitCommaSeparated(s string) []string {
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

// flattenKBIDs flattens repeated --kb flags and comma-separated values into a deduplicated list.
func flattenKBIDs(raw []string) []string {
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

// writeCustomMcpConfig writes a custom MCP server entry (e.g. Python main.py).
func writeCustomMcpConfig(cwd, mcpServerPath, serverURL, apiKey string, dryRun bool) error {
	python, ok := resolvePythonFromMainpy(mcpServerPath)
	if !ok {
		fmt.Fprintf(os.Stderr, "WARNING: Python '%s' cannot import 'mcp' module. Install with: pip install mcp\n", python)
	}
	mcpConfigPath := filepath.Join(cwd, ".mcp.json")
	env := make(map[string]string)
	if serverURL != "" {
		env["WEKNORA_BASE_URL"] = serverURL
	}
	if apiKey != "" {
		env["WEKNORA_API_KEY"] = apiKey
	}
	return memoryinternal.WriteCustomMcpServer(mcpConfigPath, python, []string{mcpServerPath}, env, dryRun)
}

type setupOptions struct {
	Platform      string
	DryRun        bool
	KBIDs         []string
	ServerURL     string
	APIKey        string
	McpServerPath string
}
