<template>
  <div class="memory-history" data-testid="memory-history-tab">
    <div class="history-toolbar">
      <t-select
        :value="memoryStore.historyAction"
        :placeholder="$t('memory.history.filterAll')"
        clearable
        class="filter-select"
        data-testid="memory-history-filter"
        @change="handleFilterChange"
      >
        <t-option key="all" value="" :label="$t('memory.history.filterAll')" />
        <t-option v-for="a in MEMORY_AUDIT_ACTIONS" :key="a" :value="a" :label="auditActionLabel(a)" />
      </t-select>
      <span class="event-count">
        {{ $t('memory.time.eventsShown', { shown: entries.length, total: memoryStore.historyPagination.total }) }}
      </span>
    </div>

    <div v-if="memoryStore.historyLoading && entries.length === 0" class="loading-container">
      <t-loading :text="$t('memory.history.loading')" size="large" />
    </div>

    <div v-else-if="entries.length === 0" class="empty-state" data-testid="memory-history-empty">
      <t-icon name="time" size="48px" class="empty-icon" />
      <span class="empty-title">
        {{ memoryStore.historyAction ? $t('memory.history.noMatchingEvents') : $t('memory.history.emptyTitle') }}
      </span>
      <span class="empty-desc">
        {{ memoryStore.historyAction ? $t('memory.history.noMatchingDesc') : $t('memory.history.emptyDesc') }}
      </span>
    </div>

    <div v-else class="timeline-container">
      <div v-for="group in groupedTimeline" :key="group.dateLabel" class="timeline-group">
        <div class="timeline-group-header">
          <span class="group-date-label">{{ group.dateLabel }}</span>
          <span class="group-date-line" />
        </div>

        <div
          v-for="entry in group.entries"
          :key="entry.id"
          class="timeline-item"
          :data-testid="`memory-history-${entry.action}`"
        >
          <div class="timeline-marker">
            <div class="timeline-dot" :style="{ background: actionColor(entry.action) }" />
            <div class="timeline-line" />
          </div>
          <div class="timeline-card">
            <div class="timeline-card-header">
              <t-icon
                :name="actionIcon(entry.action)"
                size="16px"
                class="event-type-icon"
                :style="{ color: actionColor(entry.action) }"
              />
              <span class="event-type-badge" :class="`badge-${entry.action}`">
                {{ auditActionLabel(entry.action) }}
              </span>
              <span class="event-timestamp">{{ formatTime(entry.created_at) }}</span>
              <span class="event-actor">{{ actorLabel(entry) }}</span>
            </div>
            <div class="timeline-card-body">
              <span class="event-description">{{ describeAuditChange(entry) }}</span>
              <span
                v-if="entry.action !== 'deleted' && entry.action !== 'merged_into'"
                class="event-memory-link"
                @click.stop="$emit('view-memory', entry.memory_id)"
              >
                <t-icon name="browse" size="12px" />
                {{ $t('memory.history.viewMemory') }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <div v-if="memoryStore.hasMoreHistory" class="load-more-bar">
        <t-button
          variant="outline"
          :loading="memoryStore.historyLoading"
          data-testid="memory-history-more"
          @click="memoryStore.loadHistory(kbId, true)"
        >
          {{ $t('memory.history.loadMore') }}
        </t-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMemoryStore } from '@/stores/memory'
import { MEMORY_AUDIT_ACTIONS, type MemoryAuditEntry } from '@/api/memory/index'
import { useMemoryLabels } from './useMemoryLabels'

const props = defineProps<{ kbId: string }>()

defineEmits<{
  'view-memory': [memoryId: string]
}>()

const { t } = useI18n()
const memoryStore = useMemoryStore()
const { auditActionLabel, actorLabel, describeAuditChange } = useMemoryLabels()

watch(
  () => props.kbId,
  (id) => {
    if (id) memoryStore.loadHistory(id)
  },
  { immediate: true },
)

const entries = computed<MemoryAuditEntry[]>(() => memoryStore.historyEntries)

function handleFilterChange(action: string) {
  memoryStore.setHistoryAction(props.kbId, action || '')
}

const ACTION_STYLE: Record<string, { icon: string; color: string }> = {
  created: { icon: 'add-circle', color: '#00a870' },
  patched: { icon: 'edit-1', color: '#4094f7' },
  merged: { icon: 'merge-cells', color: '#722ed1' },
  merged_into: { icon: 'merge-cells', color: '#8c8c8c' },
  deleted: { icon: 'delete', color: '#e34d59' },
  unmerged: { icon: 'rollback', color: '#ed7b2f' },
  dreamed: { icon: 'lightbulb', color: '#ed7b2f' },
  reclaimed: { icon: 'clear', color: '#fa8c16' },
}

function actionIcon(action: string): string {
  return ACTION_STYLE[action]?.icon ?? 'time'
}

function actionColor(action: string): string {
  return ACTION_STYLE[action]?.color ?? '#8c8c8c'
}

interface TimelineGroup {
  dateLabel: string
  entries: MemoryAuditEntry[]
}

