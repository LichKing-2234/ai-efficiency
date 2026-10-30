import client from './client'
import axios from 'axios'
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
  hci_enabled?: boolean
  ldap_enabled: boolean
  dev_login_enabled: boolean
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

export function hciBootstrap() {
	return axios.post<ApiResponse<AuthTokenPayload>>('/auth/hci/bootstrap', {}, { timeout: 15000, withCredentials: true })
}
