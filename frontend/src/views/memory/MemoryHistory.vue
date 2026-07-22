<template>
  <div class="memory-history">
    <!-- Loading skeleton -->
    <div v-if="memoryStore.historyLoading" class="loading-container">
      <t-loading :text="$t('memory.history.loading') || 'Loading timeline...'" size="large" />
    </div>

    <!-- Empty state (no events at all) -->
    <div v-else-if="allEvents.length === 0" class="empty-state">
      <t-icon name="time" size="48px" class="empty-icon" />
      <span class="empty-title">{{ $t('memory.history.emptyTitle') || 'No Events' }}</span>
      <span class="empty-desc">{{ $t('memory.history.emptyDesc') || 'Timeline events will appear here as memories are created, updated, or checked by the system workers.' }}</span>
    </div>

    <!-- Content -->
    <template v-else>
      <!-- Toolbar: filter dropdown -->
      <div class="history-toolbar">
        <t-select
          :value="memoryStore.historyFilterType"
          :placeholder="$t('memory.history.filterAll') || 'All events'"
          clearable
          class="filter-select"
          @change="handleFilterChange"
        >
          <t-option key="all" value="" :label="$t('memory.history.filterAll') || 'All events'" />
          <t-option key="created" value="created" :label="eventTypeLabel('created')" />
          <t-option key="updated" value="updated" :label="eventTypeLabel('updated')" />
          <t-option key="deleted" value="deleted" :label="eventTypeLabel('deleted')" />
          <t-option key="verdict_changed" value="verdict_changed" :label="eventTypeLabel('verdict_changed')" />
          <t-option key="dreamer_action" value="dreamer_action" :label="eventTypeLabel('dreamer_action')" />
          <t-option key="consolidation" value="consolidation" :label="eventTypeLabel('consolidation')" />
          <t-option key="pruner" value="pruner" :label="eventTypeLabel('pruner')" />
          <t-option key="health_check" value="health_check" :label="eventTypeLabel('health_check')" />
        </t-select>

        <span class="event-count">{{ $t('memory.history.eventCount', { count: pagedEvents.length }) || displayedCountLabel }}</span>
      </div>

      <!-- Filtered empty state -->
      <div v-if="pagedEvents.length === 0 && allEvents.length > 0" class="empty-state">
        <t-icon name="time" size="48px" class="empty-icon" />
        <span class="empty-title">{{ $t('memory.history.noMatchingEvents') || 'No matching events' }}</span>
        <span class="empty-desc">{{ $t('memory.history.noMatchingDesc') || 'Try selecting a different event type filter.' }}</span>
      </div>

      <!-- Timeline -->
      <div v-else class="timeline-container">
        <div
          v-for="group in groupedTimeline"
          :key="group.dateLabel"
          class="timeline-group"
        >
          <!-- Date group header -->
          <div class="timeline-group-header">
            <span class="group-date-label">{{ group.dateLabel }}</span>
            <span class="group-date-line" />
          </div>

          <!-- Events in this group -->
          <div
            v-for="event in group.events"
            :key="event.id"
            class="timeline-item"
          >
            <!-- Timeline dot + line -->
            <div class="timeline-marker">
              <div
                class="timeline-dot"
                :class="`dot-${event.type}`"
                :style="{ background: eventTypeColor(event.type) }"
              />
              <div class="timeline-line" />
            </div>

            <!-- Event content card -->
            <div class="timeline-card" @click="handleEventClick(event)">
              <div class="timeline-card-header">
                <t-icon
                  :name="eventTypeIcon(event.type)"
                  size="16px"
                  class="event-type-icon"
                  :style="{ color: eventTypeColor(event.type) }"
                />
                <span class="event-type-badge" :class="`badge-${event.type}`">
                  {{ eventTypeLabel(event.type) }}
                </span>
                <span class="event-timestamp">
                  {{ formatEventTime(event.timestamp) }}
                </span>
              </div>

              <div class="timeline-card-body">
                <span class="event-description">{{ event.description }}</span>
                <span
                  v-if="event.memory_id"
                  class="event-memory-link"
                  @click.stop="handleViewMemory(event)"
                >
                  <t-icon name="browse" size="12px" />
                  {{ $t('memory.history.viewMemory') || 'View memory' }}
                </span>
              </div>

              <div
                v-if="event.memory_content_preview"
                class="timeline-card-preview"
              >
                {{ event.memory_content_preview }}
              </div>
            </div>
          </div>
        </div>

        <!-- Load more button -->
        <div v-if="hasMoreEvents" class="load-more-bar">
          <t-button
            variant="outline"
            :loading="memoryStore.historyLoading"
            @click="handleLoadMore"
          >
            {{ $t('memory.history.loadMore') || 'Load more' }}
          </t-button>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMemoryStore } from '@/stores/memory'
