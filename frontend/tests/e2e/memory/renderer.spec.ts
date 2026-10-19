import { test, expect } from '@playwright/test'

test('patched math and diagram dependencies render without allowing executable HTML', async ({ page }) => {
  // The dev-only page exercises the same renderer used by chat and embed views.
  await page.goto('/platform/dev/markdown')
  const root = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Custom Input', exact: true }) })
  await root.getByRole('textbox').fill([
    '# Memory release renderer',
    'Vietnamese: kiểm chứng ký ức.',
    'Inline math: $E=mc^2$.',
    '```mermaid\nflowchart LR\n  Memory --> Audit\n  Audit --> Graph\n```',
    '<img src="x" onerror="window.__memorySecurityTest=1">',
    '<a href="javascript:window.__memorySecurityTest=1">Unsafe link</a>',
  ].join('\n\n'))
  await expect(root.locator('.katex')).toBeVisible()
  await expect(root.locator('svg[id^="mermaid"]').first()).toBeVisible()
  await expect(root.locator('svg[id^="mermaid"]').first()).toContainText('Graph')
  expect(await root.locator('[onerror], [onclick], a[href^="javascript:" i]').count()).toBe(0)
  expect(await page.evaluate(() => (window as unknown as Record<string, unknown>).__memorySecurityTest)).toBeUndefined()
  await expect(root.locator('.test-rendered')).toContainText('kiểm chứng ký ức')
})
