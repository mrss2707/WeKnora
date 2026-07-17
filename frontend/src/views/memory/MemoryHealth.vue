<template>
  <div class="memory-health">
    <!-- Loading skeleton -->
    <div v-if="memoryStore.healthLoading || memoryStore.statsLoading" class="loading-container">
      <t-loading :text="$t('memory.health.loading') || 'Loading health report...'" size="large" />
    </div>

    <!-- Empty state (no data) -->
    <div v-else-if="!healthReport" class="empty-state">
      <t-icon name="info-circle" size="48px" class="empty-icon" />
      <span class="empty-title">{{ $t('memory.health.emptyTitle') || 'Health Report Unavailable' }}</span>
      <span class="empty-desc">{{ $t('memory.health.emptyDesc') || 'No health data available for this knowledge base. Run a health check to generate a report.' }}</span>
    </div>

    <!-- Content -->
    <template v-else>
      <!-- Metric cards -->
      <div class="metric-cards">
        <div class="metric-card">
          <div class="metric-value">{{ totalMemories }}</div>
          <div class="metric-label">{{ $t('memory.health.totalMemories') || 'Total Memories' }}</div>
        </div>
        <div class="metric-card card-fixed">
          <div class="metric-value">{{ fixedCount }}</div>
          <div class="metric-label">{{ $t('memory.health.fixed') || 'Fixed' }}</div>
        </div>
        <div class="metric-card card-refuted">
          <div class="metric-value">{{ refutedCount }}</div>
          <div class="metric-label">{{ $t('memory.health.refuted') || 'Refuted' }}</div>
        </div>
        <div class="metric-card card-orphan">
          <div class="metric-value">{{ orphansCount }}</div>
          <div class="metric-label">{{ $t('memory.health.orphans') || 'Orphans' }}</div>
        </div>
        <div class="metric-card card-stale">
          <div class="metric-value">{{ staleCount }}</div>
          <div class="metric-label">{{ $t('memory.health.stale') || 'Stale' }}</div>
        </div>
      </div>

      <!-- Charts row -->
      <div class="charts-row">
        <!-- Distribution by memory type -->
        <div class="health-section chart-section">
          <h3 class="section-title">{{ $t('memory.health.distByType') || 'Distribution by Type' }}</h3>
          <div v-if="typeItems.length === 0" class="chart-empty">{{ $t('memory.health.noData') || 'No data' }}</div>
          <div v-else class="bar-chart">
            <div v-for="item in typeItems" :key="item.type" class="bar-row">
              <span class="bar-label">{{ typeLabel(item.type) }}</span>
              <div class="bar-track">
                <div
                  class="bar-fill"
                  :style="{ width: item.percentage + '%', background: typeColorMap[item.type] || '#4094f7' }"
                />
              </div>
              <span class="bar-count">{{ item.count }}</span>
            </div>
          </div>
        </div>

        <!-- Issues by severity -->
        <div class="health-section chart-section">
          <h3 class="section-title">{{ $t('memory.health.issuesBySeverity') || 'Issues by Severity' }}</h3>
          <div v-if="severityItems.length === 0" class="chart-empty">{{ $t('memory.health.noIssues') || 'No issues' }}</div>
          <div v-else class="bar-chart">
            <div v-for="item in severityItems" :key="item.severity" class="bar-row">
              <span class="bar-label">{{ severityLabel(item.severity) }}</span>
              <div class="bar-track">
                <div
                  class="bar-fill"
                  :style="{ width: item.percentage + '%', background: severityColorMap[item.severity] || '#8c8c8c' }"
                />
              </div>
              <span class="bar-count">{{ item.count }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Issue list grouped by severity -->
      <div class="health-section issue-section">
        <h3 class="section-title">
          {{ $t('memory.health.issues') || 'Issues' }}
          <span class="issue-total">({{ totalIssues }})</span>
        </h3>

        <div v-if="totalIssues === 0" class="chart-empty">
          {{ $t('memory.health.noIssues') || 'No issues found' }}
        </div>

        <!-- Critical group -->
        <div v-if="criticalIssues.length > 0" class="issue-group">
          <div class="issue-group-header severity-critical">
            <span class="issue-group-badge" />
            <span class="issue-group-label">Critical ({{ criticalIssues.length }})</span>
          </div>
          <div
            v-for="issue in criticalIssues"
            :key="issue.memory_id + '-' + issue.type"
            class="issue-item issue-critical"
          >
            <div class="issue-row">
              <span class="issue-type-tag type-critical">{{ severityLabel(issue.severity) }}</span>
              <span class="issue-description">{{ issue.description }}</span>
            </div>
            <div class="issue-row issue-meta">
              <span class="issue-rule-label">{{ issue.type }}</span>
              <div class="issue-actions">
                <t-button
                  v-if="issue.memory_id"
                  size="small"
                  variant="outline"
                  @click="handleViewMemory(issue)"
                >
                  {{ $t('memory.health.viewMemory') || 'View' }}
                </t-button>
                <t-button size="small" variant="outline" @click="handleSuggestion(issue)">
                  {{ $t('memory.health.applySuggestion') || 'Apply Fix' }}
                </t-button>
              </div>
            </div>
          </div>
        </div>

        <!-- Warning group -->
        <div v-if="warningIssues.length > 0" class="issue-group">
          <div class="issue-group-header severity-warning">
            <span class="issue-group-badge" />
            <span class="issue-group-label">Warning ({{ warningIssues.length }})</span>
          </div>
          <div
            v-for="issue in warningIssues"
            :key="issue.memory_id + '-' + issue.type"
            class="issue-item issue-warning"
          >
            <div class="issue-row">
              <span class="issue-type-tag type-warning">{{ severityLabel(issue.severity) }}</span>
              <span class="issue-description">{{ issue.description }}</span>
            </div>
            <div class="issue-row issue-meta">
              <span class="issue-rule-label">{{ issue.type }}</span>
              <div class="issue-actions">
                <t-button
                  v-if="issue.memory_id"
                  size="small"
                  variant="outline"
                  @click="handleViewMemory(issue)"
                >
                  {{ $t('memory.health.viewMemory') || 'View' }}
                </t-button>
                <t-button size="small" variant="outline" @click="handleSuggestion(issue)">
                  {{ $t('memory.health.applySuggestion') || 'Apply Fix' }}
                </t-button>
              </div>
            </div>
          </div>
        </div>

        <!-- Info group -->
        <div v-if="infoIssues.length > 0" class="issue-group">
          <div class="issue-group-header severity-info">
            <span class="issue-group-badge" />
            <span class="issue-group-label">Info ({{ infoIssues.length }})</span>
          </div>
          <div
            v-for="issue in infoIssues"
            :key="issue.memory_id + '-' + issue.type"
            class="issue-item issue-info"
          >
            <div class="issue-row">
              <span class="issue-type-tag type-info">{{ severityLabel(issue.severity) }}</span>
              <span class="issue-description">{{ issue.description }}</span>
            </div>
            <div class="issue-row issue-meta">
              <span class="issue-rule-label">{{ issue.type }}</span>
              <div class="issue-actions">
                <t-button
                  v-if="issue.memory_id"
                  size="small"
                  variant="outline"
                  @click="handleViewMemory(issue)"
                >
                  {{ $t('memory.health.viewMemory') || 'View' }}
                </t-button>
                <t-button size="small" variant="outline" @click="handleSuggestion(issue)">
                  {{ $t('memory.health.applySuggestion') || 'Apply Fix' }}
                </t-button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Last checked timestamp -->
      <div v-if="healthReport" class="health-footer">
        <span class="checked-at">
          {{ $t('memory.health.lastChecked') || 'Last checked' }}: {{ formatDate(healthReport.checked_at) }}
        </span>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useMemoryStore } from '@/stores/memory'
