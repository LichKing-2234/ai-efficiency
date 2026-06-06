import client from './client'
import type {
  ApiResponse,
  UserUsageStats,
  UsageTrendResponse,
  UsageModelResponse,
} from '@/types'

export function getUserUsageStats() {
  return client.get<ApiResponse<UserUsageStats | null>>('/user/usage/stats')
}

export function getUserUsageTrend(params: {
  start_date?: string
  end_date?: string
  granularity?: 'day' | 'hour'
  timezone?: string
}) {
  return client.get<ApiResponse<UsageTrendResponse>>('/user/usage/trend', { params })
}

export function getUserUsageModels(params: {
  start_date?: string
  end_date?: string
  timezone?: string
}) {
  return client.get<ApiResponse<UsageModelResponse>>('/user/usage/models', { params })
}
