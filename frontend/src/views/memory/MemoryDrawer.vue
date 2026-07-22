<template>
  <t-drawer
    v-model:visible="drawerVisible"
    :header="drawerTitle"
    size="560px"
    placement="right"
    destroy-on-close
    :close-btn="true"
    @close="handleClose"
    @visible-change="onVisibleChange"
  >
    <!-- Loading state -->
    <div v-if="loading" class="drawer-loading">
      <t-loading :text="$t('memory.drawer.loading')" size="large" />
    </div>

    <!-- Content -->
    <template v-else-if="localMemory">
      <!-- Verdict selector -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.verdict') }}</label>
        <t-tooltip
          :content="$t('memory.drawer.verdictProtected')"
          :disabled="!isProtectedVerdict"
        >
          <t-select
            :value="localMemory.verdict"
            :disabled="isProtectedVerdict"
            class="drawer-verdict-select"
            @change="handleVerdictChange"
          >
            <t-option
              v-for="v in MEMORY_VERDICTS"
              :key="v"
              :value="v"
              :label="$t(`memory.verdicts.${v}`)"
            />
          </t-select>
        </t-tooltip>
      </div>

      <!-- Tier selector -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.tier') }}</label>
        <t-select
          :value="localMemory.tier"
          class="drawer-tier-select"
          @change="handleTierChange"
        >
          <t-option
            v-for="t in 4"
            :key="t - 1"
            :value="t - 1"
            :label="$t(`memory.tiers.${t - 1}`)"
          />
        </t-select>
      </div>

      <!-- Importance stars -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.importance') }}</label>
        <div class="drawer-importance">
          <t-icon
            v-for="i in 10"
            :key="i"
            :name="i <= localMemory.importance ? 'star-filled' : 'star'"
            :class="['star-icon', { filled: i <= localMemory.importance }]"
            size="18px"
            class="importance-star"
            @click="handleImportanceClick(i)"
          />
          <span class="importance-value">{{ localMemory.importance }}/10</span>
        </div>
      </div>

      <!-- Full content -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.content') }}</label>
        <div class="drawer-content-text">{{ localMemory.content }}</div>
      </div>

      <!-- Metadata grid -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.metadata') }}</label>
        <div class="metadata-grid">
          <div class="metadata-item">
            <span class="metadata-key">{{ $t('memory.drawer.type') }}</span>
            <span class="metadata-value">{{ $t(`memory.types.${localMemory.memory_type}`) }}</span>
          </div>
          <div class="metadata-item">
            <span class="metadata-key">{{ $t('memory.drawer.created') }}</span>
            <span class="metadata-value">{{ formatDate(localMemory.created_at) }}</span>
          </div>
          <div class="metadata-item">
            <span class="metadata-key">{{ $t('memory.drawer.updated') }}</span>
            <span class="metadata-value">{{ formatDate(localMemory.updated_at) }}</span>
          </div>
          <div class="metadata-item">
            <span class="metadata-key">{{ $t('memory.drawer.accessCount') }}</span>
            <span class="metadata-value">{{ localMemory.access_count }}</span>
          </div>
          <div class="metadata-item">
            <span class="metadata-key">{{ $t('memory.drawer.sessionId') }}</span>
            <span class="metadata-value metadata-value-mono">{{ localMemory.session_id || '-' }}</span>
          </div>
        </div>
      </div>

      <!-- Tags with add/remove -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.tags') }}</label>
        <div class="tags-container">
          <t-tag
            v-for="(tag, idx) in localMemory.tags"
            :key="idx"
            closable
            size="small"
            variant="light"
            class="memory-tag"
            @close="handleRemoveTag(idx)"
          >
            {{ tag }}
          </t-tag>

          <div v-if="showTagInput" class="tag-input-wrapper">
            <t-input
              ref="tagInputRef"
              v-model="newTag"
              size="small"
              class="tag-input-field"
              :placeholder="$t('memory.drawer.addTag')"
              @blur="handleAddTag"
              @keyup.enter="handleAddTag"
            />
          </div>
          <t-button
            v-else
            size="small"
            variant="outline"
            class="add-tag-btn"
            @click="showAddTagInput"
          >
            {{ $t('memory.drawer.addTag') }}
          </t-button>
        </div>
      </div>

      <!-- Relations list -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.relations') }}</label>
        <div v-if="relations.length > 0" class="relations-list">
          <div v-for="rel in relations" :key="rel.id" class="relation-item">
            <div class="relation-info">
              <span class="relation-type">{{ rel.relation }}</span>
              <span class="relation-target-id">{{ rel.to_uuid }}</span>
            </div>
            <t-link
              theme="primary"
              size="small"
              @click="$emit('view-in-graph', rel)"
            >
              {{ $t('memory.drawer.viewInGraph') }}
            </t-link>
          </div>
        </div>
        <span v-else class="drawer-empty-text">{{ $t('memory.drawer.noRelations') }}</span>
      </div>

      <!-- Lint issues list -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.lintIssues') }}</label>
        <div v-if="lintIssues.length > 0" class="lint-list">
          <div
            v-for="(issue, idx) in lintIssues"
            :key="idx"
            :class="['lint-item', `lint-severity-${issue.severity}`]"
          >
            <t-icon
              :name="lintSeverityIcon(issue.severity)"
              size="16px"
              class="lint-icon"
            />
            <div class="lint-content">
              <span class="lint-rule">{{ issue.rule }}</span>
              <span class="lint-message">{{ issue.message }}</span>
            </div>
          </div>
        </div>
        <span v-else class="drawer-empty-text">{{ $t('memory.drawer.noLintIssues') }}</span>
      </div>

      <!-- History timeline -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.history') }}</label>
        <div v-if="historyTimeline.length > 0" class="history-timeline">
          <div
            v-for="event in historyTimeline"
            :key="event.id"
            class="history-event"
          >
            <div class="history-event-dot" :class="`history-dot-${event.type}`" />
            <div class="history-event-body">
              <div class="history-event-header">
                <span class="history-event-type">{{ historyEventLabel(event.type) }}</span>
                <span class="history-event-time">{{ formatRelativeTime(event.timestamp) }}</span>
              </div>
              <div class="history-event-desc">{{ event.description }}</div>
            </div>
          </div>
        </div>
        <span v-else class="drawer-empty-text">{{ $t('memory.drawer.noHistory') }}</span>
      </div>
    </template>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AgentMemory, MemoryRelation, MemoryLintIssue, MemoryVerdict, TimelineEvent } from '@/api/memory/index'
