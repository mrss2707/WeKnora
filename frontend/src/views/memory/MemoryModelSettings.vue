<template>
  <section class="dream-section" data-testid="memory-model-settings">
    <h3 class="section-title">{{ $t('memory.settings.title') }}</h3>
    <p class="section-desc">{{ $t('memory.settings.desc') }}</p>
    <div class="model-rows">
      <div v-for="row in rows" :key="row.purpose" class="model-row" :data-testid="`memory-model-${row.purpose}`">
        <div class="model-label">
          <span class="model-key">{{ $t(`memory.settings.${row.purpose}`) }}</span>
          <span class="model-hint">{{ $t(`memory.settings.${row.purpose}Hint`) }}</span>
        </div>
        <ModelSelector
          v-if="canManage"
          model-type="KnowledgeQA"
          clearable
          :selected-model-id="draft[row.purpose]"
          :placeholder="$t('memory.settings.useSummary')"
          :data-testid="`memory-model-${row.purpose}-select`"
          @update:selected-model-id="(id: string) => (draft[row.purpose] = id)"
          @add-model="openModelSettings"
        />
        <div class="model-effective" :data-testid="`memory-model-${row.purpose}-effective`">
          <t-tag
            :theme="row.effective?.source === 'unavailable' ? 'danger' : 'default'"
            variant="light"
            size="small"
          >
            {{ $t(`memory.settings.source.${row.effective?.source || 'kb_summary'}`) }}
          </t-tag>
          <span class="model-name">{{ row.effective?.name || row.effective?.model_id || '—' }}</span>
        </div>
        <t-alert
          v-if="row.effective?.source === 'unavailable'"
          theme="warning"
          :message="$t(`memory.settings.unavailable.${row.purpose}`)"
        />
      </div>
    </div>
    <div v-if="canManage" class="model-actions">
      <t-button
        theme="primary"
        :loading="saving"
        :disabled="!dirty"
        data-testid="memory-model-save"
        @click="save"
      >
        {{ $t('memory.settings.save') }}
      </t-button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import ModelSelector from '@/components/ModelSelector.vue'
import { useMemoryStore } from '@/stores/memory'
import { useUIStore } from '@/stores/ui'

type Purpose = 'dreamer' | 'text'

const props = withDefaults(defineProps<{ kbId: string; canManage?: boolean }>(), { canManage: false })

const { t } = useI18n()
const memoryStore = useMemoryStore()
const uiStore = useUIStore()
const saving = ref(false)
const draft = reactive<Record<Purpose, string>>({ dreamer: '', text: '' })

const settings = computed(() => memoryStore.modelSettings)
const rows = computed(() =>
  (['dreamer', 'text'] as Purpose[]).map((purpose) => ({ purpose, effective: settings.value?.[purpose] })),
)
const dirty = computed(
  () =>
    draft.dreamer !== (settings.value?.dreamer_model_id ?? '') || draft.text !== (settings.value?.text_model_id ?? ''),
)

watch(
  settings,
  (value) => {
    draft.dreamer = value?.dreamer_model_id ?? ''
    draft.text = value?.text_model_id ?? ''
  },
  { immediate: true },
)

watch(
  () => props.kbId,
  (id) => {
    if (id) memoryStore.loadModelSettings(id).catch(() => undefined)
  },
  { immediate: true },
)

async function save() {
  saving.value = true
  try {
    await memoryStore.saveModelSettings(props.kbId, draft.dreamer, draft.text)
    MessagePlugin.success(t('memory.settings.saved'))
  } catch (err) {
    MessagePlugin.error((err as { message?: string })?.message || t('memory.settings.saveFailed'))
  } finally {
    saving.value = false
  }
}

function openModelSettings() {
  uiStore.openSettings('models', 'chat')
  window.dispatchEvent(new CustomEvent('settings-nav', { detail: { section: 'models', subsection: 'chat' } }))
}
</script>

<style scoped lang="less">
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

.section-desc {
  margin: 0 0 12px;
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
}

.model-rows {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 16px;
}

.model-row {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.model-label {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.model-key {
  font-size: var(--app-text-md);
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.model-hint {
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
}

.model-effective {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.model-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--app-text-md);
  color: var(--td-text-color-secondary);
}

.model-actions {
  margin-top: 12px;
}
</style>
