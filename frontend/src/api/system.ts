import client from './client'
import type { ApiResponse, SystemVersionStatus } from '@/types'

export function getSystemVersion() {
  return client.get<ApiResponse<SystemVersionStatus>>('/system/version')
}

export function checkSystemUpdate() {
  return client.post<ApiResponse<SystemVersionStatus>>('/system/version/check')
}
