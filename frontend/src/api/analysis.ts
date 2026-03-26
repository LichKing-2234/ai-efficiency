import client from './client'
import type { ApiResponse, ScanResult } from '@/types'

export interface OptimizePreviewFile {
  path: string
  old_content: string
  new_content: string
  is_new: boolean
}

export interface OptimizePreview {
  files: OptimizePreviewFile[]
  score: number
  message?: string
}

export interface OptimizeResult {
  branch_name: string
  pr_url: string
  pr_id: number
  files_added: number
}

// Long-running operations need extended timeout
const LONG_TIMEOUT = 120000

export function triggerScan(repoId: number) {
  return client.post<ApiResponse<ScanResult>>(`/repos/${repoId}/scan`, null, { timeout: LONG_TIMEOUT })
}

export function listScans(repoId: number, limit = 20) {
  return client.get<ApiResponse<ScanResult[]>>(`/repos/${repoId}/scans`, { params: { limit } })
}

export function getLatestScan(repoId: number) {
  return client.get<ApiResponse<ScanResult>>(`/repos/${repoId}/scans/latest`)
}

export function triggerOptimize(repoId: number) {
  return client.post<ApiResponse<OptimizeResult>>(`/repos/${repoId}/optimize`, null, { timeout: LONG_TIMEOUT })
}

export function optimizePreview(repoId: number) {
  return client.post<ApiResponse<OptimizePreview>>(`/repos/${repoId}/optimize/preview`, null, { timeout: LONG_TIMEOUT })
}

export function optimizeConfirm(repoId: number, files: Record<string, string>, score: number) {
  return client.post<ApiResponse<OptimizeResult>>(`/repos/${repoId}/optimize/confirm`, { files, score }, { timeout: LONG_TIMEOUT })
}
