import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import RepositoryOperationsSection from '@/components/repos/RepositoryOperationsSection.vue'
import { setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/repo', () => ({
  getRepo: vi.fn(),
  updateRepo: vi.fn(),
  repairWebhook: vi.fn(),
}))

vi.mock('@/api/pr', () => ({
  listPRs: vi.fn(),
  getPR: vi.fn(),
  syncPRs: vi.fn(),
  getPRSyncJob: vi.fn(),
  getLatestPRSyncJob: vi.fn(),
  refreshPRUsage: vi.fn(),
}))

vi.mock('@/api/scmProvider', () => ({
  listProviders: vi.fn(),
}))

describe('RepositoryOperationsSection', () => {
  beforeEach(async () => {
    setLocale('en-US')
    vi.clearAllMocks()
    const pr = await import('@/api/pr')
    vi.mocked(pr.getLatestPRSyncJob).mockResolvedValue({ data: { data: null } } as any)
    vi.mocked(pr.syncPRs).mockResolvedValue({ data: { data: { job_id: 44, status: 'completed', phase: 'completed' } } } as any)
    vi.mocked(pr.getPRSyncJob).mockResolvedValue({
      data: {
        data: {
          id: 44,
          repo_config_id: 9,
          status: 'completed',
          phase: 'completed',
          current_page: 0,
          page_size: 100,
          fetched_prs: 0,
          total_prs: 0,
          processed_prs: 0,
          created_prs: 0,
          changed_prs: 0,
          unchanged_prs: 0,
          usage_total_prs: 0,
          usage_refreshed_prs: 0,
          usage_skipped_prs: 0,
          usage_failed_prs: 0,
        },
      },
    } as any)
    const provider = await import('@/api/scmProvider')
    vi.mocked(provider.listProviders).mockResolvedValue({ data: { data: [] } } as any)
  })

  it('owns lazy operations loading and starts PR sync only after an explicit click', async () => {
    const pr = await import('@/api/pr')
    const wrapper = mount(RepositoryOperationsSection, {
      props: {
        repoId: 9,
        repo: {
          id: 9,
          repo_key: 'github.com/org/repo-a',
          name: 'repo-a',
          full_name: 'org/repo-a',
          clone_url: 'https://github.com/org/repo-a.git',
          default_branch: 'main',
          status: 'active',
          binding_state: 'bound',
          group_id: 1,
          created_at: '2026-01-01T00:00:00Z',
          edges: {},
        },
      },
      global: { plugins: [createPinia()] },
    })
    await flushPromises()

    expect(pr.getLatestPRSyncJob).toHaveBeenCalledWith(9)
    expect(pr.syncPRs).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="repo-sync-prs"]').trigger('click')
    await flushPromises()

    expect(pr.syncPRs).toHaveBeenCalledOnce()
    expect(pr.syncPRs).toHaveBeenCalledWith(9)
  })

  it('does not load or render PR usage analytics on the operations page', async () => {
    const wrapper = mount(RepositoryOperationsSection, {
      props: {
        repoId: 9,
        repo: {
          id: 9,
          repo_key: 'github.com/org/repo-a',
          name: 'repo-a',
          full_name: 'org/repo-a',
          clone_url: 'https://github.com/org/repo-a.git',
          default_branch: 'main',
          status: 'active',
          binding_state: 'bound',
          group_id: 1,
          created_at: '2026-01-01T00:00:00Z',
          edges: {},
        },
      },
      global: { plugins: [createPinia()] },
    })
    await flushPromises()

    const pr = await import('@/api/pr')
    expect(pr.listPRs).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="repo-pr-summary-header"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('PR Usage Summary')
  })

  it('preserves the existing SCM provider when saving without changing the selection', async () => {
    const repoApi = await import('@/api/repo')
    const providerApi = await import('@/api/scmProvider')
    const repo = {
      id: 9,
      repo_key: 'github.com/org/repo-a',
      name: 'repo-a',
      full_name: 'org/repo-a',
      clone_url: 'https://github.com/org/repo-a.git',
      default_branch: 'main',
      status: 'active',
      binding_state: 'bound',
      scm_provider_id: 7,
      group_id: 1,
      created_at: '2026-01-01T00:00:00Z',
      edges: { scm_provider: { id: 7, name: 'GitHub' } },
    }
    vi.mocked(providerApi.listProviders).mockResolvedValue({ data: { data: [{ id: 7, name: 'GitHub' }] } } as any)
    vi.mocked(repoApi.updateRepo).mockResolvedValue({ data: { data: repo } } as any)
    vi.mocked(repoApi.getRepo).mockResolvedValue({ data: { data: repo } } as any)
    const pinia = createPinia()
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const wrapper = mount(RepositoryOperationsSection, {
      props: { repoId: 9, repo: repo as any },
      global: { plugins: [pinia] },
    })
    await flushPromises()

    const providerSelect = wrapper.get('[data-testid="repo-provider-select"]')
    expect(providerSelect.classes()).toContain('el-select')
    expect(providerSelect.text()).toContain('GitHub')
    await wrapper.get('[data-testid="repo-save-binding"]').trigger('click')
    await flushPromises()

    expect(repoApi.updateRepo).toHaveBeenCalledWith(9, { scm_provider_id: 7 })
  })

  it('renders a failed binding save as an Element Plus error alert', async () => {
    const repoApi = await import('@/api/repo')
    const providerApi = await import('@/api/scmProvider')
    const repo = {
      id: 9,
      repo_key: 'github.com/org/repo-a',
      name: 'repo-a',
      full_name: 'org/repo-a',
      clone_url: 'https://github.com/org/repo-a.git',
      default_branch: 'main',
      status: 'active',
      binding_state: 'bound',
      scm_provider_id: 7,
      group_id: 1,
      created_at: '2026-01-01T00:00:00Z',
      edges: { scm_provider: { id: 7, name: 'GitHub' } },
    }
    vi.mocked(providerApi.listProviders).mockResolvedValue({ data: { data: [{ id: 7, name: 'GitHub' }] } } as any)
    vi.mocked(repoApi.updateRepo).mockRejectedValue({ response: { data: { message: 'binding failed' } } })
    const pinia = createPinia()
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const wrapper = mount(RepositoryOperationsSection, {
      props: { repoId: 9, repo: repo as any },
      global: { plugins: [pinia] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="repo-save-binding"]').trigger('click')
    await flushPromises()

    const alert = wrapper.get('.el-alert--error')
    expect(alert.text()).toContain('binding failed')
  })

  it('renders a successful binding save as an Element Plus success alert', async () => {
    const repoApi = await import('@/api/repo')
    const providerApi = await import('@/api/scmProvider')
    const repo = {
      id: 9,
      repo_key: 'github.com/org/repo-a',
      name: 'repo-a',
      full_name: 'org/repo-a',
      clone_url: 'https://github.com/org/repo-a.git',
      default_branch: 'main',
      status: 'active',
      binding_state: 'bound',
      scm_provider_id: 7,
      group_id: 1,
      created_at: '2026-01-01T00:00:00Z',
      edges: { scm_provider: { id: 7, name: 'GitHub' } },
    }
    vi.mocked(providerApi.listProviders).mockResolvedValue({ data: { data: [{ id: 7, name: 'GitHub' }] } } as any)
    vi.mocked(repoApi.updateRepo).mockResolvedValue({ data: { data: repo } } as any)
    vi.mocked(repoApi.getRepo).mockResolvedValue({ data: { data: repo } } as any)
    const pinia = createPinia()
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const wrapper = mount(RepositoryOperationsSection, {
      props: { repoId: 9, repo: repo as any },
      global: { plugins: [pinia] },
    })
    await flushPromises()

    await wrapper.get('[data-testid="repo-save-binding"]').trigger('click')
    await flushPromises()

    const alert = wrapper.get('.el-alert--success')
    expect(alert.text()).toContain('Code platform binding saved')
  })
})
