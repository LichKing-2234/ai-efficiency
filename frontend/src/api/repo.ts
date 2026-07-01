import client from './client'
import type {
  ApiResponse,
  PagedResponse,
  RepoAutoBindResult,
  RepoConfig,
  RepoInventoryProviderSummary,
  RepoListParams,
  RepoWebhookRepairBatchResult,
  RepoWebhookRepairItem,
  RepoWebhookRepairRequest,
} from '@/types'

export function listRepos(page?: number, pageSize?: number): Promise<{ data: ApiResponse<PagedResponse<RepoConfig>> }>
export function listRepos(params?: RepoListParams): Promise<{ data: ApiResponse<PagedResponse<RepoConfig>> }>
export function listRepos(paramsOrPage: RepoListParams | number = {}, pageSize = 20) {
  const params = normalizeRepoListParams(paramsOrPage, pageSize)
  return client.get<ApiResponse<PagedResponse<RepoConfig>>>('/repos', {
    params,
  })
}

function normalizeRepoListParams(paramsOrPage: RepoListParams | number | undefined, pageSize = 20) {
  const opts: RepoListParams =
    typeof paramsOrPage === 'number'
      ? { page: paramsOrPage, pageSize }
      : (paramsOrPage ?? {})

  const params: Record<string, string | number> = {
    page: opts.page ?? 1,
    page_size: opts.pageSize ?? 20,
  }
  if (opts.scmProviderId) {
    params.scm_provider_id = opts.scmProviderId
  }
  if (opts.status) {
    params.status = opts.status
  }
  if (opts.groupId) {
    params.group_id = opts.groupId
  }
  if (opts.scope) {
    params.scope = opts.scope
  }
  if (opts.bindingState) {
    params.binding_state = opts.bindingState
  }
  return params
}

export function getRepoInventory() {
  return client.get<ApiResponse<RepoInventoryProviderSummary[]>>('/repos/inventory')
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

export function autoBindUnboundRepos() {
  return client.post<ApiResponse<RepoAutoBindResult>>('/repos/auto-bind-unbound')
}

export function repairFailedWebhooks(data: RepoWebhookRepairRequest = { force: false }) {
  return client.post<ApiResponse<RepoWebhookRepairBatchResult>>('/repos/repair-webhooks', data)
}

export function repairWebhook(id: number, data: RepoWebhookRepairRequest = { force: false }) {
  return client.post<ApiResponse<RepoWebhookRepairItem>>(`/repos/${id}/repair-webhook`, data)
}

export function updateRepo(id: number, data: Partial<RepoConfig>) {
  return client.put<ApiResponse<RepoConfig>>(`/repos/${id}`, data)
}

export function deleteRepo(id: number) {
  return client.delete<ApiResponse<null>>(`/repos/${id}`)
}
