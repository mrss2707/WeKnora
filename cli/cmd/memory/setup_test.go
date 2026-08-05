package memorycmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/cli/internal/cmdutil"
)

func TestFlattenKBIDs_SingleValue(t *testing.T) {
	result := flattenKBIDs([]string{"kb_abc"})
	assert.Equal(t, []string{"kb_abc"}, result)
}

func TestFlattenKBIDs_MultipleFlags(t *testing.T) {
	result := flattenKBIDs([]string{"kb_abc", "kb_def"})
	assert.Equal(t, []string{"kb_abc", "kb_def"}, result)
}

func TestFlattenKBIDs_CommaSeparated(t *testing.T) {
	result := flattenKBIDs([]string{"kb_abc,kb_def"})
	assert.Equal(t, []string{"kb_abc", "kb_def"}, result)
}

func TestFlattenKBIDs_Mixed(t *testing.T) {
	result := flattenKBIDs([]string{"kb_abc,kb_def", "kb_ghi"})
	assert.Equal(t, []string{"kb_abc", "kb_def", "kb_ghi"}, result)
}

func TestFlattenKBIDs_Deduplication(t *testing.T) {
	result := flattenKBIDs([]string{"kb_abc", "kb_abc"})
	assert.Equal(t, []string{"kb_abc"}, result)
}

func TestFlattenKBIDs_DeduplicationWithCommas(t *testing.T) {
	result := flattenKBIDs([]string{"kb_abc,kb_def", "kb_abc"})
	assert.Equal(t, []string{"kb_abc", "kb_def"}, result)
}

func TestFlattenKBIDs_EmptyInput(t *testing.T) {
	result := flattenKBIDs(nil)
	assert.Nil(t, result)
}

func TestFlattenKBIDs_EmptyStrings(t *testing.T) {
	result := flattenKBIDs([]string{""})
	assert.Nil(t, result)
}

func TestFlattenKBIDs_Whitespace(t *testing.T) {
	result := flattenKBIDs([]string{" kb_abc , kb_def "})
	assert.Equal(t, []string{"kb_abc", "kb_def"}, result)
}

func TestSplitCommaSeparated_Basic(t *testing.T) {
	result := splitCommaSeparated("kb_abc,kb_def")
	assert.Equal(t, []string{"kb_abc", "kb_def"}, result)
}

func TestSplitCommaSeparated_Single(t *testing.T) {
	result := splitCommaSeparated("kb_abc")
	assert.Equal(t, []string{"kb_abc"}, result)
}

func TestSplitCommaSeparated_Empty(t *testing.T) {
	result := splitCommaSeparated("")
	assert.Nil(t, result)
}

func TestSplitCommaSeparated_Whitespace(t *testing.T) {
	result := splitCommaSeparated(" kb_abc , kb_def ")
	assert.Equal(t, []string{"kb_abc", "kb_def"}, result)
}

func TestSetupLegacyWrapperDryRun(t *testing.T) {
	cmd := NewCmdSetup(cmdutil.New())
	cmd.SetArgs([]string{"--platform", "claude-code", "--dry-run"})
	require.NoError(t, cmd.Execute())
}

func TestSetupLegacyWrapperDeprecationWarning(t *testing.T) {
	cmd := NewCmd(cmdutil.New())
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"setup", "--platform", "claude-code", "--dry-run"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, errBuf.String(), "deprecated")
	assert.Contains(t, errBuf.String(), "weknora mcp setup")
}

func TestSetupLegacyWrapperVisibleInHelp(t *testing.T) {
	cmd := NewCmd(cmdutil.New())
	var out bytes.Buffer
	cmd.SetOut(&out)

	require.NoError(t, cmd.Help())
	assert.Contains(t, out.String(), "setup")
	assert.Contains(t, out.String(), "deprecated")
}
