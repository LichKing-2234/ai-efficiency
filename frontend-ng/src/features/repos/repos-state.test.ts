import { describe, expect, test } from 'vitest'
import type { RepoConfig, RepoInventoryProviderSummary, RepoWebhookRepairBatchResult, RepoWebhookRepairItem, SCMProvider } from '@/lib/api/types'
import {
  applyBindingFilter,
  buildRepoCloneUrl,
  buildRepoCreatePayload,
  buildRepoListParams,
  buildRepoSearch,
  buildScopeNavItems,
  canRepairWebhook,
  compareInventoryProviders,
  firstScope,
  groupRepos,
  healthSummary,
  parseRepoUrl,
  parseRepoSearch,
  repoRepairMessage,
  webhookRepairBatchMessage,
  selectProviderForRepoOrigin
} from './repos-state'

function provider(overrides: Partial<SCMProvider>): SCMProvider {
  return {
    id: 1,
    name: 'GitHub',
    type: 'github',
    base_url: 'https://github.com',
    status: 'active',
    created_at: '2026-06-01T00:00:00Z',
    ...overrides
  }
}

function repo(overrides: Partial<RepoConfig>): RepoConfig {
  return {
    id: 1,
    repo_key: 'github:org/api',
    name: 'api',
    full_name: 'org/api',
    clone_url: 'https://github.com/org/api.git',
    default_branch: 'main',
    status: 'active',
    binding_state: 'bound',
    group_id: null,
    scm_provider_id: 1,
    created_at: '2026-06-01T00:00:00Z',
    pr_summary: {
      total_prs: 0,
      ai_prs: 0,
      ai_share: 0
    },
    edges: {
      scm_provider: provider({ id: 1, name: 'GitHub', type: 'github', base_url: 'https://github.com' })
    },
    ...overrides
  }
}

