import client from './client'
import type { ApiResponse, LoginRequest, User } from '@/types'

export function login(req: LoginRequest) {
  return client.post<ApiResponse<{ token: string; refresh_token: string }>>('/auth/login', req)
}

export function devLogin() {
  return client.post<ApiResponse<{ token: string; refresh_token: string }>>('/auth/dev-login')
}

export function refresh(token: string) {
  return client.post<ApiResponse<{ token: string }>>('/auth/refresh', { refresh_token: token })
}

export function getMe() {
  return client.get<ApiResponse<User>>('/auth/me')
}
