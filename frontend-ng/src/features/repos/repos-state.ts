import type {
  RepoConfig,
  RepoDirectCreateRequest,
  RepoInventoryProviderSummary,
  RepoListParams,
  RepoWebhookRepairBatchResult,
  RepoWebhookRepairItem,
  SCMProvider
} from '@/lib/api/types'

export type RepoBindingFilter = 'all' | 'bound' | 'unbound'
export type RepoCloneProtocol = 'http' | 'ssh'

export interface RepoWorkbenchSearch {
  binding: RepoBindingFilter
  provider: string
  scope: string
  page: number
  pageSize: number
}

export interface ParsedRepoUrl {
  origin: string
  project: string
  repo: string
  type: 'github' | 'bitbucket'
}

export interface RepoGroup {
  key: string
  scmName: string
  scmType: string
  org: string
  repos: RepoConfig[]
}

export interface RepoHealthSummary {
  total: number
  bound: number
  unbound: number
  active: number
}

export interface RepoScopeNavItem {
  value: string
  label: string
  trailing: string
}

export interface RepoCreateState {
  providerId: number
  parsed: ParsedRepoUrl
  cloneProtocol: RepoCloneProtocol
  sshHost?: string
  defaultBranch: string
}

export function parseRepoUrl(raw: string): ParsedRepoUrl | null {
  const input = raw.trim()
  if (!input) return null

  let parsed: URL
  try {
    parsed = new URL(input)
  } catch {
    return null
  }

  const githubMatch = parsed.pathname.match(/^\/([^/]+)\/([^/]+?)(?:\.git)?$/)
  if (githubMatch) {
    const [, project, repo] = githubMatch
    return { origin: parsed.origin, project, repo, type: 'github' }
  }

  const bitbucketMatch = parsed.pathname.match(/^\/projects\/([^/]+)\/repos\/([^/]+)/)
  if (bitbucketMatch) {
    const [, project, repo] = bitbucketMatch
    return { origin: parsed.origin, project, repo, type: 'bitbucket' }
  }

  return null
}

export function buildRepoCloneUrl(info: ParsedRepoUrl, protocol: RepoCloneProtocol, sshHost = '') {
  if (info.type === 'github') {
    return protocol === 'http'
      ? `${info.origin}/${info.project}/${info.repo}.git`
      : `git@github.com:${info.project}/${info.repo}.git`
  }

  const projectPath = info.project.toLowerCase()
  if (protocol === 'http') return `${info.origin}/scm/${projectPath}/${info.repo}.git`

  const fallbackHost = new URL(info.origin).hostname
  return `ssh://git@${sshHost || fallbackHost}/${projectPath}/${info.repo}.git`
}

export function selectProviderForRepoOrigin(providers: SCMProvider[], origin: string) {
  const match = providers.find((provider) => {
    try {
      return new URL(provider.base_url).origin === origin
    } catch {
      return false
    }
  })
  return match?.id ?? null
}

export function buildRepoCreatePayload(state: RepoCreateState): RepoDirectCreateRequest {
  return {
    scm_provider_id: state.providerId,
    name: state.parsed.repo,
    full_name: `${state.parsed.project}/${state.parsed.repo}`,
    clone_url: buildRepoCloneUrl(state.parsed, state.cloneProtocol, state.sshHost),
    default_branch: state.defaultBranch.trim() || 'main'
  }
}

export function applyBindingFilter(rows: RepoConfig[], filter: RepoBindingFilter) {
  if (filter === 'all') return rows
  return rows.filter((repo) => repo.binding_state === filter)
}

export function healthSummary(rows: RepoConfig[]): RepoHealthSummary {
  return rows.reduce<RepoHealthSummary>(
    (next, repo) => {
      next.total += 1
      if (repo.binding_state === 'bound') next.bound += 1
      if (repo.binding_state === 'unbound') next.unbound += 1
      if (repo.status === 'active') next.active += 1
      return next
    },
    { total: 0, bound: 0, unbound: 0, active: 0 }
  )
}

