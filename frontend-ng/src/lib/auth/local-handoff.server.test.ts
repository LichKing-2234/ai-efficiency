import { afterEach, describe, expect, test } from 'vitest'
import { BACKEND_URL_COOKIE } from './cookies'
import { localHandoffCallbackResponse, localHandoffIssueResponse } from './local-handoff.server'

describe('local auth handoff server routes', () => {
  const originalEnv = { ...process.env }

  afterEach(() => {
    process.env.AE_FRONTEND_BACKEND_URL = originalEnv.AE_FRONTEND_BACKEND_URL
    process.env.VITE_BACKEND_URL = originalEnv.VITE_BACKEND_URL
  })

  test('issues an oauth2 local callback with the configured backend target', () => {
    process.env.AE_FRONTEND_BACKEND_URL = 'https://ai-efficiency.example.com'
    const response = localHandoffIssueResponse(new Request('https://web.example.com/oauth2/local?target=http://127.0.0.1:4317', {
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
    expect(location.searchParams.get('backend_url')).toBe('https://ai-efficiency.example.com')
  })

  test('fails handoff when no backend target is configured', async () => {
    delete process.env.AE_FRONTEND_BACKEND_URL
    delete process.env.VITE_BACKEND_URL
    const response = localHandoffIssueResponse(new Request('https://web.example.com/oauth2/local?target=http://127.0.0.1:4317', {
      headers: {
        cookie: 'ae_app_access=access-token'
      }
    }), '/oauth2/local')

    expect(response.status).toBe(503)
    await expect(response.json()).resolves.toMatchObject({
      message: 'local handoff backend target is not configured'
    })
  })

  test('writes the backend target into the localhost cookie on callback', () => {
    const response = localHandoffCallbackResponse(new Request(
      'http://127.0.0.1:4317/oauth2/local?access_token=access-token&refresh_token=refresh-token&backend_url=https%3A%2F%2Fai-efficiency.example.com'
    ))

    expect(response.status).toBe(302)
    const cookies = response.headers.getSetCookie()
    expect(cookies.find((cookie) => cookie.startsWith(`${BACKEND_URL_COOKIE}=`))).toContain(
      'ae_backend_url=https%3A%2F%2Fai-efficiency.example.com'
    )
  })
})
