import { test, expect } from '@playwright/test'
import { api, kbPath, world, openMemory } from './world'

const kb = world.knowledge_bases['acme-eng'].id

test('create validates short content and exact duplicates do not add a row', async ({ page }) => {
  await page.goto(kbPath())
  const id = world.memories['acme-eng'].jwt_rotation
  const original = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
  const before = (await (await api(page, 'GET', `/memories/stats?kb_id=${kb}`)).json()).data.total_memories
  await page.getByTestId('memory-create-button').click()
  const dialog = page.getByTestId('memory-create-dialog')
  const input = page.getByTestId('memory-create-content').locator('textarea')
  await input.fill('abc')
  await expect(dialog.getByRole('button', { name: 'Lưu', exact: true })).toBeDisabled()
  await input.fill(original.content)
  const saved = page.waitForResponse(r => r.url().endsWith('/memories') && r.request().method() === 'POST')
  await dialog.getByRole('button', { name: 'Lưu', exact: true }).click()
  const response = await saved
  expect(response.status()).toBe(200)
  const result = (await response.json()).data
  expect(result.created).toBe(false)
  expect(result.memory.id).toBe(id)
  const after = (await (await api(page, 'GET', `/memories/stats?kb_id=${kb}`)).json()).data.total_memories
  expect(after).toBe(before)
})

test('drawer updates tags, importance and verdict with optimistic versions', async ({ page }) => {
  await page.goto(kbPath())
  const id = world.memories['acme-eng'].vi_minio
  const before = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
  const input = page.getByTestId('memory-search').locator('input')
  await input.fill('bucket MinIO')
  await input.press('Enter')
  await openMemory(page, id)
  await expect(page.getByTestId('memory-content')).toBeVisible()
  try {
    await page.getByTestId('memory-add-tag').click()
    const tag = page.getByTestId('memory-tag-input').locator('input')
    await tag.fill('ui-metadata-check')
    const tagged = page.waitForResponse(r => r.url().endsWith(`/memories/${id}`) && r.request().method() === 'PUT')
    await tag.press('Enter')
    const response = await tagged
    expect(response.ok()).toBeTruthy()
    expect(response.request().postDataJSON().expected_updated_at).toBeTruthy()
    expect((await response.json()).data.tags).toContain('ui-metadata-check')
    const changed = page.waitForResponse(r => r.url().endsWith(`/memories/${id}`) && r.request().method() === 'PUT')
    await page.getByTestId('memory-importance-2').click()
    expect((await changed).ok()).toBeTruthy()
    await page.getByTestId('memory-verdict').click()
    const verdict = page.waitForResponse(r => r.url().endsWith(`/memories/${id}`) && r.request().method() === 'PUT')
    await page.getByText('Cạm bẫy', { exact: true }).last().click()
    expect((await verdict).ok()).toBeTruthy()
    const after = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
    expect(after.importance).toBe(2)
    expect(after.verdict).toBe('gotcha')
    expect(after.content).toBe(before.content)
    expect(after.tags).toContain('ui-metadata-check')
  } finally {
    const current = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
    expect((await api(page, 'PUT', `/memories/${id}`, {
      tags: before.tags || [], importance: before.importance, verdict: before.verdict, expected_updated_at: current.updated_at,
    })).ok()).toBeTruthy()
  }
})

test('bulk importance updates both selected rows and stats agree with list', async ({ page }) => {
  await page.goto(kbPath())
  const ids = ['vi_cert', 'cert_renewal'].map(key => world.memories['acme-eng'][key])
  const originals = []
  for (const id of ids) {
    const before = (await (await api(page, 'GET', `/memories/${id}`)).json()).data
    originals.push(before)
    expect((await api(page, 'PUT', `/memories/${id}`, { importance: 2, expected_updated_at: before.updated_at })).ok()).toBeTruthy()
  }
  try {
    await page.reload()
    const input = page.getByTestId('memory-search').locator('input')
    await input.fill('cert-manager')
    await input.press('Enter')
    for (const id of ids) await page.getByTestId(`memory-select-${id}`).click()
    await expect(page.getByTestId('memory-bulk-bar')).toBeVisible()
    const writes = ids.map(id => page.waitForResponse(r => r.url().endsWith(`/memories/${id}`) && r.request().method() === 'PUT'))
    await page.getByTestId('memory-bulk-importance-up').click()
    for (const response of await Promise.all(writes)) expect(response.ok()).toBeTruthy()
    for (const id of ids) expect((await (await api(page, 'GET', `/memories/${id}`)).json()).data.importance).toBe(3)
    const stats = (await (await api(page, 'GET', `/memories/stats?kb_id=${kb}`)).json()).data
    const list = (await (await api(page, 'GET', `/memories?kb_id=${kb}&page_size=100&verdicts=none,fixed,refuted,decision,gotcha,wip`)).json()).data
    expect(stats.total_memories).toBe(list.total)
    expect(Object.values(stats.by_type).reduce((a: number, b: any) => a + Number(b), 0)).toBe(list.total)
  } finally {
    for (const before of originals) {
      const current = (await (await api(page, 'GET', `/memories/${before.id}`)).json()).data
      expect((await api(page, 'PUT', `/memories/${before.id}`, { importance: before.importance, expected_updated_at: current.updated_at })).ok()).toBeTruthy()
    }
  }
})
