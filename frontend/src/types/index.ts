export interface User {
  id: number
  username: string
  email: string
  role: string
  auth_source: string
  relay_auth_password?: string | null
}

export interface SCMProvider {
  id: number
  name: string
  type: string
  base_url: string
  ssh_host?: string | null
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
  webhook_id?: string | null
  created_at: string
  edges?: {
    scm_provider?: SCMProvider
  }
}

export interface RepoInventoryScopeSummary {
  scope: string
  total_repos: number
  bound_repos: number
  unbound_repos: number
  active_repos: number
  webhook_failed_repos: number
}

export interface RepoInventoryProviderSummary {
  provider_key: string
  provider_id?: number
  name: string
  type: string
  base_url?: string
  total_repos: number
  bound_repos: number
  unbound_repos: number
  active_repos: number
  webhook_failed_repos: number
  scopes: RepoInventoryScopeSummary[]
}

export interface RepoListParams {
  page?: number
  pageSize?: number
  scmProviderId?: number
  status?: string
  groupId?: string
  scope?: string
  bindingState?: 'bound' | 'unbound'
}

export interface RepoAutoBindSummary {
  scanned: number
  bound: number
  already_bound: number
  skipped_no_match: number
  skipped_ambiguous: number
  webhook_failed: number
  errors: number
}

export interface RepoAutoBindItem {
  repo_config_id: number
  repo_key?: string
  full_name?: string
  result: 'matched' | 'already_bound' | 'no_match' | 'ambiguous' | 'invalid_repo_host' | 'provider_error'
  scm_provider_id?: number
  scm_provider_name?: string
  webhook_status?: 'skipped' | 'registered' | 'failed'
  error?: string
}

export interface RepoAutoBindResult {
  summary: RepoAutoBindSummary
  items: RepoAutoBindItem[]
}

export interface RepoWebhookRepairRequest {
  force: boolean
}

export interface RepoWebhookRepairSummary {
  scanned: number
  repaired: number
  already_registered: number
  failed: number
}

export interface RepoWebhookRepairItem {
  repo_config_id: number
  full_name: string
  previous_status: string
  status: string
  webhook_status: 'registered' | 'already_registered' | 'failed'
  webhook_id?: string
  callback_url?: string
  error?: string
}

export interface RepoWebhookRepairBatchResult {
  summary: RepoWebhookRepairSummary
  items: RepoWebhookRepairItem[]
}

export type UsageStatus =
  | 'fresh'
  | 'pending_upload'
  | 'no_checkpoint'
  | 'no_usage_events'
  | 'unbound'
  | 'stale_snapshot'
  | 'refresh_failed'
  | 'unknown'

export interface CommitFreshness {
  commit_sha: string
  usage_status: UsageStatus
  usage_status_reason: string
  checkpoint_found: boolean
  usage_event_found: boolean
}

