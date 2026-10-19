<template>
  <t-drawer
    v-model:visible="drawerVisible"
    :header="drawerTitle"
    size="560px"
    placement="right"
    destroy-on-close
    :footer="false"
    :close-btn="true"
    data-testid="memory-drawer"
    @close="handleClose"
  >
    <!-- Loading state -->
    <div v-if="loading && !localMemory" class="drawer-loading">
      <t-loading :text="$t('memory.drawer.loading')" size="large" />
    </div>

    <!-- Content -->
    <template v-else-if="localMemory">
      <div v-if="canEdit" class="drawer-actions">
        <t-button
          v-if="canManage && hasMergeHistory"
          size="small"
          variant="outline"
          data-testid="memory-undo-merge"
          @click="handleUndoMerge"
        >
          <template #icon><t-icon name="rollback" /></template>
          {{ $t('memory.drawer.undoMerge') }}
        </t-button>
        <t-button
          size="small"
          variant="outline"
          theme="danger"
          data-testid="memory-delete"
          @click="handleDelete"
        >
          <template #icon><t-icon name="delete" /></template>
          {{ $t('memory.drawer.delete') }}
        </t-button>
      </div>

      <!-- Verdict selector -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.verdict') }}</label>
        <t-tooltip :content="$t('memory.drawer.verdictProtectedHint')" :disabled="!isProtectedVerdict">
          <t-select
            :value="localMemory.verdict"
            :disabled="!canEdit || saving"
            class="drawer-verdict-select"
            data-testid="memory-verdict"
            @change="handleVerdictChange"
          >
            <t-option v-for="v in MEMORY_VERDICTS" :key="v" :value="v" :label="$t(`memory.verdicts.${v}`)" />
          </t-select>
        </t-tooltip>
      </div>

      <!-- Tier selector -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.tier') }}</label>
        <t-select
          :value="localMemory.tier"
          :disabled="!canEdit || saving"
          class="drawer-tier-select"
          data-testid="memory-tier"
          @change="handleTierChange"
        >
          <t-option v-for="n in 4" :key="n - 1" :value="n - 1" :label="$t(`memory.tiers.${n - 1}`)" />
        </t-select>
      </div>

      <!-- Importance stars -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.importance') }}</label>
        <div class="drawer-importance" data-testid="memory-importance">
          <button
            v-for="i in MEMORY_IMPORTANCE_MAX"
            :key="i"
            type="button"
            class="importance-choice"
            :disabled="!canEdit || saving"
            :aria-label="`${$t('memory.drawer.importance')}: ${i}`"
            :aria-pressed="i === localMemory.importance"
            :data-testid="`memory-importance-${i}`"
            @click="handleImportanceClick(i)"
          >
            <t-icon
              :name="i <= localMemory.importance ? 'star-filled' : 'star'"
              :class="['star-icon', 'importance-star', { filled: i <= localMemory.importance }]"
              size="18px"
            />
          </button>
          <span class="importance-value">{{ localMemory.importance }}/{{ MEMORY_IMPORTANCE_MAX }}</span>
        </div>
      </div>

      <!-- Content (editable) -->
      <div class="drawer-section">
        <label class="drawer-section-label">
          {{ $t('memory.drawer.content') }}
          <t-link
            v-if="canEdit && !editingContent"
            theme="primary"
            size="small"
            data-testid="memory-edit-content"
            @click="startEditContent"
          >
            {{ $t('memory.drawer.editContent') }}
          </t-link>
        </label>
        <template v-if="editingContent">
          <t-textarea
            v-model="contentDraft"
            :autosize="{ minRows: 4, maxRows: 16 }"
            :maxlength="MEMORY_CONTENT_MAX"
            data-testid="memory-content-editor"
          />
          <div class="content-editor-actions">
            <t-button size="small" variant="outline" @click="editingContent = false">{{ $t('common.cancel') }}</t-button>
            <t-button
              size="small"
              theme="primary"
              :loading="saving"
              :disabled="!contentDraftValid"
              data-testid="memory-save-content"
              @click="saveContent"
            >
              {{ $t('common.save') }}
            </t-button>
          </div>
        </template>
        <div v-else class="drawer-content-text" data-testid="memory-content">{{ localMemory.content }}</div>
      </div>

      <!-- Metadata grid -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.metadata') }}</label>
        <div class="metadata-grid">
          <div class="metadata-item">
            <span class="metadata-key">{{ $t('memory.drawer.type') }}</span>
            <span class="metadata-value">{{ typeLabel(localMemory.memory_type) }}</span>
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

      <!-- Tags -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.tags') }}</label>
        <div class="tags-container" data-testid="memory-tags">
          <t-tag
            v-for="(tag, idx) in localMemory.tags || []"
            :key="tag"
            :closable="canEdit"
            size="small"
            variant="light"
            class="memory-tag"
            @close="handleRemoveTag(idx)"
          >
            {{ tag }}
          </t-tag>
          <template v-if="canEdit">
            <div v-if="showTagInput" class="tag-input-wrapper">
              <t-input
                v-model="newTag"
                size="small"
                class="tag-input-field"
                :placeholder="$t('memory.drawer.addTag')"
                data-testid="memory-tag-input"
                @blur="handleAddTag"
                @enter="handleAddTag"
              />
            </div>
            <t-button
              v-else
              size="small"
              variant="outline"
              class="add-tag-btn"
              data-testid="memory-add-tag"
              @click="showAddTagInput"
            >
              {{ $t('memory.drawer.addTag') }}
            </t-button>
          </template>
        </div>
      </div>

      <!-- Relations (one hop, from the graph API) -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.relations') }}</label>
        <div v-if="relations.length > 0" class="relations-list" data-testid="memory-relations">
          <div
            v-for="rel in relations"
            :key="rel.key"
            class="relation-item clickable"
            @click="$emit('view-memory', rel.otherId)"
          >
            <div class="relation-info">
              <span class="relation-type">{{ relationLabel(rel.type) }}</span>
              <span class="relation-target-id">{{ rel.label }}</span>
            </div>
            <t-link theme="primary" size="small" @click.stop="$emit('view-in-graph', localMemory.id)">
              {{ $t('memory.drawer.viewInGraph') }}
            </t-link>
          </div>
        </div>
        <span v-else class="drawer-empty-text">{{ $t('memory.drawer.noRelations') }}</span>
      </div>

      <!-- History (audit trail) -->
      <div class="drawer-section">
        <label class="drawer-section-label">{{ $t('memory.drawer.history') }}</label>
        <div v-if="history.length > 0" class="history-timeline" data-testid="memory-history">
          <div v-for="entry in history" :key="entry.id" class="history-event">
            <div class="history-event-dot" :class="`history-dot-${entry.action}`" />
            <div class="history-event-body">
              <div class="history-event-header">
                <span class="history-event-type">{{ auditActionLabel(entry.action) }}</span>
                <span class="history-event-time">{{ formatRelativeTime(entry.created_at) }}</span>
              </div>
              <div class="history-event-desc">{{ describeAuditChange(entry) }}</div>
              <div class="history-event-actor">{{ actorLabel(entry) }}</div>
            </div>
          </div>
        </div>
        <span v-else class="drawer-empty-text">{{ $t('memory.drawer.noHistory') }}</span>
      </div>
    </template>

    <div v-else class="drawer-empty-placeholder">
      <t-empty :description="$t('memory.browse.empty')" />
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import type { AgentMemory, MemoryAuditEntry, MemoryVerdict, UpdateMemoryRequest } from '@/api/memory/index'
import {
  MEMORY_VERDICTS,
  MEMORY_IMPORTANCE_MAX,
  MEMORY_CONTENT_MAX,
  MEMORY_CONTENT_MIN,
  isVerdictProtected,
  clampImportance,
  getMemory,
  getMemoryGraph,
  getMemoryHistory,
} from '@/api/memory/index'
import { useMemoryStore } from '@/stores/memory'
import { isConflict } from '@/stores/memoryState'
import { useMemoryLabels } from './useMemoryLabels'

