import client from './client'
import type {
  ApiResponse,
  UserUsageDashboardParams,
  UserUsageDashboardSnapshot,
} from '@/types'

export function getUserUsageDashboard(params: UserUsageDashboardParams) {
  return client.get<ApiResponse<UserUsageDashboardSnapshot>>('/user/usage/dashboard', { params })
}
