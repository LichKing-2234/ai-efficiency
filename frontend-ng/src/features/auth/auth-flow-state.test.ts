import { describe, expect, test } from 'vitest'
import {
  buildLoginRedirect,
  buildCurrentRouteRedirectPath,
  buildPublicOAuthAuthQueryOptions,
  buildOAuthAuthorizePayload,
  normalizeDeviceCode,
  safeRedirect,
  shouldNavigateToLoginRedirect,
  selectInitialLoginSource
} from './auth-flow-state'

describe('auth flow state helpers', () => {
  test('keeps OAuth callback query when redirecting unauthenticated users to login', () => {
    expect(buildLoginRedirect('/oauth/authorize?client_id=ae-cli&redirect_uri=http%3A%2F%2F127.0.0.1%2Fcallback')).toEqual({
      to: '/login',
      search: { redirect: '/oauth/authorize?client_id=ae-cli&redirect_uri=http%3A%2F%2F127.0.0.1%2Fcallback' }
    })
    expect(buildLoginRedirect('/oauth/device')).toEqual({
      to: '/login',
      search: { redirect: '/oauth/device' }
    })
    expect(buildLoginRedirect('/login?redirect=%2Foauth%2Fdevice')).toEqual({
      to: '/login',
      search: { redirect: '/' }
    })
  })

  test('builds a safe route-local redirect path from router location parts', () => {
    expect(buildCurrentRouteRedirectPath('/oauth/device', '')).toBe('/oauth/device')
    expect(buildCurrentRouteRedirectPath('/oauth/authorize', '?client_id=ae-cli')).toBe('/oauth/authorize?client_id=ae-cli')
  })

  test('does not repeat login redirects once already at the target redirect', () => {
    const redirect = buildLoginRedirect('/oauth/device')

    expect(shouldNavigateToLoginRedirect({
      currentPath: '/oauth/device',
      redirect
    })).toBe(true)

    expect(shouldNavigateToLoginRedirect({
      currentPath: '/login',
      redirect
    })).toBe(false)
  })

  test('configures public OAuth auth queries without retry loops', () => {
    expect(buildPublicOAuthAuthQueryOptions('oauth-device')).toEqual({
      queryKey: ['auth', 'me', 'oauth-device'],
      retry: false,
      refetchOnWindowFocus: false
    })
  })

  test('builds OAuth approve payload from current search params', () => {
    expect(buildOAuthAuthorizePayload({
      client_id: 'ae-cli',
      redirect_uri: 'http://127.0.0.1:9876/callback',
      code_challenge: 'challenge',
      code_challenge_method: 'S256',
      state: 'opaque'
    }, true)).toEqual({
      client_id: 'ae-cli',
      redirect_uri: 'http://127.0.0.1:9876/callback',
      code_challenge: 'challenge',
      code_challenge_method: 'S256',
      state: 'opaque',
      approved: true
    })
  })

  test('normalizes device codes for backend verification', () => {
    expect(normalizeDeviceCode(' abcd-efgh ')).toBe('ABCD-EFGH')
    expect(normalizeDeviceCode('abcd efgh')).toBe('ABCDEFGH')
  })

  test('chooses the current backend login source casing and safe redirects', () => {
    expect(selectInitialLoginSource({ ldap_enabled: true, dev_login_enabled: false })).toBe('LDAP')
    expect(selectInitialLoginSource({ ldap_enabled: false, dev_login_enabled: false })).toBe('SSO')
    expect(safeRedirect('/repos?binding=unbound')).toBe('/repos?binding=unbound')
    expect(safeRedirect('//evil.example.com')).toBe('/')
    expect(safeRedirect('/login?redirect=/settings')).toBe('/')
  })
})
