<template>
  <div class="memory-dream" data-testid="memory-dream-tab">
    <!-- Status -->
    <section class="dream-section">
      <h3 class="section-title">{{ $t('memory.dream.statusTitle') }}</h3>
      <p class="section-desc">{{ $t('memory.dream.desc') }}</p>
      <div v-if="status" class="status-grid" data-testid="memory-dream-status">
        <div class="status-item">
          <span class="status-key">{{ $t('memory.dream.schedule') }}</span>
          <t-tag :theme="status.scheduled ? 'success' : 'default'" variant="light" size="small">
            {{ status.scheduled ? $t('memory.dream.scheduledEvery', { interval: status.interval }) : $t('memory.dream.notScheduled') }}
          </t-tag>
        </div>
        <div class="status-item">
          <span class="status-key">{{ $t('memory.dream.mode') }}</span>
          <span class="status-value">{{ status.dry_run ? $t('memory.dream.modeDryRun') : $t('memory.dream.modeLive') }}</span>
        </div>
        <div class="status-item">
          <span class="status-key">{{ $t('memory.dream.lastRun') }}</span>
          <span class="status-value">{{ status.last_run_at ? formatDate(status.last_run_at) : $t('memory.dream.never') }}</span>
        </div>
        <div class="status-item">
          <span class="status-key">{{ $t('memory.dream.runs') }}</span>
          <span class="status-value">{{ status.runs }}</span>
        </div>
        <div class="status-item">
          <span class="status-key">{{ $t('memory.dream.merges') }}</span>
          <span class="status-value">
            {{
              status.merge_live
                ? $t('memory.dream.mergesLive')
                : $t('memory.dream.mergesProbation', { runs: status.runs, total: status.merge_live_after_runs })
            }}
          </span>
        </div>
        <div v-if="status.running" class="status-item">
          <t-tag theme="warning" variant="light" size="small">{{ $t('memory.dream.running') }}</t-tag>
        </div>
      </div>
      <p class="section-note">{{ $t('memory.dream.safety') }}</p>
    </section>

    <!-- Actions (Admin) -->
    <section v-if="canManage" class="dream-section">
      <h3 class="section-title">{{ $t('memory.dream.actionsTitle') }}</h3>
      <div class="dream-actions">
        <t-button
          variant="outline"
          :loading="memoryStore.dreamerLoading && previewing"
          :disabled="memoryStore.dreamerLoading"
          data-testid="memory-dream-preview"
          @click="run(true)"
        >
          <template #icon><t-icon name="browse" /></template>
          {{ $t('memory.dream.preview') }}
        </t-button>
        <t-button
          theme="primary"
          :loading="memoryStore.dreamerLoading && !previewing"
          :disabled="memoryStore.dreamerLoading"
          data-testid="memory-dream-run"
          @click="confirmRun"
        >
          <template #icon><t-icon name="lightbulb" /></template>
          {{ $t('memory.dream.run') }}
        </t-button>
        <t-button variant="outline" :loading="relinking" data-testid="memory-relink" @click="relink">
          <template #icon><t-icon name="link" /></template>
          {{ $t('memory.dream.relink') }}
        </t-button>
      </div>
    </section>

    <!-- Last result -->
    <section v-if="result" class="dream-section" data-testid="memory-dream-result">
      <h3 class="section-title">
        {{ memoryStore.lastDreamPreview ? $t('memory.dream.previewResult') : $t('memory.dream.runResult') }}
      </h3>
      <p class="section-desc">
        {{
          $t('memory.dream.resultSummary', {
            proposed: result.actions_proposed,
            applied: result.actions_applied,
            tokens: result.token_used,
          })
        }}
      </p>
      <div v-if="result.actions.length === 0" class="empty-text">{{ $t('memory.dream.noProposals') }}</div>
      <div v-else class="proposal-list">
        <div v-for="(a, i) in result.actions" :key="i" class="proposal" data-testid="memory-dream-proposal">
          <div class="proposal-head">
            <t-tag size="small" variant="light">{{ actionLabel(a.type) }}</t-tag>
            <span class="proposal-change">{{ describe(a) }}</span>
            <span class="proposal-confidence">{{ Math.round(a.confidence * 100) }}%</span>
          </div>
          <div class="proposal-reason">{{ a.reason }}</div>
          <t-link theme="primary" size="small" @click="$emit('view-memory', a.target_id)">
            {{ $t('memory.history.viewMemory') }}
          </t-link>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { useMemoryStore } from '@/stores/memory'
