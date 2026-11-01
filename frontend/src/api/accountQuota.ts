import client from './client'
import type { ApiResponse } from '@/types'

/**
 * Admin account quota facts.
 *
 * The backend keeps two kinds of facts per OpenAI OAuth account:
 * - `snapshot`: the last cached upstream observation, possibly stale or absent.
 * - `observation`: the facts read by one quota sweep, absent until a sweep
 *   actually observed the account.
 *
 * Values coming from the upstream API are already sanitized by the backend;
 * this client only transports them.
 */

export interface AccountQuotaGroup {
  id: number
  name: string
  priority: number
}

export interface AccountQuotaAccount {
  id: number
  name: string
  email: string
  status: string
  schedulable: boolean
  shadow: boolean
  auto_reset_credit_enabled: boolean
  groups: AccountQuotaGroup[]
}

export interface AccountQuotaCredits {
  has_credits: boolean
  unlimited: boolean
  balance: string | null
}

export interface AccountQuotaResetCredits {
  available: number
  expires_at: string[]
}

export interface AccountQuotaUsageWindow {
  used_percent: number
  /** 18000 for the 5h window, 604800 for the 7d window. */
  window_seconds: number
  resets_at: string | null
}

export interface AccountQuotaObservation {
  sweep_id: number
  observed_at: string
  plan_type: string
  windows: AccountQuotaUsageWindow[]
  credits: AccountQuotaCredits | null
  reset_credits: AccountQuotaResetCredits | null
}

export interface AccountQuotaSnapshot {
  observed_at: string | null
  usage_updated_at: string | null
  credits: AccountQuotaCredits | null
  reset_credits: AccountQuotaResetCredits | null
}

export interface AccountQuotaItem {
  account: AccountQuotaAccount
  snapshot: AccountQuotaSnapshot | null
  observation: AccountQuotaObservation | null
}

export interface AccountQuotaSweepResult {
  account_id: number
  status: 'succeeded' | 'failed' | 'skipped'
  /** Empty when the result succeeded; a stable reason code otherwise. */
  error_code: string
  /** Sanitized upstream message, empty when the result succeeded. */
  message: string
  observation: AccountQuotaObservation | null
}

export type AccountQuotaSweepStatus = 'queued' | 'running' | 'completed' | 'failed' | 'abandoned'

export interface AccountQuotaSweep {
  id: number
  status: AccountQuotaSweepStatus
  scope: 'all' | 'selected'
  include_auto_reset: boolean
  total_count: number
  processed_count: number
  success_count: number
  skipped_count: number
  failed_count: number
  created_at: string
  started_at: string | null
  finished_at: string | null
  last_error: string | null
  /** Empty while the sweep is queued or running. */
  results: AccountQuotaSweepResult[]
}

export interface AccountQuotaPage {
  provider_id: number
  platform: string
  items: AccountQuotaItem[]
  total: number
  latest_sweep: AccountQuotaSweep | null
}

export interface AccountQuotaListParams {
  provider_id: number
  platform: string
}

export interface StartAccountQuotaSweepInput {
  provider_id: number
  platform: 'openai'
  /** Omitted or empty means "all eligible accounts". */
  account_ids?: number[]
  include_auto_reset?: boolean
}

export function listAccountQuotas(params: AccountQuotaListParams) {
  return client.get<ApiResponse<AccountQuotaPage>>('/admin/account-quotas', { params })
}

export function startAccountQuotaSweep(data: StartAccountQuotaSweepInput) {
  return client.post<ApiResponse<AccountQuotaSweep>>('/admin/account-quotas/sweeps', data)
}

export function getLatestAccountQuotaSweep(params: { provider_id: number }) {
  return client.get<ApiResponse<AccountQuotaSweep | null>>('/admin/account-quotas/sweeps/latest', { params })
}

export function getAccountQuotaSweep(id: number) {
  return client.get<ApiResponse<AccountQuotaSweep>>(`/admin/account-quotas/sweeps/${id}`)
}
