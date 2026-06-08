import { parse, serialize } from 'cookie'

export const ACCESS_COOKIE = 'ae_app_access'
export const REFRESH_COOKIE = 'ae_app_refresh'

export interface AppTokens {
  accessToken: string
  refreshToken?: string
}

function isProductionRequest(request: Request) {
  const url = new URL(request.url)
  return url.protocol === 'https:' && !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
}

export function readAppTokens(request: Request): AppTokens | null {
  const cookies = parse(request.headers.get('cookie') ?? '')
  const accessToken = cookies[ACCESS_COOKIE]
  if (!accessToken) return null
  return {
    accessToken,
    refreshToken: cookies[REFRESH_COOKIE]
  }
}

export function appendTokenCookies(headers: Headers, tokens: AppTokens, request: Request) {
  const secure = isProductionRequest(request)
  headers.append(
    'Set-Cookie',
    serialize(ACCESS_COOKIE, tokens.accessToken, {
      httpOnly: true,
      secure,
      sameSite: 'lax',
      path: '/',
      maxAge: 60 * 60 * 2
    })
  )
  if (tokens.refreshToken) {
    headers.append(
      'Set-Cookie',
      serialize(REFRESH_COOKIE, tokens.refreshToken, {
        httpOnly: true,
        secure,
        sameSite: 'lax',
        path: '/',
        maxAge: 60 * 60 * 24 * 7
      })
    )
  }
}

export function appendClearTokenCookies(headers: Headers, request: Request) {
  const secure = isProductionRequest(request)
  for (const name of [ACCESS_COOKIE, REFRESH_COOKIE]) {
    headers.append(
      'Set-Cookie',
      serialize(name, '', {
        httpOnly: true,
        secure,
        sameSite: 'lax',
        path: '/',
        maxAge: 0
      })
    )
  }
}