describe('repos state helpers', () => {
  test('parses GitHub and Bitbucket repository URLs into canonical repo info', () => {
    expect(parseRepoUrl('https://github.com/acme/platform.git')).toEqual({
      origin: 'https://github.com',
      project: 'acme',
      repo: 'platform',
      type: 'github'
    })
    expect(parseRepoUrl('https://bitbucket.example.com/projects/AE/repos/server/browse')).toEqual({
      origin: 'https://bitbucket.example.com',
      project: 'AE',
      repo: 'server',
      type: 'bitbucket'
    })
    expect(parseRepoUrl('not a url')).toBeNull()
  })

  test('builds HTTP and SSH clone URLs from parsed repo info', () => {
    const github = parseRepoUrl('https://github.com/acme/platform')!
    const bitbucket = parseRepoUrl('https://bitbucket.example.com/projects/AE/repos/server/browse')!

    expect(buildRepoCloneUrl(github, 'http')).toBe('https://github.com/acme/platform.git')
    expect(buildRepoCloneUrl(github, 'ssh')).toBe('git@github.com:acme/platform.git')
    expect(buildRepoCloneUrl(bitbucket, 'http')).toBe('https://bitbucket.example.com/scm/ae/server.git')
    expect(buildRepoCloneUrl(bitbucket, 'ssh', 'git.example.com')).toBe('ssh://git@git.example.com/ae/server.git')
  })

  test('matches SCM providers by base URL origin', () => {
    const providers = [
      provider({ id: 1, base_url: 'https://github.com/api/v3' }),
      provider({ id: 2, name: 'Bitbucket', type: 'bitbucket', base_url: 'https://bitbucket.example.com' })
    ]

    expect(selectProviderForRepoOrigin(providers, 'https://github.com')).toBe(1)
    expect(selectProviderForRepoOrigin(providers, 'https://bitbucket.example.com')).toBe(2)
    expect(selectProviderForRepoOrigin(providers, 'https://gitlab.example.com')).toBeNull()
  })

  test('builds direct create payload from parsed repo state', () => {
    const parsed = parseRepoUrl('https://bitbucket.example.com/projects/AE/repos/server/browse')!

    expect(buildRepoCreatePayload({
      providerId: 2,
      parsed,
      cloneProtocol: 'ssh',
      sshHost: 'git.example.com',
      defaultBranch: ''
    })).toEqual({
      scm_provider_id: 2,
      name: 'server',
      full_name: 'AE/server',
      clone_url: 'ssh://git@git.example.com/ae/server.git',
      default_branch: 'main'
    })
  })

  test('filters, summarizes, and groups repos by binding, SCM provider, and org', () => {
    const rows = [
      repo({ id: 1, full_name: 'beta/api', name: 'api', binding_state: 'bound', status: 'active', edges: { scm_provider: provider({ name: 'GitHub' }) } }),
      repo({ id: 2, full_name: 'alpha/web', name: 'web', binding_state: 'unbound', status: 'inactive', scm_provider_id: null, edges: undefined }),
      repo({ id: 3, full_name: 'alpha/worker', name: 'worker', binding_state: 'bound', status: 'active', edges: { scm_provider: provider({ name: 'Bitbucket', type: 'bitbucket' }) } })
    ]

    expect(healthSummary(rows)).toEqual({ total: 3, bound: 2, unbound: 1, active: 2 })
    expect(applyBindingFilter(rows, 'unbound').map((item) => item.id)).toEqual([2])
    expect(groupRepos(rows).map((group) => ({ scmName: group.scmName, org: group.org, ids: group.repos.map((item) => item.id) }))).toEqual([
      { scmName: 'Bitbucket', org: 'alpha', ids: [3] },
      { scmName: 'GitHub', org: 'beta', ids: [1] },
      { scmName: 'Unbound', org: 'alpha', ids: [2] }
    ])
  })

  test('sorts inventory providers with unbound last and stable platform priority', () => {
    const rows: RepoInventoryProviderSummary[] = [
      { provider_key: 'unbound', name: 'Unbound', type: 'unbound', total_repos: 1, bound_repos: 0, unbound_repos: 1, active_repos: 0, webhook_failed_repos: 0, scopes: [] },
      { provider_key: 'bb', provider_id: 2, name: 'Bitbucket', type: 'bitbucket_server', total_repos: 2, bound_repos: 2, unbound_repos: 0, active_repos: 2, webhook_failed_repos: 1, scopes: [] },
      { provider_key: 'gh', provider_id: 1, name: 'GitHub', type: 'github', total_repos: 3, bound_repos: 3, unbound_repos: 0, active_repos: 3, webhook_failed_repos: 0, scopes: [] }
    ]
    expect([...rows].sort(compareInventoryProviders).map((row) => row.provider_key)).toEqual(['gh', 'bb', 'unbound'])
  })

  test('reads first scope and summarizes webhook repair results', () => {
    const provider: RepoInventoryProviderSummary = {
      provider_key: 'gh',
      provider_id: 1,
      name: 'GitHub',
      type: 'github',
      total_repos: 2,
      bound_repos: 2,
      unbound_repos: 0,
      active_repos: 1,
      webhook_failed_repos: 1,
      scopes: [{ scope: 'org', total_repos: 2, bound_repos: 2, unbound_repos: 0, active_repos: 1, webhook_failed_repos: 1 }]
    }
    expect(firstScope(provider)).toBe('org')
    expect(buildScopeNavItems(provider, (value) => `${value} repos`)).toEqual([
      { value: 'org', label: 'org', trailing: '2 repos' }
    ])
    expect(buildScopeNavItems(null, String)).toEqual([])

    const batch: RepoWebhookRepairBatchResult = {
      summary: { scanned: 3, repaired: 1, already_registered: 1, failed: 1 },
      items: []
    }
    expect(webhookRepairBatchMessage(batch)).toEqual({ repaired: 1, alreadyRegistered: 1, failed: 1 })
  })

  test('classifies repo detail webhook repair eligibility and result', () => {
    expect(canRepairWebhook({ role: 'admin', bindingState: 'bound', status: 'webhook_failed', webhookId: 'old' })).toBe(true)
    expect(canRepairWebhook({ role: 'admin', bindingState: 'bound', status: 'active', webhookId: '' })).toBe(true)
    expect(canRepairWebhook({ role: 'user', bindingState: 'bound', status: 'webhook_failed', webhookId: 'old' })).toBe(false)
    expect(canRepairWebhook({ role: 'admin', bindingState: 'unbound', status: 'webhook_failed', webhookId: 'old' })).toBe(false)

    const failed: RepoWebhookRepairItem = {
      repo_config_id: 9,
      full_name: 'org/repo',
      previous_status: 'webhook_failed',
      status: 'webhook_failed',
      webhook_status: 'failed',
      error: 'bitbucket API returned 502'
    }
    expect(repoRepairMessage(failed)).toEqual({ kind: 'error', error: 'bitbucket API returned 502' })
  })

  test('parses and serializes repo workbench URL state', () => {
    expect(parseRepoSearch({ binding: 'unbound', provider: 'gh', scope: 'org', page: '2', page_size: '50' })).toEqual({
      binding: 'unbound',
      provider: 'gh',
      scope: 'org',
      page: 2,
      pageSize: 50
    })
    expect(parseRepoSearch({ binding: 'bad', page: '-1', page_size: 'NaN' })).toEqual({
      binding: 'all',
      provider: '',
      scope: '',
      page: 1,
      pageSize: 20
    })
    expect(buildRepoSearch({ binding: 'all', provider: '', scope: '', page: 1, pageSize: 20 })).toEqual({})
    expect(buildRepoSearch({ binding: 'bound', provider: 'gh', scope: 'org', page: 3, pageSize: 100 })).toEqual({
      binding: 'bound',
      provider: 'gh',
      scope: 'org',
      page: '3',
      page_size: '100'
    })
  })

  test('builds repo list params from selected inventory provider and scope', () => {
    const inventoryProvider: RepoInventoryProviderSummary = {
      provider_key: 'gh',
      provider_id: 1,
      name: 'GitHub',
      type: 'github',
      total_repos: 3,
      bound_repos: 3,
      unbound_repos: 0,
      active_repos: 2,
      webhook_failed_repos: 1,
      scopes: []
    }
    expect(buildRepoListParams({ provider: inventoryProvider, scope: 'org', binding: 'bound', page: 2, pageSize: 50 })).toEqual({
      page: 2,
      pageSize: 50,
      scmProviderId: 1,
      bindingState: 'bound',
      scope: 'org'
    })
    expect(buildRepoListParams({ provider: { ...inventoryProvider, provider_key: 'unbound', provider_id: undefined }, scope: 'unknown', binding: 'all', page: 1, pageSize: 20 })).toMatchObject({
      bindingState: 'unbound'
    })
  })
})
