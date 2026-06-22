import client from './client'
import type {
  ApiResponse,
  DirectoryDepartment,
  DirectoryMember,
  DirectoryOffboardingAction,
  DirectoryOffboardingCandidate,
  DirectorySource,
  DirectorySourceListResponse,
  DirectorySourceRequest,
  DirectorySyncRun,
  DirectoryValidationResponse,
} from '@/types'

export function listDirectorySources() {
  return client.get<ApiResponse<DirectorySourceListResponse>>('/admin/directory/sources')
}

export function createDirectorySource(data: DirectorySourceRequest) {
  return client.post<ApiResponse<DirectorySource>>('/admin/directory/sources', data)
}

export function updateDirectorySource(id: number, data: DirectorySourceRequest) {
  return client.put<ApiResponse<DirectorySource>>(`/admin/directory/sources/${id}`, data)
}

export function deleteDirectorySource(id: number) {
  return client.delete<ApiResponse<{ deleted: boolean }>>(`/admin/directory/sources/${id}`)
}

export function validateDirectorySource(id: number) {
  return client.post<ApiResponse<DirectoryValidationResponse>>(`/admin/directory/sources/${id}/validate`)
}

export function previewDirectorySource(id: number) {
  return client.post<ApiResponse<DirectorySyncRun>>(`/admin/directory/sources/${id}/preview`)
}

export function startDirectoryRun(id: number, data: { mode: 'apply' | 'preview' | 'validate' }) {
  return client.post<ApiResponse<DirectorySyncRun>>(`/admin/directory/sources/${id}/runs`, data)
}

export function getDirectoryRun(id: number) {
  return client.get<ApiResponse<DirectorySyncRun>>(`/admin/directory/runs/${id}`)
}

export function listDirectoryDepartments(params: { source_id: number; q?: string }) {
  return client.get<ApiResponse<{ items: DirectoryDepartment[] }>>('/admin/directory/departments', { params })
}

export function listDirectoryMembers(params: { source_id: number; q?: string }) {
  return client.get<ApiResponse<{ items: DirectoryMember[] }>>('/admin/directory/members', { params })
}

export function listDirectoryOffboardingCandidates(params: { source_id: number; q?: string }) {
  return client.get<ApiResponse<{ items: DirectoryOffboardingCandidate[] }>>('/admin/directory/offboarding-candidates', { params })
}

export function disableDirectoryRelayUser(userID: number, data: { source_id: number; confirm_email: string; reason: string }) {
  return client.post<ApiResponse<DirectoryOffboardingAction>>(`/admin/directory/offboarding-candidates/${userID}/disable-relay-user`, data)
}
