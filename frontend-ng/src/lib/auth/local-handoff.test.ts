import { describe, expect, test } from 'vitest'
import { buildLocalCallbackUrl, isAllowedBackendUrl, isAllowedLocalTarget } from './local-handoff'

describe('local auth handoff', () => {
  test('only allows localhost callback targets', () => {
    expect(isAllowedLocalTarget('http://127.0.0.1:4317')).toBe(true)
    expect(isAllowedLocalTarget('http://localhost:4317')).toBe(true)
    expect(isAllowedLocalTarget('https://localhost:4317')).toBe(true)
    expect(isAllowedLocalTarget('https://example.com')).toBe(false)
    expect(isAllowedLocalTarget('javascript:alert(1)')).toBe(false)
  })

  test('only allows http backend handoff targets', () => {
    expect(isAllowedBackendUrl('https://ai-efficiency.la3.agoralab.co')).toBe(true)
    expect(isAllowedBackendUrl('http://localhost:8081')).toBe(true)
    expect(isAllowedBackendUrl('javascript:alert(1)')).toBe(false)
    expect(isAllowedBackendUrl('ftp://example.com')).toBe(false)
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

  test('can target the oauth2 local route expected by gateway deployments', () => {
    const callback = buildLocalCallbackUrl(
      'http://127.0.0.1:4317',
      'access-token',
      'refresh-token',
      '/oauth2/local',
      'https://ai-efficiency.la3.agoralab.co'
    )

    expect(callback.origin).toBe('http://127.0.0.1:4317')
    expect(callback.pathname).toBe('/oauth2/local')
    expect(callback.searchParams.get('access_token')).toBe('access-token')
    expect(callback.searchParams.get('refresh_token')).toBe('refresh-token')
    expect(callback.searchParams.get('backend_url')).toBe('https://ai-efficiency.la3.agoralab.co')
  })
})
