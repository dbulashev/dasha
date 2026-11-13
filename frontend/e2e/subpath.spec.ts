import { test, expect } from '@playwright/test'

// Стенд — готовый frontend-контейнер; PostgreSQL и настоящий IdP не используются.
// eslint-disable-next-line playwright/no-skipped-test -- отдельный контейнерный стенд
test.skip(!process.env.DASHA_SUBPATH_URL, 'Нужен frontend-контейнер и DASHA_SUBPATH_URL')

for (const entry of ['/', '/settings/ci?host=h&db=d']) {
  test(`runtime-префикс: возврат после SSO с ${entry}`, async ({ page, baseURL }) => {
    const url = new URL(baseURL!)
    const prefix = url.pathname.replace(/\/$/, '')
    const target = prefix + '/settings/ci?host=h&db=d'
    await page.addInitScript(saved => sessionStorage.setItem('dasha_return_url', saved), target)
    await page.route(`${url.origin}${prefix}/api/**`, route => route.fulfill({
      json: new URL(route.request().url()).pathname.endsWith('/auth/info')
        ? { mode: 'oidc', oidc_login_url: prefix + '/auth/login' }
        : [],
    }))
    await page.route(`${url.origin}${prefix}/auth/me`, route => route.fulfill({
      json: { name: 'test-user', email: 'test@example.test', role: 'admin' },
    }))
    await page.goto(url.origin + prefix + entry)
    await expect(page).toHaveURL(url.origin + target)
    await expect(page.locator('nav').first()).toBeVisible()
    expect(await page.evaluate(() => sessionStorage.getItem('dasha_return_url'))).toBeNull()
  })
}

test('runtime-префикс: deep link, ресурсы и reload', async ({ page, baseURL }) => {
  const url = new URL(baseURL!)
  const prefix = url.pathname.replace(/\/$/, '')
  const escaped: string[] = []
  const failed: string[] = []
  const errors: string[] = []
  page.on('request', request => {
    const target = new URL(request.url())
    if (target.origin === url.origin && !target.pathname.startsWith(prefix + '/')) escaped.push(target.pathname)
  })
  page.on('response', response => { if (response.status() >= 400) failed.push(response.url()) })
  page.on('pageerror', error => errors.push(error.message))
  await page.route(`${url.origin}${prefix}/api/**`, route => route.fulfill({
    json: new URL(route.request().url()).pathname.endsWith('/auth/info') ? { mode: 'none' } : [],
  }))
  await page.goto(`${url.origin}${prefix}/settings`)
  await expect(page.locator('nav').first()).toBeVisible()
  await page.reload()
  await expect(page.locator('nav').first()).toBeVisible()
  expect(await page.evaluate(() => document.querySelector('base')?.getAttribute('href'))).toBe(prefix + '/')
  expect(escaped).toEqual([])
  expect(failed).toEqual([])
  expect(errors).toEqual([])
})

test('runtime-префикс: SSO-кнопка не дублирует префикс backend', async ({ page, baseURL }) => {
  const url = new URL(baseURL!)
  const prefix = url.pathname.replace(/\/$/, '')
  await page.route(`${url.origin}${prefix}/api/auth/info`, route => route.fulfill({
    json: { mode: 'oidc', oidc_login_url: prefix + '/auth/login' },
  }))
  await page.route(`${url.origin}${prefix}/auth/me`, route => route.fulfill({ status: 401, json: {} }))
  await page.route(`${url.origin}${prefix}/auth/login`, route => route.fulfill({ body: 'Тестовый вход' }))
  await page.goto(baseURL!)
  const logo = page.getByRole('img', { name: 'Dasha', exact: true })
  await expect(logo).toHaveAttribute('src', prefix + '/logo.png')
  await expect.poll(() => logo.evaluate(image => (image as HTMLImageElement).naturalWidth)).toBeGreaterThan(0)
  await page.getByRole('button', { name: /SSO/i }).click()
  await expect(page).toHaveURL(`${url.origin}${prefix}/auth/login`)
})
