export interface GatewayClaims {
  email: string
  username?: string
  name?: string
  displayName?: string
  subject?: string
}

const EMAIL_HEADERS = ['x-hci-user-email', 'x-auth-request-email', 'x-forwarded-email', 'x-user-email']
const NAME_HEADERS = ['x-hci-user-name', 'x-auth-request-user', 'x-forwarded-user', 'x-user-name']

export function readGatewayClaims(request: Request): GatewayClaims | null {
  const headers = request.headers
  const email = firstHeader(headers, EMAIL_HEADERS)
  if (!email) return null
  const username = firstHeader(headers, NAME_HEADERS)
  return {
    email,
    username,
    name: username,
    displayName: firstHeader(headers, ['x-hci-display-name', 'x-user-display-name']),
    subject: firstHeader(headers, ['x-hci-user-id', 'x-auth-request-subject', 'x-user-id'])
  }
}

function firstHeader(headers: Headers, keys: string[]) {
  for (const key of keys) {
    const value = headers.get(key)
    if (value && value.trim()) return value.trim()
  }
  return undefined
}
