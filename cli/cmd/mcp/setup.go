package mcpcmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
	"github.com/Tencent/WeKnora/cli/internal/mcpsetup"
)

// NewCmdSetup builds `weknora mcp setup [--platform <name>]`.
func NewCmdSetup(_ *cmdutil.Factory) *cobra.Command {
	opts := &mcpsetup.Options{}

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure WeKnora MCP integration for agent platforms",
		Long: `Set up WeKnora MCP integration for supported AI agent platforms.

Writes MCP server config for ` + "`weknora mcp serve`" + `. In interactive mode,
you can also choose Memory lifecycle hooks and Memory instruction rules as setup
components. Non-interactive and flag-driven runs keep the historical default and
configure all components: MCP server config, Memory hooks, and Memory rules.

Supported platforms:
  claude-code  - .mcp.json + .claude/settings.json + CLAUDE.md
  paicode      - .mcp.json + .paicode/settings.json + AGENTS.md
  cursor       - .cursor/mcp.json + .cursor/hooks.json + .cursorrules
  copilot      - .vscode/mcp.json + .github/hooks/weknora.json + .github/copilot-instructions.md
  windsurf     - ~/.codeium/windsurf/mcp_config.json + .windsurf/hooks.json + .windsurfrules
  cline        - OS-dependent MCP config + .clinerules (no hooks)
  continue     - .continue/config.yaml + .continue/rules/weknora.md (no hooks)
  gemini       - .gemini/settings.json + GEMINI.md + .agents/rules/weknora.md (no hooks)
  auto         - detect platform from project markers

The command is idempotent: running it twice does not duplicate entries.
Use --dry-run to preview changes without writing files.`,
		Example: `  weknora mcp setup                       # interactive TUI
  weknora mcp setup --platform claude-code
  weknora mcp setup --platform cursor --dry-run
  weknora mcp setup --platform auto`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "get working directory")
			}

			platforms, err := resolveSetupInputs(opts)
			if err != nil {
				return err
			}
			if len(platforms) == 0 {
				return nil
			}
			return mcpsetup.Run(cwd, platforms, *opts)
		},
	}
	addSetupFlags(cmd, opts)
	cmdutil.SetAgentHelp(cmd, setupAgentHelp("configure WeKnora MCP integration for an AI agent platform, including optional Memory hooks and rules in interactive mode"))
	return cmd
}

func addSetupFlags(cmd *cobra.Command, opts *mcpsetup.Options) {
	cmd.Flags().StringVar(&opts.Platform, "platform", "", "Target platform: claude-code | paicode | cursor | copilot | windsurf | cline | continue | gemini | auto")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Preview changes without writing files")
	cmd.Flags().StringSliceVar(&opts.KBIDs, "kb", nil, "Knowledge base ID (supports --kb a,b and --kb a --kb b; overrides WEKNORA_KB_ID env var)")
	cmd.Flags().StringVar(&opts.ServerURL, "server-url", "", "WeKnora server base URL (overrides WEKNORA_BASE_URL env var)")
	cmd.Flags().StringVar(&opts.APIKey, "api-key", "", "WeKnora API key (overrides WEKNORA_API_KEY env var)")
}

func resolveSetupInputs(opts *mcpsetup.Options) ([]string, error) {
	if opts.Platform != "" {
		opts.Components = mcpsetup.DefaultComponents()
		return []string{opts.Platform}, nil
	}
	if mcpsetup.IsInteractive() && !mcpsetup.IsAccessible() {
		selected, err := mcpsetup.RunInteractiveSetup()
		if err != nil {
			return nil, cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "interactive setup")
		}
		if len(selected.Platforms) == 0 {
			return nil, nil
		}
		if len(selected.KBIDs) > 0 && len(opts.KBIDs) == 0 {
			opts.KBIDs = selected.KBIDs
		}
		if selected.ServerURL != "" && opts.ServerURL == "" {
			opts.ServerURL = selected.ServerURL
		}
		if selected.APIKey != "" && opts.APIKey == "" {
			opts.APIKey = selected.APIKey
		}
		if selected.McpServerPath != "" && opts.McpServerPath == "" {
			opts.McpServerPath = selected.McpServerPath
		}
		opts.Components = selected.Components
		return selected.Platforms, nil
	}
	return nil, cmdutil.NewFlagError(fmt.Errorf("required flag \"--platform\" not set; run without flags for interactive mode"))
}

func setupAgentHelp(usedFor string) cmdutil.AgentHelp {
	return cmdutil.AgentHelp{
		UsedFor:       usedFor,
		Output:        "stdout: none (all progress output goes to stderr); exit 0 on success",
		RequiredFlags: []string{"--platform"},
		Examples: []string{
			"weknora mcp setup --platform claude-code --dry-run",
			"weknora mcp setup --platform cursor",
			"weknora mcp setup --platform auto",
		},
		Warnings: []string{
			"idempotent - safe to run multiple times",
			"use --dry-run to preview changes first",
			"omit --platform to run interactive TUI form with component selection",
		},
	}
}
