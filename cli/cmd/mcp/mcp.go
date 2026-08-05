// Package mcpcmd holds the `weknora mcp` command tree.
//
// MCP (Model Context Protocol; https://spec.modelcontextprotocol.io/) is
// the JSON-RPC 2.0 wire protocol agentic IDEs use to call external tools.
// `weknora mcp serve` exposes a curated subset of the CLI as MCP tools so
// an IDE-side agent can list / view / search / chat against the user's
// active WeKnora profile without shelling out to the CLI per call. `weknora
// mcp setup` writes agent-platform integration config. Most tools are read-only;
// chat and session_ask create conversation/message records.
//
// Package name is `mcpcmd` to avoid shadowing `cli/internal/mcp` (the
// transport-and-handlers implementation). Same naming hygiene as
// `agentcmd` / `sessioncmd`.
package mcpcmd

import (
	"github.com/spf13/cobra"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
)

// NewCmd builds the `weknora mcp` parent. Called from cli/cmd/root.go.
func NewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve and configure WeKnora MCP integration",
		Long: `Run WeKnora as a Model Context Protocol server or configure supported
agent platforms to use it.

` + "`weknora mcp serve`" + ` exposes the curated MCP tool surface over JSON-RPC.
Read-only tools cover knowledge bases, documents, chunks, search, agents, and
memory inspection; chat/session tools create conversation records. Destructive
verbs (create / delete / upload) are deliberately excluded - the agent should
ask the user before mutating; the CLI's exit-10 protocol covers that path.

` + "`weknora mcp setup`" + ` writes MCP server config and, in interactive mode,
can also enable Memory lifecycle hooks and Memory instruction rules.`,
		Args: cobra.NoArgs,
		Run:  func(c *cobra.Command, _ []string) { _ = c.Help() },
	}
	cmd.AddCommand(NewCmdServe(f))
	cmd.AddCommand(NewCmdSetup(f))
	return cmd
}