const props = withDefaults(
  defineProps<{
    visible: boolean
    memory: AgentMemory | null
    canEdit?: boolean
    canManage?: boolean
  }>(),
  { canEdit: false, canManage: false },
)

const emit = defineEmits<{
  'update:visible': [visible: boolean]
  close: []
  update: [memory: AgentMemory]
  deleted: [memoryId: string]
  'view-in-graph': [memoryId: string]
  'view-memory': [memoryId: string]
}>()

const { t } = useI18n()
const memoryStore = useMemoryStore()
const { typeLabel, relationLabel, auditActionLabel, actorLabel, describeAuditChange, formatDate, formatRelativeTime } =
  useMemoryLabels()

const drawerVisible = computed({
  get: () => props.visible,
  set: (val) => {
    emit('update:visible', val)
    if (!val) handleClose()
  },
})

// -----------------------------------------------------------------------
// Local state
// -----------------------------------------------------------------------
const loading = ref(false)
const saving = ref(false)
const localMemory = ref<AgentMemory | null>(null)
const history = ref<MemoryAuditEntry[]>([])
const relations = ref<{ key: string; type: string; otherId: string; label: string }[]>([])

const showTagInput = ref(false)
const newTag = ref('')

const editingContent = ref(false)
const contentDraft = ref('')
const contentDraftValid = computed(() => {
  const n = [...contentDraft.value.trim()].length
  return n >= MEMORY_CONTENT_MIN && n <= MEMORY_CONTENT_MAX && contentDraft.value.trim() !== localMemory.value?.content
})

