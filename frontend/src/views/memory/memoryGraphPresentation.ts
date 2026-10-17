// Stable protocol codes provide a second type encoding independent of color.
export const MEMORY_GRAPH_TYPE_CODES: Record<string, string> = {
  episodic: 'EP', semantic: 'SE', procedural: 'PR', decision: 'DE', preference: 'PF', fact: 'FA',
}

export function memoryGraphNodeRadius(importance: number): number {
  const value = Number.isFinite(importance) ? Math.max(0, importance) : 0
  return Math.max(8, Math.min(20, 8 + Math.log1p(value) * 2))
}

export function memoryGraphLabel(label: string, type: string, dense: boolean, active: boolean): string {
  if (dense && !active) return MEMORY_GRAPH_TYPE_CODES[type] || '?'
  const runes = [...label]
  return runes.length > 32 ? runes.slice(0, 31).join('') + '…' : label
}
