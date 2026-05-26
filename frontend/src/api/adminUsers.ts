import client from './client'
import type {
  AdminRelayPasswordRevealResponse,
  AdminUsersListResponse,
  ApiResponse,
} from '@/types'

export interface AdminUsersListParams {
  q?: string
  page?: number
  page_size?: number
}

export function listAdminUsers(params: AdminUsersListParams) {
  return client.get<ApiResponse<AdminUsersListResponse>>('/admin/users', { params })
}

export function revealAdminUserRelayPassword(id: number) {
  return client.post<ApiResponse<AdminRelayPasswordRevealResponse>>(`/admin/users/${id}/relay-password/reveal`)
}
