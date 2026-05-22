import client from './client'
import type {
  ApiResponse,
  GroupCredentialMutationResult,
  UserProvidersResponse,
} from '@/types'

export function getUserProviders() {
  return client.get<ApiResponse<UserProvidersResponse>>('/user/providers')
}

export function createGroupCredential(providerId: number, groupId: string) {
  return client.post<ApiResponse<GroupCredentialMutationResult>>(
    `/user/providers/${providerId}/groups/${groupId}/credential`
  )
}

export function regenerateGroupCredential(providerId: number, groupId: string) {
  return client.post<ApiResponse<GroupCredentialMutationResult>>(
    `/user/providers/${providerId}/groups/${groupId}/credential/regenerate`
  )
}
