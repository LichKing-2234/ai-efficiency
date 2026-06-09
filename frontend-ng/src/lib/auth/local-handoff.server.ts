import { json } from '@/lib/api/server'
import { appendBackendUrlCookie, appendTokenCookies, readAppTokens } from '@/lib/auth/cookies'
import { buildLocalCallbackUrl, isAllowedBackendUrl, isAllowedLocalTarget } from '@/lib/auth/local-handoff'

export function localHandoffIssueResponse(request: Request, callbackPath = '/api/local/callback') {
  const url = new URL(request.url)
  const target = url.searchParams.get('target') || url.searchParams.get('local') || 'http://localhost:3000'
  if (!isAllowedLocalTarget(target)) {
    return json({ code: 400, message: 'local target must be localhost' }, 400)
  }
  const tokens = readAppTokens(request)
  if (!tokens?.accessToken && !tokens?.refreshToken) {
    return json({ code: 401, message: 'local handoff requires an active app session' }, 401)
  }
  const backendUrl = process.env.AE_FRONTEND_LOCAL_BACKEND_URL ||
    process.env.AE_FRONTEND_PUBLIC_BACKEND_URL ||
    process.env.AE_FRONTEND_BACKEND_URL ||
    process.env.VITE_BACKEND_URL ||
    import.meta.env.VITE_BACKEND_URL
  return Response.redirect(buildLocalCallbackUrl(target, tokens.accessToken, tokens.refreshToken, callbackPath, backendUrl), 302)
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

export function oauth2CallbackResponse(request: Request) {
  const url = new URL(request.url)
  const localTarget = url.searchParams.get('local') || url.searchParams.get('target')
  if (localTarget) {
    if (!isAllowedLocalTarget(localTarget)) {
      return json({ code: 400, message: 'local target must be localhost' }, 400)
    }
    return Response.redirect(new URL('/', localTarget), 302)
  }
  return Response.redirect(new URL('/', url.origin), 302)
}
