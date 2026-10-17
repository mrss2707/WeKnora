import { useI18n } from 'vue-i18n'
import type { MemoryAuditEntry } from '@/api/memory/index'
import { auditChangedFields } from './memoryAudit'

const KNOWN_TYPES = ['episodic', 'semantic', 'procedural', 'decision', 'preference', 'fact']
const KNOWN_RELATIONS = ['related_to', 'justifies', 'contradicts']
const KNOWN_ACTIONS = ['created', 'patched', 'merged', 'merged_into', 'deleted', 'unmerged', 'dreamed', 'reclaimed']
const KNOWN_WORKERS = ['dreamer', 'pruner', 'consolidator', 'system', 'reconciler']

/** Localized labels shared by the memory views. */
export function useMemoryLabels() {
  const { t } = useI18n()

  const typeLabel = (type: string) => (KNOWN_TYPES.includes(type) ? t(`memory.types.${type}`) : type)

  const relationLabel = (type: string) =>
    KNOWN_RELATIONS.includes(type) ? t(`memory.relations.${type}`) : type

  const auditActionLabel = (action: string) =>
    KNOWN_ACTIONS.includes(action) ? t(`memory.audit.actions.${action}`) : action

  const actorLabel = (entry: MemoryAuditEntry) => {
    if (KNOWN_WORKERS.includes(entry.actor)) return t(`memory.audit.actors.${entry.actor}`)
    if (entry.actor === 'user') {
      return entry.user_id ? t('memory.audit.actors.userWithId', { id: entry.user_id }) : t('memory.audit.actors.user')
    }
    return t('memory.audit.actors.agent', { id: entry.actor })
  }

  const fieldLabel = (field: string) => t(`memory.audit.fields.${field}`)

  /** One line describing what an audit entry changed. */
  const describeAuditChange = (entry: MemoryAuditEntry) => {
    const after = entry.after
    const before = entry.before
    switch (entry.action) {
      case 'patched': {
        const fields = auditChangedFields(before, after)
        if (fields.includes('verdict') && after) {
          return t('memory.audit.verdictChanged', {
            from: t(`memory.verdicts.${before?.verdict || 'none'}`),
            to: t(`memory.verdicts.${after.verdict || 'none'}`),
          })
        }
        if (fields.includes('importance') && after && before && fields.length === 1) {
          return t('memory.audit.importanceChanged', { from: before.importance, to: after.importance })
        }
        return fields.length
          ? t('memory.audit.fieldsChanged', { fields: fields.map(fieldLabel).join(', ') })
          : t('memory.audit.actions.patched')
      }
      case 'merged':
        return t('memory.audit.mergedDesc')
      case 'merged_into':
        return t('memory.audit.mergedIntoDesc')
      default:
        return preview((after ?? before)?.content ?? '')
    }
  }

  const formatDate = (dateStr: string) => {
    const d = new Date(dateStr)
    if (isNaN(d.getTime())) return ''
    return d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  const formatRelativeTime = (dateStr: string) => {
    const then = new Date(dateStr).getTime()
    if (isNaN(then)) return ''
    const diffMin = Math.floor((Date.now() - then) / 60000)
    if (diffMin < 1) return t('memory.time.justNow')
    if (diffMin < 60) return t('memory.time.minutesAgo', { n: diffMin })
    const diffHour = Math.floor(diffMin / 60)
    if (diffHour < 24) return t('memory.time.hoursAgo', { n: diffHour })
    const diffDay = Math.floor(diffHour / 24)
    if (diffDay < 7) return t('memory.time.daysAgo', { n: diffDay })
    return formatDate(dateStr)
  }

  return { typeLabel, relationLabel, auditActionLabel, actorLabel, describeAuditChange, formatDate, formatRelativeTime }
}

function preview(content: string, max = 120): string {
  const chars = [...content]
  return chars.length > max ? chars.slice(0, max - 1).join('') + '…' : content
}
