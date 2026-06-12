import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { getAuthOptionsTarget, proxyApiRequest } from './server'

describe('api server auth target resolution', () => {
  const fetchMock = vi.fn<typeof fetch>()
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    globalThis.fetch = fetchMock as typeof fetch
  })

  afterEach(() => {
    delete process.env.AE_FRONTEND_BACKEND_URL
    delete process.env.VITE_BACKEND_URL
    globalThis.fetch = originalFetch
    fetchMock.mockReset()
  })

  test('uses the backend origin directly for local backend auth options', () => {
    process.env.AE_FRONTEND_BACKEND_URL = 'http://127.0.0.1:8081'

    expect(getAuthOptionsTarget(new Request('http://127.0.0.1:4421/api/auth/options'))).toBe(
      'http://127.0.0.1:8081/api/v1/auth/options'
    )
  })

  test('rewrites the deployed web host to the public backend auth options host', () => {
    process.env.AE_FRONTEND_BACKEND_URL = 'https://ai-efficiency-web.la3.agoralab.co'

    expect(getAuthOptionsTarget(new Request('http://127.0.0.1:4421/api/auth/options'))).toBe(
      'https://ai-efficiency.la3.agoralab.co/api/v1/auth/options'
    )
  })

  test('preserves custom non-web frontend hosts for auth options', () => {
    process.env.AE_FRONTEND_BACKEND_URL = 'https://web.example.com'

    expect(getAuthOptionsTarget(new Request('http://127.0.0.1:4421/api/auth/options'))).toBe(
      'https://web.example.com/api/v1/auth/options'
    )
  })

  test('normalizes upstream oauth redirects into auth failures for browser api fetches', async () => {
    process.env.AE_FRONTEND_BACKEND_URL = 'https://ai-efficiency.la3.agoralab.co'
    fetchMock.mockResolvedValue(
      new Response('<html>redirect</html>', {
        status: 302,
        headers: {
          location: 'https://oauth.agoralab.co/oauth/authorize?client_id=test'
        }
      })
    )

    const response = await proxyApiRequest(new Request('http://127.0.0.1:4421/api/v1/auth/me'), '/api/v1/auth/me')

    expect(response.status).toBe(401)
    await expect(response.json()).resolves.toEqual({
      code: 401,
      message: 'authentication required'
    })
  })
})
