import { defineStore } from 'pinia'
import { useAuthStore } from './auth'
import {
  listMemories,
  searchMemories,
  getMemoryGraph,
  getMemoryGraphOverview,
  getMemoryStats,
  getHealthReport,
  getDreamStatus,
  getMemoryHistory,
  triggerDream,
  undoMemoryMerge,
  relinkMemories,
  updateMemory,
  deleteMemory,
  createMemory,
  clampImportance,
} from '@/api/memory/index'
import type {
  AgentMemory,
  MemoryVerdict,
  GraphData,
  MemoryStats,
  HealthReport,
  DreamResult,
  DreamStatus,
  MemoryAuditEntry,
  UpdateMemoryRequest,
} from '@/api/memory/index'
import {
  buildListQuery,
  nextFilter,
  unwrapListItems,
  summarizeBulk,
  type BulkOutcome,
  type MemoryFilterState,
} from './memoryState'

export type { MemoryFilterState, BulkOutcome } from './memoryState'

export type MemorySubTab = 'browse' | 'graph' | 'health' | 'history' | 'dream' | 'mcpconfig'

type MemoryRequestKind = 'list' | 'graph' | 'health' | 'stats' | 'dream' | 'dreamStatus' | 'history'

function memoryCacheScope(kbId: string) {
  const auth = useAuthStore()
  return JSON.stringify([auth.currentUserId, auth.effectiveTenantId, kbId])
}

