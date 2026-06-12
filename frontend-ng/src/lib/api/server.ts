import { appendClearTokenCookies, appendTokenCookies, readAppTokens, readBackendUrlCookie } from '@/lib/auth/cookies'
import { isAllowedBackendUrl } from '@/lib/auth/local-handoff'
import { readGatewayClaims } from '@/lib/auth/gateway'
import { extractTokens } from '@/lib/auth/tokens'
import type { ApiResponse, AuthTokenPayload } from '@/lib/api/types'

const DEFAULT_BACKEND_URL = 'http://localhost:8081'
const DEPLOYED_WEB_HOST_SUFFIX = '-web.'

const API_ALLOWLIST = [
  '/api/v1/auth/',
  '/api/v1/user/',
  '/api/v1/repos',
  '/api/v1/prs/',
  '/api/v1/pr-sync-jobs/',
  '/api/v1/events',
  '/api/v1/admin/',
  '/api/v1/settings/',
  '/api/v1/scm-providers',
  '/api/v1/efficiency/',
]

export function getBackendUrl(request?: Request) {
  const handoffBackendUrl = request ? readBackendUrlCookie(request) : undefined
  const configuredBackendUrl = isAllowedBackendUrl(handoffBackendUrl ?? null) ? handoffBackendUrl : undefined
  return (
    configuredBackendUrl ||
    process.env.AE_FRONTEND_BACKEND_URL ||
    process.env.VITE_BACKEND_URL ||
    import.meta.env.VITE_BACKEND_URL ||
    DEFAULT_BACKEND_URL
  ).replace(/\/$/, '')
}

function isFrontendProxyTarget(target: string) {
  try {
    const url = new URL(target)
    return url.protocol === 'https:' || !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  } catch {
    return false
  }
}

function buildBffAuthTarget(request: Request, path: `/api/auth/${string}` | `/api/v1/auth/${string}`) {
  const baseUrl = getBackendUrl(request)
  if (isFrontendProxyTarget(baseUrl)) {
    const authPath = path.replace('/api/v1/auth/', '/api/auth/')
    return `${baseUrl}${authPath}`
  }
  return `${baseUrl}${path}`
}

function getPublicAuthOptionsBaseUrl(request: Request) {
  const baseUrl = getBackendUrl(request)
  try {
    const url = new URL(baseUrl)
    if (url.hostname.includes(DEPLOYED_WEB_HOST_SUFFIX)) {
      url.hostname = url.hostname.replace(DEPLOYED_WEB_HOST_SUFFIX, '.')
      return url.toString().replace(/\/$/, '')
    }
  } catch {
    return baseUrl
  }
  return baseUrl
}

export function getAuthOptionsTarget(request: Request) {
  return `${getPublicAuthOptionsBaseUrl(request)}/api/v1/auth/options`
}

export function getGatewayExchangeSecret() {
  return process.env.AE_FRONTEND_GATEWAY_EXCHANGE_SECRET || process.env.VITE_GATEWAY_EXCHANGE_SECRET || ''
}

export function isAllowedApiPath(pathname: string) {
  return API_ALLOWLIST.some((prefix) => pathname === prefix.slice(0, -1) || pathname.startsWith(prefix))
}

export async function proxyApiRequest(request: Request, path: string) {
  const normalizedPath = path.startsWith('/api/v1/') ? path : `/api/v1/${path.replace(/^\/+/, '')}`
  if (!isAllowedApiPath(normalizedPath)) {
    return json({ code: 403, message: 'API path is not allowed by frontend proxy' }, 403)
  }
  return proxyWithRefresh(request, normalizedPath)
}

export async function proxyOAuthRequest(request: Request, path: string) {
  if (!['/oauth/authorize/approve', '/oauth/device/verify'].includes(path)) {
    return json({ code: 403, message: 'OAuth path is not allowed by frontend proxy' }, 403)
  }
  return proxyWithRefresh(request, path)
}

async function proxyWithRefresh(request: Request, path: string) {
  const tokens = readAppTokens(request)
  let accessToken = tokens?.accessToken
  let pendingTokens = null as Awaited<ReturnType<typeof refreshTokens>>
  if (!accessToken && tokens?.refreshToken) {
    pendingTokens = await refreshTokens(request, tokens.refreshToken)
    accessToken = pendingTokens?.accessToken
  }
  const first = await forwardToBackend(request, path, accessToken)
  const normalizedFirst = normalizeAuthRedirectResponse(first)
  if (pendingTokens && first.status !== 401) {
    const headers = new Headers(normalizedFirst.headers)
    appendTokenCookies(headers, pendingTokens, request)
    return new Response(normalizedFirst.body, { status: normalizedFirst.status, statusText: normalizedFirst.statusText, headers })
  }
  if (normalizedFirst.status !== 401 || path.includes('/auth/refresh')) {
    return normalizedFirst
  }
  if (!tokens?.refreshToken) {
    return normalizedFirst
  }
  const refreshed = await refreshTokens(request, tokens.refreshToken)
  if (!refreshed) {
    const headers = new Headers(normalizedFirst.headers)
    appendClearTokenCookies(headers, request)
    return new Response(normalizedFirst.body, { status: normalizedFirst.status, headers })
  }
  const retry = await forwardToBackend(request, path, refreshed.accessToken)
  const normalizedRetry = normalizeAuthRedirectResponse(retry)
  const headers = new Headers(retry.headers)
  appendTokenCookies(headers, refreshed, request)
  return new Response(normalizedRetry.body, { status: normalizedRetry.status, statusText: normalizedRetry.statusText, headers })
}

