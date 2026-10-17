import { test, expect } from '@playwright/test'
import { api, kbPath, statePath, world, openMemory } from './world'

const eng = world.knowledge_bases['acme-eng'].id

test('a stale drawer receives 409, reloads the winning version, and never overwrites it', async ({ page, browser }) => {
  const id = world.memories['acme-eng'].jwt_rotation
  await page.goto(kbPath())
  const search = page.getByTestId('memory-search').locator('input')
  await search.fill('JWT signing keys')
  await search.press('Enter')
  await openMemory(page, id)
  await expect(page.getByTestId('memory-content')).toContainText('JWT signing')
  const before = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
  const other = await browser.newContext({ storageState: statePath('bob') })
  try {
    const otherPage = await other.newPage()
    await otherPage.goto(kbPath())
    const importance = before.importance === 2 ? 3 : 2
    const winner = await api(otherPage, 'PUT', `/memories/${id}`, { importance, expected_updated_at: before.updated_at })
    expect(winner.ok()).toBeTruthy()
    await page.getByTestId('memory-edit-content').click()
    await page.getByTestId('memory-content-editor').locator('textarea').fill(before.content + ' This stale edit must not survive.')
    const conflict = page.waitForResponse(r => r.url().endsWith(`/memories/${id}`) && r.request().method() === 'PUT')
    await page.getByTestId('memory-save-content').click()
    expect((await conflict).status()).toBe(409)
    await page.getByRole('button', { name: 'Tải lại', exact: true }).click()
    await expect(page.getByTestId('memory-content')).toHaveText(before.content)
    const current = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
    expect(current.importance).toBe(importance)
    expect(current.content).toBe(before.content)
  } finally {
    const current = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
    expect((await api(page, 'PUT', `/memories/${id}`, { importance: before.importance, expected_updated_at: current.updated_at })).ok()).toBeTruthy()
    await other.close()
  }
})

test('a delayed old keyword response cannot replace the newer filter results', async ({ page }) => {
  let release!: () => void
  const delayed = new Promise<void>(resolve => { release = resolve })
  let started!: () => void
  const intercepted = new Promise<void>(resolve => { started = resolve })
  await page.route('**/api/v1/memories?**', async route => {
    if (new URL(route.request().url()).searchParams.get('keyword') !== 'JWT signing') return route.continue()
    const response = await route.fetch()
    started()
    await delayed
    await route.fulfill({ response })
  })
  try {
    await page.goto(kbPath())
    const input = page.getByTestId('memory-search').locator('input')
    await input.fill('JWT signing')
    await input.press('Enter')
    await intercepted
    const latest = page.waitForResponse(r => r.url().includes('/memories?') && new URL(r.url()).searchParams.get('keyword') === 'cert-manager')
    await input.fill('cert-manager')
    await input.press('Enter')
    expect((await latest).ok()).toBeTruthy()
    await expect(page.getByTestId('memory-table')).toContainText('cert-manager')
    const old = page.waitForResponse(r => r.url().includes('/memories?') && new URL(r.url()).searchParams.get('keyword') === 'JWT signing')
    release()
    await old
    await expect(page.getByTestId('memory-table')).not.toContainText('JWT signing')
    await expect(page.getByTestId('memory-table')).toContainText('cert-manager')
  } finally {
    release()
    await page.unrouteAll({ behavior: 'wait' })
  }
})

test.describe('Beta shared access', () => {
  test.use({ storageState: statePath('erin') })
  test('editor share can write, viewer share cannot, and Gamma is unreachable', async ({ page }) => {
    await page.goto(kbPath())
    await expect(page.getByTestId('memory-create-button')).toBeVisible()
    await page.getByTestId('memory-subtab-mcpconfig').click()
    await expect(page.getByTestId('memory-mcp-config')).toContainText(world.mcp_endpoints['beta-shared'].path)
    const id = world.memories['acme-eng'].vi_minio
    const response = await api(page, 'GET', `/memories/${id}`)
    expect(response.ok()).toBeTruthy()
    const memory = (await response.json()).data
    const tags = [...(memory.tags || []), 'beta-e2e-shared']
    try {
      expect((await api(page, 'PUT', `/memories/${id}`, { tags, expected_updated_at: memory.updated_at })).ok()).toBeTruthy()
    } finally {
      const current = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
      expect((await api(page, 'PUT', `/memories/${id}`, { tags: memory.tags || [], expected_updated_at: current.updated_at })).ok()).toBeTruthy()
    }
    await page.goto(kbPath('acme-hr'))
    await expect(page.getByTestId('memory-table')).toBeVisible()
    await expect(page.getByTestId('memory-create-button')).toHaveCount(0)
    expect((await api(page, 'POST', '/memories', {
      kb_id: world.knowledge_bases['acme-hr'].id, content: 'This write is forbidden by the viewer share.',
    })).status()).toBe(403)
    const gamma = world.knowledge_bases['gamma-private'].id
    expect([403, 404]).toContain((await api(page, 'GET', `/memories?kb_id=${gamma}`)).status())
    expect(await page.locator('body').innerText()).not.toContain(world.canary)
  })
})