import type { DreamAction } from '@/api/memory/index'
import { errorStatus } from '@/stores/memoryState'
import { useMemoryLabels } from './useMemoryLabels'

const props = withDefaults(defineProps<{ kbId: string; canManage?: boolean }>(), { canManage: false })
defineEmits<{ 'view-memory': [memoryId: string] }>()

const { t } = useI18n()
const memoryStore = useMemoryStore()
const { formatDate } = useMemoryLabels()
const previewing = ref(false)
const relinking = ref(false)

const status = computed(() => memoryStore.dreamStatus)
const result = computed(() => memoryStore.lastDreamResult)

watch(
  () => props.kbId,
  (id) => {
    if (id) memoryStore.loadDreamStatus(id).catch(() => undefined)
  },
  { immediate: true },
)

function reportError(err: unknown) {
  const e = err as { message?: string }
  if (errorStatus(err) === 429) MessagePlugin.warning(e?.message || t('memory.dream.cooldown'))
  else MessagePlugin.error(e?.message || t('memory.dream.failed'))
}

async function run(preview: boolean) {
  previewing.value = preview
  try {
    await memoryStore.runDreamer(props.kbId, preview)
    if (!preview) MessagePlugin.success(t('memory.dream.runDone', { count: result.value?.actions_applied ?? 0 }))
  } catch (err) {
    reportError(err)
  }
}

function confirmRun() {
  const dialog = DialogPlugin.confirm({
    header: t('memory.dream.run'),
    body: t('memory.dream.confirmRun'),
    confirmBtn: t('common.confirm'),
    cancelBtn: t('common.cancel'),
    onConfirm: () => {
      dialog.hide()
      run(false)
    },
    onClose: () => dialog.hide(),
  })
}

async function relink() {
  relinking.value = true
  try {
    const res = await memoryStore.relink(props.kbId)
    MessagePlugin.success(t('memory.dream.relinkDone', { memories: res?.memories ?? 0, relations: res?.relations ?? 0 }))
  } catch (err) {
    reportError(err)
  } finally {
    relinking.value = false
  }
}

const ACTIONS = ['update_verdict', 'adjust_importance', 'merge', 'remove']

function actionLabel(type: string): string {
  return ACTIONS.includes(type) ? t(`memory.dream.actions.${type}`) : type
}

function describe(a: DreamAction): string {
  switch (a.type) {
    case 'update_verdict':
      return t('memory.dream.toVerdict', { verdict: t(`memory.verdicts.${a.new_verdict || 'none'}`) })
    case 'adjust_importance':
      return (a.delta ?? 0) > 0 ? '+1' : '−1'
    case 'merge':
      return t('memory.dream.mergeCount', { count: a.target_ids?.length ?? 2 })
    default:
      return ''
  }
}
</script>

<style scoped lang="less">
.memory-dream {
  display: flex;
  flex-direction: column;
  gap: 16px;
  height: 100%;
  overflow-y: auto;
}

.dream-section {
  padding: 16px;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--app-radius-md);
  background: var(--td-bg-color-container);
}

.section-title {
  margin: 0 0 6px;
  font-size: var(--app-text-lg);
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.section-desc,
.section-note {
  margin: 0 0 12px;
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
}

.section-note {
  margin: 12px 0 0;
  color: var(--td-text-color-placeholder);
}

.status-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 10px 16px;
}

.status-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.status-key {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
}

.status-value {
  font-size: var(--app-text-md);
  color: var(--td-text-color-primary);
}

.dream-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.empty-text {
  font-size: var(--app-text-md);
  color: var(--td-text-color-placeholder);
}

.proposal-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.proposal {
  padding: 10px 12px;
  border-radius: var(--app-radius-sm);
  background: var(--td-bg-color-secondarycontainer);
}

.proposal-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.proposal-change {
  font-size: var(--app-text-md);
  color: var(--td-text-color-primary);
}

.proposal-confidence {
  margin-left: auto;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
}

.proposal-reason {
  margin: 6px 0 4px;
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
}
</style>
