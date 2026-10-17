import assert from 'node:assert/strict'
import test from 'node:test'
import { auditChangedFields } from './memoryAudit.ts'

const image = {
  id: 'm1',
  kb_id: 'kb',
  content: 'Postgres is the primary database',
  memory_type: 'fact',
  importance: 2,
  tier: 2,
  verdict: 'none' as const,
  tags: ['postgres'],
}

test('changed fields lists only what differs', () => {
  assert.deepEqual(auditChangedFields(image, { ...image, verdict: 'gotcha', importance: 3 }), ['verdict', 'importance'])
  assert.deepEqual(auditChangedFields(image, { ...image, tags: ['postgres', 'db'] }), ['tags'])
  assert.deepEqual(auditChangedFields(image, { ...image, tags: null }), ['tags'])
  assert.deepEqual(auditChangedFields(image, { ...image }), [])
})

test('creation and deletion have no field diff', () => {
  assert.deepEqual(auditChangedFields(undefined, image), [])
  assert.deepEqual(auditChangedFields(image, null), [])
})
