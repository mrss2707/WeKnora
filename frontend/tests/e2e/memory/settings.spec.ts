import { test, expect } from '@playwright/test'
import { api, kbPath, statePath, world } from './world'

const kb = world.knowledge_bases['acme-eng'].id

async function chatAndEmbeddingModels(page: import('@playwright/test').Page) {
  const response = await api(page, 'GET', '/models')
  expect(response.ok()).toBeTruthy()
  const models = (await response.json()).data as { id: string; name: string; display_name?: string; type: string }[]
  const chat = models.find(m => m.type === 'KnowledgeQA')
  const embedding = models.find(m => m.type === 'Embedding')
  expect(chat).toBeTruthy()
  expect(embedding).toBeTruthy()
  return { chat: chat!, embedding: embedding! }
}

test('an admin chooses the Dreamer model per knowledge base and it persists', async ({ page }) => {
  await page.goto(kbPath())
  const { chat, embedding } = await chatAndEmbeddingModels(page)
  try {
    await page.getByTestId('memory-subtab-dream').click()
    const section = page.getByTestId('memory-model-settings')
    await expect(section).toBeVisible()
    await expect(page.getByTestId('memory-model-save')).toBeDisabled()

    await page.getByTestId('memory-model-dreamer-select').click()
    await page.locator('.t-select-option').filter({ hasText: chat.display_name || chat.name }).first().click()
    const saved = page.waitForResponse(r => r.url().includes('/memories/settings') && r.request().method() === 'PUT')
    await page.getByTestId('memory-model-save').click()
    expect((await saved).ok()).toBeTruthy()

    await page.reload()
    await page.getByTestId('memory-subtab-dream').click()
    await expect(page.getByTestId('memory-model-dreamer-effective')).toContainText(chat.display_name || chat.name)
    const settings = (await (await api(page, 'GET', `/memories/settings?kb_id=${kb}`)).json()).data
    expect(settings.dreamer_model_id).toBe(chat.id)
    expect(settings.dreamer.source).toBe('setting')
    expect(settings.text.source).not.toBe('setting')

    // Server-side validation: only active chat models of the workspace.
    const invalid = await api(page, 'PUT', '/memories/settings', { kb_id: kb, dreamer_model_id: embedding.id, text_model_id: '' })
    expect(invalid.status()).toBe(400)
  } finally {
    const reset = await api(page, 'PUT', '/memories/settings', { kb_id: kb, dreamer_model_id: '', text_model_id: '' })
    expect(reset.ok()).toBeTruthy()
  }
  const cleared = (await (await api(page, 'GET', `/memories/settings?kb_id=${kb}`)).json()).data
  expect(cleared.dreamer.source).not.toBe('setting')
})

test.describe('viewer', () => {
  test.use({ storageState: statePath('dave') })
  test('a viewer sees the effective models but cannot change them', async ({ page }) => {
    await page.goto(kbPath())
    await page.getByTestId('memory-subtab-dream').click()
    await expect(page.getByTestId('memory-model-dreamer-effective')).toBeVisible()
    await expect(page.getByTestId('memory-model-dreamer-select')).toHaveCount(0)
    await expect(page.getByTestId('memory-model-save')).toHaveCount(0)
    const forbidden = await api(page, 'PUT', '/memories/settings', { kb_id: kb, dreamer_model_id: '', text_model_id: '' })
    expect(forbidden.status()).toBe(403)
  })
})
