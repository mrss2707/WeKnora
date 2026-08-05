package mcpsetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
	memoryinternal "github.com/Tencent/WeKnora/cli/internal/memory"
)

// Run configures the requested platforms with the selected WeKnora MCP setup components.
func Run(cwd string, platforms []string, opts Options) error {
	kbIDs := FlattenKBIDs(opts.KBIDs)
	components := NormalizeComponents(opts.Components)

	for _, platformName := range platforms {
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

		if err := setupPlatform(cwd, name, kbIDs, opts.ServerURL, opts.APIKey, opts.McpServerPath, opts.DryRun, components); err != nil {
			return err
		}
	}
	return nil
}

func setupPlatform(cwd, platformName string, kbIDs []string, serverURL, apiKey, mcpServerPath string, dryRun bool, components []Component) error {
	platform, err := memoryinternal.NewPlatform(platformName)
	if err != nil {
		return cmdutil.NewFlagError(err)
	}

	if len(kbIDs) == 0 {
		kbIDs = SplitCommaSeparated(os.Getenv("WEKNORA_KB_ID"))
	}
	if serverURL == "" {
		serverURL = os.Getenv("WEKNORA_BASE_URL")
	}
	if serverURL == "" {
		serverURL = os.Getenv("WEKNORA_HOST")
	}
	if apiKey == "" {
		apiKey = os.Getenv("WEKNORA_API_KEY")
	}

	fmt.Fprintf(os.Stderr, "\n-- %s --\n", platform.Name())
	fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.scanning"))
	if len(kbIDs) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(memoryinternal.T("setup.kb_detected"), strings.Join(kbIDs, ", ")))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.no_kb"))
	}

	if hasComponent(components, ComponentMCP) {
		if err := writeMCP(cwd, platform, serverURL, apiKey, kbIDs, mcpServerPath, dryRun); err != nil {
			return err
		}
	}

	if hasComponent(components, ComponentMemoryHooks) {
		if err := writeHooks(cwd, platform, serverURL, apiKey, dryRun); err != nil {
			return err
		}
	}

	if hasComponent(components, ComponentMemoryRules) {
		if err := writeRules(cwd, platform, kbIDs, dryRun); err != nil {
			return err
		}
	}

	if !dryRun {
		fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.done"))
	}
	return nil
}

func writeMCP(cwd string, platform memoryinternal.Platform, serverURL, apiKey string, kbIDs []string, mcpServerPath string, dryRun bool) error {
	if mcpServerPath != "" {
		if err := writeCustomMcpConfig(cwd, mcpServerPath, serverURL, apiKey, dryRun); err != nil {
			return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write MCP config")
		}
		return nil
	}

	strat, ok := platform.(memoryinternal.SetupStrategy)
	if !ok {
		return nil
	}
	if err := strat.WriteMCPConfig(cwd, dryRun); err != nil {
		return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write MCP config")
	}
	if dryRun {
		return nil
	}

	mcpConfigPath := platform.MCPConfigPath(cwd)
	mcpEnv := memoryinternal.ReadMcpEnv(mcpConfigPath, "weknora")
	if mcpEnv == nil {
		mcpEnv = make(map[string]string)
	}
	if serverURL != "" {
		mcpEnv["WEKNORA_BASE_URL"] = serverURL
		mcpEnv["WEKNORA_HOST"] = serverURL
		if apiKey != "" {
			mcpEnv["WEKNORA_API_KEY"] = apiKey
		}
	}
	if len(kbIDs) > 0 {
		mcpEnv["WEKNORA_KB_ID"] = kbIDs[0]
	}
	if len(mcpEnv) > 0 {
		if err := memoryinternal.UpsertMcpEnv(mcpConfigPath, "weknora", mcpEnv); err != nil {
			return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write MCP env")
		}
	}
	return nil
}

func writeHooks(cwd string, platform memoryinternal.Platform, serverURL, apiKey string, dryRun bool) error {
	strat, ok := platform.(memoryinternal.SetupStrategy)
	if !ok {
		return nil
	}

	hookEnv := make(map[string]string)
	if serverURL != "" {
		hookEnv["WEKNORA_BASE_URL"] = serverURL
		hookEnv["WEKNORA_HOST"] = serverURL
		if apiKey != "" {
			hookEnv["WEKNORA_API_KEY"] = apiKey
		}
	} else {
		hookEnv = memoryinternal.ReadMcpEnv(platform.MCPConfigPath(cwd), "weknora")
	}
	if err := strat.WriteHooksConfig(cwd, hookEnv, dryRun); err != nil {
		if err == memoryinternal.ErrHooksNotSupported {
			fmt.Fprintf(os.Stderr, "%s\n", memoryinternal.T("setup.no_hooks"))
			return nil
		}
		return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write hooks config")
	}
	return nil
}

func writeRules(cwd string, platform memoryinternal.Platform, kbIDs []string, dryRun bool) error {
	if err := memoryinternal.WriteRulesFile(platform.RulesFilePath(cwd), kbIDs, dryRun); err != nil {
		return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write rules file")
	}
	if extra, ok := platform.(memoryinternal.ExtraRulesPlatform); ok {
		for _, p := range extra.ExtraRulesPaths(cwd) {
			if err := memoryinternal.WriteRulesFile(p, kbIDs, dryRun); err != nil {
				return cmdutil.Wrapf(cmdutil.CodeLocalFileIO, err, "write extra rules file")
			}
		}
	}
	return nil
}

func writeCustomMcpConfig(cwd, mcpServerPath, serverURL, apiKey string, dryRun bool) error {
	python, ok := ResolvePythonFromMainpy(mcpServerPath)
	if !ok {
		fmt.Fprintf(os.Stderr, "WARNING: Python '%s' cannot import 'mcp' module. Install with: pip install mcp\n", python)
	}
	mcpConfigPath := filepath.Join(cwd, ".mcp.json")
	env := make(map[string]string)
	if serverURL != "" {
		env["WEKNORA_BASE_URL"] = serverURL
		env["WEKNORA_HOST"] = serverURL
	}
	if apiKey != "" {
		env["WEKNORA_API_KEY"] = apiKey
	}
	return memoryinternal.WriteCustomMcpServer(mcpConfigPath, python, []string{mcpServerPath}, env, dryRun)
}

// ResolvePythonFromMainpy tries to find a Python interpreter near main.py.
func ResolvePythonFromMainpy(mainPyPath string) (string, bool) {
	dir := filepath.Dir(mainPyPath)
	venvPython := filepath.Join(dir, ".venv", "bin", "python")
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython, canImportMcp(venvPython)
	}
	return "python3", canImportMcp("python3")
}

func canImportMcp(python string) bool {
	cmd := exec.Command(python, "-c", "import mcp")
	cmd.Stderr = nil
	return cmd.Run() == nil
}
