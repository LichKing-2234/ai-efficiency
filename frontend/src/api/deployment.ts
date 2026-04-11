import client from './client'
import type { ApiResponse, ApplyUpdateRequest, DeploymentStatus, UpdateStatus } from '@/types'

export function getDeploymentStatus() {
  return client.get<ApiResponse<DeploymentStatus>>('/settings/deployment')
}

export function checkForUpdate() {
  return client.post<ApiResponse<DeploymentStatus>>('/settings/deployment/update/check')
}

export function applyUpdate(data: ApplyUpdateRequest) {
  return client.post<ApiResponse<UpdateStatus>>('/settings/deployment/update/apply', data)
}

export function rollbackUpdate() {
  return client.post<ApiResponse<UpdateStatus>>('/settings/deployment/update/rollback')
}

export function restartDeployment() {
  return client.post<ApiResponse<UpdateStatus>>('/settings/deployment/restart')
}
