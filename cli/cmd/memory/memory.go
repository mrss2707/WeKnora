// Package memorycmd holds `weknora memory` command tree (hook / setup)
// for Memory V2 agent integration.
package memorycmd

import (
	"github.com/spf13/cobra"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
)

// NewCmd builds the `weknora memory` parent command.
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Memory V2 agent integration (hooks, setup)",
		Long: `Memory V2 agent integration commands.

Manage weknora memory hooks and platform setup for AI agents
(Claude Code, PaiCode, Cursor, VS Code Copilot, Windsurf,
Cline, Continue, Gemini CLI) to auto-save and recall memories
during development sessions.`,
	}
	cmd.AddCommand(NewCmdHook(f))
	cmd.AddCommand(NewCmdSetup(f))
	return cmd
}
