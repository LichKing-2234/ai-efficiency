import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { ACCESS_COOKIE, BACKEND_URL_COOKIE } from './cookies'
import { localHandoffCallbackResponse, localHandoffIssueResponse, oauth2CallbackResponse } from './local-handoff.server'

describe('local auth handoff server routes', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    globalThis.fetch = originalFetch
  })

  afterEach(() => {
    delete process.env.AE_FRONTEND_BACKEND_URL
    delete process.env.VITE_BACKEND_URL
    globalThis.fetch = originalFetch
  })

  test('issues an oauth2 local callback with the current frontend origin as proxy target', async () => {
    const response = await localHandoffIssueResponse(new Request('https://web.example.com/oauth2/local?target=http://127.0.0.1:4317', {
      headers: {
        cookie: 'ae_app_access=access-token; ae_app_refresh=refresh-token'
      }
    }), '/oauth2/local')

    expect(response.status).toBe(302)
    const location = new URL(response.headers.get('Location') ?? '')
    expect(location.origin).toBe('http://127.0.0.1:4317')
    expect(location.pathname).toBe('/oauth2/local')
    expect(location.searchParams.get('access_token')).toBe('access-token')
    expect(location.searchParams.get('refresh_token')).toBe('refresh-token')
    expect(location.searchParams.get('backend_url')).toBe('https://web.example.com')
  })

  test('bootstraps from gateway before issuing local handoff when app cookies are absent', async () => {
    process.env.AE_FRONTEND_GATEWAY_EXCHANGE_SECRET = 'exchange-secret'
    process.env.AE_FRONTEND_BACKEND_URL = 'https://api.example.com'
    globalThis.fetch = vi.fn(async (input) => {
      expect(String(input)).toBe('https://api.example.com/api/auth/gateway-exchange')
      return new Response(JSON.stringify({
        code: 0,
        data: {
          token: 'bootstrapped-access-token',
          refresh_token: 'bootstrapped-refresh-token'
        }
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    }) as typeof fetch

    const response = await localHandoffIssueResponse(new Request('https://web.example.com/oauth2/local?target=http://127.0.0.1:4317', {
      headers: {
        'x-oauth-email': 'alice@example.com',
        'x-oauth-displayname': 'Alice Zhang'
      }
    }), '/oauth2/local')

    expect(response.status).toBe(302)
    const location = new URL(response.headers.get('Location') ?? '')
    expect(location.searchParams.get('access_token')).toBe('bootstrapped-access-token')
    expect(location.searchParams.get('refresh_token')).toBe('bootstrapped-refresh-token')
    expect(location.searchParams.get('backend_url')).toBe('https://web.example.com')
  })

  test('writes the proxy target into the localhost cookie on callback', () => {
    const response = localHandoffCallbackResponse(new Request(
      'http://127.0.0.1:4317/oauth2/local?access_token=access-token&refresh_token=refresh-token&backend_url=https%3A%2F%2Fweb.example.com'
    ))

    expect(response.status).toBe(302)
    const cookies = response.headers.getSetCookie()
    expect(cookies.find((cookie) => cookie.startsWith(`${BACKEND_URL_COOKIE}=`))).toContain(
      'ae_backend_url=https%3A%2F%2Fweb.example.com'
    )
  })

  test('bootstraps app cookies during oauth callback before redirecting back to localhost', async () => {
    process.env.AE_FRONTEND_GATEWAY_EXCHANGE_SECRET = 'exchange-secret'
    process.env.AE_FRONTEND_BACKEND_URL = 'https://api.example.com'
    globalThis.fetch = vi.fn(async () => {
      return new Response(JSON.stringify({
        code: 0,
        data: {
          token: 'bootstrapped-access-token',
          refresh_token: 'bootstrapped-refresh-token'
        }
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    }) as typeof fetch

    const response = await oauth2CallbackResponse(new Request('https://web.example.com/oauth2/callback?local=http://127.0.0.1:4317', {
      headers: {
        'x-oauth-email': 'alice@example.com',
        'x-oauth-displayname': 'Alice Zhang'
      }
    }))

    expect(response.status).toBe(302)
    expect(response.headers.get('Location')).toBe('http://127.0.0.1:4317/')
    const cookies = response.headers.getSetCookie()
    expect(cookies.find((cookie) => cookie.startsWith(`${ACCESS_COOKIE}=`))).toContain('bootstrapped-access-token')
  })

  test('redirects to the app root when oauth callback already has an app session', async () => {
    const response = await oauth2CallbackResponse(new Request('https://web.example.com/oauth2/callback', {
      headers: {
        cookie: 'ae_app_access=access-token'
      }
    }))

    expect(response.status).toBe(302)
    expect(response.headers.get('Location')).toBe('https://web.example.com/')
    expect(response.headers.getSetCookie()).toEqual([])
  })
})
