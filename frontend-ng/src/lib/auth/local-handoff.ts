export function isAllowedLocalTarget(value: string | null) {
  if (!value) return false
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  } catch {
    return false
  }
}

export function buildLocalCallbackUrl(target: string, accessToken?: string, refreshToken?: string) {
  const callback = new URL('/api/local/callback', target)
  if (accessToken) callback.searchParams.set('access_token', accessToken)
  if (refreshToken) callback.searchParams.set('refresh_token', refreshToken)
  return callback
}