import type { HealthReport, MemoryHealthIssue, MemoryStats } from '@/api/memory/index'

const props = defineProps<{
  kbId: string
}>()

const emit = defineEmits<{
  'critical-issues-changed': [hasCritical: boolean]
  'view-memory': [memoryId: string]
}>()

const memoryStore = useMemoryStore()

// -----------------------------------------------------------------------
// Data fetching
// -----------------------------------------------------------------------
watch(
  () => props.kbId,
  (newKbId) => {
    if (newKbId) {
      memoryStore.loadHealth(newKbId)
      memoryStore.loadStats(newKbId)
    }
  },
  { immediate: true }
)

// -----------------------------------------------------------------------
// Derived data
// -----------------------------------------------------------------------
const healthReport = computed<HealthReport | null>(() => memoryStore.healthReport)
const stats = computed<MemoryStats | null>(() => memoryStore.stats)

// Metric card values
const totalMemories = computed(() => stats.value?.total_memories ?? 0)

const fixedCount = computed(() => {
  if (!stats.value?.by_verdict) return 0
  return Number(stats.value.by_verdict['fixed']) || 0
})

const refutedCount = computed(() => {
  if (!stats.value?.by_verdict) return 0
  return Number(stats.value.by_verdict['refuted']) || 0
})

const orphansCount = computed(() => {
  if (!healthReport.value?.issues) return 0
  return healthReport.value.issues.filter(
    (i) => i.type === 'orphan' || i.type === 'orphans'
  ).length
})

