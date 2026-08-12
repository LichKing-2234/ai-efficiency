import client from './client'
import type { ApiResponse } from '@/types'
import type { ReportingReadiness } from '@/types/reporting'

export function getReportingReadiness() {
  return client.get<ApiResponse<ReportingReadiness>>('/attribution/status')
}
