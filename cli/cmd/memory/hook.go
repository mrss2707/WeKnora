package memorycmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
	memoryinternal "github.com/Tencent/WeKnora/cli/internal/memory"
)

// NewCmdHook builds `weknora memory hook <event>`.
// Reads stdin JSON, processes the hook, writes stdout JSON.
func NewCmdHook(f *cmdutil.Factory) *cobra.Command {
	opts := &hookOptions{}

	cmd := &cobra.Command{
		Use:   "hook <event>",
		Short: "Run a memory lifecycle hook (session-start | post-tool)",
		Long: `Execute a single memory lifecycle hook. Reads the hook's stdin JSON
payload from stdin, processes it against the WeKnora Memory V2 API,
and writes the result JSON to stdout.

Supported events:
  session-start   — load session context from past memories
  post-tool       — classify and optionally save tool actions as memories

Requires an active profile (--profile) or WEKNORA_PROFILE env var.
The KB is resolved from WEKNORA_KB_ID env var or linked project.`,
		Example: `  weknora memory hook session-start < payload.json
  echo '{"session_id":"s1","cwd":"/project"}' | weknora memory hook session-start`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			event := args[0]
			// Validate event name
			switch event {
			case "session-start", "post-tool":
			default:
				return cmdutil.NewFlagError(fmt.Errorf("unknown hook event: %s (valid: session-start, post-tool)", event))
			}

			// Build SDK client
			cli, err := f.Client()
			if err != nil {
				return err
			}

			// Check if stdin is available
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) != 0 {
				return cmdutil.NewFlagError(fmt.Errorf("hook requires stdin JSON payload; pipe it in or redirect from a file"))
			}

			hctx := &memoryinternal.HookContext{
				Client: cli,
				Cache:  memoryinternal.NewCache(),
				KBID:   opts.KBID,
			}

			return memoryinternal.RunHook(event, hctx)
		},
	}

	cmd.Flags().StringVar(&opts.KBID, "kb", "", "Knowledge base ID (overrides WEKNORA_KB_ID env var)")
	cmdutil.SetAgentHelp(cmd, cmdutil.AgentHelp{
		UsedFor: "run a memory lifecycle hook — agent platforms invoke this via their hook system",
		Output:  "stdout: JSON payload (shape depends on the hook event); stderr: log messages",
		RequiredFlags: []string{"<event>"},
		Warnings: []string{
			"requires stdin JSON — pipe it from the agent platform's hook dispatcher",
			"errors are written to stderr; stdout always carries the hook response JSON",
		},
	})
	return cmd
}

type hookOptions struct {
	KBID string
}
