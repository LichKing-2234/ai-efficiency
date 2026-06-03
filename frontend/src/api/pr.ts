import client from './client'
import type { ApiResponse, PRListSummary, PRRecord, PRSyncJob } from '@/types'

export function listPRs(repoId: number, params?: { status?: string; limit?: number; offset?: number; months?: number }) {
  return client.get<ApiResponse<{ items: PRRecord[]; total: number; summary?: PRListSummary }>>(`/repos/${repoId}/prs`, { params })
}

export function getPR(prId: number) {
  return client.get<ApiResponse<PRRecord>>(`/prs/${prId}`)
}

export function syncPRs(repoId: number) {
  return client.post<ApiResponse<{ job_id: number; status: string; phase: string; reused?: boolean }>>(`/repos/${repoId}/sync-prs`)
}

export function getPRSyncJob(jobId: number) {
  return client.get<ApiResponse<PRSyncJob>>(`/pr-sync-jobs/${jobId}`)
}

export function getLatestPRSyncJob(repoId: number) {
  return client.get<ApiResponse<PRSyncJob | null>>(`/repos/${repoId}/pr-sync-job/latest`)
}

export function settlePR(prId: number) {
  return client.post<ApiResponse<{ attribution_status: string }>>(`/prs/${prId}/settle`)
}

export function refreshPRUsage(prId: number) {
  return client.post<ApiResponse<PRRecord>>(`/prs/${prId}/refresh-usage`)
}
