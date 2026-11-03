import client from './client'
import type { ApiResponse } from '@/types'

export interface LLMConfig {
  relay_url?: string
  relay_admin_api_key?: string
  enabled?: boolean
  model: string
}

export interface LLMTestResult {
  success: boolean
  message: string
  response?: string
}

export interface LLMTestRequest {
  prompt?: string
}

export function getLLMConfig() {
  return client.get<ApiResponse<LLMConfig>>('/settings/llm')
}

export function updateLLMConfig(data: Partial<LLMConfig>) {
  return client.put<ApiResponse<LLMConfig>>('/settings/llm', data)
}

export function testLLMConnection(data?: LLMTestRequest) {
  return client.post<ApiResponse<LLMTestResult>>('/settings/llm/test', data)
}

export interface OIDCSettings {
  enabled: boolean
  label: string
  issuer: string
  client_id: string
  client_secret: string
  redirect_urls: string[]
  allow_email_link: boolean
  scopes: string[]
  source: 'environment' | 'database'
  callback_path: string
}

export type OIDCSettingsUpdate = Omit<OIDCSettings, 'source' | 'callback_path'>

export interface OIDCDiscovery {
  issuer: string
  authorization_endpoint: string
  token_endpoint: string
  userinfo_endpoint: string
  jwks_uri: string
}

export function getOIDCSettings() {
  return client.get<ApiResponse<OIDCSettings>>('/admin/settings/oidc')
}

export function updateOIDCSettings(data: OIDCSettingsUpdate) {
  return client.put<ApiResponse<OIDCSettings>>('/admin/settings/oidc', data)
}

export function resetOIDCSettings() {
  return client.delete<ApiResponse<OIDCSettings>>('/admin/settings/oidc')
}

export function testOIDCSettings(data: OIDCSettingsUpdate) {
  return client.post<ApiResponse<OIDCDiscovery>>('/admin/settings/oidc/test', data)
}
