import { describe, expect, it } from 'vitest'

import { ApiError, assertOk, featureUnavailableReason } from '../api'

function errorOf(status: number, data: unknown): unknown {
  try {
    assertOk({ status, data })
  } catch (err) {
    return err
  }
  return null
}

describe('featureUnavailableReason', () => {
  it('reads the reason of a 501', () => {
    expect(featureUnavailableReason(errorOf(501, { reason: 'unsupported_version' }), 'no_storage')).toBe(
      'unsupported_version',
    )
  })

  it('falls back when the 501 has no known reason', () => {
    expect(featureUnavailableReason(errorOf(501, { message: 'HTTP 501' }), 'no_storage')).toBe('no_storage')
  })

  it('ignores other errors', () => {
    expect(featureUnavailableReason(errorOf(500, {}), 'no_storage')).toBeNull()
    expect(featureUnavailableReason(new Error('x'), 'no_storage')).toBeNull()
    expect(errorOf(404, { message: 'nope' })).toBeInstanceOf(ApiError)
  })
})
