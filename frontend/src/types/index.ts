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
  tool_invocations: Array<{ tool: string; start: string; end: string }>
  edges?: {
    repo_config?: RepoConfig
  }
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
