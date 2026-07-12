<template>
  <div class="memory-browse">
    <!-- Toolbar: search + filters + view toggle -->
    <div class="browse-toolbar">
      <div class="toolbar-left">
        <!-- Keyword search -->
        <t-input
          v-model="searchValue"
          :placeholder="$t('memory.browse.searchPlaceholder')"
          clearable
          class="search-input"
          @enter="handleSearch"
          @clear="handleSearch"
        >
          <template #prefix-icon>
            <t-icon name="search" />
          </template>
        </t-input>

        <!-- Type filter -->
        <t-select
          :value="memoryStore.filter.type"
          :placeholder="$t('memory.browse.filterType')"
          clearable
          class="filter-select"
          @change="(val: string) => memoryStore.setFilter({ type: val })"
        >
          <t-option key="episodic" value="episodic" :label="$t('memory.types.episodic')" />
          <t-option key="semantic" value="semantic" :label="$t('memory.types.semantic')" />
          <t-option key="procedural" value="procedural" :label="$t('memory.types.procedural')" />
          <t-option key="decision" value="decision" :label="$t('memory.types.decision')" />
          <t-option key="preference" value="preference" :label="$t('memory.types.preference')" />
          <t-option key="fact" value="fact" :label="$t('memory.types.fact')" />
        </t-select>

        <!-- Verdict filter -->
        <t-select
          :value="memoryStore.filter.verdict"
          :placeholder="$t('memory.browse.filterVerdict')"
          clearable
          class="filter-select"
          @change="(val: string) => memoryStore.setFilter({ verdict: val })"
        >
          <t-option key="none" value="none" :label="$t('memory.verdicts.none')" />
          <t-option key="fixed" value="fixed" :label="$t('memory.verdicts.fixed')" />
          <t-option key="refuted" value="refuted" :label="$t('memory.verdicts.refuted')" />
          <t-option key="decision" value="decision" :label="$t('memory.verdicts.decision')" />
          <t-option key="gotcha" value="gotcha" :label="$t('memory.verdicts.gotcha')" />
          <t-option key="wip" value="wip" :label="$t('memory.verdicts.wip')" />
        </t-select>

        <!-- Tier filter -->
        <t-select
          :value="memoryStore.filter.tier"
          :placeholder="$t('memory.browse.filterTier')"
          clearable
          class="filter-select filter-select-narrow"
          @change="(val: number | undefined) => memoryStore.setFilter({ tier: val })"
        >
          <t-option key="0" :value="0" :label="$t('memory.tiers.0')" />
          <t-option key="1" :value="1" :label="$t('memory.tiers.1')" />
          <t-option key="2" :value="2" :label="$t('memory.tiers.2')" />
          <t-option key="3" :value="3" :label="$t('memory.tiers.3')" />
        </t-select>
      </div>

      <div class="toolbar-right">
        <!-- View mode toggle -->
        <t-button
          variant="text"
          class="view-toggle-btn"
          :class="{ active: memoryStore.viewMode === 'grid' }"
          :title="$t('memory.browse.viewModeGrid')"
          @click="memoryStore.viewMode = 'grid'"
        >
          <t-icon name="dashboard" />
        </t-button>
        <t-button
          variant="text"
          class="view-toggle-btn"
          :class="{ active: memoryStore.viewMode === 'table' }"
          :title="$t('memory.browse.viewModeTable')"
          @click="memoryStore.viewMode = 'table'"
        >
          <t-icon name="list" />
        </t-button>
      </div>
    </div>

    <!-- Bulk operations bar -->
    <div v-if="memoryStore.hasSelection" class="bulk-bar">
      <span class="bulk-selected-count">
        {{ $t('memory.browse.selectedCount', { count: memoryStore.selectedIds.length }) }}
      </span>

      <t-select
        :placeholder="$t('memory.browse.bulkVerdict')"
        class="bulk-verdict-select"
        @change="(val: string) => handleBulkVerdict(val)"
      >
        <t-option key="none" value="none" :label="$t('memory.verdicts.none')" />
        <t-option key="fixed" value="fixed" :label="$t('memory.verdicts.fixed')" />
        <t-option key="refuted" value="refuted" :label="$t('memory.verdicts.refuted')" />
        <t-option key="decision" value="decision" :label="$t('memory.verdicts.decision')" />
        <t-option key="gotcha" value="gotcha" :label="$t('memory.verdicts.gotcha')" />
        <t-option key="wip" value="wip" :label="$t('memory.verdicts.wip')" />
      </t-select>

      <t-button
        variant="outline"
        size="small"
        @click="memoryStore.bulkChangeImportance(1)"
      >
        <t-icon name="arrow-up" />
        {{ $t('memory.browse.bulkImportanceUp') }}
      </t-button>

      <t-button
        variant="outline"
        size="small"
        @click="memoryStore.bulkChangeImportance(-1)"
      >
        <t-icon name="arrow-down" />
        {{ $t('memory.browse.bulkImportanceDown') }}
      </t-button>

      <t-button
        variant="outline"
        theme="danger"
        size="small"
        @click="handleBulkDelete"
      >
        <t-icon name="delete" />
        {{ $t('memory.browse.bulkDelete') }}
      </t-button>

      <t-button
        variant="text"
        size="small"
        @click="memoryStore.selectedIds = []"
      >
        {{ $t('common.cancel') }}
      </t-button>
    </div>

    <!-- Content area -->
    <div class="browse-content">
      <!-- Loading skeleton -->
      <div v-if="memoryStore.loading && displayMemories.length === 0" class="loading-container">
        <t-loading :text="$t('memory.browse.loading')" size="large" />
      </div>

      <!-- Grid view -->
      <div
        v-else-if="memoryStore.viewMode === 'grid' && displayMemories.length > 0"
        class="memory-grid"
      >
        <MemoryCard
          v-for="mem in displayMemories"
          :key="mem.id"
          :memory="mem"
          :selected="memoryStore.selectedIds.includes(mem.id)"
          @select="(id: string) => memoryStore.toggleSelect(id)"
          @click="(mem: AgentMemory) => $emit('open-detail', mem)"
        />
      </div>

      <!-- Table view -->
      <t-table
        v-else-if="memoryStore.viewMode === 'table' && displayMemories.length > 0"
        :data="displayMemories"
        :columns="tableColumns"
        row-key="id"
        size="small"
        :table-layout="'auto'"
        :hover="true"
        :pagination="null"
        class="memory-table"
        @row-click="(row: { row: AgentMemory }) => $emit('open-detail', row.row)"
      >
        <!-- Selection column -->
        <template #selection="{ row }">
          <t-checkbox
            :checked="memoryStore.selectedIds.includes(row.id)"
            @click.stop
            @change="memoryStore.toggleSelect(row.id)"
          />
        </template>

        <!-- Type column -->
        <template #memory_type="{ row }">
          <div class="table-type-cell">
            <t-icon :name="tableTypeIcon(row.memory_type)" size="14px" />
            <span>{{ $t(`memory.types.${row.memory_type}`) }}</span>
          </div>
        </template>

        <!-- Content column -->
        <template #content="{ row }">
          <div class="table-content-cell" :title="row.content">
            {{ row.content.length > 100 ? row.content.slice(0, 97) + '...' : row.content }}
          </div>
        </template>

        <!-- Verdict column -->
        <template #verdict="{ row }">
          <t-tag
            :class="['verdict-badge', `verdict-${row.verdict || 'none'}`]"
            size="small"
            variant="light"
          >
            {{ $t(`memory.verdicts.${row.verdict || 'none'}`) }}
          </t-tag>
        </template>

        <!-- Importance column -->
        <template #importance="{ row }">
          <div class="table-importance">
            <t-icon
              v-for="i in 5"
              :key="i"
              :name="i <= Math.round(row.importance / 2) ? 'star-filled' : 'star'"
              size="12px"
              :class="{ starFilled: i <= Math.round(row.importance / 2) }"
            />
          </div>
        </template>

        <!-- Tags column -->
        <template #tags="{ row }">
          <div v-if="row.tags && row.tags.length > 0" class="table-tags">
            <t-tag
              v-for="tag in row.tags.slice(0, 3)"
              :key="tag"
              size="small"
              variant="light"
            >
              {{ tag }}
            </t-tag>
            <span v-if="row.tags.length > 3" class="table-tags-more">+{{ row.tags.length - 3 }}</span>
          </div>
        </template>

        <!-- Stale column -->
        <template #stale="{ row }">
          <t-tag v-if="isMemoryStale(row)" size="small" theme="warning" variant="light">
            {{ $t('memory.browse.stale') }}
          </t-tag>
        </template>

        <!-- Date column -->
        <template #created_at="{ row }">
          <span class="table-date">{{ formatTableDate(row.created_at) }}</span>
        </template>
      </t-table>

      <!-- Empty state -->
      <div v-if="displayMemories.length === 0 && !memoryStore.loading" class="empty-state">
        <t-icon name="memory" size="48px" class="empty-icon" />
        <span class="empty-title">{{ $t('memory.browse.empty') }}</span>
        <span class="empty-desc">{{ $t('memory.browse.emptyDesc') }}</span>
      </div>
    </div>

    <!-- Pagination -->
    <div class="browse-pagination">
      <t-pagination
        :current="memoryStore.pagination.page"
        :page-size="memoryStore.pagination.pageSize"
        :total="memoryStore.pagination.total"
        :page-size-options="[10, 20, 50, 100]"
        show-page-size
        show-jumper
        @current-change="handlePageChange"
        @page-size-change="handlePageSizeChange"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useMemoryStore } from '@/stores/memory'