const staleCount = computed(() => {
  if (!healthReport.value?.issues) return 0
  return healthReport.value.issues.filter(
    (i) => i.type.includes('stale')
  ).length
})

// Total issues count
const totalIssues = computed(() => healthReport.value?.total_issues ?? 0)

// -----------------------------------------------------------------------
// Distribution by memory type
// -----------------------------------------------------------------------
const typeColorMap: Record<string, string> = {
  episodic: '#1890ff',
  semantic: '#52c41a',
  procedural: '#fa8c16',
  decision: '#722ed1',
  preference: '#eb2f96',
  fact: '#8c8c8c',
}

const typeItems = computed(() => {
  const byType = stats.value?.by_type
  if (!byType) return []
  const entries = Object.entries(byType)
  const total = entries.reduce((sum, [, count]) => sum + count, 0)
  if (total === 0) return []
  return entries.map(([type, count]) => ({
    type,
    count,
    percentage: Math.round((count / total) * 100),
  }))
})

function typeLabel(type: string): string {
  const labels: Record<string, string> = {
    episodic: 'Episodic',
    semantic: 'Semantic',
    procedural: 'Procedural',
    decision: 'Decision',
    preference: 'Preference',
    fact: 'Fact',
  }
  return labels[type] || type
}

// -----------------------------------------------------------------------
// Distribution by severity
// -----------------------------------------------------------------------
const severityColorMap: Record<string, string> = {
  critical: '#e34d59',
  high: '#ed7b2f',
  medium: '#ed7b2f',
  low: '#4094f7',
}

const severityItems = computed(() => {
  const bySeverity = healthReport.value?.by_severity
  if (!bySeverity) return []
  const entries = Object.entries(bySeverity)
  const total = entries.reduce((sum, [, count]) => sum + count, 0)
  if (total === 0) return []
  return entries.map(([severity, count]) => ({
    severity,
    count,
    percentage: Math.round((count / total) * 100),
  }))
})

function severityLabel(severity: string): string {
  return severity.charAt(0).toUpperCase() + severity.slice(1)
}

// -----------------------------------------------------------------------
// Issue grouping by display severity
// -----------------------------------------------------------------------
const criticalIssues = computed<MemoryHealthIssue[]>(() => {
  if (!healthReport.value?.issues) return []
  return healthReport.value.issues.filter(
    (i) => i.severity === 'critical'
  )
})

const warningIssues = computed<MemoryHealthIssue[]>(() => {
  if (!healthReport.value?.issues) return []
  return healthReport.value.issues.filter(
    (i) => i.severity === 'high' || i.severity === 'medium'
  )
})

const infoIssues = computed<MemoryHealthIssue[]>(() => {
  if (!healthReport.value?.issues) return []
  return healthReport.value.issues.filter(
    (i) => i.severity === 'low'
  )
})

const hasCriticalIssues = computed(() => criticalIssues.value.length > 0)

// Watch for critical issues and emit changes
watch(hasCriticalIssues, (val) => {
  emit('critical-issues-changed', val)
})

// -----------------------------------------------------------------------
// Actions
// -----------------------------------------------------------------------
function handleViewMemory(issue: MemoryHealthIssue) {
  if (issue.memory_id) {
    emit('view-memory', issue.memory_id)
  }
}

function handleSuggestion(issue: MemoryHealthIssue) {
  // Placeholder: in a full implementation, this would apply the issue.suggestion
  // e.g., update verdict, delete orphan, etc.
  console.log('Apply suggestion:', issue.suggestion)
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------
function formatDate(dateStr: string): string {
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<style scoped lang="less">
.memory-health {
  display: flex;
  flex-direction: column;
  gap: 16px;
  height: 100%;
  overflow-y: auto;
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
  max-width: 360px;
  line-height: 1.5;
}

// -----------------------------------------------------------------------
// Metric cards
// -----------------------------------------------------------------------
.metric-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 12px;
  flex-shrink: 0;
}

.metric-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 16px 12px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  text-align: center;
}

.metric-value {
  font-size: 28px;
  font-weight: 600;
  line-height: 1.2;
  color: var(--td-text-color-primary);
}

.metric-label {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  white-space: nowrap;
}

