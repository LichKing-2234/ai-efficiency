import { appendClearTokenCookies, appendTokenCookies, readAppTokens } from '@/lib/auth/cookies'
import { readGatewayClaims } from '@/lib/auth/gateway'
import { extractTokens } from '@/lib/auth/tokens'
import type { ApiResponse, AuthTokenPayload } from '@/lib/api/types'

const DEFAULT_BACKEND_URL = 'http://localhost:8081'

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

export function getBackendUrl() {
  return (
    process.env.AE_FRONTEND_BACKEND_URL ||
    process.env.VITE_BACKEND_URL ||
    import.meta.env.VITE_BACKEND_URL ||
    DEFAULT_BACKEND_URL
  ).replace(/\/$/, '')
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
  if (pendingTokens && first.status !== 401) {
    const headers = new Headers(first.headers)
    appendTokenCookies(headers, pendingTokens, request)
    return new Response(first.body, { status: first.status, statusText: first.statusText, headers })
  }
  if (first.status !== 401 || path.includes('/auth/refresh')) {
    return first
  }
  if (!tokens?.refreshToken) {
    return first
  }
  const refreshed = await refreshTokens(request, tokens.refreshToken)
  if (!refreshed) {
    const headers = new Headers(first.headers)
    appendClearTokenCookies(headers, request)
    return new Response(first.body, { status: first.status, headers })
  }
  const retry = await forwardToBackend(request, path, refreshed.accessToken)
  const headers = new Headers(retry.headers)
  appendTokenCookies(headers, refreshed, request)
  return new Response(retry.body, { status: retry.status, statusText: retry.statusText, headers })
}

async function forwardToBackend(request: Request, path: string, accessToken?: string) {
  const sourceUrl = new URL(request.url)
  const target = `${getBackendUrl()}${path}${sourceUrl.search}`
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

async function refreshTokens(request: Request, refreshToken: string) {
  const res = await fetch(`${getBackendUrl()}/api/v1/auth/refresh`, {
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
  const res = await fetch(`${getBackendUrl()}/api/v1/auth/login`, {
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
  const res = await fetch(`${getBackendUrl()}/api/v1/auth/dev-login`, { method: 'POST' })
  const payload = (await res.json()) as ApiResponse<AuthTokenPayload>
  if (!res.ok) return json(payload, res.status)
  const headers = new Headers({ 'Content-Type': 'application/json' })
  appendTokenCookies(headers, extractTokens(payload), request)
  return new Response(JSON.stringify(payload), { status: res.status, headers })
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
  const res = await fetch(`${getBackendUrl()}/api/v1/auth/gateway-exchange`, {
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
