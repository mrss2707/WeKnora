import { test, expect } from '@playwright/test'
import { api, kbPath, statePath, world, openMemory } from './world'

const kb = world.knowledge_bases['acme-eng'].id

test('browse uses server pagination and Vietnamese keyword search', async ({ page }) => {
  await page.goto(kbPath())
  await expect(page.getByTestId('memory-table')).toBeVisible()
  const response = await api(page, 'GET', `/memories?kb_id=${kb}&page=1&page_size=20`)
  expect(response.ok()).toBeTruthy()
  const list = await response.json()
  expect(list.data.total).toBeGreaterThan(20)
  const second = page.waitForResponse(r => r.url().includes('/memories?') && new URL(r.url()).searchParams.get('page') === '2')
  await page.getByTestId('memory-pagination').getByText('2', { exact: true }).click()
  expect((await second).ok()).toBeTruthy()
  const search = page.waitForResponse(r => r.url().includes('/memories?') && new URL(r.url()).searchParams.get('keyword') === 'chứng chỉ')
  await page.getByTestId('memory-search').locator('input').fill('chứng chỉ')
  await page.getByTestId('memory-search').locator('input').press('Enter')
  const searched = await search
  expect(searched.ok()).toBeTruthy()
  expect(new URL(searched.url()).searchParams.get('page')).toBe('1')
  await expect(page.getByTestId('memory-table')).toContainText('Chứng chỉ')
})

test('hybrid search uses the real search API and shows ranked top-k results', async ({ page }) => {
  await page.goto(kbPath())
  await page.getByTestId('memory-search-hybrid').click()
  const search = page.waitForResponse(r => r.url().includes('/memories/search?') && new URL(r.url()).searchParams.get('q') === 'how often do JWT signing keys rotate')
  const input = page.getByTestId('memory-search').locator('input')
  await input.fill('how often do JWT signing keys rotate')
  await input.press('Enter')
  const response = await search
  expect(response.ok()).toBeTruthy()
  const rows = (await response.json()).data
  expect(rows.some((row: any) => row.memory.id === world.memories['acme-eng'].jwt_rotation)).toBeTruthy()
  await expect(page.getByTestId('memory-search-topk')).toBeVisible()
  await expect(page.getByTestId('memory-pagination')).toHaveCount(0)
})

test('graph overview loads directly, expands a node, and opens its detail', async ({ page }) => {
  await page.goto(kbPath())
  await page.getByTestId('memory-subtab-graph').click()
  await expect(page.getByTestId('memory-graph-canvas')).toBeVisible()
  const nodes = page.locator('[data-testid^="memory-graph-node-"]')
  await expect(nodes.first()).toBeVisible()
  const response = await api(page, 'GET', `/memories/graph?kb_id=${kb}&limit=50`)
  expect(response.ok()).toBeTruthy()
  expect((await response.json()).data.nodes.length).toBeGreaterThan(0)
  expect(await page.getByTestId('memory-graph-canvas').innerHTML()).not.toContain('NaN')
  await page.locator('.legend-header').click()
  await nodes.first().locator('circle').last().click()
  await expect(page.getByTestId('memory-graph-open')).toBeVisible()
  await page.getByTestId('memory-graph-open').click()
  await expect(page.getByTestId('memory-content')).toBeVisible()
})

test('graph supports wheel zoom, background pan and keyboard focus after async load', async ({ page }) => {
  await page.goto(kbPath())
  await page.getByTestId('memory-subtab-graph').click()
  const canvas = page.getByTestId('memory-graph-canvas')
  await expect(canvas).toBeVisible()
  await page.getByTestId('memory-graph-fit').click()
  const root = page.locator('.graph-root')
  const fitted = await root.getAttribute('transform')
  const box = await canvas.boundingBox()
  expect(box).not.toBeNull()
  await page.mouse.move(box!.x + 15, box!.y + 15)
  await page.mouse.wheel(0, -160)
  await expect.poll(() => root.getAttribute('transform')).not.toBe(fitted)
  const zoomed = await root.getAttribute('transform')
  await page.mouse.down()
  await page.mouse.move(box!.x + 65, box!.y + 35, { steps: 3 })
  await page.mouse.up()
  await expect.poll(() => root.getAttribute('transform')).not.toBe(zoomed)
  const node = page.locator('[data-testid^="memory-graph-node-"]').first()
  const label = await node.getAttribute('aria-label')
  await node.focus()
  await expect(page.getByTestId('memory-graph-readout')).toHaveText(label!)
  await node.press('Enter')
  await expect(page.getByTestId('memory-graph-open')).toBeVisible()
})

