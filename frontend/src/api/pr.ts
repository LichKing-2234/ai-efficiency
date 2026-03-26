import client from './client'
import type { ApiResponse, PRRecord } from '@/types'

export function listPRs(repoId: number, params?: { status?: string; limit?: number; offset?: number; months?: number }) {
  return client.get<ApiResponse<{ items: PRRecord[]; total: number }>>(`/repos/${repoId}/prs`, { params })
}

export function getPR(prId: number) {
  return client.get<ApiResponse<PRRecord>>(`/prs/${prId}`)
}

export function syncPRs(repoId: number) {
  return client.post<ApiResponse<{ created: number; updated: number; total: number }>>(`/repos/${repoId}/sync-prs`)
}
