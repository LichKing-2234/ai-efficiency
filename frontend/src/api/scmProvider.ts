import client from './client'
import type { ApiResponse, PagedResponse, SCMProvider } from '@/types'

export function listProviders(page = 1, pageSize = 20) {
  return client.get<ApiResponse<PagedResponse<SCMProvider>>>('/scm-providers', {
    params: { page, page_size: pageSize },
  })
}

export function getProvider(id: number) {
  return client.get<ApiResponse<SCMProvider>>(`/scm-providers/${id}`)
}

export function createProvider(data: Partial<SCMProvider>) {
  return client.post<ApiResponse<SCMProvider>>('/scm-providers', data)
}

export function updateProvider(id: number, data: Partial<SCMProvider>) {
  return client.put<ApiResponse<SCMProvider>>(`/scm-providers/${id}`, data)
}

export function deleteProvider(id: number) {
  return client.delete<ApiResponse<null>>(`/scm-providers/${id}`)
}
