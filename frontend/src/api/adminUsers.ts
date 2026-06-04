import client from './client'
import type {
  AdminAssignSubscriptionRequest,
  AdminAssignSubscriptionResponse,
  AdminRelayPasswordRevealResponse,
  AdminSubscriptionOptionsResponse,
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

export function listAdminUserSubscriptionOptions() {
  return client.get<ApiResponse<AdminSubscriptionOptionsResponse>>('/admin/users/subscription-options')
}

export function assignAdminUserSubscription(id: number, data: AdminAssignSubscriptionRequest) {
  return client.post<ApiResponse<AdminAssignSubscriptionResponse>>(`/admin/users/${id}/subscriptions`, data)
}

export function revealAdminUserRelayPassword(id: number) {
  return client.post<ApiResponse<AdminRelayPasswordRevealResponse>>(`/admin/users/${id}/relay-password/reveal`)
}