export const useMemoryStore = defineStore('memory', {
  state: () => ({
    /** Knowledge base the cached data belongs to. */
    kbId: '',
    cacheScope: '',
    requests: { list: 0, graph: 0, health: 0, stats: 0, dream: 0, dreamStatus: 0, history: 0 },

    /** Current page of memories (filtered and paged by the server). */
    memories: [] as AgentMemory[],

    filter: {
      type: '',
      verdict: '',
      tier: undefined as number | undefined,
      keyword: '',
      hybrid: false,
      mineOnly: false,
      page: 1,
      pageSize: 20,
    } as MemoryFilterState,

    pagination: { total: 0, page: 1, pageSize: 20 },

    viewMode: 'table' as 'grid' | 'table',
    activeSubTab: 'browse' as MemorySubTab,
    selectedIds: [] as string[],

    graphData: null as GraphData | null,
    healthReport: null as HealthReport | null,
    stats: null as MemoryStats | null,

    dreamStatus: null as DreamStatus | null,
    lastDreamResult: null as DreamResult | null,
    /** Whether lastDreamResult was a preview (nothing applied). */
    lastDreamPreview: false,

    loading: false,
    graphLoading: false,
    healthLoading: false,
    statsLoading: false,
    dreamerLoading: false,

    /** Audit trail of the knowledge base (server-paged). */
    historyEntries: [] as MemoryAuditEntry[],
    historyLoading: false,
    historyAction: '' as string,
    historyPagination: { page: 1, pageSize: 20, total: 0 },
  }),

  getters: {
    selectedMemories(state): AgentMemory[] {
      return state.memories.filter((m) => state.selectedIds.includes(m.id))
    },
    hasSelection(state): boolean {
      return state.selectedIds.length > 0
    },
    hasMoreHistory(state): boolean {
      return state.historyEntries.length < state.historyPagination.total
    },
  },

  actions: {
    isCurrentRequest(resource: MemoryRequestKind, request: number, kbId: string) {
      return this.requests[resource] === request && this.cacheScope === memoryCacheScope(kbId)
    },

    /** Drops cached data when switching knowledge bases. */
    useKnowledgeBase(kbId: string) {
      const scope = memoryCacheScope(kbId)
      if (this.kbId === kbId && this.cacheScope === scope) return
      this.kbId = kbId
      this.cacheScope = scope
      for (const resource of Object.keys(this.requests) as (keyof typeof this.requests)[]) this.requests[resource]++
      this.loading = this.graphLoading = this.healthLoading = this.statsLoading = this.historyLoading = this.dreamerLoading = false
      this.pagination = { total: 0, page: 1, pageSize: this.filter.pageSize }
      this.historyPagination = { total: 0, page: 1, pageSize: 20 }
      this.memories = []
      this.selectedIds = []
      this.graphData = null
      this.healthReport = null
      this.stats = null
      this.dreamStatus = null
      this.lastDreamResult = null
      this.historyEntries = []
      this.filter = nextFilter(this.filter, { type: '', verdict: '', tier: undefined, keyword: '', mineOnly: false })
    },

    // -----------------------------------------------------------------------
    // Browse
    // -----------------------------------------------------------------------

    async loadMemories(kbId: string) {
      this.useKnowledgeBase(kbId)
      const request = ++this.requests.list
      const query = buildListQuery(kbId, this.filter)
      this.loading = true
      try {
        if (this.filter.hybrid && query.keyword) {
          const resp = await searchMemories({ ...query, q: query.keyword, limit: query.page_size })
          if (!this.isCurrentRequest('list', request, kbId)) return
          this.memories = unwrapListItems<AgentMemory>(resp.data ?? [])
          this.pagination = { total: this.memories.length, page: 1, pageSize: query.page_size }
        } else {
          const resp = await listMemories(query)
          if (!this.isCurrentRequest('list', request, kbId)) return
          this.memories = unwrapListItems<AgentMemory>(resp.data?.items ?? [])
          this.pagination.total = resp.data?.total ?? 0
          this.pagination.page = resp.data?.page ?? query.page
          this.pagination.pageSize = resp.data?.page_size ?? query.page_size
        }
        const visible = new Set(this.memories.map((m) => m.id))
        this.selectedIds = this.selectedIds.filter((id) => visible.has(id))
      } finally {
        if (this.isCurrentRequest('list', request, kbId)) this.loading = false
      }
    },

    /** Changes filters or paging and reloads the page from the server. */
    async setFilter(partial: Partial<MemoryFilterState>) {
      this.filter = nextFilter(this.filter, partial)
      if (this.kbId) await this.loadMemories(this.kbId)
    },

    async createMemory(kbId: string, content: string, opts: { sessionId?: string } = {}) {
      const scope = this.cacheScope
      const result = await createMemory({ kb_id: kbId, content, session_id: opts.sessionId })
      if (this.kbId === kbId && scope === this.cacheScope && scope === memoryCacheScope(kbId)) {
        await this.loadMemories(kbId)
        this.stats = null
      }
      return result.data
    },

    /**
     * Patches one memory. `expected_updated_at` is always sent, so a change
     * made by someone else meanwhile is rejected with 409 instead of lost.
     */
    async patchMemory(memory: AgentMemory, patch: UpdateMemoryRequest): Promise<AgentMemory> {
      const resp = await updateMemory(memory.id, { expected_updated_at: memory.updated_at, ...patch })
      const updated = resp.data
      this.replaceMemory(updated)
      return updated
    },

    replaceMemory(updated: AgentMemory) {
      const idx = this.memories.findIndex((m) => m.id === updated.id)
      if (idx !== -1) this.memories[idx] = { ...this.memories[idx], ...updated }
    },

    async deleteMemory(id: string) {
      const scope = this.cacheScope
      await deleteMemory(id)
      if (scope !== this.cacheScope || scope !== memoryCacheScope(this.kbId)) return
      this.memories = this.memories.filter((m) => m.id !== id)
      this.selectedIds = this.selectedIds.filter((s) => s !== id)
      this.pagination.total = Math.max(0, this.pagination.total - 1)
      this.stats = null
    },

    // -----------------------------------------------------------------------
    // Bulk operations (each memory is locked on the version shown)
    // -----------------------------------------------------------------------

    async bulkPatch(build: (m: AgentMemory) => UpdateMemoryRequest | null): Promise<BulkOutcome> {
      const scope = this.cacheScope
      const targets = this.selectedMemories
        .map((m) => ({ m, patch: build(m) }))
        .filter((t): t is { m: AgentMemory; patch: UpdateMemoryRequest } => t.patch !== null)
      const results = await Promise.allSettled(
        targets.map(({ m, patch }) => updateMemory(m.id, { expected_updated_at: m.updated_at, ...patch })),
      )
      if (scope !== this.cacheScope || scope !== memoryCacheScope(this.kbId)) return summarizeBulk(results)
      results.forEach((r) => {
        if (r.status === 'fulfilled' && r.value?.data) this.replaceMemory(r.value.data)
      })
      const failed = new Set(targets.filter((_, i) => results[i].status === 'rejected').map(({ m }) => m.id))
      this.selectedIds = this.selectedIds.filter((id) => failed.has(id))
      return summarizeBulk(results)
    },

    bulkChangeVerdict(verdict: MemoryVerdict) {
      return this.bulkPatch(() => ({ verdict }))
    },

    bulkChangeImportance(delta: number) {
      return this.bulkPatch((m) => {
        const importance = clampImportance(m.importance + delta)
        return importance === m.importance ? null : { importance }
      })
    },

    async bulkDelete(): Promise<BulkOutcome> {
      const scope = this.cacheScope
      const ids = [...this.selectedIds]
      const results = await Promise.allSettled(ids.map((id) => deleteMemory(id)))
      if (scope !== this.cacheScope || scope !== memoryCacheScope(this.kbId)) return summarizeBulk(results)
      const ok = new Set(ids.filter((_, i) => results[i].status === 'fulfilled'))
      this.memories = this.memories.filter((m) => !ok.has(m.id))
      this.pagination.total = Math.max(0, this.pagination.total - ok.size)
      this.selectedIds = ids.filter((id) => !ok.has(id))
      this.stats = null
      return summarizeBulk(results)
    },

    // -----------------------------------------------------------------------
    // Graph, health, stats
    // -----------------------------------------------------------------------

    /**
     * Loads the knowledge base overview, or — with a memory id — that memory's
     * neighbourhood `depth` hops deep.
     */
    async loadGraph(kbId: string, memoryId?: string, depth = 1) {
      if (!kbId && !memoryId) return
      this.useKnowledgeBase(kbId)
      const request = ++this.requests.graph
      this.graphLoading = true
      try {
        const resp = memoryId ? await getMemoryGraph(memoryId, { depth }) : await getMemoryGraphOverview(kbId)
        if (this.isCurrentRequest('graph', request, kbId)) this.graphData = resp.data ?? null
      } finally {
        if (this.isCurrentRequest('graph', request, kbId)) this.graphLoading = false
      }
    },

    async relink(kbId: string) {
      const resp = await relinkMemories(kbId)
      this.graphData = null
      return resp.data
    },

    async loadHealth(kbId: string) {
      this.useKnowledgeBase(kbId)
      const request = ++this.requests.health
      this.healthLoading = true
      try {
        const resp = await getHealthReport(kbId)
        if (this.isCurrentRequest('health', request, kbId)) this.healthReport = resp.data ?? null
      } finally {
        if (this.isCurrentRequest('health', request, kbId)) this.healthLoading = false
      }
    },

    async loadStats(kbId: string) {
      this.useKnowledgeBase(kbId)
      const request = ++this.requests.stats
      this.statsLoading = true
      try {
        const resp = await getMemoryStats(kbId)
        if (this.isCurrentRequest('stats', request, kbId)) this.stats = resp.data ?? null
      } finally {
        if (this.isCurrentRequest('stats', request, kbId)) this.statsLoading = false
      }
    },

    // -----------------------------------------------------------------------
    // Dreamer
    // -----------------------------------------------------------------------

    async loadDreamStatus(kbId: string) {
      this.useKnowledgeBase(kbId)
      const request = ++this.requests.dreamStatus
      const resp = await getDreamStatus(kbId)
      if (this.isCurrentRequest('dreamStatus', request, kbId)) this.dreamStatus = resp.data ?? null
    },

    /** Runs a dream pass; `preview` returns proposals without applying anything. */
    async runDreamer(kbId: string, preview = false) {
      this.useKnowledgeBase(kbId)
      const request = ++this.requests.dream
      this.dreamerLoading = true
      try {
        const resp = await triggerDream({ kb_id: kbId, dry_run: preview })
        if (!this.isCurrentRequest('dream', request, kbId)) return
        this.lastDreamResult = resp.data ?? null
        this.lastDreamPreview = preview
        if (!preview) {
          await Promise.allSettled([this.loadDreamStatus(kbId), this.loadMemories(kbId)])
        }
      } finally {
        if (this.isCurrentRequest('dream', request, kbId)) this.dreamerLoading = false
      }
    },

    async undoMerge(memoryId: string) {
      const resp = await undoMemoryMerge(memoryId)
      if (this.kbId) await this.loadMemories(this.kbId)
      return resp.data
    },

    // -----------------------------------------------------------------------
    // History (audit trail)
    // -----------------------------------------------------------------------

    async loadHistory(kbId: string, more = false) {
      this.useKnowledgeBase(kbId)
      const request = ++this.requests.history
      this.historyLoading = true
      try {
        const page = more ? this.historyPagination.page + 1 : 1
        const resp = await getMemoryHistory({
          kb_id: kbId,
          action: this.historyAction || undefined,
          page,
          page_size: this.historyPagination.pageSize,
        })
        if (!this.isCurrentRequest('history', request, kbId)) return
        const items = resp.data?.items ?? []
        this.historyEntries = more ? [...this.historyEntries, ...items] : items
        this.historyPagination.page = page
        this.historyPagination.total = resp.data?.total ?? 0
      } finally {
        if (this.isCurrentRequest('history', request, kbId)) this.historyLoading = false
      }
    },

    async setHistoryAction(kbId: string, action: string) {
      this.historyAction = action
      await this.loadHistory(kbId)
    },

    // -----------------------------------------------------------------------
    // View state
    // -----------------------------------------------------------------------

    toggleViewMode() {
      this.viewMode = this.viewMode === 'grid' ? 'table' : 'grid'
    },

    setSubTab(tab: MemorySubTab) {
      this.activeSubTab = tab
    },

    toggleSelectAll() {
      this.selectedIds = this.selectedIds.length === this.memories.length ? [] : this.memories.map((m) => m.id)
    },

    toggleSelect(id: string) {
      const idx = this.selectedIds.indexOf(id)
      if (idx === -1) this.selectedIds.push(id)
      else this.selectedIds.splice(idx, 1)
    },
  },
})
