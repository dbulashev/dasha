import { test, expect, type Page } from '@playwright/test'

const CLUSTERS = [
  {
    name: 'prod-auth',
    instances: [{ host_name: 'auth-01' }],
    databases: ['auth'],
    supports_logs: false,
  },
  {
    name: 'prod-billing',
    instances: [{ host_name: 'pg-01' }, { host_name: 'pg-02' }],
    databases: ['app', 'db01'],
    supports_logs: true,
  },
]

const ROLES: Record<string, { InRecovery: boolean; VersionNum: number }> = {
  'auth-01': { InRecovery: false, VersionNum: 170000 },
  'pg-01': { InRecovery: false, VersionNum: 170000 },
  'pg-02': { InRecovery: true, VersionNum: 170000 },
}

const mod = process.platform === 'darwin' ? 'Meta' : 'Control'

test.use({ locale: 'en-US' })

async function mockApi(page: Page) {
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/auth/info')) return route.fulfill({ json: { mode: 'none' } })
    if (path.endsWith('/api/clusters')) return route.fulfill({ json: CLUSTERS })
    if (path.endsWith('/common/instance-info')) {
      const host = url.searchParams.get('instance') ?? ''
      // A slow primary: the digit must wait for its role instead of guessing.
      if (host === 'pg-01') await new Promise(r => setTimeout(r, 300))
      return ROLES[host] ? route.fulfill({ json: ROLES[host] }) : route.fulfill({ status: 404, json: {} })
    }
    return route.fulfill({ json: path.includes('status') ? {} : [] })
  })
}

test.beforeEach(async ({ page }) => {
  await mockApi(page)
})

const palette = (page: Page) => page.getByRole('combobox', { name: 'Command palette' })

test('Ctrl-K, 1 goes to the primary of the current cluster', async ({ page }) => {
  await page.goto('/locks/prod-billing?host=pg-02&db=app')
  await expect(page.locator('nav').first()).toBeVisible()
  await page.keyboard.press(`${mod}+k`)
  await expect(palette(page)).toBeFocused()
  await page.keyboard.press('1')
  await expect(page).toHaveURL(/\/locks\/prod-billing\?host=pg-01&db=app$/)
  await expect(palette(page)).toBeHidden()
})

test('a cluster and a section in one query', async ({ page }) => {
  await page.goto('/main/prod-auth?host=auth-01&db=auth')
  await expect(page.locator('nav').first()).toBeVisible()
  await page.keyboard.press(`${mod}+k`)
  await palette(page).fill('billing repl')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/replication\/prod-billing\?host=pg-01/)
})

test('digits are text once the field is not empty', async ({ page }) => {
  await page.goto('/locks/prod-billing?host=pg-02&db=app')
  await expect(page.locator('nav').first()).toBeVisible()
  const before = page.url()
  await page.keyboard.press(`${mod}+k`)
  await page.keyboard.type('db01')
  await expect(palette(page)).toHaveValue('db01')
  expect(page.url()).toBe(before)
})

test('opens from a text field and gives the focus back on Esc', async ({ page }) => {
  await page.goto('/logs/prod-billing?host=pg-01&db=app')
  const field = page.getByRole('combobox', { name: 'Search in text' })
  await field.click()
  await page.keyboard.press(`${mod}+k`)
  await expect(palette(page)).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(palette(page)).toBeHidden()
  await expect(field).toBeFocused()
})
