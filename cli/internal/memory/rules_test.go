package memory

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The memory protocol is agent-facing and always emitted in English targeting
// the built-in HTTP MCP (knowledge_base_id), so every locale yields the same text.
func TestGenerateRulesEnglishRegardlessOfLocale(t *testing.T) {
	for _, locale := range []string{"vi-VN", "en-US", ""} {
		t.Run("locale="+locale, func(t *testing.T) {
			t.Setenv("WEKNORA_LANGUAGE", locale)
			rules := GenerateRules()

			assert.Contains(t, rules, "WEKNORA_MEMORY_PROTOCOL")
			assert.Contains(t, rules, "### 1. Recall")
			assert.Contains(t, rules, "### 2. Save")
			assert.Contains(t, rules, "### 3. Graph")
			assert.Contains(t, rules, "### 4. Status")
		})
	}
}

// Dual-audience: concrete rules for weaker models, a skimmable core loop for
// stronger models, and the built-in HTTP MCP parameter name.
func TestGenerateRulesDualModelDesign(t *testing.T) {
	rules := GenerateRules()

	// Strong-model skim: a one-line core loop up top.
	assert.Contains(t, rules, "Core loop:")
	assert.Contains(t, rules, "Recall → answer → Save")

	// Weak-model scaffolding: explicit triggers, write-back, templates, gate.
	assert.Contains(t, rules, "Trigger")
	assert.Contains(t, rules, "write-back")
	assert.Contains(t, rules, "before** you reply")
	assert.Contains(t, rules, "cause")
	assert.Contains(t, rules, "fix")
	assert.Contains(t, rules, "choice")
	assert.Contains(t, rules, "why")
	assert.Contains(t, rules, "Quality gate")
	assert.Contains(t, rules, "Self-contained")
	assert.Contains(t, rules, "Never save")
	assert.Contains(t, rules, "memory_detail")
	assert.Contains(t, rules, "memory_status()")

	// Built-in HTTP MCP signature; derived params must not be advertised.
	assert.Contains(t, rules, "knowledge_base_id")
	assert.NotContains(t, rules, "kb_id=")
	assert.NotContains(t, rules, "memory_type=")
	assert.NotContains(t, rules, "importance=")
}

// The instruction is shared across every project: no KB id or token is baked in.
// The agent resolves the KB from the MCP setting via list_knowledge_bases and
// passes it as knowledge_base_id, written <kb> throughout.
func TestGenerateRulesIsProjectAgnostic(t *testing.T) {
	rules := GenerateRules()

	// KB/token come from the MCP setting, not the text.
	assert.Contains(t, rules, "MCP setting")
	assert.Contains(t, rules, "list_knowledge_bases")
	assert.Contains(t, rules, "<kb>")

	// Every memory call uses the generic placeholder — not a concrete id.
	assert.Contains(t, rules, `memory_recall(knowledge_base_id="<kb>", query="<2-4 keywords>")`)
	assert.Contains(t, rules, `memory_save(knowledge_base_id="<kb>", content="...")`)
	assert.Contains(t, rules, `memory_detail(knowledge_base_id="<kb>", memory_id="<id>")`)
	assert.Contains(t, rules, `memory_graph(knowledge_base_id="<kb>", memory_id="<id>")`)

	// No hardcoded KB id or "Linked KBs" line.
	assert.NotContains(t, rules, "Linked KBs")
	assert.NotContains(t, rules, "kb_abc")
	assert.NotContains(t, rules, "kb_def")
	assert.NotContains(t, rules, "a44800f4")

	// The placeholder is consistent: no stray concrete-style ids remain.
	assert.NotContains(t, rules, `knowledge_base_id="kb_`)
}

// GenerateRules is deterministic: the same shared block is emitted every time.
func TestGenerateRulesDeterministic(t *testing.T) {
	assert.Equal(t, GenerateRules(), GenerateRules())
}

func TestHasMemoryProtocolRules(t *testing.T) {
	assert.True(t, HasMemoryProtocolRules("<!-- WEKNORA_MEMORY_PROTOCOL -->\ncontent\n<!-- /WEKNORA_MEMORY_PROTOCOL -->"))
	assert.False(t, HasMemoryProtocolRules("regular content without marker"))
	assert.False(t, HasMemoryProtocolRules(""))
}

func TestInjectRulesIdempotent(t *testing.T) {
	existing := "<!-- WEKNORA_MEMORY_PROTOCOL -->\nold content\n<!-- /WEKNORA_MEMORY_PROTOCOL -->"
	result := InjectRules(existing)
	assert.Equal(t, existing, result, "should return unchanged when marker already present")
}

func TestInjectRulesAppends(t *testing.T) {
	result := InjectRules("Some existing content.")
	assert.True(t, strings.Contains(result, "Some existing content."))
	assert.True(t, strings.Contains(result, "WEKNORA_MEMORY_PROTOCOL"))
}
