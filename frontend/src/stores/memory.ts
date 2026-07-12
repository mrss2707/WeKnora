import { defineStore } from 'pinia'
import {
  listMemories,
  getMemoryGraph,
  getHealthReport,
  triggerDream,
  updateMemory,
  deleteMemory,
} from '@/api/memory/index'
import type {
  AgentMemory,
  MemoryListParams,
  MemoryVerdict,
  GraphData,
  HealthReport,
  DreamResult,
} from '@/api/memory/index'

export interface MemoryFilterState {
  type: string
  verdict: string
  tier: number | undefined
  keyword: string
  page: number
  pageSize: number
}

export const useMemoryStore = defineStore('memory', {
  state: () => ({
    /** Flat list of memories for the current KB. */
    memories: [] as AgentMemory[],

    /** Active filter / pagination state. */
    filter: {
      type: '',
      verdict: '',
      tier: undefined as number | undefined,
      keyword: '',
      page: 1,
      pageSize: 20,
    } as MemoryFilterState,

    /** Server-side pagination metadata. */
    pagination: {
      total: 0,
      page: 1,
      pageSize: 20,
    },

    /** Current display mode. */
    viewMode: 'table' as 'grid' | 'table',

    /** Active sub-tab within the Memory tab. */
    activeSubTab: 'browse' as 'browse' | 'graph' | 'health' | 'history',

    /** IDs selected for bulk operations. */
    selectedIds: [] as string[],

    /** Graph data (cached when loaded). */
    graphData: null as GraphData | null,

    /** Health report (cached when loaded). */
    healthReport: null as HealthReport | null,

    /** Dream result from the last run. */
    lastDreamResult: null as DreamResult | null,

    /** Loading indicator. */
    loading: false,

    /** Graph loading indicator. */
    graphLoading: false,

    /** Health loading indicator. */
    healthLoading: false,

    /** Dreamer loading indicator. */
    dreamerLoading: false,
  }),

  getters: {
    /** Memories filtered by the current `filter` state (client-side keyword filter on top of server results). */
    filteredMemories(state): AgentMemory[] {
      let result = state.memories

      if (state.filter.keyword) {
        const kw = state.filter.keyword.toLowerCase()
        result = result.filter(
          (m) =>
            m.content.toLowerCase().includes(kw) ||
            m.memory_type.toLowerCase().includes(kw)
        )
      }
      if (state.filter.type) {
        result = result.filter((m) => m.memory_type === state.filter.type)
      }
      if (state.filter.verdict) {
        result = result.filter((m) => m.verdict === state.filter.verdict)
      }
      if (state.filter.tier !== undefined && state.filter.tier !== null) {
        result = result.filter((m) => m.tier === state.filter.tier)
      }

      return result
    },

    /** Memories matching the currently selected IDs. */
    selectedMemories(state): AgentMemory[] {
      return state.memories.filter((m) => state.selectedIds.includes(m.id))
    },

    /** Whether any memories are selected. */
    hasSelection(state): boolean {
      return state.selectedIds.length > 0
    },
  },

  actions: {
    // -----------------------------------------------------------------------
    // Data fetching
    // -----------------------------------------------------------------------

    async loadMemories(kbId: string) {
      this.loading = true
      try {
        const params: MemoryListParams = {
          kb_id: kbId,
          page: this.filter.page,
          page_size: this.filter.pageSize,
        }
        if (this.filter.type) params.type = this.filter.type
        if (this.filter.verdict) params.verdict = this.filter.verdict
        if (this.filter.tier !== undefined) params.tier = this.filter.tier
        if (this.filter.keyword) params.keyword = this.filter.keyword

        const resp = await listMemories(params)
        this.memories = resp.data?.data ?? []
        this.pagination.total = resp.data?.total ?? 0
        this.pagination.page = resp.data?.page ?? this.filter.page
        this.pagination.pageSize = resp.data?.page_size ?? this.filter.pageSize
      } finally {
        this.loading = false
      }
    },

    async loadGraph(kbId: string, memoryId?: string) {
      this.graphLoading = true
      try {
        const id = memoryId || (this.memories.length > 0 ? this.memories[0].id : '')
        if (!id) return
        const resp = await getMemoryGraph(id, { kb_id: kbId, deep: false })
        this.graphData = resp.data?.data ?? null
      } finally {
        this.graphLoading = false
      }
    },

    async loadHealth(kbId: string) {
      this.healthLoading = true
      try {
        const resp = await getHealthReport(kbId)
        this.healthReport = resp.data?.data ?? null
      } finally {
        this.healthLoading = false
      }
    },

    async runDreamer(kbId: string) {
      this.dreamerLoading = true
      try {
        const resp = await triggerDream({ kb_id: kbId })
        this.lastDreamResult = resp.data?.data ?? null
      } finally {
        this.dreamerLoading = false
      }
    },

    // -----------------------------------------------------------------------
    // View mode / sub-tab
    // -----------------------------------------------------------------------

    toggleViewMode() {
      this.viewMode = this.viewMode === 'grid' ? 'table' : 'grid'
    },

    setSubTab(tab: 'browse' | 'graph' | 'health' | 'history') {
      this.activeSubTab = tab
    },

    // -----------------------------------------------------------------------
    // Filters
    // -----------------------------------------------------------------------

    setFilter(partial: Partial<MemoryFilterState>) {
      // Reset to page 1 whenever any filter changes (except page/pageSize).
      if (!('page' in partial) && !('pageSize' in partial)) {
        partial.page = 1
      }
      Object.assign(this.filter, partial)
    },

    // -----------------------------------------------------------------------
    // Bulk operations
    // -----------------------------------------------------------------------

    async bulkChangeVerdict(verdict: MemoryVerdict) {
      await Promise.all(
        this.selectedIds.map((id) => updateMemory(id, { verdict }))
      )
      // Update local state optimistically.
      this.memories.forEach((m) => {
        if (this.selectedIds.includes(m.id)) {
          m.verdict = verdict
        }
      })
      this.selectedIds = []
    },

    async bulkChangeImportance(delta: number) {
      await Promise.all(
        this.selectedIds.map((id) => {
          const mem = this.memories.find((m) => m.id === id)
          if (!mem) return Promise.resolve()
          const newImportance = Math.max(0, Math.min(10, mem.importance + delta))
          return updateMemory(id, { importance: newImportance })
        })
      )
      // Reload to get fresh data.
      this.memories = [...this.memories]
      // Update local state optimistically.
      this.memories.forEach((m) => {
        if (this.selectedIds.includes(m.id)) {
          const newVal = Math.max(0, Math.min(10, m.importance + delta))
          m.importance = newVal
        }
      })
      this.selectedIds = []
    },

    async bulkDelete() {
      await Promise.all(this.selectedIds.map((id) => deleteMemory(id)))
      this.memories = this.memories.filter(
        (m) => !this.selectedIds.includes(m.id)
      )
      this.selectedIds = []
    },

    // -----------------------------------------------------------------------
    // Selection
    // -----------------------------------------------------------------------

    toggleSelectAll() {
      if (this.selectedIds.length === this.memories.length) {
        this.selectedIds = []
      } else {
        this.selectedIds = this.memories.map((m) => m.id)
      }
    },

    toggleSelect(id: string) {
      const idx = this.selectedIds.indexOf(id)
      if (idx === -1) {
        this.selectedIds.push(id)
      } else {
        this.selectedIds.splice(idx, 1)
      }
    },
  },
})
