import enUS from './locales/en-US.ts'
import zhCN from './locales/zh-CN.ts'
import koKR from './locales/ko-KR.ts'
import ruRU from './locales/ru-RU.ts'
import viVN from './locales/vi-VN.ts'

export const LOCALE_BUNDLES = {
  'zh-CN': zhCN,
  'en-US': enUS,
  'ru-RU': ruRU,
  'ko-KR': koKR,
  'vi-VN': viVN,
} as const

export type LocaleName = keyof typeof LOCALE_BUNDLES

/** Collect every leaf translation key as a dot-joined path (e.g. `memory.graph.depth`). */
export function collectLocaleKeys(node: unknown, prefix = '', keys = new Set<string>()): Set<string> {
  if (node === null || typeof node !== 'object') return keys
  for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
    const full = prefix ? `${prefix}.${key}` : key
    if (value !== null && typeof value === 'object') {
      collectLocaleKeys(value, full, keys)
    } else if (typeof value === 'string') {
      keys.add(full)
    }
  }
  return keys
}