import type { TimelineEvent, TimelineEventType } from '@/api/memory/index'

const props = defineProps<{
  kbId: string
}>()

const emit = defineEmits<{
  'view-memory': [memoryId: string]
}>()

const { t } = useI18n()
const memoryStore = useMemoryStore()

// -----------------------------------------------------------------------
// Data fetching
// -----------------------------------------------------------------------
watch(
  () => props.kbId,
  (newKbId) => {
    if (newKbId) {
      memoryStore.loadHistory(newKbId)
    }
  },
  { immediate: true }
)

// -----------------------------------------------------------------------
// Event type display config
// -----------------------------------------------------------------------
const eventTypeIcons: Record<TimelineEventType, string> = {
  created: 'add-circle',
  updated: 'edit-1',
  deleted: 'delete',
  verdict_changed: 'check-circle',
  dreamer_action: 'auto',
  consolidation: 'refresh',
  pruner: 'clear',
  health_check: 'info-circle',
}

const eventTypeColors: Record<TimelineEventType, string> = {
  created: '#00a870',
  updated: '#4094f7',
  deleted: '#e34d59',
  verdict_changed: '#722ed1',
  dreamer_action: '#ed7b2f',
  consolidation: '#8c8c8c',
  pruner: '#fa8c16',
  health_check: '#0052d9',
}

const eventTypeLabels: Record<TimelineEventType, string> = {
  created: 'created',
  updated: 'updated',
  deleted: 'deleted',
  verdict_changed: 'verdictChanged',
  dreamer_action: 'dreamer',
  consolidation: 'consolidation',
  pruner: 'pruner',
  health_check: 'healthCheck',
}

function eventTypeIcon(type: string): string {
  return eventTypeIcons[type as TimelineEventType] || 'time'
}

function eventTypeColor(type: string): string {
  return eventTypeColors[type as TimelineEventType] || '#8c8c8c'
}

function eventTypeLabel(type: string): string {
  const key = `memory.history.eventTypes.${eventTypeLabels[type as TimelineEventType] || type}`
  return t(key)
}

// -----------------------------------------------------------------------
// Filtered events
// -----------------------------------------------------------------------
const allEvents = computed<TimelineEvent[]>(() => memoryStore.historyEvents)

const filteredEvents = computed<TimelineEvent[]>(() => {
  const typeFilter = memoryStore.historyFilterType
  if (!typeFilter) return allEvents.value
  return allEvents.value.filter((e) => e.type === typeFilter)
})

const pagedEvents = computed<TimelineEvent[]>(() => {
  const { page, pageSize } = memoryStore.historyPagination
  return filteredEvents.value.slice(0, page * pageSize)
})

const hasMoreEvents = computed(() => {
  return pagedEvents.value.length < filteredEvents.value.length
})

const displayedCountLabel = computed(() => {
  const shown = pagedEvents.value.length
  const total = filteredEvents.value.length
  if (shown === total) return t('memory.time.eventsShown', { shown, total })
  return t('memory.time.eventsShown', { shown, total })
})

// -----------------------------------------------------------------------
// Date grouping
// -----------------------------------------------------------------------
interface TimelineGroup {
  dateLabel: string
  events: TimelineEvent[]
}

