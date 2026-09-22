import { test, expect, type Page } from '@playwright/test'

async function setupMockApis(page: Page) {
  await page.route(
    (url) =>
      !url.pathname.startsWith('/src/') &&
      !url.pathname.startsWith('/@') &&
      !url.pathname.startsWith('/node_modules/') &&
      url.pathname.includes('/api/'),
    async (route) => {
      const url = route.request().url()

      if (url.includes('/auth/me')) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              user: {
                id: 1,
                username: 'admin',
                nickname: 'Admin',
                role: 'system_admin',
              },
              tenant: {
                id: 1,
                name: 'Không gian làm việc',
                owner_id: 1,
              },
              memberships: [
                {
                  tenant_id: 1,
                  tenant_name: 'Không gian làm việc',
                  role: 'owner',
                },
              ],
              capabilities: {
                can_create_tenant: true,
              },
            },
          }),
        })
      }

      if (url.includes('/api-principal-config')) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              mode: 'tenant',
              direct_header_name: 'X-External-User-ID',
              require_direct_header: false,
              token_header_name: 'X-External-User-Token',
              has_hmac_secret: false,
            },
          }),
        })
      }

      if (url.includes('/api-keys')) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: [
              {
                id: 'key-test-1',
                name: 'Khóa API Thử nghiệm',
                prefix: 'wk_live_abc12345',
                capabilities: ['full_access'],
                knowledge_base_ids: [],
                created_at: '2026-09-20T10:00:00Z',
              },
            ],
          }),
        })
      }

      if (url.includes('/knowledge-bases') || url.includes('/agents') || url.includes('/im-channels') || url.includes('/embed-channels')) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: [],
          }),
        })
      }

      // Default catch-all for any other API calls
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: [],
        }),
      })
    },
  )
}

async function assertNoUnrenderedI18nKeys(page: Page) {
  // Pattern to catch untranslated keys like integrations.api.title or error.tenant.listFailed
  const i18nKeyRegex = /\b(?:integrations|settings|common|error|input|chat|knowledge|file|datasource|mcpServiceDialog|platformApiKeys)\.[a-zA-Z0-9_.-]+\b/g

  // Scan text content of the page body
  const bodyText = await page.evaluate(() => document.body.innerText)
  const matches = bodyText.match(i18nKeyRegex) || []

  // Filter out any potential code snippets or technical mentions
  const offending = matches.filter((m) => !m.endsWith('.ts') && !m.endsWith('.vue') && !m.endsWith('.js'))

  expect(
    offending,
    `Found unrendered raw i18n keys on page: ${JSON.stringify(offending)}`,
  ).toEqual([])
}

