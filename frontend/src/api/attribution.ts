import client from './client'
import type { ApiResponse, AttributionReport } from '@/types'

export interface AttributionReportParams {
  from?: string
  to?: string
  user_id?: number
}

export function normalizeAttributionReport(report: AttributionReport): AttributionReport {
  return {
    ...report,
    repositories: (report.repositories ?? []).map((repository) => ({
      ...repository,
      worktrees: repository.worktrees ?? [],
      branches: repository.branches ?? [],
      commits: (repository.commits ?? []).map((commit) => ({
        ...commit,
        inherited_from_commit_shas: commit.inherited_from_commit_shas ?? [],
        prs: commit.prs ?? [],
      })),
    })),
    buckets: report.buckets ?? [],
  }
}

export function getAttributionReport(params?: AttributionReportParams) {
  return client.get<ApiResponse<AttributionReport>>('/attribution/report', { params })
}
