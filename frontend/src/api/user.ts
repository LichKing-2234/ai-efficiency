import client from './client'
import type {
  ApiResponse,
  ManagedKeyMutationResult,
  UserProvidersResponse,
} from '@/types'

export function getUserProviders() {
  return client.get<ApiResponse<UserProvidersResponse>>('/user/providers')
}

export function createManagedKey(providerId: number) {
  return client.post<ApiResponse<ManagedKeyMutationResult>>(`/user/providers/${providerId}/managed-key`)
}

export function regenerateManagedKey(providerId: number) {
  return client.post<ApiResponse<ManagedKeyMutationResult>>(`/user/providers/${providerId}/managed-key/regenerate`)
}
