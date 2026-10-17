import { chromium } from '@playwright/test'
import { mkdir, chmod } from 'node:fs/promises'
import { dirname } from 'node:path'
import { world, statePath, frontendURL } from './world'

// Use the real login flow: session storage is never mocked. State contains
// bearer tokens, so keep it ignored and readable only by its owner.
export default async function globalSetup() {
  await mkdir(dirname(statePath('alice')), { recursive: true, mode: 0o700 })
  const browser = await chromium.launch()
  try {
    for (const name of Object.keys(world.users)) {
      const context = await browser.newContext()
      const page = await context.newPage()
      await page.goto(`${frontendURL}/login`)
      await page.locator('input[type="email"], input[autocomplete="email"]').first().fill(world.users[name].email)
      await page.locator('input[type="password"]').fill(world.password)
      await page.locator('button[type="submit"]').click()
      await page.waitForURL('**/platform/**')
      await page.evaluate(({ tenant, name }) => {
        localStorage.setItem('locale', 'vi-VN')
        localStorage.setItem('weknora:new-user-guide-done:v1', '1')
        localStorage.setItem('weknora:contextual-guide-kb-detail:v1', '1')
        localStorage.setItem('weknora:contextual-guide-kb-list:v2', '1')
        if (name === 'bob' || name === 'carol' || name === 'dave') {
          localStorage.setItem('weknora_selected_tenant_id', String(tenant))
        }
      }, { tenant: world.tenants.acme, name })
      await context.storageState({ path: statePath(name) })
      await chmod(statePath(name), 0o600)
      await context.close()
    }
  } finally {
    await browser.close()
  }
}