import type { AgentMemory, MemoryVerdict } from '@/api/memory/index'
import { isVerdictProtected } from '@/api/memory/index'
import { MessagePlugin } from 'tdesign-vue-next'
import MemoryCard from './MemoryCard.vue'

const props = defineProps<{
  kbId: string
}>()

defineEmits<{
  'open-detail': [memory: AgentMemory]
}>()

const memoryStore = useMemoryStore()

// -----------------------------------------------------------------------
// Search with debounce
// -----------------------------------------------------------------------
const searchValue = ref(memoryStore.filter.keyword || '')
let searchTimer: ReturnType<typeof setTimeout> | null = null

function handleSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    memoryStore.setFilter({ keyword: searchValue.value })
  }, 300)
}

watch(
  () => props.kbId,
  (newKbId) => {
    if (newKbId) {
      memoryStore.loadMemories(newKbId)
    }
  },
  { immediate: true }
)

// -----------------------------------------------------------------------
// Filtered / paginated memories
// -----------------------------------------------------------------------
const displayMemories = computed(() => {
  const filtered = memoryStore.filteredMemories
  const { page, pageSize } = memoryStore.pagination
  const start = (page - 1) * pageSize
  return filtered.slice(start, start + pageSize)
})

// -----------------------------------------------------------------------
// Table columns
// -----------------------------------------------------------------------
const tableColumns = computed(() => [
  { colKey: 'selection', width: 40, cell: 'selection' },
  { colKey: 'memory_type', title: 'Type', width: 100, cell: 'memory_type' },
  { colKey: 'content', title: 'Content', minWidth: 200, cell: 'content', ellipsis: true },
  { colKey: 'verdict', title: 'Verdict', width: 90, cell: 'verdict' },
  { colKey: 'importance', title: 'Imp.', width: 80, cell: 'importance' },
  { colKey: 'tags', title: 'Tags', width: 140, cell: 'tags' },
  { colKey: 'tier', title: 'Tier', width: 60 },
  { colKey: 'stale', title: '', width: 50, cell: 'stale' },
  { colKey: 'created_at', title: 'Date', width: 100, cell: 'created_at' },
])

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------
function tableTypeIcon(type: string): string {
  const map: Record<string, string> = {
    episodic: 'time',
    semantic: 'bookmark',
    procedural: 'control-platform',
    decision: 'check-circle',
    preference: 'thumb-up',
    fact: 'info-circle',
  }
  return map[type] || 'memory'
}

