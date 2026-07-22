package memorycmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
