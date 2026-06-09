import { describe, expect, test } from 'vitest'
import { ACCESS_COOKIE, appendTokenCookies, readAppTokens, REFRESH_COOKIE } from './cookies'

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
})
