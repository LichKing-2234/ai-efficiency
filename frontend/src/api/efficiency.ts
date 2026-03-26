import client from './client'
import type { ApiResponse, DashboardData, EfficiencyMetric } from '@/types'

export function getDashboard() {
  return client.get<ApiResponse<DashboardData>>('/efficiency/dashboard')
}

export function getRepoMetrics(repoId: number, period = 'daily') {
  return client.get<ApiResponse<EfficiencyMetric[]>>(`/efficiency/repos/${repoId}/metrics`, { params: { period } })
}

export function getRepoTrend(repoId: number, period = 'weekly', limit = 12) {
  return client.get<ApiResponse<EfficiencyMetric[]>>(`/efficiency/repos/${repoId}/trend`, { params: { period, limit } })
}
