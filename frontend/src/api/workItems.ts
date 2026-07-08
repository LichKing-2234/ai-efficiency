import client from './client'
import type { ApiResponse, WorkItemCounts } from '@/types'

export function getWorkItemCounts() {
  return client.get<ApiResponse<WorkItemCounts>>('/work-items/counts')
}
