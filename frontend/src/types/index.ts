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
  status: string
  created_at: string
}

export interface RepoConfig {
  id: number
  name: string
  full_name: string
  clone_url: string
  default_branch: string
  ai_score: number
  status: string
  last_scan_at: string | null
  group_id: number
  scan_prompt_override?: { system_prompt?: string; user_prompt_template?: string }
  created_at: string
  edges?: {
    scm_provider?: SCMProvider
  }
}

export interface Session {
  id: string
  branch: string
  status: string
  started_at: string
  ended_at: string | null
  provider_name?: string | null
  relay_api_key_id?: number | null
  runtime_ref?: string | null
  initial_workspace_root?: string | null
  last_seen_at?: string | null
  tool_invocations: Array<{ tool: string; start: string; end: string }>
  edges?: {
    repo_config?: RepoConfig
    session_workspaces?: SessionWorkspace[]
    commit_checkpoints?: CommitCheckpoint[]
    session_usage_events?: SessionUsageEvent[]
    session_events?: SessionEvent[]
  }
}

export interface SessionWorkspace {
  session_id?: string
  workspace_id: string
  workspace_root: string
  git_dir: string
  git_common_dir: string
  first_seen_at: string
  last_seen_at: string
  binding_source: string
}

export interface CommitCheckpoint {
  event_id?: string
  session_id?: string
  workspace_id: string
  repo_config_id?: number
  commit_sha: string
  parent_shas?: string[]
  branch_snapshot?: string | null
  head_snapshot?: string | null
  binding_source: string
  agent_snapshot?: Record<string, any>
  captured_at: string
}

export interface SessionUsageEvent {
  event_id: string
  session_id?: string
  workspace_id: string
  request_id?: string
  provider_name: string
  model: string
  started_at: string
  finished_at: string
  input_tokens?: number
  output_tokens?: number
  total_tokens?: number
  status: string
}

export interface SessionEvent {
  event_id: string
  session_id?: string
  workspace_id: string
  event_type: string
  source: string
  captured_at: string
}

export interface ScanResult {
  id: number
  score: number
  dimensions: Record<string, { score: number; max_score: number; details: string }>
  suggestions: Array<{ category: string; message: string; priority: string; auto_fix: boolean }>
  scan_type: string
  commit_sha: string | null
  created_at: string
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
  metadata_summary?: Record<string, any>
  last_attributed_at?: string | null
  cycle_time_hours: number
  merged_at: string | null
  created_at: string
}

export interface EfficiencyMetric {
  id: number
  period_type: string
  period_start: string
  total_prs: number
  ai_prs: number
  human_prs: number
  avg_cycle_time_hours: number
  total_tokens: number
  total_token_cost: number
  ai_vs_human_ratio: number
}

export interface DashboardData {
  total_repos: number
  active_sessions: number
  avg_ai_score: number
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

export interface SessionListParams {
  page?: number
  page_size?: number
  status?: string
  repo_id?: number
  repo_query?: string
  branch?: string
  owner_scope?: 'all' | 'mine' | 'unowned'
}
