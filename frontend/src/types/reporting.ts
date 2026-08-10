export type ReportingReadinessState =
  | 'not_enrolled'
  | 'disabled'
  | 'waiting_for_data'
  | 'active'
  | 'revoked'

export interface ReportingReadiness {
  state: ReportingReadinessState
  installation_count: number
  enabled_installation_count: number
  latest_bucket_at?: string
}
