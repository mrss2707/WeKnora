// Pure helpers behind the memory store, kept free of Vue/Pinia so they can be
// unit-tested with node:test.

export interface MemoryFilterState {
  type: string
  verdict: string
  tier: number | undefined
  keyword: string
  /** Only the caller's own memories. */
  mineOnly: boolean
  page: number
  pageSize: number
}

export interface MemoryListQuery {
  kb_id: string
  page: number
  page_size: number
  type?: string
  verdict?: string
  tier?: number
  keyword?: string
  author?: string
}

/** Builds the server-side list query: filters, keyword and paging are all applied by the backend. */
export function buildListQuery(kbId: string, f: MemoryFilterState): MemoryListQuery {
  const q: MemoryListQuery = { kb_id: kbId, page: f.page, page_size: f.pageSize }
  if (f.type) q.type = f.type
  if (f.verdict) q.verdict = f.verdict
  if (f.tier !== undefined && f.tier !== null) q.tier = f.tier
  const keyword = f.keyword.trim()
  if (keyword) q.keyword = keyword
  if (f.mineOnly) q.author = 'me'
  return q
}

/**
 * Applies a filter change. Any change other than paging returns to page 1, so a
 * narrower filter never lands on an empty page past its last result.
 */
export function nextFilter(current: MemoryFilterState, partial: Partial<MemoryFilterState>): MemoryFilterState {
  const next = { ...current, ...partial }
  if (!('page' in partial)) next.page = 1
  return next
}

/** List rows may come wrapped as search results ({memory, score}); unwrap them. */
export function unwrapListItems<T extends { id: string }>(items: unknown[]): T[] {
  return items.map((item) => {
    const row = item as { memory?: T; score?: number; stale_days?: number; is_stale?: boolean }
    if (row && row.memory) {
      return { ...row.memory, score: row.score, stale_days: row.stale_days, is_stale: row.is_stale } as T
    }
    return item as T
  })
}

/** HTTP status of a rejected request (the request layer attaches `status`). */
export function errorStatus(err: unknown): number {
  const e = err as { status?: number; $httpStatus?: number } | null
  return e?.status ?? e?.$httpStatus ?? 0
}

/** True when a write was rejected because the memory changed meanwhile. */
export function isConflict(err: unknown): boolean {
  return errorStatus(err) === 409
}

export interface BulkOutcome {
  succeeded: number
  failed: number
  conflicts: number
}

/** Summarizes Promise.allSettled results of a bulk operation. */
export function summarizeBulk(results: PromiseSettledResult<unknown>[]): BulkOutcome {
  let succeeded = 0
  let conflicts = 0
  for (const r of results) {
    if (r.status === 'fulfilled') succeeded++
    else if (isConflict(r.reason)) conflicts++
  }
  return { succeeded, failed: results.length - succeeded, conflicts }
}
