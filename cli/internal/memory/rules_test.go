package memory

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateRulesViVN(t *testing.T) {
	// Ensure vi-VN locale
	t.Setenv("WEKNORA_LANGUAGE", "vi-VN")
	rules := GenerateRules([]string{"kb_test123"})

	assert.Contains(t, rules, "WEKNORA_MEMORY_PROTOCOL")
	assert.Contains(t, rules, "Thu hồi (Recall)")
	assert.Contains(t, rules, "Lưu (Save)")
	assert.Contains(t, rules, "Đồ thị (Graph)")
	assert.Contains(t, rules, "Trạng thái (Status)")
	assert.Contains(t, rules, "kb_test123")
}

func TestGenerateRulesEnUS(t *testing.T) {
	t.Setenv("WEKNORA_LANGUAGE", "en-US")
	rules := GenerateRules([]string{"kb_test456"})

	assert.Contains(t, rules, "WEKNORA_MEMORY_PROTOCOL")
	assert.Contains(t, rules, "Recall")
	assert.Contains(t, rules, "Save")
	assert.Contains(t, rules, "Graph")
	assert.Contains(t, rules, "Status")
	assert.Contains(t, rules, "kb_test456")
}

func TestGenerateRulesMultiKB(t *testing.T) {
	t.Setenv("WEKNORA_LANGUAGE", "en-US")
	rules := GenerateRules([]string{"kb_abc", "kb_def"})

	assert.Contains(t, rules, "Linked KBs: kb_abc, kb_def")
	assert.Contains(t, rules, `memory_recall(kb_id="kb_abc"`)
	assert.Contains(t, rules, `memory_recall(kb_id="kb_def"`)
	assert.Contains(t, rules, " and\n")
}

func TestGenerateRulesEmptyKB(t *testing.T) {
	t.Setenv("WEKNORA_LANGUAGE", "en-US")
	rules := GenerateRules([]string{})

	assert.Contains(t, rules, "Linked KBs: (none)")
	assert.Contains(t, rules, `memory_recall(kb_id="<your-kb-id>"`)
}

func TestGenerateRulesViVNEmptyKB(t *testing.T) {
	t.Setenv("WEKNORA_LANGUAGE", "vi-VN")
	rules := GenerateRules([]string{})

	assert.Contains(t, rules, "Linked KBs: (không có)")
	assert.Contains(t, rules, `memory_recall(kb_id="<your-kb-id>"`)
}

func TestHasMemoryProtocolRules(t *testing.T) {
	assert.True(t, HasMemoryProtocolRules("<!-- WEKNORA_MEMORY_PROTOCOL -->\ncontent\n<!-- /WEKNORA_MEMORY_PROTOCOL -->"))
	assert.False(t, HasMemoryProtocolRules("regular content without marker"))
	assert.False(t, HasMemoryProtocolRules(""))
}

func TestInjectRulesIdempotent(t *testing.T) {
	existing := "<!-- WEKNORA_MEMORY_PROTOCOL -->\nold content\n<!-- /WEKNORA_MEMORY_PROTOCOL -->"
	result := InjectRules(existing, []string{"kb_test"})
	assert.Equal(t, existing, result, "should return unchanged when marker already present")
}

func TestInjectRulesAppends(t *testing.T) {
	result := InjectRules("Some existing content.", []string{"kb_test"})
	assert.True(t, strings.Contains(result, "Some existing content."))
	assert.True(t, strings.Contains(result, "WEKNORA_MEMORY_PROTOCOL"))
}
