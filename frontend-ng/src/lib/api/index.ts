import { apiFetch, encodeQuery } from '@/lib/api/client'
import type {
  AdminManageSubscriptionsRequest,
  AdminManageSubscriptionsResponse,
  AdminRelayPasswordRevealResponse,
  AdminSubscriptionJob,
  AdminSubscriptionOptionsResponse,
  AdminUsersListResponse,
  ApplyUpdateRequest,
  AuthOptions,
  AuthTokenPayload,
  Credential,
  DashboardData,
  DeploymentStatus,
  GroupCredentialMutationResult,
  LoginRequest,
  PagedResponse,
  PRListSummary,
  PRRecord,
  PRSyncJob,
  RelayProvider,
  RepoAutoBindResult,
  RepoConfig,
  SCMProvider,
  ToolUsageEventDetail,
  ToolUsageEventListResponse,
  ToolUsageEventSummary,
  ToolUsageEventUserOption,
  UpdateStatus,
  User,
  UserProviderModelsResponse,
  UserProvidersResponse,
  UserProviderTestRequest,
  UserProviderTestResult
} from '@/lib/api/types'

export const api = {
  auth: {
    login: (data: LoginRequest) => apiFetch<AuthTokenPayload>('/api/auth/login', { method: 'POST', body: JSON.stringify(data) }),
    devLogin: () => apiFetch<AuthTokenPayload>('/api/auth/dev-login', { method: 'POST' }),
    logout: () => apiFetch<{ message: string }>('/api/auth/logout', { method: 'POST' }),
    bootstrap: () => apiFetch<AuthTokenPayload | { message: string }>('/api/auth/bootstrap', { method: 'POST' }),
    options: () => apiFetch<AuthOptions>('/auth/options'),
    me: () => apiFetch<User>('/auth/me')
  },
  dashboard: () => apiFetch<DashboardData>('/efficiency/dashboard'),
  userProviders: () => apiFetch<UserProvidersResponse>('/user/providers'),
  createGroupCredential: (providerId: number, groupId: string) =>
    apiFetch<GroupCredentialMutationResult>(`/user/providers/${providerId}/groups/${groupId}/credential`, { method: 'POST' }),
  regenerateGroupCredential: (providerId: number, groupId: string) =>
    apiFetch<GroupCredentialMutationResult>(`/user/providers/${providerId}/groups/${groupId}/credential/regenerate`, { method: 'POST' }),
  userProviderModels: (providerId: number, groupId: string, platform: string) =>
    apiFetch<UserProviderModelsResponse>(`/user/providers/${providerId}/groups/${groupId}/models${encodeQuery({ platform })}`),
  testUserProvider: (providerId: number, data: UserProviderTestRequest) =>
    apiFetch<UserProviderTestResult>(`/user/providers/${providerId}/test`, { method: 'POST', body: JSON.stringify(data) }),
  events: {
    summary: (params?: Record<string, unknown>) => apiFetch<ToolUsageEventSummary>(`/events/summary${encodeQuery(params)}`),
    list: (params?: Record<string, unknown>) => apiFetch<ToolUsageEventListResponse>(`/events${encodeQuery(params)}`),
    detail: (id: number) => apiFetch<ToolUsageEventDetail>(`/events/${id}`),
    users: (params?: Record<string, unknown>) => apiFetch<ToolUsageEventUserOption[]>(`/events/users${encodeQuery(params)}`)
  },
  repos: {
    list: (page = 1, pageSize = 20) => apiFetch<PagedResponse<RepoConfig>>(`/repos${encodeQuery({ page, page_size: pageSize })}`),
    get: (id: number) => apiFetch<RepoConfig>(`/repos/${id}`),
    create: (data: Partial<RepoConfig>) => apiFetch<RepoConfig>('/repos', { method: 'POST', body: JSON.stringify(data) }),
    createDirect: (data: Partial<RepoConfig>) => apiFetch<RepoConfig>('/repos/direct', { method: 'POST', body: JSON.stringify(data) }),
    autoBindUnbound: () => apiFetch<RepoAutoBindResult>('/repos/auto-bind-unbound', { method: 'POST' }),
    update: (id: number, data: Partial<RepoConfig>) => apiFetch<RepoConfig>(`/repos/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: number) => apiFetch<null>(`/repos/${id}`, { method: 'DELETE' }),
    prs: (repoId: number, params?: Record<string, unknown>) =>
      apiFetch<{ items: PRRecord[]; total: number; summary?: PRListSummary }>(`/repos/${repoId}/prs${encodeQuery(params)}`),
    syncPRs: (repoId: number) => apiFetch<{ job_id: number; status: string; phase: string; reused?: boolean }>(`/repos/${repoId}/sync-prs`, { method: 'POST' }),
    latestPRSyncJob: (repoId: number) => apiFetch<PRSyncJob | null>(`/repos/${repoId}/pr-sync-job/latest`)
  },
  prs: {
    get: (id: number) => apiFetch<PRRecord>(`/prs/${id}`),
    settle: (id: number) => apiFetch<{ attribution_status: string }>(`/prs/${id}/settle`, { method: 'POST' }),
    refreshUsage: (id: number) => apiFetch<PRRecord>(`/prs/${id}/refresh-usage`, { method: 'POST' }),
    job: (id: number) => apiFetch<PRSyncJob>(`/pr-sync-jobs/${id}`)
  },
  adminUsers: {
    list: (params?: Record<string, unknown>) => apiFetch<AdminUsersListResponse>(`/admin/users${encodeQuery(params)}`),
    subscriptionOptions: () => apiFetch<AdminSubscriptionOptionsResponse>('/admin/users/subscription-options'),
    startSubscriptionJob: (data: AdminManageSubscriptionsRequest) =>
      apiFetch<AdminSubscriptionJob>('/admin/users/subscription-jobs', { method: 'POST', body: JSON.stringify(data) }),
    subscriptionJob: (id: number) => apiFetch<AdminSubscriptionJob>(`/admin/users/subscription-jobs/${id}`),
    latestSubscriptionJob: () => apiFetch<AdminSubscriptionJob | null>('/admin/users/subscription-jobs/latest'),
    manageSubscriptions: (data: AdminManageSubscriptionsRequest) =>
      apiFetch<AdminManageSubscriptionsResponse>('/admin/users/subscriptions/batch', { method: 'POST', body: JSON.stringify(data) }),
    revealRelayPassword: (id: number) =>
      apiFetch<AdminRelayPasswordRevealResponse>(`/admin/users/${id}/relay-password/reveal`, { method: 'POST' })
  },
  settings: {
    relayProviders: () => apiFetch<RelayProvider[]>('/admin/providers'),
    createRelayProvider: (data: Partial<RelayProvider>) => apiFetch<RelayProvider>('/admin/providers', { method: 'POST', body: JSON.stringify(data) }),
    updateRelayProvider: (id: number, data: Partial<RelayProvider>) => apiFetch<RelayProvider>(`/admin/providers/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    deleteRelayProvider: (id: number) => apiFetch<{ message: string }>(`/admin/providers/${id}`, { method: 'DELETE' }),
    scmProviders: (page = 1, pageSize = 20) => apiFetch<PagedResponse<SCMProvider>>(`/scm-providers${encodeQuery({ page, page_size: pageSize })}`),
    credentials: () => apiFetch<Credential[]>('/admin/credentials'),
    deployment: () => apiFetch<DeploymentStatus>('/settings/deployment'),
    checkUpdate: () => apiFetch<DeploymentStatus>('/settings/deployment/update/check', { method: 'POST' }),
    applyUpdate: (data: ApplyUpdateRequest) => apiFetch<UpdateStatus>('/settings/deployment/update/apply', { method: 'POST', body: JSON.stringify(data) }),
    rollback: () => apiFetch<UpdateStatus>('/settings/deployment/update/rollback', { method: 'POST' }),
    restart: () => apiFetch<UpdateStatus>('/settings/deployment/restart', { method: 'POST' })
  }
}