export interface PRSyncJob {
  id: number
  repo_config_id: number
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled' | 'abandoned'
  phase: 'queued' | 'fetching_prs' | 'upserting_prs' | 'labeling' | 'refreshing_usage' | 'completed' | 'failed'
  current_page: number
  page_size: number
  fetched_prs: number
  total_prs: number
  processed_prs: number
  created_prs: number
  changed_prs: number
  unchanged_prs: number
  usage_total_prs: number
  usage_refreshed_prs: number
  usage_skipped_prs: number
  usage_failed_prs: number
  last_error?: string | null
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
  usage_status?: UsageStatus
  usage_status_reason?: string
  usage_status_checked_at?: string | null
  commit_freshness?: CommitFreshness[]
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

export interface PRListSummary {
  total: number
  with_usage: number
  pending_upload: number
  no_checkpoint: number
  refresh_failed: number
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

export interface BuildVersion {
  version: string
  commit: string
  build_time: string
}

export interface ReleaseInfo {
  version: string
  url: string
}

export interface SystemVersionStatus {
  version: BuildVersion
  check_enabled: boolean
  checked?: boolean
  check_error?: string
  update_available: boolean
  latest_release?: ReleaseInfo
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

export interface ToolUsageEventUserOption {
  id: number
  username: string
  email: string
  role: string
  event_count: number
  latest_event_at: string
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

export interface UserProviderModel {
  id: string
  display_name?: string
}

export interface UserProviderModelsResponse {
  models: UserProviderModel[]
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

export interface AdminUser {
  id: number
  username: string
  email: string
  role: string
  auth_source: string
  relay_user_id?: number | null
  relay_auth_password: string
  access_status?: AdminUserAccessStatus
  token_valid_after?: string | null
  offboarding_status?: string
  department?: AdminUserDepartment | null
  created_at: string
  updated_at: string
}

export type AdminUserAccessStatus = 'configured' | 'disabled' | 'missing_credential'

export interface AdminUserDepartment {
  external_id: string
  name: string
  path?: string
  display_path?: string
}

export interface AdminUsersListResponse {
  items: AdminUser[]
  total: number
  page: number
  page_size: number
}

export interface AdminDirectoryDepartmentSummary {
  external_id: string
  parent_external_id?: string | null
  name: string
  path?: string
  display_path?: string
  depth?: number
  child_count?: number
  member_count: number
  matched_user_count: number
  subtree_member_count?: number
  subtree_matched_user_count?: number
  representative_count?: number
  matched_representative_count?: number
}

export interface AdminUserDepartmentsResponse {
  items: AdminDirectoryDepartmentSummary[]
}

export interface AdminRelayPasswordRevealResponse {
  password: string
}

export interface AdminAssignableSubscriptionGroup {
  group_id: string
  group_name: string
  platform: string
  subscription_type: string
}

export interface AdminAssignableSubscriptionProvider {
  id: number
  name: string
  display_name: string
  groups: AdminAssignableSubscriptionGroup[]
}

export interface AdminSubscriptionOptionsResponse {
  providers: AdminAssignableSubscriptionProvider[]
}

export interface DirectorySource {
  id: number
  name: string
  description: string
  scope: 'full_company'
  enabled: boolean
  dsl: string
  schedule_enabled: boolean
  schedule_interval: 'hourly' | 'daily' | 'weekly'
  schedule_timezone: string
  last_successful_run_id?: number | null
  last_run_id?: number | null
  created_at?: string
  updated_at?: string
}

export interface DirectorySourceRequest {
  name: string
  description: string
  scope: 'full_company'
  enabled: boolean
  dsl: string
  schedule_enabled: boolean
  schedule_interval: 'hourly' | 'daily' | 'weekly'
  schedule_timezone: string
}

export interface DirectorySourceListResponse {
  items: DirectorySource[]
}

export interface DirectoryValidationIssue {
  path: string
  message: string
}

export interface DirectoryValidationResponse {
  valid: boolean
  issues: DirectoryValidationIssue[]
}

export interface DirectorySyncWarning {
  code: string
  message?: string
  step_id?: string
}

export interface DirectorySyncRun {
  id: number
  source_id: number
  mode: 'validate' | 'preview' | 'apply'
  trigger?: 'manual' | 'schedule'
  status: 'queued' | 'running' | 'completed' | 'completed_with_warnings' | 'failed'
  phase?: string
  department_count?: number
  member_count?: number
  warning_count?: number
  warnings?: DirectorySyncWarning[]
  error_message?: string | null
  created_at?: string
  updated_at?: string
}

export interface DirectoryDepartment {
  id: number
  source_id: number
  external_id: string
  name: string
  path?: string
}

export interface DirectoryMember {
  id: number
  source_id: number
  email_normalized: string
  display_name?: string
  department_external_id?: string
  status?: string
  matched_user_id?: number | null
}

export interface DirectoryOffboardingCandidate {
  user_id: number
  username: string
  email: string
  auth_source: string
  relay_user_id: number
  reason: string
  directory_run_id: number
  directory_run_at?: string | null
  token_valid_after?: string | null
  offboarding_status?: string
}

export interface DirectoryOffboardingAction {
  id: number
  source_id: number
  user_id: number
  relay_user_id: number
  status: 'running' | 'succeeded' | 'failed' | 'partial_failed'
  reason: string
}

export interface AdminAssignSubscriptionRequest {
  provider_id: number
  group_id: string
  validity_days: number
}

export interface AdminAssignSubscriptionResponse {
  status: string
  provider_id: number
  group_id: string
  relay_user_id: number
}

export type AdminSubscriptionManageScope = 'selected' | 'current_filter' | 'all_mapped'
export type AdminSubscriptionManageOperation = 'add' | 'extend' | 'remove' | 'reset_quota'

export interface AdminManageSubscriptionsRequest {
  scope: AdminSubscriptionManageScope
  user_ids?: number[]
  filters?: {
    q?: string
    department_id?: string
    access_status?: string
  }
  operation: AdminSubscriptionManageOperation
  provider_id: number
  group_id: string
  validity_days?: number
  days?: number
}

export interface AdminManageSubscriptionsResultRow {
  user_id: number
  username?: string
  email?: string
  relay_user_id?: number | null
  status: 'success' | 'skipped' | 'failed'
  message?: string
}

export interface AdminManageSubscriptionsResponse {
  status: string
  scope: AdminSubscriptionManageScope
  operation: AdminSubscriptionManageOperation
  provider_id: number
  group_id: string
  total_count: number
  success_count: number
  skipped_count: number
  failed_count: number
  results: AdminManageSubscriptionsResultRow[]
}

export type AdminSubscriptionJobStatus = 'queued' | 'running' | 'completed' | 'failed' | 'abandoned'
export type AdminSubscriptionJobPhase = 'queued' | 'resolving_targets' | 'processing' | 'completed' | 'failed'

export interface AdminSubscriptionJob {
  id: number
  status: AdminSubscriptionJobStatus
  phase: AdminSubscriptionJobPhase
  scope: AdminSubscriptionManageScope
  operation: AdminSubscriptionManageOperation
  provider_id: number
  group_id: string
  validity_days?: number
  days?: number
  filter_query?: string
  target_user_ids?: number[]
  requested_user_ids?: number[]
  total_count: number
  processed_count: number
  success_count: number
  skipped_count: number
  failed_count: number
  results: AdminManageSubscriptionsResultRow[]
  last_error?: string | null
  started_at?: string | null
  completed_at?: string | null
  created_at?: string
  updated_at?: string
}

export interface UserUsageDashboardParams {
  start_date?: string
  end_date?: string
  granularity?: 'day' | 'hour'
  timezone?: string
}

export interface UserUsageDashboardRange {
  start_date: string
  end_date: string
  granularity: 'day' | 'hour' | string
  timezone?: string
}

export interface UserUsageDashboardStats {
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_creation_tokens: number
  total_cache_read_tokens: number
  total_tokens: number
  total_cost: number
  total_actual_cost: number
  today_requests: number
  today_input_tokens: number
  today_output_tokens: number
  today_cache_creation_tokens: number
  today_cache_read_tokens: number
  today_tokens: number
  today_cost: number
  today_actual_cost: number
  average_duration_ms: number
  rpm: number
  tpm: number
}

export interface UserUsageTrendPoint {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface UserUsageModelStat {
  model: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface UserUsageGroupQuotaItem {
  group_id: string
  group_name: string
  platform: string
  used_amount?: number | null
  quota_amount?: number | null
  is_unlimited: boolean
  quota_source?: string
}

export interface UserUsageGroupQuotaState {
  status: 'ok' | 'empty' | 'unavailable' | string
  unit_label?: string
  message?: string
  groups: UserUsageGroupQuotaItem[]
}

export interface UserUsageDashboardSnapshot {
  configured: boolean
  range: UserUsageDashboardRange
  stats: UserUsageDashboardStats | null
  trend: UserUsageTrendPoint[]
  models: UserUsageModelStat[]
  group_quotas?: UserUsageGroupQuotaState
}

export interface TeamUsageDepartment {
  external_id: string
  name: string
  display_path: string
  subtree_member_count: number
  matched_user_count: number
}

export interface TeamUsageSubject {
  subject_type: 'self' | 'member' | string
  user_id: number
  directory_member_external_id?: string
  display_name: string
  email: string
  department_display_path?: string
  relay_user_id?: number | null
  selectable: boolean
}

export interface SubjectSubscriptionGroup {
  group_id: string
  group_name: string
  platform: string
  subscription_status: string
  group_default_multiplier?: number | null
  system_default_multiplier: number
  inherited_default_multiplier: number
  user_multiplier?: number | null
  effective_multiplier: number
  multiplier_source: 'user' | 'group' | 'system' | 'unknown'
  daily_limit_usd?: number | null
  weekly_limit_usd?: number | null
  monthly_limit_usd?: number | null
  daily_effective_allowance_usd?: number | null
  weekly_effective_allowance_usd?: number | null
  monthly_effective_allowance_usd?: number | null
  daily_effective_allowance_unlimited?: boolean
  weekly_effective_allowance_unlimited?: boolean
  monthly_effective_allowance_unlimited?: boolean
  daily_display_used_usd: number
  weekly_display_used_usd: number
  monthly_display_used_usd: number
  daily_usage_usd: number
  weekly_usage_usd: number
  monthly_usage_usd: number
  usage_value_basis: 'raw_actual_cost' | 'normalized_display_cost' | string
  quota_window_basis: string
  editable: boolean
  editable_reason?: string | null
}

export interface SelectedSubjectUsageSnapshot extends UserUsageDashboardSnapshot {
  subject: TeamUsageSubject
  subject_subscription_groups: SubjectSubscriptionGroup[]
}

export interface TeamOverviewWindow {
  start_date: string
  end_date: string
  granularity: string
  today: string
  rolling_days: number
  timezone: string
}

export interface TeamOverviewSummary {
  unavailable: boolean
  unavailable_reason?: string | null
  member_count: number
  relay_member_count: number
  range_actual_cost?: number | null
  range_total_tokens?: number | null
  today_actual_cost?: number | null
  total_actual_cost?: number | null
  unit_label: string
}

export interface TeamOverviewMember {
  rank?: number
  user_id: number
  directory_member_external_id?: string
  display_name: string
  email: string
  department_external_id?: string
  department_display_path: string
  relay_user_id?: number | null
  range_actual_cost: number
  today_actual_cost: number
  total_actual_cost: number
  total_tokens?: number | null
  subscription_count?: number | null
  selectable: boolean
}

export interface TeamOverviewMemberNode {
  department_external_id: string
  parent_external_id?: string | null
  name: string
  display_path: string
  depth: number
  child_count: number
  member_count: number
  connected_member_count: number
  range_actual_cost: number
  range_total_tokens?: number | null
  members: TeamOverviewMember[]
  children: TeamOverviewMemberNode[]
}

export interface TeamUsageTrendPoint {
  date: string
  actual_cost: number
  total_tokens?: number | null
}

export interface TeamMemberTrendSeries {
  user_id: number
  directory_member_external_id?: string
  display_name: string
  rank: number
  unavailable: boolean
  unavailable_reason?: string | null
  points: TeamUsageTrendPoint[]
}

export interface TeamMemberTrendState {
  unit_label: string
  rank_basis: string
  unavailable: boolean
  unavailable_reason?: string | null
  series: TeamMemberTrendSeries[]
}

export interface TeamDepartmentTrendSeries {
  series_type: 'team_total' | 'department' | string
  department_external_id?: string
  display_name: string
  rank?: number
  unavailable: boolean
  unavailable_reason?: string | null
  points: TeamUsageTrendPoint[]
}

export interface TeamDepartmentTrendState {
  unit_label: string
  unavailable: boolean
  unavailable_reason?: string | null
  series: TeamDepartmentTrendSeries[]
}

export interface TeamOverviewResponse {
  configured: boolean
  is_representative: boolean
  window: TeamOverviewWindow
  summary: TeamOverviewSummary
  top_members: TeamOverviewMember[]
  top_member_trend: TeamMemberTrendState
  department_trend?: TeamDepartmentTrendState
  members: TeamOverviewMember[]
  member_tree?: TeamOverviewMemberNode[]
}

export interface TeamUsageScopeResponse {
  is_representative: boolean
  departments: TeamUsageDepartment[]
}

export interface ListTeamUsageSubjectsResponse {
  page: number
  page_size: number
  total: number
  subjects: TeamUsageSubject[]
}

export interface TeamUsageOverviewParams {
  start_date?: string
  end_date?: string
  granularity?: 'day' | 'hour'
  timezone?: string
  page?: number
  page_size?: number
}

export interface TeamUsageAuditParams {
  page?: number
  page_size?: number
  target_user_id?: number
}

export interface TeamUsageAuditRecord {
  id: number
  actor_user_id: number
  target_user_id?: number | null
  target_display_name?: string
  target_email?: string
  group_id: string
  group_name: string
  action: string
  status: string
  old_multiplier?: number | null
  new_multiplier?: number | null
  changed: boolean
  rejection_reason?: string | null
  reason: string
  error_message?: string
  request_metadata?: Record<string, unknown> | null
  created_at: string
  updated_at: string
}

export interface GetTeamUsageAuditResponse {
  items: TeamUsageAuditRecord[]
  page: number
  page_size: number
  total: number
}

export interface UpdateTeamUsageRateMultiplierRequest {
  mode: 'set' | 'reset'
  rate_multiplier?: number
  reason?: string
}

export interface UpdateTeamUsageRateMultiplierResponse {
  status: string
  audit_id: number
  group_id: string
  old_multiplier?: number | null
  old_multiplier_source: string
  new_multiplier?: number | null
  new_multiplier_source: string
  changed: boolean
  old_effective_monthly_allowance_usd?: number | null
  new_effective_monthly_allowance_usd?: number | null
}
