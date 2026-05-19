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
  last_scan_at: string | null
  group_id: number | string | null
  scm_provider_id?: number | null
  scan_prompt_override?: { system_prompt?: string; user_prompt_template?: string }
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
  metadata_summary?: Record<string, any>
  last_attributed_at?: string | null
  edges?: {
    last_attribution_run?: PRAttributionRun | null
  }
  cycle_time_hours: number
  merged_at: string | null
  created_at: string
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