const drawerTitle = computed(() => {
  if (!localMemory.value) return t('memory.drawer.title')
  const verdictLabel = t(`memory.verdicts.${localMemory.value.verdict || 'none'}`)
  const content = localMemory.value.content
  const preview = [...content].length > 60 ? [...content].slice(0, 57).join('') + '...' : content
  return `${verdictLabel}: ${preview}`
})

const isProtectedVerdict = computed(() => !!localMemory.value && isVerdictProtected(localMemory.value.verdict))
const hasMergeHistory = computed(() => history.value.some((e) => e.action === 'merged'))

// -----------------------------------------------------------------------
// Loading
// -----------------------------------------------------------------------
let detailsRequest = 0
async function loadDetails(id: string) {
  const request = ++detailsRequest
  loading.value = true
  try {
    const [mem, hist, graph] = await Promise.allSettled([
      getMemory(id),
      getMemoryHistory({ memory_id: id, page_size: 20 }),
      getMemoryGraph(id, { depth: 1, limit: 30 }),
    ])
    if (request !== detailsRequest || !props.visible || props.memory?.id !== id) return
    if (mem.status === 'fulfilled' && mem.value?.data?.id === id) localMemory.value = { ...mem.value.data }
    history.value = hist.status === 'fulfilled' ? hist.value.data?.items ?? [] : []
    if (graph.status === 'fulfilled' && graph.value.data) {
      const labels = new Map(graph.value.data.nodes.map((n) => [n.id, n.label]))
      relations.value = graph.value.data.edges
        .filter((e) => e.source === id || e.target === id)
        .map((e, i) => {
          const otherId = e.source === id ? e.target : e.source
          return { key: `${e.id || i}`, type: e.relation_type, otherId, label: labels.get(otherId) || otherId }
        })
    } else {
      relations.value = []
    }
  } finally {
    if (request === detailsRequest) loading.value = false
  }
}

watch(
  () => [props.visible, props.memory?.id] as const,
  ([visible]) => {
    ++detailsRequest
    loading.value = false
    showTagInput.value = false
    newTag.value = ''
    editingContent.value = false
    if (visible && props.memory) {
      localMemory.value = { ...props.memory }
      loadDetails(props.memory.id)
    } else if (!visible) {
      localMemory.value = null
      history.value = []
      relations.value = []
    }
  },
  { immediate: true },
)

