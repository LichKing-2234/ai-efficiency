import client from './client'
import type { ApiResponse, RelayProvider } from '@/types'

export interface RelayProviderPayload {
  name: string
  display_name: string
  base_url: string
  admin_url: string
  admin_api_key: string
  is_primary: boolean
  enabled: boolean
}

export interface RelayProviderUpdatePayload {
  display_name?: string
  base_url?: string
  admin_url?: string
  admin_api_key?: string
  is_primary?: boolean
  enabled?: boolean
}

export interface RelayProviderTestRequest {
  platform: string
  model: string
  prompt?: string
}

export interface RelayProviderTestResult {
  success: boolean
  message: string
  response?: string
}

export function listRelayProviders() {
  return client.get<ApiResponse<RelayProvider[]>>('/admin/providers')
}

export function createRelayProvider(data: RelayProviderPayload) {
  return client.post<ApiResponse<RelayProvider>>('/admin/providers', data)
}

export function updateRelayProvider(id: number, data: RelayProviderUpdatePayload) {
  return client.put<ApiResponse<RelayProvider>>(`/admin/providers/${id}`, data)
}

export function deleteRelayProvider(id: number) {
  return client.delete<ApiResponse<{ message: string }>>(`/admin/providers/${id}`)
}

export function testRelayProvider(id: number, data?: RelayProviderTestRequest) {
  return client.post<ApiResponse<RelayProviderTestResult>>(`/admin/providers/${id}/test`, data)
}
