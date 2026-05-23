export interface User {
  id: number
  username: string
  email: string
  role: string
  auth_source: string
}

export interface SCMProvider {
  id: number
  name: string
  type: string
  base_url: string
  api_credential_id?: number
  clone_protocol?: 'https' | 'ssh'
  clone_credential_id?: number | null
  status: string
  created_at: string
}

export interface RelayProvider {
  id: number
  name: string
  display_name: string
  base_url: string
  admin_url: string
  relay_type: string
  admin_api_key: string
  default_model: string
  is_primary: boolean
  enabled: boolean
}

export interface Credential {
  id: number
  name: string
  description: string
  kind: 'secret_text' | 'username_password' | 'ssh_username_with_private_key'
  usage_count: number
  summary: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface RepoConfig {
  id: number
  repo_key: string
  name: string
  full_name: string
  clone_url: string
  default_branch: string
  status: string
  binding_state: 'bound' | 'unbound'
  group_id: number | string | null
  scm_provider_id?: number | null
  created_at: string
  edges?: {
    scm_provider?: SCMProvider
  }
}

export interface PRRecord {
  id: number
  scm_pr_id: number
  scm_pr_url: string
  author: string
  title: string
  source_branch: string
  target_branch: string
  status: string
  labels: string[]
  lines_added: number
  lines_deleted: number
  ai_label: string
  ai_ratio: number
  token_cost: number
  attribution_status?: 'not_run' | 'clear' | 'ambiguous' | 'failed'
  attribution_confidence?: 'high' | 'medium' | 'low' | null
  primary_token_count?: number
  primary_token_cost?: number
  usage_input_tokens?: number
  usage_output_tokens?: number
  usage_cached_input_tokens?: number
  usage_reasoning_tokens?: number
  usage_credit_usage?: number
  usage_request_count?: number
  usage_commit_count?: number
  usage_refreshed_at?: string | null
  metadata_summary?: Record<string, any>
  last_attributed_at?: string | null
  edges?: {
    last_attribution_run?: PRAttributionRun | null
    pr_commit_usage_snapshots?: PRCommitUsageSnapshot[] | null
  }
  cycle_time_hours: number
  merged_at: string | null
  created_at: string
}

export interface PRCommitUsageSnapshot {
  id?: number
  commit_sha: string
  commit_checkpoint_id?: number | null
  captured_at?: string | null
  input_tokens: number
  output_tokens: number
  cached_input_tokens: number
  reasoning_tokens: number
  credit_usage: number
  request_count: number
  sort_order?: number
}

export interface PRAttributionRun {
  id?: number
  result_classification?: string
  matched_commit_shas?: string[]
  matched_session_ids?: string[]
  primary_usage_summary?: Record<string, any>
  metadata_summary?: Record<string, any>
  validation_summary?: Record<string, any>
}

export interface DashboardData {
  total_repos: number
  tracked_workflows: number
  total_ai_prs: number
}

export interface LoginRequest {
  username: string
  password: string
  source: string
}

export interface PagedResponse<T> {
  total: number
  page: number
  page_size: number
  items: T[]
}

export interface ApiResponse<T> {
  code: number
  message?: string
  data?: T
}

export interface VersionInfo {
  version: string
  commit: string
  build_time: string
}

export interface ReleaseInfo {
  version: string
  url: string
}

export interface UpdateStatus {
  phase: string
  target_version?: string
  message?: string
}

export interface DeploymentStatus {
  version: VersionInfo
  mode: string
  update_available: boolean
  latest_release?: ReleaseInfo
  update_status: UpdateStatus
}

export interface ApplyUpdateRequest {
  target_version: string
}

export interface ToolUsageEventSummary {
  total_events: number
  bound_events: number
  unbound_events: number
  tool_counts: Array<{
    tool: string
    count: number
  }>
}

export interface ToolUsageEventRow {
  id: number
  tool: string
  repo_id: number
  repo_name: string
  username?: string
  tool_session_id: string
  tool_event_id?: string | null
  dedupe_key: string
  observed_end_at: string
  request_count: number
  input_tokens: number
  output_tokens: number
  cached_input_tokens: number
  reasoning_tokens: number
  credit_usage: number
  commit_checkpoint_id?: number | null
  commit_sha?: string | null
  source_basename: string
  binding_status: 'bound' | 'unbound'
}

export interface MatchedPR {
  pr_record_id: number
  scm_pr_id: number
  title: string
  status: string
  scm_pr_url: string
}

export interface ToolUsageEventDetail {
  id: number
  tool: string
  repo_id: number
  repo_name: string
  user_id: number
  username?: string
  workspace_id: string
  tool_session_id: string
  tool_event_id?: string | null
  dedupe_key: string
  observed_start_at: string
  observed_end_at: string
  request_count: number
  input_tokens: number
  output_tokens: number
  cached_input_tokens: number
  reasoning_tokens: number
  credit_usage: number
  context_usage_pct: number
  commit_checkpoint_id?: number | null
  commit_sha?: string | null
  checkpoint_captured_at?: string | null
  source_basename: string
  raw_source_path?: string
  raw_source_locator?: string
  raw_payload?: Record<string, unknown>
  binding_status: 'bound' | 'unbound'
  matched_prs: MatchedPR[]
}

export interface ToolUsageEventListResponse {
  total: number
  page: number
  page_size: number
  items: ToolUsageEventRow[]
}

export interface UserGroupCredentialState {
  state: 'missing' | 'existing_hidden'
  api_key_id?: number
  key?: string
  name?: string
  status?: string
  created_at?: string | null
  last_used_at?: string | null
}

export interface UserGroupCredentialSummary {
  group_id: string
  group_name: string
  platform: string
  credential: UserGroupCredentialState
}

export interface UserProviderSummary {
  id: number
  name: string
  display_name: string
  base_url: string
  default_model: string
  is_primary: boolean
  groups: UserGroupCredentialSummary[]
}

export interface UserProvidersResponse {
  providers: UserProviderSummary[]
  message?: string
}

export interface GroupCredentialMutationResult {
  api_key_id: number
  name: string
  status: string
  secret: string
}

export interface UserProviderTestRequest {
  platform: string
  group_id: string
  model: string
  prompt?: string
}

export interface UserProviderTestResult {
  success: boolean
  message: string
  response?: string
}

export interface VerifyReviewItem {
  status: 'looks_good' | 'needs_attention' | 'cannot_determine'
  message: string
}

export interface VerifyReviewSummary {
  version: VerifyReviewItem
  discover: VerifyReviewItem
  doctor: VerifyReviewItem
}
