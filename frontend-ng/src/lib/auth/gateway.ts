export interface GatewayClaims {
  email: string
  username?: string
  name?: string
  displayName?: string
  subject?: string
  permissions?: string[]
}

interface HciJwtPayload {
  sub?: string
  user?: {
    email?: string
    displayName?: string
    permissions?: string[]
  }
}

const EMAIL_HEADERS = [
  'x-oauth-email',
  'x-hci-user-email',
  'x-auth-request-email',
  'x-forwarded-email',
  'x-user-email'
]
const NAME_HEADERS = [
  'x-oauth-displayname',
  'x-hci-user-name',
  'x-auth-request-user',
  'x-forwarded-user',
  'x-user-name'
]
const DISPLAY_NAME_HEADERS = ['x-oauth-displayname', 'x-hci-display-name', 'x-user-display-name']
const SUBJECT_HEADERS = ['x-hci-user-id', 'x-auth-request-subject', 'x-user-id']
const PERMISSIONS_HEADERS = ['x-iam-permissions']

export function readGatewayClaims(request: Request): GatewayClaims | null {
  const headers = request.headers
  const jwtClaims = readClaimsFromJwt(headers)
  const email = firstHeader(headers, EMAIL_HEADERS) ?? jwtClaims?.email
  if (!email) return null
  const displayName = firstHeader(headers, DISPLAY_NAME_HEADERS) ?? jwtClaims?.displayName
  const username = firstHeader(headers, NAME_HEADERS) ?? displayName
  const permissions = readPermissions(headers) ?? jwtClaims?.permissions
  return {
    email,
    username,
    name: username,
    displayName,
    subject: firstHeader(headers, SUBJECT_HEADERS) ?? jwtClaims?.subject,
    permissions
  }
}

function firstHeader(headers: Headers, keys: string[]) {
  for (const key of keys) {
    const value = headers.get(key)
    if (value && value.trim()) return value.trim()
  }
  return undefined
}

function readPermissions(headers: Headers) {
  const raw = firstHeader(headers, PERMISSIONS_HEADERS)
  if (!raw) return undefined
  const permissions = raw
    .split(',')
    .map((permission) => permission.trim())
    .filter(Boolean)
  return permissions.length > 0 ? permissions : undefined
}

function readClaimsFromJwt(headers: Headers) {
  const token = headers.get('x-hci-auth-jwt')?.trim()
  if (!token) return null
  const payload = decodeJwtPayload(token)
  if (!payload?.user?.email) return null
  return {
    email: payload.user.email.trim(),
    displayName: payload.user.displayName?.trim(),
    subject: payload.sub?.trim(),
    permissions: payload.user.permissions?.filter(Boolean)
  }
}

function decodeJwtPayload(token: string): HciJwtPayload | null {
  const segments = token.split('.')
  if (segments.length < 2) return null
  try {
    const base64 = segments[1].replace(/-/g, '+').replace(/_/g, '/')
    const normalized = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), '=')
    return JSON.parse(atob(normalized)) as HciJwtPayload
  } catch {
    return null
  }
}
