import type { MemoryAuditImage } from '@/api/memory/index'

const AUDITED_FIELDS: (keyof MemoryAuditImage)[] = ['content', 'verdict', 'importance', 'tier', 'memory_type', 'tags']

/** Names of the audited fields that differ between two memory images. */
export function auditChangedFields(before?: MemoryAuditImage | null, after?: MemoryAuditImage | null): string[] {
  if (!before || !after) return []
  return AUDITED_FIELDS.filter((f) => {
    if (f === 'tags') return (before.tags ?? []).join('\u0000') !== (after.tags ?? []).join('\u0000')
    return before[f] !== after[f]
  }) as string[]
}
