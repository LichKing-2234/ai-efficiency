import client from './client'
import type { ApiResponse, PagedResponse, Session } from '@/types'

export function listSessions(params?: { page?: number; page_size?: number; status?: string; repo_id?: number }) {
  return client.get<ApiResponse<PagedResponse<Session>>>('/sessions', { params })
}

export function getSession(id: string | number) {
  return client.get<ApiResponse<Session>>(`/sessions/${id}`)
}
