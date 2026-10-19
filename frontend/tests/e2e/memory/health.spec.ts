import { test, expect } from '@playwright/test'
import { api, kbPath, world } from './world'

const kb = world.knowledge_bases['acme-eng'].id

test('health exposes real bounded coverage and heuristic limitations', async ({ page }) => {
  await page.goto(kbPath())
  await page.getByTestId('memory-subtab-health').click()
  await expect(page.getByTestId('memory-health-coverage')).toBeVisible()
  const response = await api(page, 'GET', `/memories/health?kb_id=${kb}`)
  expect(response.ok()).toBeTruthy()
  const report = (await response.json()).data
  expect(report.coverage.total).toBeGreaterThan(0)
  expect(report.coverage.scanned).toBeLessThanOrEqual(report.coverage.limit)
  expect(report.coverage.scanned).toBeLessThanOrEqual(report.coverage.total)
  expect(report.checks.contradiction.examined).toBeLessThanOrEqual(report.coverage.scanned)
  await expect(page.getByTestId('memory-health-coverage')).toContainText(/heuristic/)
})

test('a transport failure never becomes a clean health report and can be retried', async ({ page }) => {
  // Deliberately fail the transport, not a fabricated successful API response.
  // Repository failures are covered separately by backend error-propagation tests.
  await page.route('**/api/v1/memories/health?*', route => route.abort('failed'))
  await page.goto(kbPath())
  await page.getByTestId('memory-subtab-health').click()
  await expect(page.getByTestId('memory-health-error')).toBeVisible()
  await expect(page.getByTestId('memory-health-coverage')).toHaveCount(0)
  await page.unroute('**/api/v1/memories/health?*')
  await page.getByTestId('memory-health-error').getByRole('button').click()
  await expect(page.getByTestId('memory-health-coverage')).toBeVisible()
  await expect(page.getByTestId('memory-health-error')).toHaveCount(0)
})
