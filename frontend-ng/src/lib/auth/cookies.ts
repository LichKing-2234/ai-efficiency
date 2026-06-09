import { parse, serialize } from 'cookie'

export const ACCESS_COOKIE = 'ae_app_access'
export const REFRESH_COOKIE = 'ae_app_refresh'
export const BACKEND_URL_COOKIE = 'ae_backend_url'

export interface AppTokens {
  accessToken?: string
  refreshToken?: string
}

function isProductionRequest(request: Request) {
  const url = new URL(request.url)
  const forwardedProto = request.headers.get('x-forwarded-proto')?.split(',')[0]?.trim().toLowerCase()
  if (forwardedProto === 'https') return true
  const forwarded = request.headers.get('forwarded')?.toLowerCase()
  if (forwarded?.split(';').some((part) => part.trim() === 'proto=https')) return true
  return url.protocol === 'https:' && !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
}

export function readAppTokens(request: Request): AppTokens | null {
  const cookies = parse(request.headers.get('cookie') ?? '')
  const accessToken = cookies[ACCESS_COOKIE]
  const refreshToken = cookies[REFRESH_COOKIE]
  if (!accessToken && !refreshToken) return null
  return {
    accessToken,
    refreshToken
  }
}

export function readBackendUrlCookie(request: Request) {
  const cookies = parse(request.headers.get('cookie') ?? '')
  return cookies[BACKEND_URL_COOKIE]
}

export function appendTokenCookies(headers: Headers, tokens: AppTokens, request: Request) {
  const secure = isProductionRequest(request)
  if (tokens.accessToken) {
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
  }
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

export function appendBackendUrlCookie(headers: Headers, backendUrl: string, request: Request) {
  headers.append(
    'Set-Cookie',
    serialize(BACKEND_URL_COOKIE, backendUrl, {
      httpOnly: true,
      secure: isProductionRequest(request),
      sameSite: 'lax',
      path: '/',
      maxAge: 60 * 60 * 24 * 7
    })
  )
}

export function appendClearTokenCookies(headers: Headers, request: Request) {
  const secure = isProductionRequest(request)
  for (const name of [ACCESS_COOKIE, REFRESH_COOKIE, BACKEND_URL_COOKIE]) {
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
