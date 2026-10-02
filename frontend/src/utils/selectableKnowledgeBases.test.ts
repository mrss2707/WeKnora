import assert from 'node:assert/strict'
import test from 'node:test'
import {
  mergeSelectableKnowledgeBases,
  toKnowledgeBaseSelectOption,
} from './selectableKnowledgeBases'

const share = (id: string, permission: string, org = 'Org', type = 'document') => ({
  knowledge_base: { id, name: `kb-${id}`, type },
  permission,
  org_name: org,
})

test('includes shared KBs after owned ones', () => {
  const out = mergeSelectableKnowledgeBases([{ id: 'a', name: 'A' }], [share('b', 'viewer')])
  assert.deepEqual(out.map((k) => [k.id, k.isShared]), [['a', false], ['b', true]])
})

test('owned wins over a shared duplicate', () => {
  const out = mergeSelectableKnowledgeBases([{ id: 'a', name: 'A' }], [share('a', 'admin')])
  assert.equal(out.length, 1)
  assert.equal(out[0].isShared, false)
})

test('repeated shares collapse to the highest permission', () => {
  const out = mergeSelectableKnowledgeBases([], [share('b', 'viewer', 'X'), share('b', 'editor', 'Y')])
  assert.equal(out.length, 1)
  assert.equal(out[0].permission, 'editor')
  assert.equal(out[0].orgName, 'Y')
})

test('minPermission filters shares but never owned KBs', () => {
  const out = mergeSelectableKnowledgeBases(
    [{ id: 'a' }],
    [share('b', 'viewer'), share('c', 'editor')],
    { minPermission: 'editor' },
  )
  assert.deepEqual(out.map((k) => k.id), ['a', 'c'])
})

test('type filter treats a missing type as document and skips null shares', () => {
  const out = mergeSelectableKnowledgeBases(
    [{ id: 'a' }, { id: 'f', type: 'faq' }],
    [share('b', 'viewer', 'O', 'faq'), { knowledge_base: null, permission: 'admin' }],
    { types: ['document'] },
  )
  assert.deepEqual(out.map((k) => k.id), ['a'])
})

test('select option labels shared KBs with their organization', () => {
  const [kb] = mergeSelectableKnowledgeBases([], [share('b', 'viewer', 'Acme')])
  assert.deepEqual(toKnowledgeBaseSelectOption(kb), { label: 'kb-b (Acme)', value: 'b' })
})