function dateLabel(timestamp: string): string {
  const d = new Date(timestamp)
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const day = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  if (day === today) return t('memory.history.today')
  if (day === today - 86400000) return t('memory.history.yesterday')
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

const groupedTimeline = computed<TimelineGroup[]>(() => {
  const groups: TimelineGroup[] = []
  for (const entry of entries.value) {
    const label = dateLabel(entry.created_at)
    const last = groups[groups.length - 1]
    if (last && last.dateLabel === label) last.entries.push(entry)
    else groups.push({ dateLabel: label, entries: [entry] })
  }
  return groups
})

function formatTime(timestamp: string): string {
  const d = new Date(timestamp)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}
</script>

<style scoped lang="less">
.memory-history {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 12px;
}

// -----------------------------------------------------------------------
// Loading / Empty
// -----------------------------------------------------------------------
.loading-container {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  flex: 1;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 240px;
  flex: 1;
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
  max-width: 420px;
  line-height: 1.5;
}

// -----------------------------------------------------------------------
// Toolbar
// -----------------------------------------------------------------------
.history-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-shrink: 0;
}

.filter-select {
  width: 200px;
}

.event-count {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
  white-space: nowrap;
}

// -----------------------------------------------------------------------
// Timeline container
// -----------------------------------------------------------------------
.timeline-container {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding-bottom: 8px;
}

// -----------------------------------------------------------------------
// Timeline group
// -----------------------------------------------------------------------
.timeline-group {
  position: relative;
}

.timeline-group-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 0 8px;
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--td-bg-color-page);
}

.group-date-label {
  font-size: var(--app-text-md);
  font-weight: 600;
  color: var(--td-text-color-primary);
  white-space: nowrap;
  flex-shrink: 0;
}

.group-date-line {
  flex: 1;
  height: 1px;
  background: var(--td-component-stroke);
}

// -----------------------------------------------------------------------
// Timeline item
// -----------------------------------------------------------------------
.timeline-item {
  display: flex;
  gap: 12px;
  min-height: 60px;
}

// -----------------------------------------------------------------------
// Timeline marker (dot + line)
// -----------------------------------------------------------------------
.timeline-marker {
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 16px;
  flex-shrink: 0;
}

.timeline-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
  margin-top: 6px;
  z-index: 1;
  box-shadow: 0 0 0 2px var(--td-bg-color-page);
}

.timeline-line {
  flex: 1;
  width: 2px;
  background: var(--td-component-stroke);
  min-height: 100%;
}

// -----------------------------------------------------------------------
// Timeline card
// -----------------------------------------------------------------------
.timeline-card {
  flex: 1;
  padding: 8px 12px;
  margin-bottom: 8px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  cursor: pointer;
  transition: box-shadow var(--app-motion-base) ease;
  min-width: 0;
}

.timeline-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}

.timeline-card-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}

.event-type-icon {
  flex-shrink: 0;
}

.event-type-badge {
  display: inline-block;
  padding: 1px 6px;
  font-size: var(--app-text-2xs);
  font-weight: 500;
  border-radius: 3px;
  line-height: 1.6;
  flex-shrink: 0;
}

.event-type-badge.badge-created {
  color: #00a870;
  background: rgba(0, 168, 112, 0.08);
}

.event-type-badge.badge-patched {
  color: #4094f7;
  background: rgba(64, 148, 247, 0.08);
}

.event-type-badge.badge-deleted {
  color: #e34d59;
  background: rgba(227, 77, 89, 0.08);
}

.event-type-badge.badge-merged {
  color: #722ed1;
  background: rgba(114, 46, 209, 0.08);
}

.event-type-badge.badge-merged_into {
  color: #ed7b2f;
  background: rgba(237, 123, 47, 0.08);
}

.event-type-badge.badge-unmerged {
  color: #8c8c8c;
  background: rgba(140, 140, 140, 0.08);
}

.event-type-badge.badge-reclaimed {
  color: #fa8c16;
  background: rgba(250, 140, 22, 0.08);
}

.event-type-badge.badge-dreamed {
  color: #0052d9;
  background: rgba(0, 82, 217, 0.08);
}

.event-timestamp {
  margin-left: auto;
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
  white-space: nowrap;
  flex-shrink: 0;
}

.timeline-card-body {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--app-text-md);
  color: var(--td-text-color-primary);
  line-height: 1.5;
}

.event-description {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.event-memory-link {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: var(--app-text-xs);
  color: var(--td-brand-color);
  cursor: pointer;
  white-space: nowrap;
  flex-shrink: 0;
  padding: 2px 6px;
  border-radius: var(--app-radius-xs);
  transition: background var(--app-motion-fast) ease;
}

.event-memory-link:hover {
  background: var(--td-brand-color-light);
  text-decoration: underline;
}

.timeline-card-preview {
  margin-top: 4px;
  padding: 4px 8px;
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  line-height: 1.4;
}

// -----------------------------------------------------------------------
// Load more
// -----------------------------------------------------------------------
.load-more-bar {
  display: flex;
  justify-content: center;
  padding: 12px 0 4px;
  flex-shrink: 0;
}

.event-actor {
  margin-left: auto;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
}
</style>
