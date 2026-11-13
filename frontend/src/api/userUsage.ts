import client from './client'
import type {
  ApiResponse,
  UserUsageDashboardParams,
  UserUsageDashboardSnapshot,
  UserUsageGroupPoolUsageResponse,
  UserUsageGroupQuotaResponse,
  UserUsageKiroSnapshot,
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

export function getUserKiroUsage(params: UserUsageDashboardParams, signal?: AbortSignal) {
  return client.get<ApiResponse<UserUsageKiroSnapshot>>('/user/usage/kiro', {
    params: { start_date: params.start_date, end_date: params.end_date, timezone: params.timezone },
    signal,
  })
}

export function getUserUsageGroupPoolUsage(params: UserUsageDashboardParams, signal?: AbortSignal) {
  return client.get<ApiResponse<UserUsageGroupPoolUsageResponse>>('/user/usage/group-pool-usage', {
    params,
    signal,
  })
}