// -----------------------------------------------------------------------
// Saving (optimistic lock on the version shown)
// -----------------------------------------------------------------------
async function persist(patch: UpdateMemoryRequest): Promise<boolean> {
  if (!localMemory.value) return false
  const before = { ...localMemory.value }
  saving.value = true
  try {
    const updated = await memoryStore.patchMemory(before, patch)
    localMemory.value = { ...before, ...updated }
    emit('update', { ...localMemory.value })
    const hist = await getMemoryHistory({ memory_id: before.id, page_size: 20 }).catch(() => null)
    if (hist) history.value = hist.data?.items ?? []
    return true
  } catch (err: any) {
    localMemory.value = before
    if (isConflict(err)) {
      promptReload(err?.message)
    } else {
      MessagePlugin.error(err?.message || t('memory.drawer.saveFailed'))
    }
    return false
  } finally {
    saving.value = false
  }
}

function promptReload(message?: string) {
  const dialog = DialogPlugin.confirm({
    header: t('memory.drawer.conflictTitle'),
    body: message ? `${t('memory.drawer.conflictBody')} (${message})` : t('memory.drawer.conflictBody'),
    confirmBtn: t('memory.drawer.conflictReload'),
    cancelBtn: t('common.cancel'),
    onConfirm: async () => {
      dialog.hide()
      editingContent.value = false
      contentDraft.value = ''
      if (localMemory.value) await loadDetails(localMemory.value.id)
    },
    onClose: () => dialog.hide(),
  })
}

function confirmThen(body: string, run: () => void, danger = false) {
  const dialog = DialogPlugin.confirm({
    header: t('memory.browse.confirmTitle'),
    body,
    theme: danger ? 'danger' : 'warning',
    confirmBtn: { content: t('common.confirm'), theme: danger ? 'danger' : 'primary' },
    cancelBtn: t('common.cancel'),
    onConfirm: () => {
      dialog.hide()
      run()
    },
    onClose: () => dialog.hide(),
  })
}

function handleVerdictChange(verdict: MemoryVerdict) {
  if (!localMemory.value || verdict === localMemory.value.verdict) return
  if (isProtectedVerdict.value) {
    confirmThen(t('memory.drawer.confirmUnprotect'), () => persist({ verdict }))
    return
  }
  persist({ verdict })
}

function handleTierChange(tier: number) {
  persist({ tier })
}

function handleImportanceClick(importance: number) {
  if (!props.canEdit || !localMemory.value || saving.value) return
  // Clicking the current value clears it to 0.
  persist({ importance: clampImportance(localMemory.value.importance === importance ? 0 : importance) })
}

function startEditContent() {
  contentDraft.value = localMemory.value?.content ?? ''
  editingContent.value = true
}

async function saveContent() {
  if (!contentDraftValid.value) return
  if (await persist({ content: contentDraft.value.trim() })) editingContent.value = false
}

// -----------------------------------------------------------------------
// Tags
// -----------------------------------------------------------------------
function showAddTagInput() {
  showTagInput.value = true
  nextTick(() => {
    const inputEl = document.querySelector('.tag-input-field input') as HTMLInputElement | null
    inputEl?.focus()
  })
}

async function handleAddTag() {
  const tag = newTag.value.trim()
  showTagInput.value = false
  newTag.value = ''
  if (!tag || !localMemory.value) return
  const tags = localMemory.value.tags ?? []
  if (tags.includes(tag)) return
  await persist({ tags: [...tags, tag] })
}

async function handleRemoveTag(idx: number) {
  const tags = [...(localMemory.value?.tags ?? [])]
  tags.splice(idx, 1)
  await persist({ tags })
}

