<template>
  <div
    class="memory-card"
    :class="[
      `verdict-${memory.verdict}`,
      { 'is-stale': isStale, 'is-selected': selected }
    ]"
    @click="$emit('click', memory)"
  >
    <!-- Selection checkbox -->
    <div class="card-checkbox" @click.stop="$emit('select', memory.id)">
      <t-checkbox :checked="selected" />
    </div>

    <!-- Card header: type icon + date -->
    <div class="card-header">
      <div class="card-type-icon" :style="{ color: typeColor }" :title="$t(`memory.types.${memory.memory_type}`)">
        <t-icon :name="typeIcon" size="18px" />
      </div>
      <span class="card-date">{{ formattedDate }}</span>
    </div>

    <!-- Content preview -->
    <div class="card-content" :title="memory.content">
      {{ truncatedContent }}
    </div>

    <!-- Tags -->
    <div v-if="memory.tags && memory.tags.length > 0" class="card-tags">
      <t-tag
        v-for="tag in memory.tags"
        :key="tag"
        size="small"
        variant="light"
        class="memory-tag"
      >
        {{ tag }}
      </t-tag>
    </div>

    <!-- Bottom row: importance + verdict + tier + stale -->
    <div class="card-footer">
      <!-- Importance stars -->
      <div class="card-importance" :title="$t('memory.card.importance', { count: memory.importance })">
        <t-icon
          v-for="i in 10"
          :key="i"
          :name="i <= memory.importance ? 'star-filled' : 'star'"
          :class="['star-icon', { filled: i <= memory.importance }]"
          size="12px"
        />
      </div>

      <!-- Verdict badge: Soft pill -->
      <span :class="['verdict-soft-pill', `verdict-${memory.verdict || 'none'}`]">
        <t-icon :name="verdictIcon" size="12px" class="verdict-icon" />
        <span>{{ $t(`memory.verdicts.${memory.verdict || 'none'}`) }}</span>
      </span>

      <!-- Tier label: Outline pill -->
      <t-tooltip
        v-if="memory.tier !== undefined && memory.tier !== null"
        :content="tierTooltip"
        placement="top"
      >
        <span :class="['tier-outline-pill', `tier-${memory.tier}`]">
          <span>{{ $t(`memory.tiers.${memory.tier}`) }}</span>
        </span>
      </t-tooltip>

      <!-- Stale indicator -->
      <t-tooltip v-if="isStale" :content="$t('memory.card.staleTitle', { days: staleDays })">
        <t-tag size="small" theme="warning" variant="light" class="stale-badge">
          <t-icon name="time" size="12px" />
          {{ $t('memory.card.stale') }}
        </t-tag>
      </t-tooltip>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { AgentMemory } from '@/api/memory/index'

const props = defineProps<{
  memory: AgentMemory
  selected: boolean
}>()

defineEmits<{
  select: [memoryId: string]
  click: [memory: AgentMemory]
}>()

/** Truncate content to ~120 chars for card preview. */
const truncatedContent = computed(() => {
  const text = props.memory.content
  if (text.length <= 120) return text
  return text.slice(0, 117) + '...'
})

/** Format the created_at date in a short form. */
const formattedDate = computed(() => {
  const d = new Date(props.memory.created_at)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
})

/** Whether the memory is stale (>90 days since last update). */
const staleDays = computed(() => {
  const now = Date.now()
  const updated = new Date(props.memory.updated_at).getTime()
  if (isNaN(updated)) return 0
  return Math.floor((now - updated) / (1000 * 60 * 60 * 24))
})

const isStale = computed(() => staleDays.value > 90)

/** Map memory type to a TDesign icon name. */
const typeIcon = computed(() => {
  const iconMap: Record<string, string> = {
    episodic: 'time',
    semantic: 'bookmark',
    procedural: 'control-platform',
    decision: 'check-circle',
    preference: 'thumb-up',
    fact: 'info-circle',
  }
  return iconMap[props.memory.memory_type] || 'memory'
})

const TYPE_COLORS: Record<string, string> = {
  episodic: '#1890ff',
  semantic: '#52c41a',
  procedural: '#fa8c16',
  decision: '#722ed1',
  preference: '#eb2f96',
  fact: '#8c8c8c',
}

const typeColor = computed(() => {
  return TYPE_COLORS[props.memory.memory_type] || '#8c8c8c'
})

const verdictIcon = computed(() => {
  const map: Record<string, string> = {
    decision: 'lock-on',
    fixed: 'check-circle',
    gotcha: 'error-circle',
    wip: 'time',
    refuted: 'close-circle',
    none: 'minus-circle',
  }
  return map[props.memory.verdict || 'none'] || 'minus-circle'
})

const tierTooltip = computed(() => {
  const map: Record<number, string> = {
    0: 'Bậc 0 (Mỗi lượt): Luôn được đưa vào context của Agent trong mọi lượt trò chuyện',
    1: 'Bậc 1 (Tình huống): Kích hoạt khi câu hỏi hoặc bối cảnh cuộc trò chuyện có liên quan',
    2: 'Bậc 2 (Chủ đề thường gặp): Mối quan tâm dài hạn, chỉ kích hoạt khi chạm ngưỡng tần suất',
    3: 'Bậc 3 (Theo dõi trước): Thu thập & đếm tần suất lặp lại trước, chưa kích hoạt trực tiếp',
  }
  return map[props.memory.tier ?? -1] || `Bậc ${props.memory.tier}`
})
</script>

<style scoped lang="less">
.memory-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  cursor: pointer;
  transition: border-color 0.2s, box-shadow 0.2s;

  &:hover {
    border-color: var(--td-brand-color);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  }

  &.is-selected {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }

  // Verdict-specific dimming
  &.verdict-refuted {
    opacity: 0.7;
  }
}

.card-checkbox {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 2;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.card-type-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-brand-color);
  flex-shrink: 0;
}

.card-date {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  margin-left: auto;
}

.card-content {
  font-size: 13px;
  line-height: 1.5;
  color: var(--td-text-color-secondary);
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
}

.card-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;

  .memory-tag {
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.card-footer {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: auto;
}

.card-importance {
  display: flex;
  align-items: center;
  gap: 1px;

  .star-icon {
    color: var(--td-text-color-disabled);

    &.filled {
      color: var(--td-warning-color);
    }
  }
}

.verdict-soft-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  font-weight: 500;
  padding: 2px 7px;
  border-radius: 12px;
  line-height: 16px;
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
  gap: 3px;
  font-size: 11px;
  font-weight: 500;
  padding: 1px 6px;
  border-radius: 4px;
  line-height: 16px;
  background: transparent;
  white-space: nowrap;
  cursor: help;
  transition: all 0.2s ease;

  &.tier-0 {
    border: 1px solid #d48806;
    color: #d48806;
    font-weight: 600;
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

.stale-badge {
  font-size: 11px;
  display: inline-flex;
  align-items: center;
  gap: 2px;
}
</style>
