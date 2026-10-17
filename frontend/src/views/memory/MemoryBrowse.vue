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
          data-testid="memory-search"
          @change="handleSearch"
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
          data-testid="memory-filter-type"
          @change="(val: string) => memoryStore.setFilter({ type: val || '' })"
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
          data-testid="memory-filter-verdict"
          @change="(val: string) => memoryStore.setFilter({ verdict: val || '' })"
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
          data-testid="memory-filter-tier"
          @change="(val: number | undefined) => memoryStore.setFilter({ tier: val ?? undefined })"
        >
          <t-option key="0" :value="0" :label="$t('memory.tiers.0')" />
          <t-option key="1" :value="1" :label="$t('memory.tiers.1')" />
          <t-option key="2" :value="2" :label="$t('memory.tiers.2')" />
          <t-option key="3" :value="3" :label="$t('memory.tiers.3')" />
        </t-select>

        <t-checkbox
          :checked="memoryStore.filter.mineOnly"
          data-testid="memory-filter-mine"
          @change="(val: boolean) => memoryStore.setFilter({ mineOnly: val })"
        >
          {{ $t('memory.browse.mineOnly') }}
        </t-checkbox>
      </div>

      <div class="toolbar-right">
        <t-button
          v-if="canEdit"
          theme="primary"
          size="small"
          data-testid="memory-create-button"
          @click="createVisible = true"
        >
          <template #icon><t-icon name="add" /></template>
          {{ $t('memory.create.button') }}
        </t-button>
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
          <t-icon name="view-list" />
        </t-button>
      </div>
    </div>

    <!-- Bulk operations bar -->
    <div v-if="canEdit && memoryStore.hasSelection" class="bulk-bar" data-testid="memory-bulk-bar">
      <span class="bulk-selected-count">
        {{ $t('memory.browse.selectedCount', { count: memoryStore.selectedIds.length }) }}
      </span>

      <t-select
        :placeholder="$t('memory.browse.bulkVerdict')"
        class="bulk-verdict-select"
        data-testid="memory-bulk-verdict"
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
        data-testid="memory-bulk-importance-up"
        @click="handleBulkImportance(1)"
      >
        <t-icon name="arrow-up" />
        {{ $t('memory.browse.bulkImportanceUp') }}
      </t-button>

      <t-button
        variant="outline"
        size="small"
        data-testid="memory-bulk-importance-down"
        @click="handleBulkImportance(-1)"
      >
        <t-icon name="arrow-down" />
        {{ $t('memory.browse.bulkImportanceDown') }}
      </t-button>

      <t-button
        variant="outline"
        theme="danger"
        size="small"
        data-testid="memory-bulk-delete"
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
          :selectable="canEdit"
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
        data-testid="memory-table"
        @row-click="(row: { row: AgentMemory }) => $emit('open-detail', row.row)"
      >
        <!-- Selection column -->
        <template #selection="{ row }">
          <t-checkbox
            v-if="canEdit"
            :checked="memoryStore.selectedIds.includes(row.id)"
            :data-testid="`memory-select-${row.id}`"
            @click.stop
            @change="memoryStore.toggleSelect(row.id)"
          />
        </template>

        <!-- Type column: Colored Icon + Text -->
        <template #memory_type="{ row }">
          <div class="table-type-cell">
            <t-icon
              :name="tableTypeIcon(row.memory_type)"
              :style="{ color: typeColor(row.memory_type) }"
              size="15px"
              class="type-icon-glyph"
            />
            <span class="type-name">{{ $t(`memory.types.${row.memory_type}`) }}</span>
          </div>
        </template>

        <!-- Content column -->
        <template #content="{ row }">
          <div class="table-content-cell" :title="row.content" :data-testid="`memory-row-${row.id}`">
            {{ row.content.length > 100 ? row.content.slice(0, 97) + '...' : row.content }}
          </div>
        </template>

        <!-- Verdict column: Soft Background Tint + Icon -->
        <template #verdict="{ row }">
          <span :class="['verdict-soft-pill', `verdict-${row.verdict || 'none'}`]">
            <t-icon :name="verdictIcon(row.verdict)" size="12px" class="verdict-icon" />
            <span>{{ $t(`memory.verdicts.${row.verdict || 'none'}`) }}</span>
          </span>
        </template>

        <!-- Importance column -->
        <template #importance="{ row }">
          <div class="table-importance">
            <t-icon
              v-for="i in MEMORY_IMPORTANCE_MAX"
              :key="i"
              :name="i <= row.importance ? 'star-filled' : 'star'"
              size="12px"
              :class="{ starFilled: i <= row.importance }"
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

        <!-- Tier column: Outline Border + Text -->
        <template #tier="{ row }">
          <t-tooltip
            v-if="row.tier !== undefined && row.tier !== null"
            :content="tierTooltip(row.tier)"
            placement="top"
          >
            <span :class="['tier-outline-pill', `tier-${row.tier}`]">
              <span>{{ $t(`memory.tiers.${row.tier}`) }}</span>
            </span>
          </t-tooltip>
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
      <div v-if="displayMemories.length === 0 && !memoryStore.loading" class="empty-state" data-testid="memory-empty">
        <t-icon name="layers" size="48px" class="empty-icon" />
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
        data-testid="memory-pagination"
        @current-change="handlePageChange"
        @page-size-change="handlePageSizeChange"
      />
    </div>

    <MemoryCreateDialog v-model:visible="createVisible" :kb-id="kbId" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMemoryStore } from '@/stores/memory'
