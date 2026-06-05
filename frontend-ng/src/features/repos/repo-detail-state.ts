import type { CommitFreshness, PRCommitUsageSnapshot, PRListSummary, PRRecord, PRSyncJob } from '@/lib/api/types'

const terminalPRSyncStatuses = new Set<PRSyncJob['status']>(['completed', 'failed', 'cancelled', 'abandoned'])

export interface PRListPageState {
  page: number
  pageSize: number
  months: number
}

export interface PRSyncJobProgress {
  fetched: number
  processed: number
  usageTotal: number
  usageRefreshed: number
}

export function buildPRListParams({ page, pageSize, months }: PRListPageState) {
  return {
    limit: pageSize,
    offset: Math.max(0, page) * pageSize,
    months
  }
}

export function canGoPreviousPRPage(page: number) {
  return page > 0
}

export function canGoNextPRPage(page: number, total: number, pageSize: number) {
  return (page + 1) * pageSize < total
}

export function isTerminalPRSyncJob(job?: PRSyncJob | null) {
  return job ? terminalPRSyncStatuses.has(job.status) : false
}

export function isActivePRSyncJob(job?: PRSyncJob | null) {
  return job ? !isTerminalPRSyncJob(job) : false
}

export function prSyncJobProgress(job: PRSyncJob): PRSyncJobProgress {
  return {
    fetched: job.fetched_prs,
    processed: job.processed_prs,
    usageTotal: job.usage_total_prs,
    usageRefreshed: job.usage_refreshed_prs
  }
}

export function prSyncJobMessage(job: PRSyncJob) {
  if (job.status === 'completed') {
    return `Sync completed: ${job.created_prs} created, ${job.changed_prs} changed, ${job.unchanged_prs} unchanged.`
  }
  if (job.status === 'failed') {
    return `Sync failed: ${job.last_error || 'unknown error'}`
  }
  if (job.status === 'cancelled' || job.status === 'abandoned') {
    return `Sync ${job.status}.`
  }
  const total = job.total_prs || job.fetched_prs
  return `Sync ${job.phase}: ${job.processed_prs}/${total} PRs processed.`
}

export function prUsageSummary(summary: PRListSummary | undefined, rows: PRRecord[]): PRListSummary {
  if (summary) return summary

  return rows.reduce<PRListSummary>(
    (next, row) => {
      next.total += 1
      if ((row.usage_input_tokens ?? 0) + (row.usage_output_tokens ?? 0) + (row.usage_cached_input_tokens ?? 0) > 0) {
        next.with_usage += 1
      }
      if (row.usage_status === 'pending_upload') next.pending_upload += 1
      if (row.usage_status === 'no_checkpoint') next.no_checkpoint += 1
      if (row.usage_status === 'refresh_failed') next.refresh_failed += 1
      return next
    },
    { total: 0, with_usage: 0, pending_upload: 0, no_checkpoint: 0, refresh_failed: 0 }
  )
}

export function commitSnapshots(pr: PRRecord): PRCommitUsageSnapshot[] {
  const snapshots = pr.edges?.pr_commit_usage_snapshots
  return Array.isArray(snapshots) ? [...snapshots].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0)) : []
}

export function commitFreshnessFor(pr: PRRecord, commitSha: string): CommitFreshness | undefined {
  return pr.commit_freshness?.find((item) => item.commit_sha === commitSha)
}

export function hasUsageSnapshot(pr: PRRecord) {
  return commitSnapshots(pr).length > 0
}

export function usageSummaryNeedsRefresh(pr: PRRecord) {
  return !hasUsageSnapshot(pr) && !pr.usage_refreshed_at
}
