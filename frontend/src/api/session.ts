import client from './client'
import type { ApiResponse, PagedResponse, Session, SessionListParams } from '@/types'

export function listSessions(params?: SessionListParams) {
  return client.get<ApiResponse<PagedResponse<Session>>>('/sessions', { params })
}

export function getSession(id: string) {
  return client.get<ApiResponse<Session>>(`/sessions/${id}`)
}
