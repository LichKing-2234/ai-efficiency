import client from './client'
import type { ApiResponse, PagedResponse, RepoConfig } from '@/types'

export function listRepos(page = 1, pageSize = 20) {
  return client.get<ApiResponse<PagedResponse<RepoConfig>>>('/repos', {
    params: { page, page_size: pageSize },
  })
}

export function getRepo(id: number) {
  return client.get<ApiResponse<RepoConfig>>(`/repos/${id}`)
}

export function createRepo(data: Partial<RepoConfig>) {
  return client.post<ApiResponse<RepoConfig>>('/repos', data)
}

export function createRepoDirect(data: {
  scm_provider_id: number
  name: string
  full_name: string
  clone_url: string
  default_branch: string
}) {
  return client.post<ApiResponse<RepoConfig>>('/repos/direct', data)
}

export function updateRepo(id: number, data: Partial<RepoConfig>) {
  return client.put<ApiResponse<RepoConfig>>(`/repos/${id}`, data)
}

export function deleteRepo(id: number) {
  return client.delete<ApiResponse<null>>(`/repos/${id}`)
}
