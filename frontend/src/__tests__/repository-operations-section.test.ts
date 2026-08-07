import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import RepositoryOperationsSection from '@/components/repos/RepositoryOperationsSection.vue'
import { setLocale } from '@/i18n'

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
    vi.mocked(pr.listPRs).mockResolvedValue({ data: { data: { items: [], total: 0 } } } as any)
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

    expect(pr.listPRs).toHaveBeenCalledWith(9, expect.objectContaining({ limit: 10, offset: 0, months: 3 }))
    expect(pr.getLatestPRSyncJob).toHaveBeenCalledWith(9)
    expect(pr.syncPRs).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="repo-sync-prs"]').trigger('click')
    await flushPromises()

    expect(pr.syncPRs).toHaveBeenCalledOnce()
    expect(pr.syncPRs).toHaveBeenCalledWith(9)
  })
})
