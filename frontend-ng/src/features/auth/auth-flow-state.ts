import type { AuthOptions } from '@/lib/api/types'

export interface OAuthAuthorizeSearch {
  client_id?: string
  redirect_uri?: string
  code_challenge?: string
  code_challenge_method?: string
  state?: string
}

export function safeRedirect(raw?: string) {
  if (!raw || !raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/login')) return '/'
  return raw
}

export function selectInitialLoginSource(options?: Pick<AuthOptions, 'ldap_enabled' | 'dev_login_enabled'> | null) {
  return options?.ldap_enabled ? 'LDAP' : 'SSO'
}

export function buildLoginRedirect(currentPath: string) {
  return {
    to: '/login',
    search: { redirect: currentPath || '/' }
  }
}

export function buildOAuthAuthorizePayload(search: OAuthAuthorizeSearch, approved: boolean) {
  return {
    client_id: search.client_id || '',
    redirect_uri: search.redirect_uri || '',
    code_challenge: search.code_challenge || '',
    code_challenge_method: search.code_challenge_method || '',
    state: search.state || '',
    approved
  }
}

export function normalizeDeviceCode(raw: string) {
  return raw.trim().replace(/\s+/g, '').toUpperCase()
}