import { MEMORY_VERDICTS, isVerdictProtected } from '@/api/memory/index'
import { getMemory } from '@/api/memory/index'
import { MessagePlugin } from 'tdesign-vue-next'

const props = defineProps<{
  visible: boolean
  memory: AgentMemory | null
  relations?: MemoryRelation[]
  lintIssues?: MemoryLintIssue[]
  historyEvents?: TimelineEvent[]
}>()

const emit = defineEmits<{
  close: []
  update: [memory: AgentMemory]
  'view-in-graph': [relation: MemoryRelation]
}>()

const { t } = useI18n()

// -----------------------------------------------------------------------
// Visibility (v-model)
// -----------------------------------------------------------------------
const drawerVisible = computed({
  get: () => props.visible,
  set: (val) => {
    if (!val) handleClose()
  },
})

// -----------------------------------------------------------------------
// Local state
// -----------------------------------------------------------------------
const loading = ref(false)
const localMemory = ref<AgentMemory | null>(null)
const relations = ref<MemoryRelation[]>(props.relations || [])
const lintIssues = ref<MemoryLintIssue[]>(props.lintIssues || [])
const historyTimeline = ref<TimelineEvent[]>(props.historyEvents || [])

// Tag input
const showTagInput = ref(false)
const newTag = ref('')
const tagInputRef = ref<InstanceType<typeof import('tdesign-vue-next')['Input']> | null>(null)

