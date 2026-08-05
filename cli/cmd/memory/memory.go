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
		Short: "Memory V2 runtime hooks and legacy setup",
		Long: `Memory V2 runtime integration commands.

Manage WeKnora memory lifecycle hooks used by configured AI agent platforms to
auto-save and recall memories during development sessions. New platform setup
should use ` + "`weknora mcp setup`" + `; ` + "`weknora memory setup`" + ` remains as a deprecated
legacy alias.`,
	}
	cmd.AddCommand(NewCmdHook(f))
	cmd.AddCommand(NewCmdSetup(f))
	return cmd
}