export function groupRepos(rows: RepoConfig[]): RepoGroup[] {
  const groups = new Map<string, RepoGroup>()

  for (const repo of rows) {
    const scm = repo.edges?.scm_provider
    const scmName = scm?.name ?? 'Unbound'
    const scmType = scm?.type ?? ''
    const org = repo.full_name.split('/')[0] || repo.name
    const key = `${scmName}::${org}`
    const group = groups.get(key) ?? { key, scmName, scmType, org, repos: [] }
    group.repos.push(repo)
    groups.set(key, group)
  }

  return Array.from(groups.values()).sort((a, b) => a.scmName.localeCompare(b.scmName) || a.org.localeCompare(b.org))
}

export function compareInventoryProviders(a: RepoInventoryProviderSummary, b: RepoInventoryProviderSummary) {
  if (a.provider_key === 'unbound') return 1
  if (b.provider_key === 'unbound') return -1
  const priority = (provider: RepoInventoryProviderSummary) => {
    if (provider.type === 'github') return 0
    if (provider.type === 'bitbucket_server' || provider.type === 'bitbucket') return 1
    return 2
  }
  return priority(a) - priority(b) || a.name.localeCompare(b.name) || a.provider_key.localeCompare(b.provider_key)
}

export function firstScope(provider: RepoInventoryProviderSummary | null | undefined) {
  return provider?.scopes[0]?.scope ?? ''
}

export function buildScopeNavItems(provider: RepoInventoryProviderSummary | null | undefined, formatCount: (value: number) => string): RepoScopeNavItem[] {
  return (provider?.scopes ?? []).map((scope) => ({
    value: scope.scope,
    label: scope.scope,
    trailing: formatCount(scope.total_repos)
  }))
}

export function webhookRepairBatchMessage(result: RepoWebhookRepairBatchResult) {
  return {
    repaired: result.summary.repaired,
    alreadyRegistered: result.summary.already_registered,
    failed: result.summary.failed
  }
}

export function canRepairWebhook(state: {
  role?: string
  bindingState?: 'bound' | 'unbound'
  status?: string
  webhookId?: string | null
}) {
  return state.role === 'admin'
    && state.bindingState === 'bound'
    && (state.status === 'webhook_failed' || !state.webhookId)
}

export function repoRepairMessage(item: RepoWebhookRepairItem): { kind: 'success' | 'error'; error?: string } {
  if (item.webhook_status === 'failed' || item.status === 'webhook_failed' || item.error) {
    return { kind: 'error', error: item.error || 'Webhook repair failed' }
  }
  return { kind: 'success' }
}

function positiveInt(value: unknown, fallback: number) {
  const parsed = Number.parseInt(String(value ?? ''), 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}

export function parseRepoSearch(search: Record<string, unknown>): RepoWorkbenchSearch {
  const binding = search.binding === 'bound' || search.binding === 'unbound' ? search.binding : 'all'
  return {
    binding,
    provider: typeof search.provider === 'string' ? search.provider : '',
    scope: typeof search.scope === 'string' ? search.scope : '',
    page: positiveInt(search.page, 1),
    pageSize: positiveInt(search.page_size, 20)
  }
}

export function buildRepoSearch(state: RepoWorkbenchSearch) {
  const next: Record<string, string> = {}
  if (state.binding !== 'all') next.binding = state.binding
  if (state.provider) next.provider = state.provider
  if (state.scope) next.scope = state.scope
  if (state.page > 1) next.page = String(state.page)
  if (state.pageSize !== 20) next.page_size = String(state.pageSize)
  return next
}

export function buildRepoListParams(state: {
  provider: RepoInventoryProviderSummary | null
  scope: string
  binding: RepoBindingFilter
  page: number
  pageSize: number
}) {
  const params: RepoListParams = {
    page: state.page,
    pageSize: state.pageSize
  }
  if (!state.provider) return params
  if (state.provider.provider_key === 'unbound') {
    params.bindingState = 'unbound'
  } else {
    if (state.provider.provider_id) params.scmProviderId = state.provider.provider_id
    if (state.binding !== 'all') params.bindingState = state.binding
  }
  if (state.scope) params.scope = state.scope
  return params
}