// -----------------------------------------------------------------------
// Delete / undo merge
// -----------------------------------------------------------------------
function handleDelete() {
  const mem = localMemory.value
  if (!mem) return
  confirmThen(
    t('memory.drawer.confirmDelete'),
    async () => {
      try {
        await memoryStore.deleteMemory(mem.id)
        MessagePlugin.success(t('memory.drawer.deleted'))
        emit('deleted', mem.id)
        handleClose()
      } catch (err: any) {
        MessagePlugin.error(err?.message || t('memory.drawer.saveFailed'))
      }
    },
    true,
  )
}

function handleUndoMerge() {
  const mem = localMemory.value
  if (!mem) return
  confirmThen(t('memory.drawer.confirmUndoMerge'), async () => {
    try {
      const restored = await memoryStore.undoMerge(mem.id)
      MessagePlugin.success(t('memory.drawer.undoMergeDone'))
      if (restored) emit('update', restored)
      await loadDetails(mem.id)
    } catch (err: any) {
      MessagePlugin.error(err?.message || t('memory.drawer.saveFailed'))
    }
  })
}

function handleClose() {
  showTagInput.value = false
  editingContent.value = false
  emit('update:visible', false)
  emit('close')
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
  font-size: var(--app-text-md);
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

.importance-choice {
  display: inline-flex;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;

  &:disabled {
    opacity: 0.5;
    cursor: not-allowed;
    pointer-events: none;
  }
}

.importance-star {
  color: var(--td-text-color-disabled);
  cursor: pointer;
  transition: color var(--app-motion-fast), transform var(--app-motion-fast);

  &.filled {
    color: var(--td-warning-color);
  }

  &:hover {
    transform: scale(1.2);
  }
}

.importance-value {
  margin-left: 10px;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
}

// -----------------------------------------------------------------------
// Content
// -----------------------------------------------------------------------
.drawer-content-text {
  font-size: var(--app-text-md);
  line-height: 1.6;
  color: var(--td-text-color-secondary);
  word-break: break-word;
  white-space: pre-wrap;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: var(--app-radius-sm);
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
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.metadata-value {
  font-size: var(--app-text-md);
  color: var(--td-text-color-primary);
}

.metadata-value-mono {
  font-family: monospace;
  font-size: var(--app-text-sm);
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
  font-size: var(--app-text-sm);
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
  border-radius: var(--app-radius-xs);
}

.relation-info {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.relation-type {
  font-size: var(--app-text-sm);
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.relation-target-id {
  font-size: var(--app-text-xs);
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
  border-radius: var(--app-radius-xs);
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
  font-size: var(--app-text-xs);
  font-weight: 500;
  color: var(--td-text-color-primary);
  font-family: monospace;
}

.lint-message {
  font-size: var(--app-text-sm);
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

  &.history-dot-patched {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }

  &.history-dot-merged {
    border-color: var(--td-warning-color);
    background: var(--td-warning-color-light);
  }

  &.history-dot-merged_into {
    border-color: var(--td-info-color);
    background: var(--td-info-color-light);
  }

  &.history-dot-unmerged {
    border-color: var(--td-purple-color, #a855f7);
    background: color-mix(in srgb, var(--td-purple-color, #a855f7) 15%, transparent);
  }

  &.history-dot-deleted {
    border-color: var(--td-error-color);
    background: var(--td-error-color-light);
  }

  &.history-dot-reclaimed {
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
  font-size: var(--app-text-sm);
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.history-event-time {
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
  margin-left: auto;
}

.history-event-desc {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-secondary);
  line-height: 1.4;
  margin-top: 2px;
}

// -----------------------------------------------------------------------
// Empty text
// -----------------------------------------------------------------------
.drawer-empty-text {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
  font-style: italic;
}

.drawer-empty-placeholder {
  padding: 40px;
  text-align: center;
  color: var(--td-text-color-secondary);
}

.drawer-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-bottom: 12px;
}

.content-editor-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 8px;
}

.relation-item.clickable {
  cursor: pointer;
}

.history-event-actor {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
}
</style>