import type { BulkOutcome } from '@/stores/memory'
import type { AgentMemory, MemoryVerdict } from '@/api/memory/index'
import { isVerdictProtected, MEMORY_IMPORTANCE_MAX } from '@/api/memory/index'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import MemoryCard from './MemoryCard.vue'
import MemoryCreateDialog from './MemoryCreateDialog.vue'

const props = withDefaults(
  defineProps<{
    kbId: string
    /** Contributor+ with edit rights on this knowledge base. */
    canEdit?: boolean
  }>(),
  { canEdit: false },
)

defineEmits<{
  'open-detail': [memory: AgentMemory]
}>()

const { t } = useI18n()
const memoryStore = useMemoryStore()
const createVisible = ref(false)

// -----------------------------------------------------------------------
// Search (server-side keyword match), debounced
// -----------------------------------------------------------------------
const searchValue = ref(memoryStore.filter.keyword || '')
let searchTimer: ReturnType<typeof setTimeout> | null = null

function handleSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    if (searchValue.value.trim() !== memoryStore.filter.keyword) {
      memoryStore.setFilter({ keyword: searchValue.value.trim() })
    }
  }, 300)
}

watch(
  () => props.kbId,
  (newKbId) => {
    if (newKbId) {
      memoryStore.useKnowledgeBase(newKbId)
      searchValue.value = memoryStore.filter.keyword
      memoryStore.loadMemories(newKbId)
    }
  },
  { immediate: true }
)

// The server returns exactly one filtered page.
const displayMemories = computed(() => memoryStore.memories)

