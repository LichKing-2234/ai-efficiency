import client from './client'
import type {
  ApiResponse,
  ToolUsageEventDetail,
  ToolUsageEventListResponse,
  ToolUsageEventSummary,
} from '@/types'

export function getEventSummary(params?: Record<string, unknown>) {
  return client.get<ApiResponse<ToolUsageEventSummary>>('/events/summary', { params })
}

export function listEvents(params?: Record<string, unknown>) {
  return client.get<ApiResponse<ToolUsageEventListResponse>>('/events', { params })
}

export function getEventDetail(id: number) {
  return client.get<ApiResponse<ToolUsageEventDetail>>(`/events/${id}`)
}
