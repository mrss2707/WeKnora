import assert from 'node:assert/strict'
import test from 'node:test'
import { MEMORY_PROTOCOL_INSTRUCTION as text } from './memoryProtocolInstruction'

test('instruction is wrapped in idempotent-injection markers', () => {
  assert.ok(text.startsWith('<!-- WEKNORA_MEMORY_PROTOCOL -->'))
  assert.ok(text.endsWith('<!-- /WEKNORA_MEMORY_PROTOCOL -->'))
})

test('instruction documents every Memory V2 MCP tool', () => {
  for (const tool of ['memory_recall', 'memory_save', 'memory_detail', 'memory_graph', 'memory_status']) {
    assert.ok(text.includes(tool), `missing ${tool}`)
  }
})

test('instruction keeps the dual-model structure', () => {
  assert.ok(text.includes('**Core loop: Recall → answer → Save'))
  assert.ok(text.includes('**Trigger'))
  assert.ok(text.includes('**Action:'))
  assert.ok(text.includes('**Quality gate**'))
})

test('instruction is project-agnostic: placeholder KB, no ids or tokens', () => {
  assert.ok(text.includes('knowledge_base_id="<kb>"'))
  assert.ok(text.includes('list_knowledge_bases'))
  assert.doesNotMatch(text, /mcp_[A-Za-z0-9]{8,}/, 'must not embed an endpoint token')
  assert.doesNotMatch(text, /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i, 'must not embed a UUID')
})

test('instruction is English-only (agent-facing)', () => {
  const nonAscii = text.replace(/[→—…]/g, '').match(/[^\x00-\x7F]/g)
  assert.equal(nonAscii, null)
})
