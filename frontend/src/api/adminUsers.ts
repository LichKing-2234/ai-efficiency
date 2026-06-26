import client from './client'
import type {
  AdminAssignSubscriptionRequest,
  AdminAssignSubscriptionResponse,
  AdminManageSubscriptionsRequest,
  AdminManageSubscriptionsResponse,
  AdminUserDepartmentsResponse,
  AdminRelayPasswordRevealResponse,
  AdminSubscriptionJob,
  AdminSubscriptionOptionsResponse,
  AdminUsersListResponse,
  ApiResponse,
} from '@/types'

export interface AdminUsersListParams {
  q?: string
  department_id?: string
  page?: number
  page_size?: number
}

export function listAdminUsers(params: AdminUsersListParams) {
  return client.get<ApiResponse<AdminUsersListResponse>>('/admin/users', { params })
}

export function listAdminUserDepartments() {
  return client.get<ApiResponse<AdminUserDepartmentsResponse>>('/admin/users/departments')
}

export function listAdminUserSubscriptionOptions() {
  return client.get<ApiResponse<AdminSubscriptionOptionsResponse>>('/admin/users/subscription-options')
}

export function assignAdminUserSubscription(id: number, data: AdminAssignSubscriptionRequest) {
  return client.post<ApiResponse<AdminAssignSubscriptionResponse>>(`/admin/users/${id}/subscriptions`, data)
}

export function manageAdminUserSubscriptions(data: AdminManageSubscriptionsRequest) {
  return client.post<ApiResponse<AdminManageSubscriptionsResponse>>('/admin/users/subscriptions/batch', data)
}

export function startAdminUserSubscriptionJob(data: AdminManageSubscriptionsRequest) {
  return client.post<ApiResponse<AdminSubscriptionJob>>('/admin/users/subscription-jobs', data)
}

export function getAdminUserSubscriptionJob(id: number) {
  return client.get<ApiResponse<AdminSubscriptionJob>>(`/admin/users/subscription-jobs/${id}`)
}

export function getLatestAdminUserSubscriptionJob() {
  return client.get<ApiResponse<AdminSubscriptionJob | null>>('/admin/users/subscription-jobs/latest')
}

export function revealAdminUserRelayPassword(id: number) {
  return client.post<ApiResponse<AdminRelayPasswordRevealResponse>>(`/admin/users/${id}/relay-password/reveal`)
}
