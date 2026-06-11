import { describe, expect, test } from 'vitest'
import { readGatewayClaims } from './gateway'

describe('readGatewayClaims', () => {
  test('reads classic HCI Auth headers', () => {
    const request = new Request('http://localhost/api/auth/bootstrap', {
      headers: {
        'x-oauth-email': 'alice@example.com',
        'x-oauth-displayname': 'Alice Zhang',
        'x-iam-permissions': 'PERM_A, PERM_B '
      }
    })

    expect(readGatewayClaims(request)).toEqual({
      email: 'alice@example.com',
      username: 'Alice Zhang',
      name: 'Alice Zhang',
      displayName: 'Alice Zhang',
      subject: undefined,
      permissions: ['PERM_A', 'PERM_B']
    })
  })

  test('falls back to HCI Auth JWT payload when classic headers are absent', () => {
    const token = buildJwt({
      sub: 'user-123',
      user: {
        email: 'bob@example.com',
        displayName: 'Bob Li',
        permissions: ['PERM_C']
      }
    })
    const request = new Request('http://localhost/api/auth/bootstrap', {
      headers: {
        'x-hci-auth-jwt': token
      }
    })

    expect(readGatewayClaims(request)).toEqual({
      email: 'bob@example.com',
      username: 'Bob Li',
      name: 'Bob Li',
      displayName: 'Bob Li',
      subject: 'user-123',
      permissions: ['PERM_C']
    })
  })

  test('prefers explicit classic headers over JWT values when both are present', () => {
    const token = buildJwt({
      sub: 'jwt-user',
      user: {
        email: 'jwt@example.com',
        displayName: 'JWT User',
        permissions: ['PERM_JWT']
      }
    })
    const request = new Request('http://localhost/api/auth/bootstrap', {
      headers: {
        'x-oauth-email': 'header@example.com',
        'x-oauth-displayname': 'Header User',
        'x-iam-permissions': 'PERM_HEADER',
        'x-hci-auth-jwt': token
      }
    })

    expect(readGatewayClaims(request)).toEqual({
      email: 'header@example.com',
      username: 'Header User',
      name: 'Header User',
      displayName: 'Header User',
      subject: 'jwt-user',
      permissions: ['PERM_HEADER']
    })
  })

  test('returns null when no usable gateway identity exists', () => {
    const request = new Request('http://localhost/api/auth/bootstrap', {
      headers: {
        'x-hci-auth-jwt': 'not-a-jwt'
      }
    })

    expect(readGatewayClaims(request)).toBeNull()
  })
})

function buildJwt(payload: unknown) {
  return `header.${toBase64Url(JSON.stringify(payload))}.signature`
}

function toBase64Url(value: string) {
  return Buffer.from(value, 'utf8').toString('base64url')
}
