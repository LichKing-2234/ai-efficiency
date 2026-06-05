import { describe, expect, test } from 'vitest'
import {
  buildLoginRedirect,
  buildOAuthAuthorizePayload,
  normalizeDeviceCode,
  safeRedirect,
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