// -----------------------------------------------------------------------
// Table columns
// -----------------------------------------------------------------------
const tableColumns = computed(() => [
  { colKey: 'selection', width: 40, cell: 'selection' },
  { colKey: 'memory_type', title: t('memory.browse.table.type'), width: 120, cell: 'memory_type' },
  { colKey: 'content', title: t('memory.browse.table.content'), minWidth: 200, cell: 'content', ellipsis: true },
  { colKey: 'verdict', title: t('memory.browse.table.verdict'), width: 110, cell: 'verdict' },
  { colKey: 'importance', title: t('memory.browse.table.importance'), width: 85, cell: 'importance' },
  { colKey: 'tags', title: t('memory.browse.table.tags'), width: 140, cell: 'tags' },
  { colKey: 'tier', title: t('memory.browse.table.tier'), width: 110, cell: 'tier' },
  { colKey: 'stale', title: '', width: 50, cell: 'stale' },
  { colKey: 'created_at', title: t('memory.browse.table.date'), width: 100, cell: 'created_at' },
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

const TYPE_COLORS: Record<string, string> = {
  episodic: '#1890ff',
  semantic: '#52c41a',
  procedural: '#fa8c16',
  decision: '#722ed1',
  preference: '#eb2f96',
  fact: '#8c8c8c',
}

function typeColor(type: string): string {
  return TYPE_COLORS[type] || '#8c8c8c'
}

function verdictIcon(verdict: string): string {
  const map: Record<string, string> = {
    decision: 'lock-on',
    fixed: 'check-circle',
    gotcha: 'error-circle',
    wip: 'time',
    refuted: 'close-circle',
    none: 'minus-circle',
  }
  return map[verdict] || 'minus-circle'
}

function tierTooltip(tier: number): string {
  return tier >= 0 && tier <= 3 ? t(`memory.tierHints.${tier}`) : t('memory.browse.table.tier')
}

function isMemoryStale(memory: AgentMemory & { is_stale?: boolean }): boolean {
  if (memory.is_stale !== undefined) return memory.is_stale
  return staleDays(memory.updated_at) > 90
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
  memoryStore.setFilter({ pageSize })
}

// -----------------------------------------------------------------------
// Bulk operations
// -----------------------------------------------------------------------
function confirm(body: string, onConfirm: () => Promise<void>, danger = false) {
  const dialog = DialogPlugin.confirm({
    header: t('memory.browse.confirmTitle'),
    body,
    theme: danger ? 'danger' : 'warning',
    confirmBtn: { content: t('common.confirm'), theme: danger ? 'danger' : 'primary' },
    cancelBtn: t('common.cancel'),
    onConfirm: async () => {
      dialog.hide()
      await onConfirm()
    },
    onClose: () => dialog.hide(),
  })
}

function handleBulkVerdict(verdict: string) {
  const v = verdict as MemoryVerdict
  const protectedCount = memoryStore.selectedMemories.filter((m) => isVerdictProtected(m.verdict)).length
  const run = async () => reportBulkResult(await memoryStore.bulkChangeVerdict(v))
  if (protectedCount > 0) {
    confirm(t('memory.browse.confirmProtectedVerdict', { count: protectedCount }), run)
    return
  }
  run()
}

async function handleBulkImportance(delta: number) {
  reportBulkResult(await memoryStore.bulkChangeImportance(delta))
}

function handleBulkDelete() {
  const count = memoryStore.selectedIds.length
  confirm(t('memory.browse.confirmBulkDelete', { count }), async () => {
    reportBulkResult(await memoryStore.bulkDelete())
  }, true)
}

function reportBulkResult(result: BulkOutcome) {
  if (result.failed === 0) {
    MessagePlugin.success(t('memory.browse.bulkDone', { count: result.succeeded }))
    return
  }
  if (result.conflicts > 0) {
    MessagePlugin.warning(t('memory.browse.bulkConflicts', { count: result.conflicts }))
    if (memoryStore.kbId) memoryStore.loadMemories(memoryStore.kbId)
  }
  if (result.failed > result.conflicts) {
    MessagePlugin.error(
      t('memory.browse.bulkPartialFailure', {
        failed: result.failed - result.conflicts,
        total: result.succeeded + result.failed,
      })
    )
  }
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
  border-radius: var(--app-radius-xs);
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
  border-radius: var(--app-radius-sm);
  flex-shrink: 0;
}

.bulk-selected-count {
  font-size: var(--app-text-md);
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
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--app-text-md);

    .type-icon-glyph {
      flex-shrink: 0;
      transition: transform var(--app-motion-fast) ease;
    }

    .type-name {
      color: var(--td-text-color-primary);
      font-weight: 500;
    }
  }

  .table-content-cell {
    font-size: var(--app-text-sm);
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
    font-size: var(--app-text-xs);
    color: var(--td-text-color-placeholder);
    line-height: 20px;
  }

  .table-date {
    font-size: var(--app-text-sm);
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

// -----------------------------------------------------------------------
// Visual forms: Verdict (Soft pill) & Tier (Outline pill)
// -----------------------------------------------------------------------
.verdict-soft-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--app-text-xs);
  font-weight: 500;
  padding: 2px 8px;
  border-radius: var(--app-radius-xl);
  line-height: 18px;
  white-space: nowrap;

  .verdict-icon {
    flex-shrink: 0;
  }

  &.verdict-decision {
    background: rgba(114, 46, 209, 0.12);
    color: #722ed1;
  }
  &.verdict-fixed {
    background: rgba(0, 168, 112, 0.12);
    color: #00885a;
  }
  &.verdict-gotcha {
    background: rgba(237, 123, 47, 0.12);
    color: #d46b08;
  }
  &.verdict-wip {
    background: rgba(24, 144, 255, 0.12);
    color: #096dd9;
  }
  &.verdict-refuted {
    background: rgba(227, 77, 89, 0.12);
    color: #cf1322;
  }
  &.verdict-none {
    background: var(--td-bg-color-secondarycontainer, rgba(0, 0, 0, 0.04));
    color: var(--td-text-color-placeholder, #8c8c8c);
  }
}

.tier-outline-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--app-text-xs);
  font-weight: 500;
  padding: 1px 7px;
  border-radius: var(--app-radius-xs);
  line-height: 18px;
  background: transparent;
  white-space: nowrap;
  cursor: help;
  transition: all var(--app-motion-base) ease;

  &.tier-0 {
    border: 1px solid #d48806;
    color: #d48806;
    font-weight: 600;
    box-shadow: 0 0 0 1px rgba(212, 136, 6, 0.15);
  }
  &.tier-1 {
    border: 1px solid #096dd9;
    color: #096dd9;
  }
  &.tier-2 {
    border: 1px solid #597ef7;
    color: #597ef7;
  }
  &.tier-3 {
    border: 1px dashed var(--td-text-color-placeholder, #bfbfbf);
    color: var(--td-text-color-placeholder, #8c8c8c);
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
  font-size: var(--app-text-xl);
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.empty-desc {
  font-size: var(--app-text-md);
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
