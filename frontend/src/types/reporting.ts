export type ReportingReadinessState =
  | 'not_enrolled'
  | 'revoked'
  | 'disabled'
  | 'waiting_for_data'
  | 'active'

export interface ReportingReadiness {
  state: ReportingReadinessState
  retryable: boolean
  latest_accepted_at?: string
}
