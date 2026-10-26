import client from './client'
import type {
  ApiResponse,
  OwnerAccountInvitationRequest,
  OwnerAccountListResponse,
  OwnerAccountMutationRequest,
  OwnerAccountOperation,
  OwnerAccountOperationAccepted,
  OwnerAccountQuotaResponse,
  OwnerAccountReferralResponse,
  OwnerAccountScopeResponse,
  OwnerAccountUsageBatchParams,
  OwnerAccountUsageBatchResponse,
  OwnerAccountUsageResponse,
  PagedResponse,
} from '@/types'

export interface OwnerAccountListParams {
  page?: number
  page_size?: number
}

export interface OwnerAccountOperationListParams {
  page?: number
  page_size?: number
}

export function getOwnerAccountScope() {
  return client.get<ApiResponse<OwnerAccountScopeResponse>>('/user/owner-accounts/scope')
}

export function listOwnerAccounts(params?: OwnerAccountListParams) {
  return client.get<ApiResponse<OwnerAccountListResponse>>('/user/owner-accounts', { params })
}

export function getOwnerAccountUsage(data: OwnerAccountUsageBatchParams) {
  return client.post<ApiResponse<OwnerAccountUsageBatchResponse>>('/user/owner-accounts/usage', data, {
    timeout: 30000,
  })
}

export function refreshOwnerAccountUsage(accountID: number) {
  return client.post<ApiResponse<OwnerAccountUsageResponse>>(
    `/user/owner-accounts/${accountID}/usage/refresh`,
    undefined,
    { timeout: 30000 },
  )
}

export function refreshOwnerAccountQuota(accountID: number) {
  return client.post<ApiResponse<OwnerAccountQuotaResponse>>(
    `/user/owner-accounts/${accountID}/quota/refresh`,
    undefined,
    { timeout: 30000 },
  )
}

export function refreshOwnerAccountReferral(accountID: number) {
  return client.post<ApiResponse<OwnerAccountReferralResponse>>(
    `/user/owner-accounts/${accountID}/referral/refresh`,
    undefined,
    { timeout: 30000 },
  )
}

export function createOwnerAccountReset(accountID: number, data: OwnerAccountMutationRequest) {
  return client.post<ApiResponse<OwnerAccountOperationAccepted>>(
    `/user/owner-accounts/${accountID}/reset-credit`,
    data,
  )
}

export function createOwnerAccountInvitation(accountID: number, data: OwnerAccountInvitationRequest) {
  return client.post<ApiResponse<OwnerAccountOperationAccepted>>(
    `/user/owner-accounts/${accountID}/invitations`,
    data,
  )
}

export function getOwnerAccountOperation(operationID: number) {
  return client.get<ApiResponse<OwnerAccountOperation>>(`/user/owner-accounts/operations/${operationID}`)
}

export function listOwnerAccountOperations(accountID: number, params?: OwnerAccountOperationListParams) {
  return client.get<ApiResponse<PagedResponse<OwnerAccountOperation>>>(
    `/user/owner-accounts/${accountID}/operations`,
    { params },
  )
}
