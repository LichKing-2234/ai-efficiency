import client from './client'
import type { ApiResponse, LoginRequest, User } from '@/types'

export interface AuthTokenPayload {
  token?: string
  refresh_token?: string
  tokens?: {
    access_token?: string
    refresh_token?: string
  }
}

export function login(req: LoginRequest) {
  return client.post<ApiResponse<AuthTokenPayload>>('/auth/login', req)
}

export function devLogin() {
  return client.post<ApiResponse<AuthTokenPayload>>('/auth/dev-login')
}

export function refresh(token: string) {
  return client.post<ApiResponse<AuthTokenPayload>>('/auth/refresh', { refresh_token: token })
}

export function getMe() {
  return client.get<ApiResponse<User>>('/auth/me')
}
