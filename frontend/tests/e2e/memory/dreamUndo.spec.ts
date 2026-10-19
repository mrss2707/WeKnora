import { test, expect } from '@playwright/test'
import { api, kbPath, statePath, world } from './world'

const kb = world.knowledge_bases['acme-eng'].id

test('health warns when the chosen Dreamer model is gone, and clears when the choice is reset', async ({ page }) => {
  await page.goto(kbPath())
  const models = (await (await api(page, 'GET', '/models')).json()).data as { type: string; parameters?: { base_url?: string } }[]
  const chat = models.find(m => m.type === 'KnowledgeQA')!
  const created = await api(page, 'POST', '/models', {
    name: `memv2-e2e-dreamer-${Date.now()}`, type: 'KnowledgeQA', source: 'remote',
    parameters: { base_url: chat.parameters?.base_url || 'http://localhost:11434/v1', api_key: 'e2e', provider: 'generic' },
  })
  expect(created.ok()).toBeTruthy()
  const modelId = (await created.json()).data.id as string
  try {
    expect((await api(page, 'PUT', '/memories/settings', { kb_id: kb, dreamer_model_id: modelId, text_model_id: '' })).ok()).toBeTruthy()
    expect((await api(page, 'DELETE', `/models/${modelId}`)).ok()).toBeTruthy()

    await page.getByTestId('memory-subtab-health').click()
    // The issue text comes from the server; the label is localized.
    await expect(page.getByTestId('memory-health-tab')).toContainText('The Dreamer model chosen for this knowledge base')
    const report = (await (await api(page, 'GET', `/memories/health?kb_id=${kb}`)).json()).data
    expect(report.issues.some((i: { type: string; severity: string }) => i.type === 'dreamer_model_unavailable' && i.severity === 'high')).toBeTruthy()

    const dream = await api(page, 'POST', '/memories/dream', { kb_id: kb })
    expect(dream.status()).toBe(503)
  } finally {
    expect((await api(page, 'PUT', '/memories/settings', { kb_id: kb, dreamer_model_id: '', text_model_id: '' })).ok()).toBeTruthy()
    await api(page, 'DELETE', `/models/${modelId}`)
  }
  await page.reload()
  await page.getByTestId('memory-subtab-health').click()
  await expect(page.getByTestId('memory-health-coverage')).toBeVisible()
  await expect(page.getByTestId('memory-health-tab')).not.toContainText('The Dreamer model chosen for this knowledge base')
})

test('the Dreamer tab offers undo for the last changing pass and reports a pass with nothing to undo', async ({ page }) => {
  // Real server; only the status read is extended with a pass id that changed nothing here.
  const runId = `memv2-e2e-${Date.now()}`
  await page.route('**/api/v1/memories/dream?kb_id=*', async route => {
    const response = await route.fetch()
    const body = await response.json()
    body.data.last_stats = { ...(body.data.last_stats || {}), last_change_run: runId }
    await route.fulfill({ response, json: body })
  })
  const undoRequest = page.waitForRequest(r => r.url().endsWith('/memories/dream/undo') && r.method() === 'POST')
  await page.goto(kbPath())
  await page.getByTestId('memory-subtab-dream').click()
  await page.getByTestId('memory-dream-undo').click()
  await page.locator('.t-dialog__confirm:visible').click()
  const request = await undoRequest
  expect(request.postDataJSON()).toEqual({ kb_id: kb, run_id: runId })
  const response = await request.response()
  expect(response!.status()).toBe(400)
  await expect(page.getByText('this Dreamer pass changed nothing in this knowledge base')).toBeVisible()
})

test.describe('viewer', () => {
  test.use({ storageState: statePath('dave') })
  test('a viewer cannot undo a Dreamer pass', async ({ page }) => {
    await page.goto(kbPath())
    await page.getByTestId('memory-subtab-dream').click()
    await expect(page.getByTestId('memory-dream-status')).toBeVisible()
    await expect(page.getByTestId('memory-dream-undo')).toHaveCount(0)
    const forbidden = await api(page, 'POST', '/memories/dream/undo', { kb_id: kb, run_id: 'x' })
    expect(forbidden.status()).toBe(403)
  })
})
