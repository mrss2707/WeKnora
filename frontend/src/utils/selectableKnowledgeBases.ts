// Single source of truth for "which knowledge bases may this picker offer":
// the tenant's own KBs plus the ones shared into it through organizations.
//
// Pickers used to hand-roll this merge (own list + `sharedKnowledgeBases`,
// dedupe, permission filter) and some forgot the shared half entirely, so a
// shared KB could not be selected. Keep the module free of store/component
// imports so it stays trivially unit-testable; callers pass the two lists in.

export interface SelectableKbOwnedRow {
  id: string | number
  name?: string
  type?: string
  [key: string]: unknown
}

export interface SelectableKbSharedRow {
  knowledge_base?: {
    id: string | number
    name?: string
    type?: string
    [key: string]: unknown
  } | null
  permission?: string
  org_name?: string
  [key: string]: unknown
}

export interface SelectableKnowledgeBase {
  id: string
  name: string
  type?: string
  isShared: boolean
  /** Best grant the caller holds; undefined for owned KBs (full access). */
  permission?: string
  orgName?: string
}

export interface SelectableKbOptions {
  /** Lowest share permission accepted; owned KBs always pass. Default viewer. */
  minPermission?: 'viewer' | 'editor' | 'admin'
  /** Keep only these KB types (a missing type counts as 'document'). */
  types?: string[]
}

const PERMISSION_RANK: Record<string, number> = { viewer: 1, editor: 2, admin: 3 }

function rank(perm: string | undefined): number {
  return (perm && PERMISSION_RANK[perm]) || 0
}

function typeAllowed(type: string | undefined, types: string[] | undefined): boolean {
  return !types || types.includes(type || 'document')
}

/**
 * Owned rows win over shared duplicates; repeated shares of one KB collapse
 * to the most-privileged grant. Order: owned first, then shared.
 */
export function mergeSelectableKnowledgeBases(
  owned: SelectableKbOwnedRow[],
  shared: SelectableKbSharedRow[],
  options: SelectableKbOptions = {},
): SelectableKnowledgeBase[] {
  const minRank = rank(options.minPermission ?? 'viewer')
  const result: SelectableKnowledgeBase[] = []
  const ownedIds = new Set<string>()

  for (const kb of owned) {
    if (!kb || kb.id == null || !typeAllowed(kb.type, options.types)) continue
    const id = String(kb.id)
    if (ownedIds.has(id)) continue
    ownedIds.add(id)
    result.push({ id, name: kb.name || id, type: kb.type, isShared: false })
  }

  const sharedById = new Map<string, SelectableKnowledgeBase>()
  for (const row of shared) {
    const kb = row?.knowledge_base
    if (!kb || kb.id == null || !typeAllowed(kb.type, options.types)) continue
    if (rank(row.permission) < minRank) continue
    const id = String(kb.id)
    if (ownedIds.has(id)) continue
    const existing = sharedById.get(id)
    if (existing && rank(existing.permission) >= rank(row.permission)) continue
    sharedById.set(id, {
      id,
      name: kb.name || id,
      type: kb.type,
      isShared: true,
      permission: row.permission,
      orgName: row.org_name,
    })
  }

  return [...result, ...sharedById.values()]
}

/** Dropdown option; shared KBs carry their organization so duplicates stay distinguishable. */
export function toKnowledgeBaseSelectOption(
  kb: SelectableKnowledgeBase,
): { label: string; value: string } {
  return {
    label: kb.isShared && kb.orgName ? `${kb.name} (${kb.orgName})` : kb.name,
    value: kb.id,
  }
}
