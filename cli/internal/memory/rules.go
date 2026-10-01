package memory

import (
	"fmt"
	"strings"
)

const (
	rulesMarkerStart = "<!-- WEKNORA_MEMORY_PROTOCOL -->"
	rulesMarkerEnd   = "<!-- /WEKNORA_MEMORY_PROTOCOL -->"
)

// GenerateRules returns the memory protocol rules markdown block.
//
// The block is agent-facing system-prompt content, always emitted in English:
// instruction-following is markedly more reliable in English, and the consumer
// is the model, not the human. It targets the built-in HTTP MCP server (the
// go-forward surface) and uses the knowledge_base_id parameter.
//
// The instruction is deliberately generic and shared across every project. The
// knowledge base and access token are declared in the MCP setting (the endpoint
// scope / .mcp.json), not baked into the text, so one copy works everywhere and
// never goes stale when a project's KB changes. Agents resolve the KB at runtime
// from the MCP scope via list_knowledge_bases and pass it as knowledge_base_id.
//
// Design note: the text is written to work for both weaker models (DeepSeek,
// MiMo, GLM — which need explicit triggers, numbered steps, templates and a
// self-check) and stronger models (Opus, GPT — which skim the core loop and
// apply judgment). Concrete rules and a quality gate serve the former without
// over-constraining the latter. Wrapped in HTML comment markers for idempotent
// injection.
func GenerateRules() string {
	content := rulesContent()
	return fmt.Sprintf("%s\n%s\n%s", rulesMarkerStart, strings.TrimSpace(content), rulesMarkerEnd)
}

// HasMemoryProtocolRules checks if the given file content already contains the memory protocol marker.
func HasMemoryProtocolRules(content string) bool {
	return strings.Contains(content, rulesMarkerStart)
}

// InjectRules appends the rules block to the file content. If the marker already exists,
// returns the content unchanged (idempotent).
func InjectRules(existingContent string) string {
	if HasMemoryProtocolRules(existingContent) {
		return existingContent
	}
	rules := GenerateRules()
	if existingContent == "" {
		return rules + "\n"
	}
	return strings.TrimRight(existingContent, "\n") + "\n\n" + rules + "\n"
}

// rulesContent is the shared, project-agnostic protocol. It carries no KB ids or
// tokens; the <kb> placeholder is resolved by the agent from the MCP setting.
func rulesContent() string {
	return fmt.Sprintf(`## WeKnora Memory Protocol

Long-term memory is provided by the ` + "`weknora`" + ` MCP tools. Call these tools yourself — nothing is injected automatically. Goal: reuse context across sessions and write back durable knowledge; never narrate the current task.

**KB scope:** the knowledge base is configured in your ` + "`weknora`" + ` MCP setting, not in this file. Call ` + "`list_knowledge_bases`" + ` once to read its ` + "`id`" + `/` + "`name`" + `, then pass that value as ` + "`knowledge_base_id`" + ` (written ` + "`<kb>`" + ` below) in every memory call.

**Core loop: Recall → answer → Save anything durable that was not already in memory.**

### 1. Recall — before you answer
**Trigger** (any one): session started, topic changed, or a non-trivial question/task.
**Action:** ` + "`memory_recall(knowledge_base_id=\"<kb>\", query=\"<2-4 keywords>\")`" + `
Then ` + "`memory_detail(knowledge_base_id=\"<kb>\", memory_id=\"<id>\")`" + ` to read any memory in full.
**Skip only for:** greetings, one-line replies, simple commands, or facts already in the current context.

### 2. Save — before you answer (write-back)
**Trigger:** you learned something durable this turn **and** recall found no matching memory.
**Action:** ` + "`memory_save(knowledge_base_id=\"<kb>\", content=\"...\")`" + ` **before** you reply.
Write content in the right shape:
- Bug fixed → ` + "`cause`" + ` + ` + "`fix`" + `.
- Decision → ` + "`choice`" + ` + ` + "`why`" + `.
- Stable fact / preference / constraint → the fact, concrete and specific.
**Never save:** secrets or credentials; transient task state; anything already in code, docs, or context; small talk.
**Quality gate** — all four must hold:
1. One idea per memory.
2. Self-contained: readable without this conversation.
3. Concrete: real names, values, causes — not vague.
4. Future-useful: a later session would benefit.
**Tags:** concept words (` + "`auth`" + `, ` + "`api`" + `, ` + "`db`" + `), never file names, max 8. The system derives memory type and importance automatically — write good content and do not pass ` + "`memory_type`" + ` or ` + "`importance`" + `.

### 3. Graph — before you duplicate or edit
**Trigger:** about to save something similar to an existing memory, or to edit one.
**Action:** ` + "`memory_graph(knowledge_base_id=\"<kb>\", memory_id=\"<id>\")`" + ` → check for duplicate, supersedes, or contradiction.

### 4. Status — at session start
**Action:** ` + "`memory_status()`" + `. If it reports unavailable, skip memory operations and tell the user.`)
}