.card-fixed {
  .metric-value {
    color: var(--td-success-color, #00a870);
  }
}

.card-refuted {
  .metric-value {
    color: var(--td-error-color, #e34d59);
  }
}

.card-orphan {
  .metric-value {
    color: var(--td-warning-color, #ed7b2f);
  }
}

.card-stale {
  .metric-value {
    color: var(--td-text-color-disabled);
  }
}

// -----------------------------------------------------------------------
// Sections
// -----------------------------------------------------------------------
.health-section {
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  padding: 16px;
}

.section-title {
  margin: 0 0 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.section-title .issue-total {
  font-weight: 400;
  color: var(--td-text-color-placeholder);
}

// -----------------------------------------------------------------------
// Charts row
// -----------------------------------------------------------------------
.charts-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  flex-shrink: 0;
}

@media (max-width: 768px) {
  .charts-row {
    grid-template-columns: 1fr;
  }
}

.chart-section {
  min-width: 0;
}

// -----------------------------------------------------------------------
// Bar chart
// -----------------------------------------------------------------------
.bar-chart {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.bar-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bar-label {
  width: 80px;
  flex-shrink: 0;
  font-size: 12px;
  color: var(--td-text-color-secondary);
  text-align: right;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bar-track {
  flex: 1;
  height: 16px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 8px;
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  border-radius: 8px;
  transition: width 0.3s ease;
  min-width: 2px;
}

.bar-count {
  width: 36px;
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 500;
  color: var(--td-text-color-primary);
  text-align: right;
}

.chart-empty {
  padding: 24px 0;
  text-align: center;
  font-size: 13px;
  color: var(--td-text-color-placeholder);
}

// -----------------------------------------------------------------------
// Issue groups
// -----------------------------------------------------------------------
.issue-section {
  flex-shrink: 0;
}

.issue-group {
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;
  margin-bottom: 8px;
  overflow: hidden;
}

.issue-group-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  font-size: 13px;
  font-weight: 600;
}

.issue-group-badge {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.severity-critical {
  color: var(--td-error-color, #e34d59);
  background: rgba(227, 77, 89, 0.06);

  .issue-group-badge {
    background: var(--td-error-color, #e34d59);
  }
}

.severity-warning {
  color: var(--td-warning-color, #ed7b2f);
  background: rgba(237, 123, 47, 0.06);

  .issue-group-badge {
    background: var(--td-warning-color, #ed7b2f);
  }
}

.severity-info {
  color: var(--td-info-color, #4094f7);
  background: rgba(64, 148, 247, 0.06);

  .issue-group-badge {
    background: var(--td-info-color, #4094f7);
  }
}

.issue-item {
  padding: 10px 12px;
  border-top: 1px solid var(--td-component-stroke);
}

.issue-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.issue-row + .issue-row {
  margin-top: 6px;
}

.issue-description {
  flex: 1;
  font-size: 13px;
  color: var(--td-text-color-primary);
  line-height: 1.4;
}

.issue-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.issue-rule-label {
  font-size: 11px;
  color: var(--td-text-color-placeholder);
  font-family: monospace;
}

.issue-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.issue-type-tag {
  display: inline-block;
  padding: 1px 6px;
  font-size: 10px;
  font-weight: 500;
  border-radius: 3px;
  flex-shrink: 0;
  line-height: 1.6;
}

.type-critical {
  color: #fff;
  background: var(--td-error-color, #e34d59);
}

.type-warning {
  color: #fff;
  background: var(--td-warning-color, #ed7b2f);
}

.type-info {
  color: #fff;
  background: var(--td-info-color, #4094f7);
}

// -----------------------------------------------------------------------
// Severity color on item hover
// -----------------------------------------------------------------------
.issue-critical {
  border-left: 3px solid var(--td-error-color, #e34d59);
  margin-left: -1px;
}

.issue-warning {
  border-left: 3px solid var(--td-warning-color, #ed7b2f);
  margin-left: -1px;
}

.issue-info {
  border-left: 3px solid var(--td-info-color, #4094f7);
  margin-left: -1px;
}

.issue-critical + .issue-critical,
.issue-warning + .issue-warning,
.issue-info + .issue-info {
  border-top-color: transparent;
}

// -----------------------------------------------------------------------
// Footer
// -----------------------------------------------------------------------
.health-footer {
  display: flex;
  justify-content: flex-end;
  flex-shrink: 0;
  padding: 4px 0;
}

.checked-at {
  font-size: 11px;
  color: var(--td-text-color-placeholder);
}
</style>