function formatDateLabel(timestamp: string): string {
  const eventDate = new Date(timestamp)
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today.getTime() - 86400000)
  const eventDay = new Date(eventDate.getFullYear(), eventDate.getMonth(), eventDate.getDate())

  if (eventDay.getTime() === today.getTime()) {
    return t('memory.history.today')
  }
  if (eventDay.getTime() === yesterday.getTime()) {
    return t('memory.history.yesterday')
  }

  return eventDate.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

const groupedTimeline = computed<TimelineGroup[]>(() => {
  const groups: TimelineGroup[] = []
  let currentLabel = ''
  let currentGroup: TimelineEvent[] = []

  for (const event of pagedEvents.value) {
    const label = formatDateLabel(event.timestamp)
    if (label !== currentLabel) {
      if (currentGroup.length > 0) {
        groups.push({ dateLabel: currentLabel, events: currentGroup })
      }
      currentLabel = label
      currentGroup = []
    }
    currentGroup.push(event)
  }

  if (currentGroup.length > 0) {
    groups.push({ dateLabel: currentLabel, events: currentGroup })
  }

  return groups
})

// -----------------------------------------------------------------------
// Time formatting
// -----------------------------------------------------------------------
function formatEventTime(timestamp: string): string {
  const d = new Date(timestamp)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
  })
}

// -----------------------------------------------------------------------
// Handlers
// -----------------------------------------------------------------------
function handleFilterChange(val: string | undefined) {
  memoryStore.setHistoryFilter((val ?? '') as '' | TimelineEventType)
}

function handleLoadMore() {
  memoryStore.loadMoreHistory()
}

function handleViewMemory(event: TimelineEvent) {
  if (event.memory_id) {
    emit('view-memory', event.memory_id)
  }
}

function handleEventClick(event: TimelineEvent) {
  // Also emit view memory on card click if memory_id exists
  if (event.memory_id) {
    emit('view-memory', event.memory_id)
  }
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
  font-size: 16px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.empty-desc {
  font-size: 13px;
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
  font-size: 12px;
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
  font-size: 13px;
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
  border-radius: 8px;
  cursor: pointer;
  transition: box-shadow 0.2s ease;
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
  font-size: 10px;
  font-weight: 500;
  border-radius: 3px;
  line-height: 1.6;
  flex-shrink: 0;
}

.event-type-badge.badge-created {
  color: #00a870;
  background: rgba(0, 168, 112, 0.08);
}

.event-type-badge.badge-updated {
  color: #4094f7;
  background: rgba(64, 148, 247, 0.08);
}

.event-type-badge.badge-deleted {
  color: #e34d59;
  background: rgba(227, 77, 89, 0.08);
}

.event-type-badge.badge-verdict_changed {
  color: #722ed1;
  background: rgba(114, 46, 209, 0.08);
}

.event-type-badge.badge-dreamer_action {
  color: #ed7b2f;
  background: rgba(237, 123, 47, 0.08);
}

.event-type-badge.badge-consolidation {
  color: #8c8c8c;
  background: rgba(140, 140, 140, 0.08);
}

.event-type-badge.badge-pruner {
  color: #fa8c16;
  background: rgba(250, 140, 22, 0.08);
}

.event-type-badge.badge-health_check {
  color: #0052d9;
  background: rgba(0, 82, 217, 0.08);
}

.event-timestamp {
  margin-left: auto;
  font-size: 11px;
  color: var(--td-text-color-placeholder);
  white-space: nowrap;
  flex-shrink: 0;
}

.timeline-card-body {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
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
  font-size: 11px;
  color: var(--td-brand-color);
  cursor: pointer;
  white-space: nowrap;
  flex-shrink: 0;
  padding: 2px 6px;
  border-radius: 4px;
  transition: background 0.15s ease;
}

.event-memory-link:hover {
  background: var(--td-brand-color-light);
  text-decoration: underline;
}

.timeline-card-preview {
  margin-top: 4px;
  padding: 4px 8px;
  font-size: 11px;
  color: var(--td-text-color-placeholder);
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 4px;
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
</style>