// -----------------------------------------------------------------------
// Drawer header
// -----------------------------------------------------------------------
const drawerTitle = computed(() => {
  if (!localMemory.value) return t('memory.drawer.title')
  const verdictLabel = t(`memory.verdicts.${localMemory.value.verdict || 'none'}`)
  const preview = localMemory.value.content.length > 60
    ? localMemory.value.content.slice(0, 57) + '...'
    : localMemory.value.content
  return `${verdictLabel}: ${preview}`
})

// -----------------------------------------------------------------------
// Protected verdict check
// -----------------------------------------------------------------------
const isProtectedVerdict = computed(() => {
  if (!localMemory.value) return false
  return isVerdictProtected(localMemory.value.verdict)
})

// -----------------------------------------------------------------------
// Fetch full memory details on open
// -----------------------------------------------------------------------
async function loadMemoryDetails() {
  if (!props.memory) return

  loading.value = true
  try {
    const resp = await getMemory(props.memory.id)
    if (resp.data?.data) {
      localMemory.value = { ...resp.data.data }
    } else {
      localMemory.value = { ...props.memory }
    }
  } catch {
    // Fallback to the prop data if API fails
    localMemory.value = { ...props.memory }
  } finally {
    loading.value = false
  }
}

function onVisibleChange(visible: boolean) {
  if (visible) {
    // Reset local state
    relations.value = props.relations || []
    lintIssues.value = props.lintIssues || []
    historyTimeline.value = props.historyEvents || []
    showTagInput.value = false
    newTag.value = ''
    loadMemoryDetails()
  }
}

// -----------------------------------------------------------------------
// Watchers for prop changes
// -----------------------------------------------------------------------
watch(
  () => props.relations,
  (val) => {
    if (val) relations.value = val
  }
)

watch(
  () => props.lintIssues,
  (val) => {
    if (val) lintIssues.value = val
  }
)

watch(
  () => props.historyEvents,
  (val) => {
    if (val) historyTimeline.value = val
  }
)

// -----------------------------------------------------------------------
// Verdict change
// -----------------------------------------------------------------------
function handleVerdictChange(verdict: MemoryVerdict) {
  if (!localMemory.value) return
  if (isVerdictProtected(localMemory.value.verdict)) {
    MessagePlugin.warning(t('memory.drawer.verdictProtected'))
    return
  }
  localMemory.value.verdict = verdict
  emitUpdate()
}

// -----------------------------------------------------------------------
// Tier change
// -----------------------------------------------------------------------
function handleTierChange(tier: number) {
  if (!localMemory.value) return
  localMemory.value.tier = tier
  emitUpdate()
}

// -----------------------------------------------------------------------
// Importance change
// -----------------------------------------------------------------------
function handleImportanceClick(importance: number) {
  if (!localMemory.value) return
  // Clicking the same value toggles it off (set to 0), otherwise set to clicked value
  if (localMemory.value.importance === importance && importance === 1) {
    localMemory.value.importance = 0
  } else {
    localMemory.value.importance = importance
  }
  emitUpdate()
}

// -----------------------------------------------------------------------
// Tags
// -----------------------------------------------------------------------
function showAddTagInput() {
  showTagInput.value = true
  nextTick(() => {
    // Focus the input
    const inputEl = document.querySelector('.tag-input-field input') as HTMLInputElement | null
    inputEl?.focus()
  })
}

function handleAddTag() {
  const tag = newTag.value.trim()
  if (tag && localMemory.value) {
    if (!localMemory.value.tags) {
      localMemory.value.tags = []
    }
    if (!localMemory.value.tags.includes(tag)) {
      localMemory.value.tags.push(tag)
      emitUpdate()
    }
  }
  showTagInput.value = false
  newTag.value = ''
}

