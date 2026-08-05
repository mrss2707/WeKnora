package mcpsetup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"

	memoryinternal "github.com/Tencent/WeKnora/cli/internal/memory"
	"github.com/Tencent/WeKnora/client"
)

// AllPlatforms lists all supported platforms for the interactive form.
var AllPlatforms = []string{
	"claude-code", "paicode", "cursor", "copilot",
	"windsurf", "cline", "continue", "gemini",
}

// IsInteractive reports whether stdin can run the TUI.
func IsInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// IsAccessible reports whether TUI rendering should be disabled for screen readers.
func IsAccessible() bool {
	return os.Getenv("ACCESSIBLE") == "1"
}

// InteractiveResult carries user selections from the setup TUI.
type InteractiveResult struct {
	Platforms     []string
	Components    []Component
	KBIDs         []string
	ServerURL     string
	APIKey        string
	McpServerPath string
}

// RunInteractiveSetup runs the TUI flow for platform, component, server, and KB selection.
func RunInteractiveSetup() (InteractiveResult, error) {
	var result InteractiveResult
	var selectedComponents = []string{
		string(ComponentMCP),
		string(ComponentMemoryHooks),
		string(ComponentMemoryRules),
	}

	form1 := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select agent platforms to configure").
				Description("Space to select, Enter to confirm. At least one required.").
				Options(huh.NewOptions(AllPlatforms...)...).
				Value(&result.Platforms).
				Validate(func(v []string) error {
					if len(v) == 0 {
						return fmt.Errorf("select at least one platform")
					}
					return nil
				}),
			huh.NewMultiSelect[string]().
				Title("Select WeKnora integration components").
				Description("Defaults match the legacy full setup.").
				Options(
					huh.NewOption("MCP server config", string(ComponentMCP)),
					huh.NewOption("Memory lifecycle hooks", string(ComponentMemoryHooks)),
					huh.NewOption("Memory instruction rules", string(ComponentMemoryRules)),
				).
				Value(&selectedComponents).
				Validate(func(v []string) error {
					if len(v) == 0 {
						return fmt.Errorf("select at least one component")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewInput().
				Title(memoryinternal.T("setup.server_url")).
				Placeholder("http://localhost:8080").
				Value(&result.ServerURL),
			huh.NewInput().
				Title(memoryinternal.T("setup.api_key")).
				Placeholder("sk-...").
				Value(&result.APIKey).
				EchoMode(huh.EchoModePassword),
			huh.NewInput().
				Title(memoryinternal.T("setup.mcp_server_path")).
				Description(memoryinternal.T("setup.mcp_server_desc")).
				Placeholder("/path/to/weknora-mcp-server/main.py").
				Value(&result.McpServerPath),
		),
	).WithAccessible(IsAccessible())

	if err := form1.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return InteractiveResult{}, nil
		}
		return InteractiveResult{}, err
	}
	for _, c := range selectedComponents {
		result.Components = append(result.Components, Component(c))
	}

	fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.connecting"))
	kbs, fetchErr := fetchKBs(result.ServerURL, result.APIKey)

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
		).WithAccessible(IsAccessible())

		if err := form2.Run(); err != nil {
			if err == huh.ErrUserAborted {
				return InteractiveResult{}, nil
			}
			return InteractiveResult{}, err
		}
		result.KBIDs = selected
		return result, nil
	}

	var manualInput string
	if fetchErr != nil {
		errMsg := fetchErr.Error()
		if result.APIKey != "" && len(result.APIKey) > 8 {
			errMsg = strings.ReplaceAll(errMsg, result.APIKey, result.APIKey[:4]+"...")
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
	).WithAccessible(IsAccessible())

	if err := form2.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return InteractiveResult{}, nil
		}
		return InteractiveResult{}, err
	}
	if manualInput != "" {
		result.KBIDs = SplitCommaSeparated(manualInput)
	}
	return result, nil
}

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
