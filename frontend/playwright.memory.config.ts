import { defineConfig, devices } from '@playwright/test'
import base from './playwright.config'

// Separate entry point keeps mock tests and real credentials isolated. Project
// definitions must be identical in the runner and its workers (never argv-based).
export default defineConfig({
  ...base,
  testIgnore: [],
  testMatch: '**/memory/*.spec.ts',
  globalSetup: './tests/e2e/memory/globalSetup.ts',
  webServer: undefined,
  timeout: 120000,
  retries: 1,
  projects: [{
    name: 'memory-real',
    use: {
      ...devices['Desktop Chrome'],
      baseURL: process.env.MEMV2_E2E_FRONTEND_URL || 'http://localhost:15173',
      storageState: './tests/e2e/memory/storageState/alice.json',
      actionTimeout: 10000,
      trace: 'on-first-retry',
      locale: 'vi-VN',
    },
  }],
})
