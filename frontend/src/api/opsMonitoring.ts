import client from './client'
import type { ApiResponse } from '@/types'

export type OpsMonitoringWindow = '1h' | '6h' | '24h'
export type OpsMonitoringGroupStatus = 'ok' | 'failed'

export interface OpsMonitoringWindowStat {
  avg?: number
  current?: number
  peak?: number
}

// sub2api returns output_tps as an average plus percentiles and a sample count,
// not the current/peak shape it uses for qps and tps.
export interface OpsMonitoringOutputTpsStat {
  avg?: number
  p5?: number
  p10?: number
  p50?: number
  sample_count?: number
}

export interface OpsMonitoringDurationStat {
  avg_ms?: number
  max_ms?: number
  p50_ms?: number
  p90_ms?: number
  p95_ms?: number
  p99_ms?: number
}

// Verbatim sub2api ops overview. Every field is optional because the upstream
// payload is displayed as-is and must never be recomputed in the frontend.
export interface OpsMonitoringOverview {
  request_count_total?: number
  request_count_sla?: number
  success_count?: number
  error_count_total?: number
  error_count_sla?: number
  business_limited_count?: number
  error_rate?: number
  sla?: number
  upstream_error_rate?: number
  upstream_error_count_excl_429_529?: number
  upstream_429_count?: number
  upstream_529_count?: number
  health_score?: number
  token_consumed?: number
  qps?: OpsMonitoringWindowStat | null
  tps?: OpsMonitoringWindowStat | null
  output_tps?: OpsMonitoringOutputTpsStat | null
  duration?: OpsMonitoringDurationStat | null
  ttft?: OpsMonitoringDurationStat | null
}

export interface OpsMonitoringGroup {
  group_id: number
  group_name: string
  platform: string
  account_count: number
  active_account_count: number
  status: OpsMonitoringGroupStatus
  error: string
  overview: OpsMonitoringOverview | null
}

export interface OpsMonitoringGroupsData {
  provider_id: number
  window: string
  start_time: string
  end_time: string
  as_of: string
  refreshing: boolean
  failed_group_count: number
  groups: OpsMonitoringGroup[]
}

export interface OpsMonitoringProxy {
  proxy_id: number
  name: string
  protocol: string
  host: string
  port: number
  status: string
}

export interface OpsMonitoringAccount {
  account_id: number
  name: string
  platform: string
  type: string
  status: string
  schedulable: boolean
  proxy_id: number | null
  proxy: OpsMonitoringProxy | null
  /** Configured upstream concurrency limit, shown verbatim by the page. */
  concurrency: number
  /** Live upstream active request count, shown verbatim by the page. */
  current_concurrency: number
}

export interface OpsMonitoringAccountsData {
  group_id: number
  as_of: string
  accounts: OpsMonitoringAccount[]
}

// Verified upstream read-back after setting one account's configured concurrency.
export interface OpsMonitoringAccountCapacity {
  account_id: number
  concurrency: number
  current_concurrency: number
}

export interface OpsMonitoringProxyTestResult {
  success: boolean
  message: string
  latency_ms: number
  ip_address: string
  city: string
  region: string
  country: string
  country_code: string
  checked_at: string
  cached: boolean
}

export interface OpsMonitoringGroupsParams {
  provider_id: number
  window: OpsMonitoringWindow
  // Skips the snapshot cache read and goes upstream now; the result is still
  // written back to the cache. Only an explicit refresh sets this.
  force?: boolean
}

export interface OpsMonitoringProxyTestParams {
  provider_id: number
  // Skips the 6-hour egress-measurement cache read and re-probes now.
  force?: boolean
}

export function getOpsMonitoringGroups(params: OpsMonitoringGroupsParams) {
  return client.get<ApiResponse<OpsMonitoringGroupsData>>('/admin/ops-monitoring/groups', {
    params: {
      provider_id: params.provider_id,
      window: params.window,
      // The platform filter is client-side by contract: no platform parameter
      // is sent, so the options stay derivable from every loaded group.
      ...(params.force ? { force: true } : {}),
    },
  })
}

export function getOpsMonitoringGroupAccounts(groupId: number, params: { provider_id: number }) {
  return client.get<ApiResponse<OpsMonitoringAccountsData>>(`/admin/ops-monitoring/groups/${groupId}/accounts`, { params })
}

export function setOpsMonitoringAccountConcurrency(
  groupId: number,
  accountId: number,
  concurrency: number,
  params: { provider_id: number },
) {
  return client.put<ApiResponse<OpsMonitoringAccountCapacity>>(
    `/admin/ops-monitoring/groups/${groupId}/accounts/${accountId}/concurrency`,
    { concurrency },
    { params },
  )
}

export function testOpsMonitoringProxy(proxyId: number, params: OpsMonitoringProxyTestParams) {
  return client.post<ApiResponse<OpsMonitoringProxyTestResult>>(`/admin/ops-monitoring/proxies/${proxyId}/test`, undefined, {
    params: {
      provider_id: params.provider_id,
      ...(params.force ? { force: true } : {}),
    },
  })
}
