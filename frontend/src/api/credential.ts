import client from './client'
import type { ApiResponse, Credential } from '@/types'

export function listCredentials() {
  return client.get<ApiResponse<Credential[]>>('/admin/credentials')
}

export function createCredential(data: Record<string, unknown>) {
  return client.post<ApiResponse<Credential>>('/admin/credentials', data)
}

export function updateCredential(id: number, data: Record<string, unknown>) {
  return client.put<ApiResponse<Credential>>(`/admin/credentials/${id}`, data)
}

export function deleteCredential(id: number) {
  return client.delete<ApiResponse<null>>(`/admin/credentials/${id}`)
}
