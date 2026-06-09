import { describe, expect, test } from 'vitest'
import { buildLocalCallbackUrl, isAllowedLocalTarget } from './local-handoff'

describe('local auth handoff', () => {
  test('only allows localhost callback targets', () => {
    expect(isAllowedLocalTarget('http://127.0.0.1:4317')).toBe(true)
    expect(isAllowedLocalTarget('http://localhost:4317')).toBe(true)
    expect(isAllowedLocalTarget('https://localhost:4317')).toBe(true)
    expect(isAllowedLocalTarget('https://example.com')).toBe(false)
    expect(isAllowedLocalTarget('javascript:alert(1)')).toBe(false)
  })

  test('builds the localhost callback with app tokens', () => {
    const callback = buildLocalCallbackUrl('http://127.0.0.1:4317', 'access-token', 'refresh-token')

    expect(callback.origin).toBe('http://127.0.0.1:4317')
    expect(callback.pathname).toBe('/api/local/callback')
    expect(callback.searchParams.get('access_token')).toBe('access-token')
    expect(callback.searchParams.get('refresh_token')).toBe('refresh-token')
  })

  test('allows refresh-only handoff when the short-lived access cookie expired', () => {
    const callback = buildLocalCallbackUrl('http://127.0.0.1:4317', undefined, 'refresh-token')

    expect(callback.searchParams.has('access_token')).toBe(false)
    expect(callback.searchParams.get('refresh_token')).toBe('refresh-token')
  })
})
