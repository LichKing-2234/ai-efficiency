import { bootstrapFromGateway, json } from '@/lib/api/server'
import { appendBackendUrlCookie, appendTokenCookies, readAppTokens } from '@/lib/auth/cookies'
import { buildLocalCallbackUrl, isAllowedBackendUrl, isAllowedLocalTarget } from '@/lib/auth/local-handoff'
import { extractTokens } from '@/lib/auth/tokens'

export function getLocalHandoffProxyTarget(request: Request) {
  const origin = new URL(request.url).origin
  if (!isAllowedBackendUrl(origin)) return null
  return origin.replace(/\/$/, '')
}

async function resolveLocalHandoffTokens(request: Request) {
  const existingTokens = readAppTokens(request)
  if (existingTokens?.accessToken || existingTokens?.refreshToken) {
    return existingTokens
  }

  const bootstrapResponse = await bootstrapFromGateway(request)
  if (!bootstrapResponse.ok) {
    return null
  }

  const payload = await bootstrapResponse.json().catch(() => null)
  try {
    return extractTokens(payload)
  } catch {
    return null
  }
}

export async function localHandoffIssueResponse(request: Request, callbackPath = '/api/local/callback') {
  const url = new URL(request.url)
  const target = url.searchParams.get('target') || url.searchParams.get('local') || 'http://localhost:3000'
  if (!isAllowedLocalTarget(target)) {
    return json({ code: 400, message: 'local target must be localhost' }, 400)
  }
  const tokens = await resolveLocalHandoffTokens(request)
  if (!tokens?.accessToken && !tokens?.refreshToken) {
    return json({ code: 401, message: 'local handoff requires an active app session' }, 401)
  }
  const proxyTarget = getLocalHandoffProxyTarget(request)
  if (!proxyTarget) {
    return json({ code: 503, message: 'local handoff proxy target is not configured' }, 503)
  }
  return Response.redirect(buildLocalCallbackUrl(target, tokens.accessToken, tokens.refreshToken, callbackPath, proxyTarget), 302)
}

export function localHandoffCallbackResponse(request: Request) {
  const url = new URL(request.url)
  const accessToken = url.searchParams.get('access_token')
  const refreshToken = url.searchParams.get('refresh_token') || undefined
  const backendUrl = url.searchParams.get('backend_url') || undefined
  if (!accessToken && !refreshToken) {
    return json({ code: 400, message: 'app token is required' }, 400)
  }
  const headers = new Headers({ Location: '/' })
  appendTokenCookies(headers, {
    accessToken: accessToken || undefined,
    refreshToken
  }, request)
  if (backendUrl) {
    if (!isAllowedBackendUrl(backendUrl)) {
      return json({ code: 400, message: 'backend URL must be http or https' }, 400)
    }
    appendBackendUrlCookie(headers, backendUrl.replace(/\/$/, ''), request)
  }
  return new Response(null, { status: 302, headers })
}

export function oauth2LocalHandoffResponse(request: Request) {
  const url = new URL(request.url)
  const hasTokenParams = url.searchParams.has('access_token') || url.searchParams.has('refresh_token')
  if (hasTokenParams) {
    return localHandoffCallbackResponse(request)
  }
  return localHandoffIssueResponse(request, '/oauth2/local')
}

async function redirectWithGatewayBootstrap(request: Request, destination: URL) {
  const bootstrapResponse = await bootstrapFromGateway(request)
  const headers = new Headers({ Location: destination.toString() })
  for (const value of bootstrapResponse.headers.getSetCookie()) {
    headers.append('Set-Cookie', value)
  }
  return new Response(null, { status: 302, headers })
}

export async function oauth2CallbackResponse(request: Request) {
  const url = new URL(request.url)
  const localTarget = url.searchParams.get('local') || url.searchParams.get('target')
  if (localTarget) {
    if (!isAllowedLocalTarget(localTarget)) {
      return json({ code: 400, message: 'local target must be localhost' }, 400)
    }
    return redirectWithGatewayBootstrap(request, new URL('/', localTarget))
  }
  return redirectWithGatewayBootstrap(request, new URL('/', url.origin))
}