function isMemoryStale(memory: AgentMemory): boolean {
  const days = staleDays(memory.updated_at)
  return days > 90
}

function staleDays(dateStr: string): number {
  const now = Date.now()
  const updated = new Date(dateStr).getTime()
  if (isNaN(updated)) return 0
  return Math.floor((now - updated) / (1000 * 60 * 60 * 24))
}

function formatTableDate(dateStr: string): string {
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

// -----------------------------------------------------------------------
// Pagination
// -----------------------------------------------------------------------
function handlePageChange(page: number) {
  memoryStore.setFilter({ page })
}

function handlePageSizeChange(pageSize: number) {
  memoryStore.setFilter({ pageSize, page: 1 })
}

// -----------------------------------------------------------------------
// Bulk operations
// -----------------------------------------------------------------------
async function handleBulkVerdict(verdict: string) {
  const v = verdict as MemoryVerdict
  // Check if any selected memory has a protected verdict
  const protectedMemories = memoryStore.selectedMemories.filter(
    (m) => isVerdictProtected(m.verdict)
  )
  if (protectedMemories.length > 0) {
    MessagePlugin.warning(
      `${protectedMemories.length} memory(ies) have a protected verdict (decision/fixed) and cannot be changed via bulk operation.`
    )
  }
  await memoryStore.bulkChangeVerdict(v)
}

async function handleBulkDelete() {
  await memoryStore.bulkDelete()
}
</script>

<style scoped lang="less">
.memory-browse {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 12px;
}

// -----------------------------------------------------------------------
// Toolbar
// -----------------------------------------------------------------------
.browse-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-shrink: 0;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
}

