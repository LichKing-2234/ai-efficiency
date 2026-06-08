import type { ApiResponse, AuthTokenPayload } from '@/lib/api/types'

function hasEnvelope(payload: ApiResponse<AuthTokenPayload> | AuthTokenPayload): payload is ApiResponse<AuthTokenPayload> {
  return typeof payload === 'object' && payload !== null && 'data' in payload
}

export function extractTokens(payload: ApiResponse<AuthTokenPayload> | AuthTokenPayload | undefined | null) {
  const data = payload && hasEnvelope(payload) ? payload.data : payload
  const accessToken = data?.tokens?.access_token || data?.token
  const refreshToken = data?.tokens?.refresh_token || data?.refresh_token
  if (!accessToken) {
    throw new Error('auth response missing access token')
  }
  return { accessToken, refreshToken }
}