test.describe('Vietnamese Locale UI - Feature Integrations', () => {
  test.beforeEach(async ({ page }) => {
    await setupMockApis(page)
    await page.addInitScript(() => {
      localStorage.setItem('locale', 'vi-VN')
      localStorage.setItem('weknora_token', 'mock_jwt_token_for_playwright_test')
      localStorage.setItem('weknora_refresh_token', 'mock_refresh_token')
      localStorage.setItem(
        'weknora_memberships',
        JSON.stringify([{ tenant_id: 1, tenant_name: 'Không gian làm việc', role: 'owner' }]),
      )
      localStorage.setItem('weknora:new-user-guide-done:v1', '1')
    })
  })

  test('Integrations API tab renders Vietnamese text and zero raw keys', async ({ page }) => {
    await page.goto('/platform/settings?section=integrations&tab=api')
    await page.waitForLoadState('networkidle')

    // Wait for the API integration section title
    const apiHeading = page.locator('h2:has-text("Tích hợp API")')
    await expect(apiHeading.first()).toBeVisible({ timeout: 10000 })

    // Check Vietnamese elements
    await expect(page.locator('text=URL Cơ sở API').first()).toBeVisible()
    await expect(page.locator('text=Khóa API').first()).toBeVisible()
    await expect(page.locator('text=Tạo Khóa API').first()).toBeVisible()
    await expect(page.locator('text=Chế độ định danh người dùng').first()).toBeVisible()

    // Assert raw key integrations.api.title is NOT in the DOM
    const rawKey = page.locator('text=integrations.api.title')
    await expect(rawKey).toHaveCount(0)

    // Deep check for any unrendered i18n dot-keys
    await assertNoUnrenderedI18nKeys(page)
  })

  test('Integrations IM tab renders Vietnamese text and zero raw keys', async ({ page }) => {
    await page.goto('/platform/settings?section=integrations&tab=im')
    await page.waitForLoadState('networkidle')

    await expect(page.locator('h2:has-text("Tích hợp IM")').first()).toBeVisible({ timeout: 10000 })
    await assertNoUnrenderedI18nKeys(page)
  })

  test('Integrations Embed tab renders Vietnamese text and zero raw keys', async ({ page }) => {
    await page.goto('/platform/settings?section=integrations&tab=embed')
    await page.waitForLoadState('networkidle')

    await expect(page.locator('h2:has-text("Nhúng Web")').first()).toBeVisible({ timeout: 10000 })
    await assertNoUnrenderedI18nKeys(page)
  })

  test('Integrations Chrome tab renders Vietnamese text and zero raw keys', async ({ page }) => {
    await page.goto('/platform/settings?section=integrations&tab=chrome')
    await page.waitForLoadState('networkidle')

    await expect(page.locator('text=Tiện ích Chrome').first()).toBeVisible({ timeout: 10000 })
    await assertNoUnrenderedI18nKeys(page)
  })

  test('Integrations Claw tab renders Vietnamese text and zero raw keys', async ({ page }) => {
    await page.goto('/platform/settings?section=integrations&tab=claw')
    await page.waitForLoadState('networkidle')

    await expect(page.locator('text=Kỹ năng Claw').first()).toBeVisible({ timeout: 10000 })
    await assertNoUnrenderedI18nKeys(page)
  })

  test('Create API Key Dialog opens with Vietnamese text and zero raw keys', async ({ page }) => {
    await page.goto('/platform/settings?section=integrations&tab=api')
    await page.waitForLoadState('networkidle')

    // Click "Tạo Khóa API" button
    const createBtn = page.locator('button:has-text("Tạo Khóa API")').first()
    await expect(createBtn).toBeVisible({ timeout: 10000 })
    await createBtn.scrollIntoViewIfNeeded()
    await createBtn.click({ force: true })

    // Drawer should be visible
    const drawer = page.locator('.api-key-create-drawer')
    await expect(drawer.first()).toBeVisible({ timeout: 5000 })

    // Check drawer content for raw keys
    await assertNoUnrenderedI18nKeys(page)

    // Close drawer
    const cancelBtn = drawer.locator('button:has-text("Hủy")').first()
    if (await cancelBtn.isVisible()) {
      await cancelBtn.click({ force: true })
    }
  })

  test('API Playground Drawer opens with Vietnamese text and zero raw keys', async ({ page }) => {
    await page.goto('/platform/settings?section=integrations&tab=api')
    await page.waitForLoadState('networkidle')

    // Click "Mở Sân thử nghiệm" button
    const playgroundBtn = page.locator('button:has-text("Mở Sân thử nghiệm")').first()
    await expect(playgroundBtn).toBeVisible({ timeout: 10000 })
    await playgroundBtn.scrollIntoViewIfNeeded()
    await playgroundBtn.click({ force: true })

    // Drawer should open
    const drawer = page.locator('.api-playground-drawer')
    await expect(drawer.first()).toBeVisible({ timeout: 5000 })

    // Check drawer content for raw keys
    await assertNoUnrenderedI18nKeys(page)
  })
})

test.describe('Vietnamese Locale UI - Login Page', () => {
  test('Login page renders Vietnamese text and zero raw keys', async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('locale', 'vi-VN')
      localStorage.removeItem('weknora_token')
      localStorage.removeItem('weknora_refresh_token')
      localStorage.setItem('weknora:new-user-guide-done:v1', '1')
    })

    await page.goto('/login')
    await page.waitForLoadState('networkidle')

    // Assert Vietnamese login elements
    const loginHeading = page.locator('h2:has-text("Đăng nhập")')
    await expect(loginHeading.first()).toBeVisible({ timeout: 10000 })
    await expect(page.locator('button:has-text("Đăng nhập")').first()).toBeVisible()
    await expect(page.locator('button:has-text("Tạo Tài khoản")').first()).toBeVisible()

    // Check for zero unrendered keys
    const bodyText = await page.evaluate(() => document.body.innerText)
    const i18nKeyRegex = /\b(?:login|auth|common|error)\.[a-zA-Z0-9_.-]+\b/g
    const matches = (bodyText.match(i18nKeyRegex) || []).filter(
      (m) => !m.endsWith('.ts') && !m.endsWith('.vue') && !m.endsWith('.js'),
    )
    expect(matches, `Found raw keys on login page: ${JSON.stringify(matches)}`).toEqual([])
  })
})

