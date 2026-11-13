import { afterEach, describe, expect, it } from 'vitest'
import { appBasePath, appURL } from '@/basePath'

afterEach(() => document.querySelector('base')?.remove())

describe('runtime base path', () => {
  it('сохраняет корневые URL по умолчанию', () => {
    expect(appBasePath()).toBe('/')
    expect(appURL('/api/clusters?q=a%2Fb')).toBe('/api/clusters?q=a%2Fb')
  })
  it('использует один HTML base для API и ресурсов', () => {
    document.head.insertAdjacentHTML('afterbegin', '<base href="/tools/dasha/">')
    expect(appBasePath()).toBe('/tools/dasha/')
    expect(appURL('/api/clusters')).toBe('/tools/dasha/api/clusters')
    expect(appURL('/logo.png')).toBe('/tools/dasha/logo.png')
  })
})
