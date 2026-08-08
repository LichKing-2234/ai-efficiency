export interface ActivityWindow {
  from: string
  to: string
}

export interface ActivityCountMetric {
  value: number
  lower_bound: boolean
}

export interface ActivityMetrics {
  participating_prs: ActivityCountMetric
  merged_prs: ActivityCountMetric
  active_repositories: number
  commit_count: number
  latest_activity?: string
}

export interface ActivityQuality {
  measured_buckets: number
  unbound_buckets: number
  multi_repo_shared_buckets: number
  invalid_token_facts: number
  historical_advisory_facts: number
  coverage_gap_count: number
}

export interface ActivitySyncCoverage {
  complete: boolean
  affected_repositories: number
  unsynced_repositories: number
  stale_repositories: number
  partially_synced_repositories: number
  failed_repositories: number
}

export interface ActivityPage<T> {
  items: T[]
  next_cursor?: string
}

export interface ActivityMemberIdentity {
  user_id: number
  directory_member_external_id?: string
  display_name: string
  email: string
  department_external_ids: string[]
}

export interface ActivityPR {
  repo_config_id: number
  repo_name: string
  pr_record_id: number
  scm_pr_id: number
  title: string
  url: string
  status: string
  merged_at?: string
  cycle_time_hours?: number
  commits: Array<{ repo_config_id: number; commit_sha: string }>
}

export interface ActivityCommit {
  repo_config_id: number
  repo_name: string
  commit_sha: string
  branch?: string
  latest_activity: string
  processed_tokens: number
  prs: Array<{ repo_config_id: number; pr_record_id: number; scm_pr_id: number }>
}

export interface ActivityBucketSummary {
  bucket_id: string
  observed_end_at: string
  processed_tokens: number
  allocation_status: string
}

export interface ActivityMemberRow {
  member: ActivityMemberIdentity
  available: boolean
  metrics: ActivityMetrics
  quality: ActivityQuality
}

export interface ActivityMemberResponse {
  contract_version: string
  window: ActivityWindow
  member: ActivityMemberIdentity
  available: boolean
  metrics: ActivityMetrics
  quality: ActivityQuality
  sync_coverage: ActivitySyncCoverage
  prs: ActivityPage<ActivityPR>
  commits: ActivityPage<ActivityCommit>
  buckets: ActivityPage<ActivityBucketSummary>
  bucket_access: boolean
}

export interface ActivityTeamIdentity {
  external_id: string
  parent_external_id?: string | null
  name: string
  display_path: string
  member_count: number
}

export interface ActivityScopeResponse {
  contract_version: string
  scope_version: string
  can_view_teams: boolean
  admin: boolean
  representative: boolean
  teams: ActivityTeamIdentity[]
}

export interface ActivityMembersResponse {
  contract_version: string
  scope_version: string
  window: ActivityWindow
  members: ActivityPage<ActivityMemberRow>
}

export interface ActivityTeamResponse {
  contract_version: string
  scope_version: string
  window: ActivityWindow
  team: ActivityTeamIdentity
  active_members: number
  metrics: ActivityMetrics
  sync_coverage: ActivitySyncCoverage
  members: ActivityPage<ActivityMemberRow>
}

export interface ActivityRepositoryResponse {
  contract_version: string
  scope_version: string
  window: ActivityWindow
  repository: { repo_config_id: number; name: string }
  participating_members: number
  metrics: ActivityMetrics
  sync_coverage: ActivitySyncCoverage
  members: ActivityPage<ActivityMemberRow>
  prs: ActivityPage<ActivityPR>
  commits: ActivityPage<ActivityCommit>
}

export interface ActivityRequestIDEvidence {
  request_id: string
  observed_at: string
  transport: string
  status_code?: number
  error_category?: string
  failed: boolean
}

export interface ActivityBucketDetail {
  contract_version: string
  bucket_id: string
  owner_user_id: number
  tool: string
  model: string
  observed_start_at: string
  observed_end_at: string
  tokens: {
    fresh_input_tokens: number
    cache_read_tokens: number
    cache_write_tokens: number
    output_tokens: number
    reasoning_tokens: number
    provider_total_tokens: number
    processed_total_tokens: number
  }
  token_quality: string
  coverage_gap_count: number
  extractor_version: string
  normalization_version: number
  correlation_quality: string
  revision: {
    revision_id: string
    sequence: number
    reason: string
    evidence_version: string
    restated_at: string
    allocations: Array<Record<string, unknown>>
  }
  request_ids: {
    state: 'retained' | 'expired' | 'unlinked' | 'unavailable'
    count: number
    evidence: ActivityRequestIDEvidence[]
  }
}

export interface ActivityWindowParams {
  from?: string
  to?: string
}

export interface ActivityDetailParams extends ActivityWindowParams {
  pr_limit?: number
  pr_cursor?: string
  commit_limit?: number
  commit_cursor?: string
  bucket_limit?: number
  bucket_cursor?: string
}

export interface ActivityPageParams extends ActivityWindowParams {
  limit?: number
  cursor?: string
}

export interface ActivityRepositoryParams extends ActivityWindowParams {
  member_limit?: number
  member_cursor?: string
  pr_limit?: number
  pr_cursor?: string
  commit_limit?: number
  commit_cursor?: string
}
