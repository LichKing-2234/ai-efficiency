import { describe, expect, test } from 'vitest'
import type { PRListSummary, PRRecord, PRSyncJob } from '@/lib/api/types'
import {
  buildPRListParams,
  canGoNextPRPage,
  canGoPreviousPRPage,
  isActivePRSyncJob,
  isTerminalPRSyncJob,
  prSyncJobMessage,
  prSyncJobProgress,
  prUsageSummary
} from './repo-detail-state'

function job(overrides: Partial<PRSyncJob>): PRSyncJob {
  return {
    id: 7,
    repo_config_id: 2,
    status: 'running',
    phase: 'fetching_prs',
    current_page: 1,
    page_size: 50,
    fetched_prs: 25,
    total_prs: 100,
    processed_prs: 10,
    created_prs: 0,
    changed_prs: 0,
    unchanged_prs: 0,
    usage_total_prs: 0,
    usage_refreshed_prs: 0,
    usage_skipped_prs: 0,
    usage_failed_prs: 0,
    last_error: null,
    ...overrides
  }
}

function pr(overrides: Partial<PRRecord>): PRRecord {
  return {
    id: 1,
    scm_pr_id: 10,
    scm_pr_url: 'https://example.com/pr/10',
    author: 'alice',
    title: 'Improve attribution',
    source_branch: 'feature/attribution',
    target_branch: 'main',
    status: 'merged',
    labels: [],
    lines_added: 12,
    lines_deleted: 3,
    ai_label: 'ai_assisted',
    ai_ratio: 0.4,
    token_cost: 0,
    attribution_status: 'clear',
    attribution_confidence: 'high',
    usage_input_tokens: 0,
    usage_output_tokens: 0,
    usage_cached_input_tokens: 0,
    usage_reasoning_tokens: 0,
    usage_credit_usage: 0,
    usage_request_count: 0,
    usage_commit_count: 0,
    usage_refreshed_at: null,
    usage_status: 'unknown',
    usage_status_reason: '',
    commit_freshness: [],
    cycle_time_hours: 4,
    merged_at: '2026-06-01T00:00:00Z',
    created_at: '2026-05-31T00:00:00Z',
    ...overrides
  }
}

describe('repo detail state helpers', () => {
  test('builds backend PR list params from page state', () => {
    expect(buildPRListParams({ page: 0, pageSize: 10, months: 3 })).toEqual({ limit: 10, offset: 0, months: 3 })
    expect(buildPRListParams({ page: 2, pageSize: 25, months: 0 })).toEqual({ limit: 25, offset: 50, months: 0 })
    expect(buildPRListParams({ page: -1, pageSize: 10, months: 6 })).toEqual({ limit: 10, offset: 0, months: 6 })
  })

  test('computes previous and next page availability from total rows', () => {
    expect(canGoPreviousPRPage(0)).toBe(false)
    expect(canGoPreviousPRPage(1)).toBe(true)
    expect(canGoNextPRPage(0, 21, 10)).toBe(true)
    expect(canGoNextPRPage(2, 21, 10)).toBe(false)
    expect(canGoNextPRPage(0, 10, 10)).toBe(false)
  })

  test('separates terminal and active PR sync jobs', () => {
    expect(isTerminalPRSyncJob(job({ status: 'completed' }))).toBe(true)
    expect(isTerminalPRSyncJob(job({ status: 'failed' }))).toBe(true)
    expect(isTerminalPRSyncJob(job({ status: 'cancelled' }))).toBe(true)
    expect(isTerminalPRSyncJob(job({ status: 'abandoned' }))).toBe(true)
    expect(isTerminalPRSyncJob(job({ status: 'running' }))).toBe(false)
    expect(isActivePRSyncJob(job({ status: 'queued' }))).toBe(true)
    expect(isActivePRSyncJob(null)).toBe(false)
  })

  test('formats sync progress and terminal messages with backend counters', () => {
    expect(prSyncJobProgress(job({ phase: 'upserting_prs', fetched_prs: 40, processed_prs: 18, usage_total_prs: 12, usage_refreshed_prs: 5 }))).toEqual({
      fetched: 40,
      processed: 18,
      usageTotal: 12,
      usageRefreshed: 5
    })
    expect(prSyncJobMessage(job({ status: 'completed', created_prs: 2, changed_prs: 3, unchanged_prs: 5 }))).toBe(
      'Sync completed: 2 created, 3 changed, 5 unchanged.'
    )
    expect(prSyncJobMessage(job({ status: 'failed', last_error: 'SCM token expired' }))).toBe('Sync failed: SCM token expired')
    expect(prSyncJobMessage(job({ status: 'running', phase: 'labeling', processed_prs: 18, total_prs: 40 }))).toBe(
      'Sync labeling: 18/40 PRs processed.'
    )
  })

  test('uses backend PR summary first and falls back to visible rows', () => {
    const summary: PRListSummary = { total: 20, with_usage: 12, pending_upload: 3, no_checkpoint: 4, refresh_failed: 1 }
    expect(prUsageSummary(summary, [])).toEqual(summary)
    expect(prUsageSummary(undefined, [
      pr({ usage_status: 'fresh', usage_input_tokens: 120 }),
      pr({ usage_status: 'pending_upload' }),
      pr({ usage_status: 'no_checkpoint' }),
      pr({ usage_status: 'refresh_failed' })
    ])).toEqual({ total: 4, with_usage: 1, pending_upload: 1, no_checkpoint: 1, refresh_failed: 1 })
  })
})
