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
      <div class="card-type-icon" :title="$t(`memory.types.${memory.memory_type}`)">
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

      <!-- Verdict badge -->
      <t-tag
        :class="['verdict-badge', `verdict-${memory.verdict}`]"
        size="small"
        variant="light"
      >
        {{ $t(`memory.verdicts.${memory.verdict || 'none'}`) }}
      </t-tag>

      <!-- Tier label -->
      <t-tag
        v-if="memory.tier !== undefined && memory.tier !== null"
        size="small"
        variant="outline"
        class="tier-badge"
      >
        {{ $t(`memory.tiers.${memory.tier}`) }}
      </t-tag>

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

.tier-badge {
  font-size: 11px;
}

.stale-badge {
  font-size: 11px;
  display: inline-flex;
  align-items: center;
  gap: 2px;
}
</style>
