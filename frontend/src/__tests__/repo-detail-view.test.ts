import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import RepoDetailView from '@/views/repos/RepoDetailView.vue'
import { setLocale } from '@/i18n'

vi.mock('@/api/repo', () => ({ getRepo: vi.fn(), updateRepo: vi.fn(), repairWebhook: vi.fn() }))
vi.mock('@/api/pr', () => ({
  listPRs: vi.fn(), syncPRs: vi.fn(), getPRSyncJob: vi.fn(), getLatestPRSyncJob: vi.fn(),
}))
vi.mock('@/api/scmProvider', () => ({ listProviders: vi.fn() }))
vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn().mockResolvedValue({ data: { data: { total_count: 0 } } }),
}))

const repo = {
  id: 9, repo_key: 'github.com/example/repo-a', name: 'repo-a', full_name: 'example/repo-a',
  clone_url: 'https://github.com/example/repo-a.git', default_branch: 'main', status: 'active',
  binding_state: 'bound', group_id: 1, scm_provider_id: 3, created_at: '2026-08-01T00:00:00Z', edges: {},
}

describe('Repository administration IA', () => {
  beforeEach(async () => {
    setLocale('en-US')
    const repoApi = await import('@/api/repo')
    const prApi = await import('@/api/pr')
    const scmApi = await import('@/api/scmProvider')
    vi.mocked(repoApi.getRepo).mockResolvedValue({ data: { data: repo } } as any)
    vi.mocked(prApi.getLatestPRSyncJob).mockResolvedValue({ data: { data: null } } as any)
    vi.mocked(scmApi.listProviders).mockResolvedValue({ data: { data: [{ id: 3, name: 'GitHub' }] } } as any)
  })

  it('renders integration operations without Activity, PR usage, or Token analysis', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/repos/:id', component: RepoDetailView },
      { path: '/repos', component: { template: '<div />' } },
      { path: '/usage', component: { template: '<div />' } },
      { path: '/user', component: { template: '<div />' } },
      { path: '/work-items', component: { template: '<div />' } },
    ] })
    await router.push('/repos/9')
    await router.isReady()
    const wrapper = mount(RepoDetailView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()

    expect(wrapper.get('[data-testid="repo-operations"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-testid="repo-sync-prs"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-testid="repo-provider-select"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-testid="repo-detail-health-metrics"]').isVisible()).toBe(true)
    expect(wrapper.get('[data-testid="repo-operations"]').text()).not.toMatch(/PR Usage Summary|Token analysis|Coding Activity/)
    const prApi = await import('@/api/pr')
    expect(prApi.listPRs).not.toHaveBeenCalled()
  })
})