async function forwardToBackend(request: Request, path: string, accessToken?: string) {
  const sourceUrl = new URL(request.url)
  const target = `${getBackendUrl(request)}${path}${sourceUrl.search}`
  const headers = new Headers(request.headers)
  headers.delete('host')
  headers.delete('cookie')
  headers.delete('connection')
  headers.delete('content-length')
  if (accessToken) {
    headers.set('Authorization', `Bearer ${accessToken}`)
  }
  const method = request.method.toUpperCase()
  const body = method === 'GET' || method === 'HEAD' ? undefined : await request.arrayBuffer()
  try {
    return await fetch(target, {
      method,
      headers,
      body,
      redirect: 'manual'
    })
  } catch {
    return json({ code: 502, message: 'backend is unavailable from frontend proxy' }, 502)
  }
}

function normalizeAuthRedirectResponse(response: Response) {
  if (response.status < 300 || response.status >= 400) return response
  const location = response.headers.get('location')
  if (!location || !location.includes('/oauth/authorize')) return response
  return json({ code: 401, message: 'authentication required' }, 401)
}

async function refreshTokens(request: Request, refreshToken: string) {
  const res = await fetch(`${getBackendUrl(request)}/api/v1/auth/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken })
  })
  if (!res.ok) return null
  const payload = (await res.json()) as ApiResponse<AuthTokenPayload>
  return extractTokens(payload)
}

export async function loginThroughBackend(request: Request) {
  const body = await request.arrayBuffer()
  const res = await fetch(buildBffAuthTarget(request, '/api/v1/auth/login'), {
    method: 'POST',
    headers: { 'Content-Type': request.headers.get('content-type') || 'application/json' },
    body
  })
  const payload = (await res.json()) as ApiResponse<AuthTokenPayload>
  if (!res.ok) return json(payload, res.status)
  const headers = new Headers({ 'Content-Type': 'application/json' })
  appendTokenCookies(headers, extractTokens(payload), request)
  return new Response(JSON.stringify(payload), { status: res.status, headers })
}

export async function devLoginThroughBackend(request: Request) {
  const res = await fetch(buildBffAuthTarget(request, '/api/v1/auth/dev-login'), { method: 'POST' })
  const payload = (await res.json()) as ApiResponse<AuthTokenPayload>
  if (!res.ok) return json(payload, res.status)
  const headers = new Headers({ 'Content-Type': 'application/json' })
  appendTokenCookies(headers, extractTokens(payload), request)
  return new Response(JSON.stringify(payload), { status: res.status, headers })
}

export async function authOptionsFromBackend(request: Request) {
  try {
    const res = await fetch(getAuthOptionsTarget(request), {
      method: 'GET',
      headers: { Accept: 'application/json' },
      redirect: 'manual'
    })
    const payload = await res.text()
    return new Response(payload, {
      status: res.status,
      statusText: res.statusText,
      headers: {
        'Content-Type': res.headers.get('content-type') || 'application/json'
      }
    })
  } catch {
    return json({ code: 502, message: 'backend is unavailable from frontend proxy' }, 502)
  }
}

export async function bootstrapFromGateway(request: Request) {
  if (readAppTokens(request)?.accessToken) {
    return json({ code: 0, message: 'already bootstrapped' })
  }
  const claims = readGatewayClaims(request)
  if (!claims) {
    return json({ code: 401, message: 'gateway identity missing' }, 401)
  }
  const secret = getGatewayExchangeSecret()
  if (!secret) {
    return json({ code: 503, message: 'gateway exchange is not configured' }, 503)
  }
  const res = await fetch(buildBffAuthTarget(request, '/api/v1/auth/gateway-exchange'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-AE-Frontend-Exchange-Secret': secret
    },
    body: JSON.stringify(claims)
  })
  const payload = (await res.json()) as ApiResponse<AuthTokenPayload>
  if (!res.ok) return json(payload, res.status)
  const headers = new Headers({ 'Content-Type': 'application/json' })
  appendTokenCookies(headers, extractTokens(payload), request)
  return new Response(JSON.stringify(payload), { status: 200, headers })
}

export function logoutResponse(request: Request) {
  const headers = new Headers({ 'Content-Type': 'application/json' })
  appendClearTokenCookies(headers, request)
  return new Response(JSON.stringify({ code: 0, message: 'signed out' }), { status: 200, headers })
}

export function json(payload: unknown, status = 200) {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}