function handleRemoveTag(idx: number) {
  if (!localMemory.value?.tags) return
  localMemory.value.tags.splice(idx, 1)
  emitUpdate()
}

// -----------------------------------------------------------------------
// Emit update
// -----------------------------------------------------------------------
function emitUpdate() {
  if (localMemory.value) {
    emit('update', { ...localMemory.value })
  }
}

// -----------------------------------------------------------------------
// Close
// -----------------------------------------------------------------------
function handleClose() {
  showTagInput.value = false
  newTag.value = ''
  loading.value = false
  emit('close')
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------
function formatDate(dateStr: string): string {
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatRelativeTime(dateStr: string): string {
  const now = Date.now()
  const then = new Date(dateStr).getTime()
  if (isNaN(then)) return ''
  const diffMs = now - then
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)

  if (diffSec < 60) return t('memory.time.justNow')
  if (diffMin < 60) return t('memory.time.minutesAgo', { n: diffMin })
  if (diffHour < 24) return t('memory.time.hoursAgo', { n: diffHour })
  if (diffDay < 7) return t('memory.time.daysAgo', { n: diffDay })
  return formatDate(dateStr)
}

function historyEventLabel(type: string): string {
  const labels: Record<string, string> = {
    created: 'created',
    updated: 'updated',
    deleted: 'deleted',
    verdict_changed: 'verdictChanged',
    dreamer_action: 'dreamer',
    consolidation: 'consolidated',
    pruner: 'pruned',
    health_check: 'healthCheck',
  }
  const key = `memory.drawer.historyEvents.${labels[type] || type}`
  return t(key)
}

function lintSeverityIcon(severity: string): string {
  const icons: Record<string, string> = {
    error: 'error-circle',
    warning: 'warning-triangle',
    info: 'info-circle',
  }
  return icons[severity] || 'info-circle'
}
</script>

<style scoped lang="less">
.drawer-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}

// -----------------------------------------------------------------------
// Sections
// -----------------------------------------------------------------------
.drawer-section {
  padding: 14px 0;
  border-bottom: 1px solid var(--td-component-stroke);

  &:first-child {
    padding-top: 0;
  }

  &:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }
}

.drawer-section-label {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--td-text-color-primary);
  margin-bottom: 8px;
  user-select: none;

  &::before {
    content: '';
    display: inline-block;
    width: 3px;
    height: 14px;
    background: var(--td-brand-color);
    border-radius: 2px;
    margin-right: 8px;
    vertical-align: text-bottom;
  }
}

// -----------------------------------------------------------------------
// Verdict
// -----------------------------------------------------------------------
.drawer-verdict-select {
  width: 200px;
}

// -----------------------------------------------------------------------
// Tier
// -----------------------------------------------------------------------
.drawer-tier-select {
  width: 160px;
}

// -----------------------------------------------------------------------
// Importance stars
// -----------------------------------------------------------------------
.drawer-importance {
  display: flex;
  align-items: center;
  gap: 2px;
}

.importance-star {
  color: var(--td-text-color-disabled);
  cursor: pointer;
  transition: color 0.15s, transform 0.15s;

  &.filled {
    color: var(--td-warning-color);
  }

  &:hover {
    transform: scale(1.2);
  }
}

.importance-value {
  margin-left: 10px;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
}

// -----------------------------------------------------------------------
// Content
// -----------------------------------------------------------------------
.drawer-content-text {
  font-size: 13px;
  line-height: 1.6;
  color: var(--td-text-color-secondary);
  word-break: break-word;
  white-space: pre-wrap;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 6px;
  padding: 10px 12px;
  max-height: 300px;
  overflow-y: auto;
}

// -----------------------------------------------------------------------
// Metadata grid
// -----------------------------------------------------------------------
.metadata-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 16px;
}

