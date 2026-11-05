import client from './client'
import type {
  AdminTeamAccountCreateRequest,
  ApiResponse,
  TeamAccountCredentials,
  TeamAccountGroupOptions,
  TeamAccountCreateRequest,
  TeamAccountDetail,
  TeamAccountEventListResponse,
  TeamAccountKeyCreateRequest,
  TeamAccountKeyCreationResponse,
  TeamAccountKeyRevokeResponse,
  TeamAccountListResponse,
  TeamAccountManagerAddResponse,
  TeamAccountManagerRemoveResponse,
  TeamAccountStatus,
  TeamAccountStatusRequest,
  TeamAccountStatusResponse,
  TeamAccountSummary,
  TeamAccountTransferResponse,
  TeamAccountUsage,
  TeamAccountUsageParams,
} from '@/types'

const USER_BASE = '/user/team-accounts'
const ADMIN_BASE = '/admin/team-accounts'

function accountPath(base: string, id: number, suffix = '') {
  return `${base}/${id}${suffix}`
}

// User routes. Administrators may use them as well; the backend widens the
// actor scope for admins.

export function listTeamAccounts() {
  return client.get<ApiResponse<TeamAccountListResponse>>(USER_BASE)
}

export function createTeamAccount(data: TeamAccountCreateRequest) {
  return client.post<ApiResponse<TeamAccountSummary>>(USER_BASE, data)
}

export function getTeamAccount(id: number) {
  return client.get<ApiResponse<TeamAccountDetail>>(accountPath(USER_BASE, id))
}

export function getTeamAccountGroupOptions() {
  return client.get<ApiResponse<TeamAccountGroupOptions>>(`${USER_BASE}/group-options`)
}

export function getAdminTeamAccountGroupOptions(userID?: number) {
  return client.get<ApiResponse<TeamAccountGroupOptions>>(`${ADMIN_BASE}/group-options`, {
    params: userID ? { user_id: userID } : undefined,
  })
}

export function revealTeamAccountCredentials(id: number) {
  return client.post<ApiResponse<TeamAccountCredentials>>(accountPath(USER_BASE, id, '/credentials'))
}

export function createTeamAccountKey(id: number, data: TeamAccountKeyCreateRequest) {
  return client.post<ApiResponse<TeamAccountKeyCreationResponse>>(accountPath(USER_BASE, id, '/keys'), data)
}

export function revokeTeamAccountKey(id: number, keyID: number) {
  return client.post<ApiResponse<TeamAccountKeyRevokeResponse>>(
    accountPath(USER_BASE, id, `/keys/${keyID}/revoke`),
  )
}

export function addTeamAccountManager(id: number, userID: number) {
  return client.post<ApiResponse<TeamAccountManagerAddResponse>>(accountPath(USER_BASE, id, '/managers'), {
    user_id: userID,
  })
}

export function removeTeamAccountManager(id: number, userID: number) {
  return client.delete<ApiResponse<TeamAccountManagerRemoveResponse>>(
    accountPath(USER_BASE, id, `/managers/${userID}`),
  )
}

export function transferTeamAccount(id: number, newOwnerUserID: number) {
  return client.post<ApiResponse<TeamAccountTransferResponse>>(accountPath(USER_BASE, id, '/transfer'), {
    new_owner_user_id: newOwnerUserID,
  })
}

export function getTeamAccountUsage(id: number, params?: TeamAccountUsageParams) {
  return client.get<ApiResponse<TeamAccountUsage>>(accountPath(USER_BASE, id, '/usage'), { params })
}

export function listTeamAccountEvents(id: number, limit = 100) {
  return client.get<ApiResponse<TeamAccountEventListResponse>>(accountPath(USER_BASE, id, '/events'), {
    params: { limit },
  })
}

// Admin routes.

export function listAdminTeamAccounts() {
  return client.get<ApiResponse<TeamAccountListResponse>>(ADMIN_BASE)
}

export function createAdminTeamAccount(data: AdminTeamAccountCreateRequest) {
  return client.post<ApiResponse<TeamAccountSummary>>(ADMIN_BASE, data)
}

export function getAdminTeamAccount(id: number) {
  return client.get<ApiResponse<TeamAccountDetail>>(accountPath(ADMIN_BASE, id))
}

export function revealAdminTeamAccountCredentials(id: number) {
  return client.post<ApiResponse<TeamAccountCredentials>>(accountPath(ADMIN_BASE, id, '/credentials'))
}

export function createAdminTeamAccountKey(id: number, data: TeamAccountKeyCreateRequest) {
  return client.post<ApiResponse<TeamAccountKeyCreationResponse>>(accountPath(ADMIN_BASE, id, '/keys'), data)
}

export function revokeAdminTeamAccountKey(id: number, keyID: number) {
  return client.post<ApiResponse<TeamAccountKeyRevokeResponse>>(
    accountPath(ADMIN_BASE, id, `/keys/${keyID}/revoke`),
  )
}

export function addAdminTeamAccountManager(id: number, userID: number) {
  return client.post<ApiResponse<TeamAccountManagerAddResponse>>(accountPath(ADMIN_BASE, id, '/managers'), {
    user_id: userID,
  })
}

export function removeAdminTeamAccountManager(id: number, userID: number) {
  return client.delete<ApiResponse<TeamAccountManagerRemoveResponse>>(
    accountPath(ADMIN_BASE, id, `/managers/${userID}`),
  )
}

export function transferAdminTeamAccount(id: number, newOwnerUserID: number) {
  return client.post<ApiResponse<TeamAccountTransferResponse>>(accountPath(ADMIN_BASE, id, '/transfer'), {
    new_owner_user_id: newOwnerUserID,
  })
}

export function getAdminTeamAccountUsage(id: number, params?: TeamAccountUsageParams) {
  return client.get<ApiResponse<TeamAccountUsage>>(accountPath(ADMIN_BASE, id, '/usage'), { params })
}

export function listAdminTeamAccountEvents(id: number, limit = 100) {
  return client.get<ApiResponse<TeamAccountEventListResponse>>(accountPath(ADMIN_BASE, id, '/events'), {
    params: { limit },
  })
}

export function setAdminTeamAccountStatus(id: number, status: TeamAccountStatus) {
  const data: TeamAccountStatusRequest = { status }
  return client.post<ApiResponse<TeamAccountStatusResponse>>(accountPath(ADMIN_BASE, id, '/status'), data)
}

export interface TeamAccountUserOption {
  id: number
  username: string
  email: string
}

export function listTeamAccountUserOptions(id: number, q: string) {
  return client.get<ApiResponse<{ items: TeamAccountUserOption[] }>>(
    accountPath(USER_BASE, id, '/user-options'),
    { params: { q } },
  )
}

export function listAdminTeamAccountUserOptions(id: number, q: string) {
  return client.get<ApiResponse<{ items: TeamAccountUserOption[] }>>(
    accountPath(ADMIN_BASE, id, '/user-options'),
    { params: { q } },
  )
}
