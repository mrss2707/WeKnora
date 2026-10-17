import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildListQuery,
  nextFilter,
  unwrapListItems,
  isConflict,
  summarizeBulk,
  type MemoryFilterState,
} from './memoryState.ts'

const base: MemoryFilterState = {
  type: '',
  verdict: '',
  tier: undefined,
  keyword: '',
  mineOnly: false,
  page: 3,
  pageSize: 20,
}

test('list query sends every filter to the server', () => {
  const q = buildListQuery('kb-1', { ...base, type: 'decision', verdict: 'gotcha', tier: 0, keyword: '  redis  ', mineOnly: true })
  assert.deepEqual(q, {
    kb_id: 'kb-1',
    page: 3,
    page_size: 20,
    type: 'decision',
    verdict: 'gotcha',
    tier: 0,
    keyword: 'redis',
    author: 'me',
  })
})

test('list query omits empty filters', () => {
  assert.deepEqual(buildListQuery('kb-1', base), { kb_id: 'kb-1', page: 3, page_size: 20 })
})

test('changing a filter returns to page 1, paging keeps the filter', () => {
  assert.equal(nextFilter(base, { type: 'fact' }).page, 1)
  assert.equal(nextFilter(base, { pageSize: 50 }).page, 1)
  const paged = nextFilter({ ...base, type: 'fact' }, { page: 4 })
  assert.equal(paged.page, 4)
  assert.equal(paged.type, 'fact')
})

test('search-shaped rows are unwrapped', () => {
  const rows = unwrapListItems<{ id: string; score?: number }>([
    { memory: { id: 'a' }, score: 0.9, stale_days: 0, is_stale: false },
    { id: 'b' },
  ])
  assert.deepEqual(rows.map((r) => r.id), ['a', 'b'])
  assert.equal(rows[0].score, 0.9)
})

test('bulk summary counts conflicts separately', () => {
  const conflict = Object.assign(new Error('changed'), { status: 409 })
  const out = summarizeBulk([
    { status: 'fulfilled', value: 1 },
    { status: 'rejected', reason: conflict },
    { status: 'rejected', reason: { status: 500 } },
  ])
  assert.deepEqual(out, { succeeded: 1, failed: 2, conflicts: 1 })
  assert.ok(isConflict(conflict))
  assert.ok(!isConflict(null))
})
