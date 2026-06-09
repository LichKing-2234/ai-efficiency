import { describe, expect, test } from 'vitest'
import {
  ACCESS_COOKIE,
  appendBackendUrlCookie,
  appendClearTokenCookies,
  appendTokenCookies,
  BACKEND_URL_COOKIE,
  readAppTokens,
  readBackendUrlCookie,
  REFRESH_COOKIE
} from './cookies'

describe('app auth cookies', () => {
  test('keeps refresh-only sessions readable for server-side refresh', () => {
    const request = new Request('http://127.0.0.1:4317/repos', {
      headers: { cookie: `${REFRESH_COOKIE}=refresh-token` }
    })

    expect(readAppTokens(request)).toEqual({ accessToken: undefined, refreshToken: 'refresh-token' })
  })

  test('marks production cookies secure behind a forwarded https proxy', () => {
    const headers = new Headers()
    appendTokenCookies(headers, { accessToken: 'access-token', refreshToken: 'refresh-token' }, new Request('http://internal-service/login', {
      headers: { 'x-forwarded-proto': 'https' }
    }))

    const cookies = headers.getSetCookie()
    expect(cookies.find((cookie) => cookie.startsWith(`${ACCESS_COOKIE}=`))).toContain('Secure')
    expect(cookies.find((cookie) => cookie.startsWith(`${REFRESH_COOKIE}=`))).toContain('Secure')
  })

  test('keeps local handoff backend target in an HttpOnly cookie', () => {
    const headers = new Headers()
    appendBackendUrlCookie(headers, 'https://ai-efficiency.la3.agoralab.co', new Request('http://127.0.0.1:4317/oauth2/local'))

    const cookie = headers.getSetCookie().find((value) => value.startsWith(`${BACKEND_URL_COOKIE}=`))
    expect(cookie).toContain('HttpOnly')
    expect(cookie).toContain('SameSite=Lax')

    const request = new Request('http://127.0.0.1:4317/', {
      headers: { cookie: `${BACKEND_URL_COOKIE}=https://ai-efficiency.la3.agoralab.co` }
    })
    expect(readBackendUrlCookie(request)).toBe('https://ai-efficiency.la3.agoralab.co')
  })

  test('clears the local backend target with app auth cookies', () => {
    const headers = new Headers()
    appendClearTokenCookies(headers, new Request('http://127.0.0.1:4317/logout'))

    const cookies = headers.getSetCookie()
    expect(cookies.find((cookie) => cookie.startsWith(`${ACCESS_COOKIE}=`))).toContain('Max-Age=0')
    expect(cookies.find((cookie) => cookie.startsWith(`${REFRESH_COOKIE}=`))).toContain('Max-Age=0')
    expect(cookies.find((cookie) => cookie.startsWith(`${BACKEND_URL_COOKIE}=`))).toContain('Max-Age=0')
  })
})
