export interface ActivitySyncCoverage {
  complete: boolean
  affected_repositories: number
  unsynced_repositories: number
  stale_repositories: number
  partially_synced_repositories: number
  failed_repositories: number
}

export interface ActivityMemberIdentity {
  user_id: number
  directory_member_external_id?: string
  display_name: string
  email: string
  department_external_ids: string[]
}

export interface ActivityTeamIdentity {
  external_id: string
  parent_external_id?: string | null
  name: string
  display_path: string
  member_count: number
}

export interface ActivityWindowParams {
  from?: string
  to?: string
}

export type ActivityV2Scope = 'personal' | 'member' | 'team'

export interface ActivityV2TeamMemberAvailabilityQuery {
  from: string
  to: string
  timezone: string
  user_ids: number[]
}

export interface ActivityV2TeamMemberAvailability {
  contract_version: 'activity-v2'
  scope_version: string
  team: ActivityTeamIdentity
  available_user_ids: number[]
}

export interface ActivityTeamMemberRow {
  member: ActivityMemberIdentity
  available: boolean
}

export interface ActivityTeamMembers {
  contract_version: 'activity-v2'
  scope_version: string
  team: ActivityTeamIdentity
  members: { items: ActivityTeamMemberRow[]; next_cursor?: string }
}

export interface ActivityV2Query {
  scope: ActivityV2Scope
  subject_user_id?: number
  team_id?: string
  from: string
  to: string
  timezone: string
  repo_id?: number
  pr_record_id?: number
}

export interface ActivityV2Coverage {
  complete: boolean
  lower_bound: boolean
}

export interface ActivityV2Ratio {
  state: 'exact' | 'lower_bound' | 'complete_zero_usage' | 'true_zero_committed' | 'denominator_unavailable'
  retryable?: boolean
  committed_tokens: number
  total_tokens?: number
  percent?: number
  as_of?: string
  percentage_point_change?: number
}

export interface ActivityV2TrendPoint {
  date: string
  direct_tokens: number
  shared_tokens: number
  involved_tokens: number
}

export interface ActivityV2Overview {
  contract_version: 'activity-v2'
  scope_version: string
  from: string
  to: string
  timezone: string
  committed_tokens: number
  claim_coverage: ActivityV2Coverage
  scm_coverage: ActivitySyncCoverage
  ratio: ActivityV2Ratio
  trend: ActivityV2TrendPoint[]
  readiness: { state: 'waiting_for_data' | 'active'; first_accepted_at?: string }
}

export interface ActivityV2RepositoryRow {
  repo_config_id: number
  name: string
  direct_tokens: number
  direct_share?: number
  shared_tokens: number
  token_change?: number
}

export interface ActivityV2CommitReference {
  repo_config_id: number
  commit_sha: string
}

export interface ActivityV2PullRequestRow {
  pr_record_id: number
  repo_config_id: number
  repository_name: string
  scm_pr_id: number
  title: string
  url: string
  status: string
  involved_tokens: number
  overlap_state: 'direct' | 'shared' | 'inherited'
  token_change?: number
  commits?: ActivityV2CommitReference[]
}

export interface ActivityV2Page<T> {
  items: T[]
  next_cursor?: string
  scm_coverage?: ActivitySyncCoverage
}

export interface ActivityV2PageQuery extends ActivityV2Query {
  search?: string
  sort?: 'tokens' | 'name'
  cursor?: string
}
