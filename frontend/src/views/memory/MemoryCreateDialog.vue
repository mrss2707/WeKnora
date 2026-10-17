<template>
  <t-dialog
    :visible="visible"
    :header="$t('memory.create.title')"
    :confirm-btn="{ content: $t('memory.create.submit'), loading: saving, disabled: !valid }"
    :cancel-btn="$t('common.cancel')"
    width="560px"
    data-testid="memory-create-dialog"
    @confirm="submit"
    @close="close"
    @cancel="close"
  >
    <p class="create-hint">{{ $t('memory.create.hint') }}</p>
    <t-textarea
      v-model="content"
      :placeholder="$t('memory.create.placeholder')"
      :autosize="{ minRows: 5, maxRows: 14 }"
      :maxlength="MEMORY_CONTENT_MAX"
      data-testid="memory-create-content"
    />
    <div class="create-meta">
      <span :class="{ 'create-invalid': length > 0 && length < MEMORY_CONTENT_MIN }">
        {{ $t('memory.create.length', { count: length, min: MEMORY_CONTENT_MIN, max: MEMORY_CONTENT_MAX }) }}
      </span>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useMemoryStore } from '@/stores/memory'
import { MEMORY_CONTENT_MAX, MEMORY_CONTENT_MIN } from '@/api/memory/index'

const props = defineProps<{ visible: boolean; kbId: string }>()
const emit = defineEmits<{ 'update:visible': [visible: boolean] }>()

const { t } = useI18n()
const memoryStore = useMemoryStore()
const content = ref('')
const saving = ref(false)

const length = computed(() => [...content.value.trim()].length)
const valid = computed(() => length.value >= MEMORY_CONTENT_MIN && length.value <= MEMORY_CONTENT_MAX)

watch(
  () => props.visible,
  (open) => {
    if (open) content.value = ''
  },
)

function close() {
  emit('update:visible', false)
}

async function submit() {
  if (!valid.value || saving.value) return
  saving.value = true
  try {
    const result = await memoryStore.createMemory(props.kbId, content.value.trim())
    if (result?.created === false) {
      MessagePlugin.info(t('memory.create.duplicate'))
    } else {
      MessagePlugin.success(t('memory.create.created'))
    }
    const warnings = result?.lint_issues?.length ?? 0
    if (warnings > 0) MessagePlugin.warning(t('memory.create.lintWarnings', { count: warnings }))
    close()
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('memory.create.failed'))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped lang="less">
.create-hint {
  margin: 0 0 12px;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
}

.create-meta {
  margin-top: 6px;
  font-size: var(--app-text-sm);
  color: var(--td-text-color-placeholder);
  text-align: right;
}

.create-invalid {
  color: var(--td-error-color);
}
</style>
