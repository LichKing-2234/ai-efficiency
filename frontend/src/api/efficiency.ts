import client from './client'
import type { ApiResponse, DashboardData } from '@/types'

export function getDashboard() {
  return client.get<ApiResponse<DashboardData>>('/efficiency/dashboard')
}
