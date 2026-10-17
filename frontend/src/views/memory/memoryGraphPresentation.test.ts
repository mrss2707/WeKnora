import { test } from 'node:test'
import assert from 'node:assert/strict'
import { MEMORY_GRAPH_TYPE_CODES, memoryGraphLabel, memoryGraphNodeRadius } from './memoryGraphPresentation'

test('every legal importance and invalid numeric value produces a finite node radius', () => {
  for (const n of [-5, -4, -3, -2, -1, 0, 1, 6, NaN, Infinity, -Infinity]) {
    const radius = memoryGraphNodeRadius(n)
    assert.ok(Number.isFinite(radius))
    assert.ok(radius >= 8 && radius <= 20)
  }
})

test('dense graphs use unique type codes; active labels remain rune-safe', () => {
  assert.equal(new Set(Object.values(MEMORY_GRAPH_TYPE_CODES)).size, 6)
  assert.equal(memoryGraphLabel('complete label', 'fact', true, false), 'FA')
  const shortened = memoryGraphLabel('😊'.repeat(80), 'fact', true, true)
  assert.equal([...shortened].length, 32)
  assert.ok(shortened.endsWith('…'))
  assert.equal(shortened.includes('�'), false)
})