.search-input {
  width: 220px;
}

.filter-select {
  width: 140px;
}

.filter-select-narrow {
  width: 120px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.view-toggle-btn {
  padding: 4px 6px;
  border-radius: 4px;
  color: var(--td-text-color-secondary);

  &.active {
    color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }

  &:hover {
    background: var(--td-bg-color-secondarycontainer);
  }
}

// -----------------------------------------------------------------------
// Bulk bar
// -----------------------------------------------------------------------
.bulk-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: var(--td-brand-color-light);
  border: 1px solid var(--td-brand-color);
  border-radius: 6px;
  flex-shrink: 0;
}

.bulk-selected-count {
  font-size: 13px;
  font-weight: 500;
  color: var(--td-brand-color);
  white-space: nowrap;
}

.bulk-verdict-select {
  width: 130px;
}

// -----------------------------------------------------------------------
// Content
// -----------------------------------------------------------------------
.browse-content {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.loading-container {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}

.memory-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
  padding-bottom: 8px;
}

// -----------------------------------------------------------------------
// Table
// -----------------------------------------------------------------------
.memory-table {
  :deep(.t-table__cell--ellipsis) {
    max-width: 300px;
  }

  .table-type-cell {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
  }

  .table-content-cell {
    font-size: 12px;
    color: var(--td-text-color-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .table-tags {
    display: flex;
    gap: 2px;
    flex-wrap: wrap;
  }

  .table-tags-more {
    font-size: 11px;
    color: var(--td-text-color-placeholder);
    line-height: 20px;
  }

  .table-date {
    font-size: 12px;
    color: var(--td-text-color-placeholder);
  }

  .table-importance {
    display: flex;
    gap: 1px;
    align-items: center;

    .starFilled {
      color: var(--td-warning-color);
    }
  }
}

.verdict-badge {
  font-size: 11px;

  &.verdict-refuted {
    --td-tag-color: var(--td-error-color, #e34d59);
  }
  &.verdict-decision {
    --td-tag-color: var(--td-brand-color, #0052d9);
  }
  &.verdict-fixed {
    --td-tag-color: var(--td-success-color, #00a870);
  }
  &.verdict-gotcha {
    --td-tag-color: var(--td-warning-color, #ed7b2f);
  }
  &.verdict-wip {
    --td-tag-color: var(--td-info-color, #4094f7);
  }
}

// -----------------------------------------------------------------------
// Empty state
// -----------------------------------------------------------------------
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 240px;
  padding: 40px;
}

.empty-icon {
  color: var(--td-text-color-disabled);
}

.empty-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.empty-desc {
  font-size: 13px;
  color: var(--td-text-color-placeholder);
  text-align: center;
  max-width: 360px;
  line-height: 1.5;
}

// -----------------------------------------------------------------------
// Pagination
// -----------------------------------------------------------------------
.browse-pagination {
  display: flex;
  justify-content: flex-end;
  flex-shrink: 0;
  padding-top: 4px;
}
</style>