test('create, edit with version, audit, then delete through the UI', async ({ page }) => {
  await page.goto(kbPath())
  await page.getByTestId('memory-create-button').click()
  const content = `The ${crypto.randomUUID()} observatory uses a sapphire prism to measure solar radiation every dawn.`
  const dialog = page.getByTestId('memory-create-dialog')
  await page.getByTestId('memory-create-content').locator('textarea').fill(content)
  const saved = page.waitForResponse(r => r.url().endsWith('/memories') && r.request().method() === 'POST')
  await dialog.getByRole('button', { name: 'Lưu', exact: true }).click()
  const result = await saved
  expect(result.ok()).toBeTruthy()
  const id = (await result.json()).data.memory.id
  await openMemory(page, id)
  await expect(page.getByTestId('memory-content')).toContainText(content)
  await page.getByTestId('memory-edit-content').click()
  const updated = content + ' Calibration requires a tungsten reference lamp.'
  await page.getByTestId('memory-content-editor').locator('textarea').fill(updated)
  const patched = page.waitForResponse(r => r.url().endsWith(`/memories/${id}`) && r.request().method() === 'PUT')
  await page.getByTestId('memory-save-content').click()
  expect((await patched).ok()).toBeTruthy()
  await expect(page.getByTestId('memory-content')).toContainText(updated)
  const detail = await api(page, 'GET', `/memories/${id}?kb_id=${kb}`)
  expect((await detail.json()).data.content).toBe(updated)
  await expect(page.getByTestId('memory-history')).toContainText(/Chỉnh sửa|Cập nhật/)
  await page.getByTestId('memory-delete').click()
  const deleted = page.waitForResponse(r => r.url().includes(`/memories/${id}`) && r.request().method() === 'DELETE')
  await page.getByRole('button', { name: 'Xác nhận', exact: true }).last().click()
  expect((await deleted).ok()).toBeTruthy()
  expect((await api(page, 'GET', `/memories/${id}?kb_id=${kb}`)).status()).toBe(404)
})

for (const role of ['dave', 'carol', 'bob']) {
  test.describe(role, () => {
    test.use({ storageState: statePath(role) })
    test('role floor is reflected by controls and enforced by REST', async ({ page }) => {
      await page.goto(kbPath())
      await expect(page.getByTestId('memory-table')).toBeVisible()
      if (role === 'dave') {
        await expect(page.getByTestId('memory-create-button')).toHaveCount(0)
        expect((await api(page, 'POST', '/memories', { kb_id: kb, content: 'A viewer must not be able to create this memory.' })).status()).toBe(403)
      } else {
        await expect(page.getByTestId('memory-create-button')).toBeVisible()
      }
      await page.getByTestId('memory-subtab-dream').click()
      await expect(page.getByTestId('memory-dream-status')).toBeVisible()
      if (role !== 'bob') {
        expect((await api(page, 'GET', `/mcp-endpoints/${world.mcp_endpoints['acme-rw'].id}/token`)).status()).toBe(403)
        await expect(page.getByTestId('memory-dream-preview')).toHaveCount(0)
      } else {
        await expect(page.getByTestId('memory-dream-preview')).toBeVisible()
      }
    })
  })
}

test('dream preview has no applied actions; health and history load', async ({ page }) => {
  await page.goto(kbPath())
  await page.getByTestId('memory-subtab-dream').click()
  const preview = page.waitForResponse(r => r.url().includes('/memories/dream') && r.request().method() === 'POST')
  await page.getByTestId('memory-dream-preview').click()
  const result = await preview
  expect(result.ok()).toBeTruthy()
  expect((await result.json()).data.actions_applied).toBe(0)
  await expect(page.getByTestId('memory-dream-result')).toBeVisible()
  await page.getByTestId('memory-subtab-health').click()
  await expect(page.getByTestId('memory-health-tab')).toBeVisible()
  await page.getByTestId('memory-subtab-history').click()
  await expect(page.getByTestId('memory-history-tab')).toBeVisible()
  await expect(page.getByTestId('memory-history-created').first()).toBeVisible()
})

test('an administrator undoes an actual near-duplicate merge from the drawer', async ({ page }) => {
  await page.goto(kbPath())
  const id = world.memories['acme-eng'].dup_redis_a
  const before = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
  const saved = await api(page, 'POST', '/memories', { kb_id: kb, content: 'Redis serves exclusively as the asynq job queue backend, not an application cache.' })
  expect(saved.ok()).toBeTruthy()
  const merged = (await saved.json()).data.memory
  expect(merged.id).toBe(id)
  expect(merged.content).not.toBe(before.content)
  const input = page.getByTestId('memory-search').locator('input')
  await input.fill('Redis serves exclusively')
  await input.press('Enter')
  await openMemory(page, id)
  await page.getByTestId('memory-undo-merge').click()
  const undone = page.waitForResponse(r => r.url().endsWith(`/memories/${id}/undo-merge`) && r.request().method() === 'POST')
  await page.getByRole('button', { name: 'Xác nhận', exact: true }).last().click()
  expect((await undone).ok()).toBeTruthy()
  const after = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
  expect(after.content).toBe(before.content)
  await expect(page.getByTestId('memory-history')).toContainText('Hoàn tác gộp')
})

test('English locale renders memory labels without raw keys', async ({ page }) => {
  await page.goto(kbPath())
  await page.evaluate(() => localStorage.setItem('locale', 'en-US'))
  await page.reload()
  await expect(page.getByTestId('memory-subtab-browse')).toHaveText('Browse')
  await expect(page.getByTestId('memory-table')).toBeVisible()
  expect(await page.locator('body').innerText()).not.toMatch(/memory\.(browse|dream|graph)\./)
})
