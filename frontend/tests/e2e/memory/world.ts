import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import type { Page } from '@playwright/test'

interface World {
  base_url: string
  password: string
  tenants: Record<string, number>
  users: Record<string, { email: string }>
  knowledge_bases: Record<string, { id: string }>
  memories: Record<string, Record<string, string>>
  canary: string
  mcp_endpoints: Record<string, { id: string; path: string; token: string }>
}
export const world: World = JSON.parse(readFileSync(
  process.env.MEMV2_E2E_SEED || fileURLToPath(new URL('../../../../e2e/memory_v2/seed-output.json', import.meta.url)), 'utf8'))
export const frontendURL = process.env.MEMV2_E2E_FRONTEND_URL || 'http://localhost:15173'
export const statePath = (name: string) => process.env.MEMV2_E2E_STATE_DIR
  ? resolve(process.env.MEMV2_E2E_STATE_DIR, `${name}.json`)
  : fileURLToPath(new URL(`./storageState/${name}.json`, import.meta.url))
export const kbPath = (name = 'acme-eng') => `/platform/knowledge-bases/${world.knowledge_bases[name].id}?tab=memory`

export async function api(page: Page, method: string, path: string, data?: unknown) {
  const auth = await page.evaluate(() => ({
    token: localStorage.getItem('weknora_token') || '',
    tenant: localStorage.getItem('weknora_selected_tenant_id'),
  }))
  return page.request.fetch(`${world.base_url}/api/v1${path}`, {
    method, data,
    headers: {
      Authorization: `Bearer ${auth.token}`,
      ...(auth.tenant ? { 'X-Tenant-ID': auth.tenant } : {}),
    },
  })
}

export async function openMemory(page: Page, id: string) {
  await page.locator(`[data-testid="memory-row-${id}"], [data-testid="memory-card-${id}"]`).first().click()
}