.metadata-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.metadata-key {
  font-size: 11px;
  color: var(--td-text-color-placeholder);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.metadata-value {
  font-size: 13px;
  color: var(--td-text-color-primary);
}

.metadata-value-mono {
  font-family: monospace;
  font-size: 12px;
}

// -----------------------------------------------------------------------
// Tags
// -----------------------------------------------------------------------
.tags-container {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.memory-tag {
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag-input-wrapper {
  display: inline-flex;
}

.tag-input-field {
  width: 140px;
}

.add-tag-btn {
  font-size: 12px;
}

// -----------------------------------------------------------------------
// Relations
// -----------------------------------------------------------------------
.relations-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.relation-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 8px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 4px;
}

.relation-info {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.relation-type {
  font-size: 12px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.relation-target-id {
  font-size: 11px;
  color: var(--td-text-color-placeholder);
  font-family: monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

// -----------------------------------------------------------------------
// Lint issues
// -----------------------------------------------------------------------
.lint-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.lint-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 8px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 4px;
  border-left: 3px solid var(--td-info-color);

  &.lint-severity-error {
    border-left-color: var(--td-error-color);
  }

  &.lint-severity-warning {
    border-left-color: var(--td-warning-color);
  }

  &.lint-severity-info {
    border-left-color: var(--td-info-color);
  }
}

.lint-icon {
  flex-shrink: 0;
  margin-top: 2px;
}

.lint-severity-error .lint-icon {
  color: var(--td-error-color);
}

.lint-severity-warning .lint-icon {
  color: var(--td-warning-color);
}

.lint-severity-info .lint-icon {
  color: var(--td-info-color);
}

.lint-content {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.lint-rule {
  font-size: 11px;
  font-weight: 500;
  color: var(--td-text-color-primary);
  font-family: monospace;
}

.lint-message {
  font-size: 12px;
  color: var(--td-text-color-secondary);
  line-height: 1.4;
}

// -----------------------------------------------------------------------
// History timeline
// -----------------------------------------------------------------------
.history-timeline {
  display: flex;
  flex-direction: column;
  gap: 0;
  position: relative;

  &::before {
    content: '';
    position: absolute;
    left: 7px;
    top: 8px;
    bottom: 8px;
    width: 2px;
    background: var(--td-component-stroke);
    border-radius: 1px;
  }
}

.history-event {
  display: flex;
  gap: 10px;
  padding: 6px 0;
  position: relative;
}

.history-event-dot {
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--td-bg-color-page);
  border: 2px solid var(--td-component-stroke);
  z-index: 1;

  &.history-dot-created {
    border-color: var(--td-success-color);
    background: var(--td-success-color-light);
  }

  &.history-dot-updated {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }

  &.history-dot-verdict_changed {
    border-color: var(--td-warning-color);
    background: var(--td-warning-color-light);
  }

  &.history-dot-dreamer_action {
    border-color: var(--td-info-color);
    background: var(--td-info-color-light);
  }

  &.history-dot-consolidation {
    border-color: var(--td-purple-color, #a855f7);
    background: color-mix(in srgb, var(--td-purple-color, #a855f7) 15%, transparent);
  }

  &.history-dot-pruner {
    border-color: var(--td-error-color);
    background: var(--td-error-color-light);
  }

  &.history-dot-health_check {
    border-color: var(--td-success-color);
    background: var(--td-success-color-light);
  }
}

.history-event-body {
  flex: 1;
  min-width: 0;
}

.history-event-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.history-event-type {
  font-size: 12px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.history-event-time {
  font-size: 11px;
  color: var(--td-text-color-placeholder);
  margin-left: auto;
}

.history-event-desc {
  font-size: 12px;
  color: var(--td-text-color-secondary);
  line-height: 1.4;
  margin-top: 2px;
}

// -----------------------------------------------------------------------
// Empty text
// -----------------------------------------------------------------------
.drawer-empty-text {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  font-style: italic;
}
</style>
