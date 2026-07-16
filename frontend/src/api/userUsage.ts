import client from './client'
import type {
  ApiResponse,
  UserUsageDashboardParams,
  UserUsageDashboardSnapshot,
  UserUsageGroupQuotaResponse,
} from '@/types'

export function getUserUsageDashboard(params: UserUsageDashboardParams, signal?: AbortSignal) {
  return client.get<ApiResponse<UserUsageDashboardSnapshot>>('/user/usage/dashboard', {
    params: { ...params, include_group_quotas: false },
    signal,
  })
}

export function getUserUsageGroupQuotas(params: UserUsageDashboardParams, signal?: AbortSignal) {
  return client.get<ApiResponse<UserUsageGroupQuotaResponse>>('/user/usage/group-quotas', {
    params,
    signal,
  })
}
