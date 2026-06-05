import { describe, expect, test } from 'vitest'
import type { RepoConfig, SCMProvider } from '@/lib/api/types'
import {
  applyBindingFilter,
  buildRepoCloneUrl,
  buildRepoCreatePayload,
  groupRepos,
  healthSummary,
  parseRepoUrl,
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
})
