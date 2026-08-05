package mcpcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
)

func TestSetupCommandRegisteredWithFlags(t *testing.T) {
	cmd := NewCmd(cmdutil.New())
	setup, _, err := cmd.Find([]string{"setup"})
	require.NoError(t, err)
	require.Equal(t, "setup", setup.Name())

	for _, flag := range []string{"platform", "dry-run", "kb", "server-url", "api-key"} {
		assert.NotNil(t, setup.Flags().Lookup(flag), "missing --%s", flag)
	}
}

func TestSetupDryRunSucceedsWithoutAuth(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)

	cmd := NewCmd(cmdutil.New())
	cmd.SetArgs([]string{"setup", "--platform", "claude-code", "--dry-run"})
	require.NoError(t, cmd.Execute())
	assert.NoFileExists(t, dir+"/.mcp.json")
}

func TestSetupNonTTYRequiresPlatform(t *testing.T) {
	cmd := NewCmd(cmdutil.New())
	cmd.SetArgs([]string{"setup"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag \"--platform\" not set")
}

func TestSetupInvalidPlatformReturnsFlagError(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)

	cmd := NewCmd(cmdutil.New())
	cmd.SetArgs([]string{"setup", "--platform", "bogus", "--dry-run"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func TestSetupAgentHelp(t *testing.T) {
	t.Setenv("WEKNORA_AGENT_HELP", "1")
	cmd := NewCmd(cmdutil.New())
	setup, _, err := cmd.Find([]string{"setup"})
	require.NoError(t, err)

	var out bytes.Buffer
	setup.SetOut(&out)
	require.NoError(t, setup.Help())

	var help struct {
		UsedFor       string   `json:"used_for"`
		Output        string   `json:"output"`
		RequiredFlags []string `json:"required_flags"`
		Examples      []string `json:"examples"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &help))
	assert.Contains(t, help.UsedFor, "MCP integration")
	assert.Contains(t, help.RequiredFlags, "--platform")
	assert.NotEmpty(t, help.Output)
	assert.NotEmpty(t, help.Examples)
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(old)) })
}
