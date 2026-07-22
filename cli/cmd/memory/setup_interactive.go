package memorycmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"

	"github.com/Tencent/WeKnora/client"
	memoryinternal "github.com/Tencent/WeKnora/cli/internal/memory"
)

// allPlatforms lists all supported platforms for the multi-select form.
var allPlatforms = []string{
	"claude-code", "paicode", "cursor", "copilot",
	"windsurf", "cline", "continue", "gemini",
}

// isInteractive returns true if stdin is a terminal (can run TUI).
func isInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// isAccessible returns true if ACCESSIBLE env is set (for screen readers / non-TUI mode).
func isAccessible() bool {
	return os.Getenv("ACCESSIBLE") == "1"
}

// runInteractiveSetup runs the 3-step TUI flow:
//
//	Step 1 — platform select + server URL + API key + MCP server path
//	Step 2 — fetch KBs from server (best-effort)
//	Step 3 — KB selection (multi-select if server responded, manual input otherwise)
//
// Returns (platforms, kbIDs, serverURL, apiKey, mcpServerPath, error).
// On user cancel (ctrl+c), returns nil, nil, "", "", "", nil.
func runInteractiveSetup() ([]string, []string, string, string, string, error) {
	var selectedPlatforms []string
	var serverURL string
	var apiKey string
	var mcpServerPath string

	form1 := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select agent platforms to configure").
				Description("Space to select, Enter to confirm. At least one required.").
				Options(huh.NewOptions(allPlatforms...)...).
				Value(&selectedPlatforms).
				Validate(func(v []string) error {
					if len(v) == 0 {
						return fmt.Errorf("select at least one platform")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewInput().
				Title(memoryinternal.T("setup.server_url")).
				Placeholder("http://localhost:8080").
				Value(&serverURL),
			huh.NewInput().
				Title(memoryinternal.T("setup.api_key")).
				Placeholder("sk-...").
				Value(&apiKey).
				EchoMode(huh.EchoModePassword),
			huh.NewInput().
				Title(memoryinternal.T("setup.mcp_server_path")).
				Description(memoryinternal.T("setup.mcp_server_desc")).
				Placeholder("/path/to/weknora-mcp-server/main.py").
				Value(&mcpServerPath),
		),
	).WithAccessible(isAccessible())

	err := form1.Run()
	if err != nil {
		if err == huh.ErrUserAborted {
			return nil, nil, "", "", "", nil
		}
		return nil, nil, "", "", "", err
	}

	// Inter-form: fetch KBs from server (connection test + KB list)
	fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.connecting"))
	kbs, fetchErr := fetchKBs(serverURL, apiKey)

	var kbIDs []string

	if fetchErr == nil && len(kbs) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(memoryinternal.T("setup.kb_connected"), len(kbs)))
		var selected []string
		options := make([]huh.Option[string], len(kbs))
		for i, kb := range kbs {
			label := fmt.Sprintf("%s (%d docs)", kb.Name, kb.KnowledgeCount)
			options[i] = huh.NewOption(label, kb.ID)
		}

		form2 := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title(memoryinternal.T("setup.kb_select_title")).
					Description(memoryinternal.T("setup.kb_select_desc")).
					Options(options...).
					Value(&selected),
			),
		).WithAccessible(isAccessible())

		err = form2.Run()
		if err != nil {
			if err == huh.ErrUserAborted {
				return nil, nil, "", "", "", nil
			}
			return nil, nil, "", "", "", err
		}
		kbIDs = selected
	} else {
		var manualInput string

		if fetchErr != nil {
			// Redact API key from error message if present
			errMsg := fetchErr.Error()
			if apiKey != "" && len(apiKey) > 8 {
				errMsg = strings.ReplaceAll(errMsg, apiKey, apiKey[:4]+"...")
			}
			fmt.Fprintf(os.Stderr, "%s: %s\n", memoryinternal.T("setup.kb_fetch_error"), errMsg)
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.kb_fetch_error"))
		}

		form2 := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(memoryinternal.T("setup.kb_select_title")).
					Description(memoryinternal.T("setup.kb_manual_hint")).
					Placeholder("kb_abc123,kb_def456").
					Value(&manualInput),
			),
		).WithAccessible(isAccessible())

		err = form2.Run()
		if err != nil {
			if err == huh.ErrUserAborted {
				return nil, nil, "", "", "", nil
			}
			return nil, nil, "", "", "", err
		}
		if manualInput != "" {
			kbIDs = splitCommaSeparated(manualInput)
		}
	}

	return selectedPlatforms, kbIDs, serverURL, apiKey, mcpServerPath, nil
}

// resolvePythonFromMainpy tries to find the Python interpreter near main.py.
// Looks for .venv/bin/python in the same directory as main.py.
// Returns (pythonPath, ok). ok=false means the Python can't import 'mcp'.
func resolvePythonFromMainpy(mainPyPath string) (string, bool) {
	dir := filepath.Dir(mainPyPath)
	venvPython := filepath.Join(dir, ".venv", "bin", "python")
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython, canImportMcp(venvPython)
	}
	return "python3", canImportMcp("python3")
}

// canImportMcp checks if the given Python interpreter can import 'mcp'.
func canImportMcp(python string) bool {
	cmd := exec.Command(python, "-c", "import mcp")
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// fetchKBs tries to fetch knowledge bases from the server.
// Returns (kbs, error). Works with just serverURL (no API key — e.g. localhost).
func fetchKBs(serverURL, apiKey string) ([]client.KnowledgeBase, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("no server URL")
	}

	opts := []client.ClientOption{client.WithTimeout(5 * time.Second)}
	if apiKey != "" {
		opts = append(opts, client.WithAPIKey(apiKey))
	}
	sdkClient := client.NewClient(serverURL, opts...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	kbs, err := sdkClient.ListKnowledgeBases(ctx)
	if err != nil {
		return nil, err
	}
	return kbs, nil
}
