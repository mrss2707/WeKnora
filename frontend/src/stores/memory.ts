import { defineStore } from 'pinia'
import {
  listMemories,
  getMemoryGraph,
  getMemoryStats,
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
  MemoryStats,
  HealthReport,
  DreamResult,
  TimelineEvent,
  TimelineEventType,
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

    /** Memory statistics (cached when loaded). */
    stats: null as MemoryStats | null,

    /** Dream result from the last run. */
    lastDreamResult: null as DreamResult | null,

    /** Loading indicator. */
    loading: false,

    /** Graph loading indicator. */
    graphLoading: false,

    /** Health loading indicator. */
    healthLoading: false,

    /** Stats loading indicator. */
    statsLoading: false,

    /** Dreamer loading indicator. */
    dreamerLoading: false,

    /** History/activity timeline events. */
    historyEvents: [] as TimelineEvent[],

    /** History loading indicator. */
    historyLoading: false,

    /** History event type filter (empty = all). */
    historyFilterType: '' as '' | TimelineEventType,

    /** History pagination state (client-side). */
    historyPagination: {
      page: 1,
      pageSize: 20,
      total: 0,
    },
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

    async loadStats(kbId: string) {
      this.statsLoading = true
      try {
        const resp = await getMemoryStats(kbId)
        this.stats = resp.data?.data ?? null
      } finally {
        this.statsLoading = false
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
    // History / Timeline
    // -----------------------------------------------------------------------

    /** Generate timeline events from memories, health report, and dreamer results. */
    async loadHistory(kbId: string) {
      this.historyLoading = true
      try {
        // Ensure source data is loaded
        if (this.memories.length === 0) {
          await this.loadMemories(kbId)
        }
        if (!this.healthReport) {
          await this.loadHealth(kbId)
        }

        const events: TimelineEvent[] = []

        // 1. Created events from each memory
        for (const mem of this.memories) {
          const preview = mem.content.length > 80 ? mem.content.slice(0, 77) + '...' : mem.content
          events.push({
            id: `created-${mem.id}`,
            type: 'created',
            timestamp: mem.created_at,
            description: `Memory created`,
            memory_id: mem.id,
            memory_content_preview: preview,
          })

          // 2. Updated event if updated_at differs from created_at
          if (mem.updated_at !== mem.created_at) {
            events.push({
              id: `updated-${mem.id}-${mem.updated_at}`,
              type: 'updated',
              timestamp: mem.updated_at,
              description: `Memory updated`,
              memory_id: mem.id,
              memory_content_preview: preview,
            })
          }
        }

        // 3. Health check events
        if (this.healthReport?.checked_at) {
          const total = this.healthReport.total_issues ?? 0
          events.push({
            id: 'health-check',
            type: 'health_check',
            timestamp: this.healthReport.checked_at,
            description: `Health check completed — ${total} issue${total !== 1 ? 's' : ''} found`,
            metadata: { total_issues: total },
          })
        }

        // 4. Dreamer action events from last dream result
        if (this.lastDreamResult?.actions) {
          for (const action of this.lastDreamResult.actions) {
            const desc = `Dreamer: ${action.reason}`
            events.push({
              id: `dreamer-${action.target_id}-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
              type: 'dreamer_action',
              timestamp: new Date().toISOString(),
              description: desc.length > 120 ? desc.slice(0, 117) + '...' : desc,
              memory_id: action.target_id,
              metadata: {
                action_type: action.type,
                confidence: action.confidence,
              },
            })
          }
        }

        // 5. Verdict-changed events from dreamer actions that change verdict
        if (this.lastDreamResult?.actions) {
          for (const action of this.lastDreamResult.actions) {
            if (action.new_verdict) {
              events.push({
                id: `verdict-${action.target_id}-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
                type: 'verdict_changed',
                timestamp: new Date().toISOString(),
                description: `Verdict changed to "${action.new_verdict}"`,
                memory_id: action.target_id,
                metadata: {
                  old_verdict: action.new_verdict,
                  new_verdict: action.new_verdict,
                },
              })
            }
          }
        }

        // Sort descending by timestamp
        events.sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp))

        this.historyEvents = events
        this.historyPagination.total = events.length
        this.historyPagination.page = 1
      } finally {
        this.historyLoading = false
      }
    },

    /** Set the history event type filter and reset pagination. */
    setHistoryFilter(type: '' | TimelineEvent['type']) {
      this.historyFilterType = type
      this.historyPagination.page = 1
    },

    /** Load the next page of history events. */
    loadMoreHistory() {
      const maxPage = Math.ceil(this.historyPagination.total / this.historyPagination.pageSize)
      if (this.historyPagination.page < maxPage) {
        this.historyPagination.page++
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
