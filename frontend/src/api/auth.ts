import axios from 'axios'
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

export interface AuthOptions {
  ldap_enabled: boolean
  dev_login_enabled: boolean
  oidc_enabled: boolean
}

export function login(req: LoginRequest) {
  return client.post<ApiResponse<AuthTokenPayload>>('/auth/login', req)
}

export function getAuthOptions() {
  return client.get<ApiResponse<AuthOptions>>('/auth/options')
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

export function completeOIDC() {
  // The handoff cookie is scoped to the browser callback route, which is
  // served outside the /api/v1 application API prefix.
  return axios.get<ApiResponse<AuthTokenPayload & { redirect?: string }>>('/auth/oidc/complete', {
    timeout: 45000,
  })
}
