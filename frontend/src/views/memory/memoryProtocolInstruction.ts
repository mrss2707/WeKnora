/**
 * The shared, project-agnostic WeKnora Memory Protocol instruction.
 *
 * This mirrors `GenerateRules()` in `cli/internal/memory/rules.go` so the web UI
 * shows exactly what the CLI injects into an agent's project file. Keep the two
 * in sync: the instruction is agent-facing, English-only, and carries no KB id
 * or token (those live in the MCP setting).
 */
export const MEMORY_PROTOCOL_INSTRUCTION = `<!-- WEKNORA_MEMORY_PROTOCOL -->
## WeKnora Memory Protocol

Long-term memory is provided by the \`weknora\` MCP tools. Call these tools yourself — nothing is injected automatically. Goal: reuse context across sessions and write back durable knowledge; never narrate the current task.

**KB scope:** the knowledge base is configured in your \`weknora\` MCP setting, not in this file. Call \`list_knowledge_bases\` once to read its \`id\`/\`name\`, then pass that value as \`knowledge_base_id\` (written \`<kb>\` below) in every memory call.

**Core loop: Recall → answer → Save anything durable that was not already in memory.**

### 1. Recall — before you answer
**Trigger** (any one): session started, topic changed, or a non-trivial question/task.
**Action:** \`memory_recall(knowledge_base_id="<kb>", query="<2-4 keywords>")\`
Then \`memory_detail(knowledge_base_id="<kb>", memory_id="<id>")\` to read any memory in full.
**Skip only for:** greetings, one-line replies, simple commands, or facts already in the current context.

### 2. Save — before you answer (write-back)
**Trigger:** you learned something durable this turn **and** recall found no matching memory.
**Action:** \`memory_save(knowledge_base_id="<kb>", content="...")\` **before** you reply.
Write content in the right shape:
- Bug fixed → \`cause\` + \`fix\`.
- Decision → \`choice\` + \`why\`.
- Stable fact / preference / constraint → the fact, concrete and specific.
**Never save:** secrets or credentials; transient task state; anything already in code, docs, or context; small talk.
**Quality gate** — all four must hold:
1. One idea per memory.
2. Self-contained: readable without this conversation.
3. Concrete: real names, values, causes — not vague.
4. Future-useful: a later session would benefit.
**Tags:** concept words (\`auth\`, \`api\`, \`db\`), never file names, max 8. The system derives memory type and importance automatically — write good content and do not pass \`memory_type\` or \`importance\`.

### 3. Graph — before you duplicate or edit
**Trigger:** about to save something similar to an existing memory, or to edit one.
**Action:** \`memory_graph(knowledge_base_id="<kb>", memory_id="<id>")\` → check for duplicate, supersedes, or contradiction.

### 4. Status — at session start
**Action:** \`memory_status()\`. If it reports unavailable, skip memory operations and tell the user.
<!-- /WEKNORA_MEMORY_PROTOCOL -->`
