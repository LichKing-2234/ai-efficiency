import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { Pinia } from 'pinia'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import RepoDetailView from '@/views/repos/RepoDetailView.vue'
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
  listProviders: vi.fn().mockResolvedValue({
    data: { data: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' }] },
  }),
}))

vi.mock('@/api/activity', () => ({
  getActivityRepository: vi.fn(),
  normalizeRepository: (value: any) => ({
    ...value,
    members: { ...value.members, items: value.members?.items ?? [] },
    prs: { ...value.prs, items: value.prs?.items ?? [] },
    commits: { ...value.commits, items: value.commits?.items ?? [] },
  }),
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/repos/:id', component: RepoDetailView },
    ],
  })
}

function createAdminPinia() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }
  return pinia
}

function detailFor(prId: number) {
  return {
    data: {
      data: {
        id: prId,
        scm_pr_id: 88,
        scm_pr_url: 'https://github.com/org/repo-a/pull/88',
        author: 'alice',
        title: 'Add usage',
        source_branch: 'feat/a',
        target_branch: 'main',
        status: 'merged',
        labels: [],
        lines_added: 10,
        lines_deleted: 2,
        cycle_time_hours: 5,
        merged_at: '2026-03-30T00:00:00Z',
        created_at: '2026-03-29T00:00:00Z',
        usage_input_tokens: 1200,
        usage_output_tokens: 500,
        usage_cached_input_tokens: 300,
        usage_reasoning_tokens: 80,
        usage_credit_usage: 1.25,
        usage_request_count: 4,
        usage_commit_count: 1,
        usage_refreshed_at: '2026-03-30T01:00:00Z',
        edges: {
          pr_commit_usage_snapshots: [{
            commit_sha: 'abc123',
            captured_at: '2026-03-30T01:00:00Z',
            input_tokens: 1200,
            output_tokens: 500,
            cached_input_tokens: 300,
            reasoning_tokens: 80,
            credit_usage: 1.25,
            request_count: 4,
            sort_order: 0,
          }],
        },
      },
    },
  }
}

async function mountRepoDetail(
  repoOverride?: Record<string, unknown>,
  pinia?: Pinia,
  options?: {
    section?: 'activity' | 'operations'
    prs?: Record<string, unknown>[]
    total?: number
    summary?: Record<string, unknown>
    listPRsImpl?: () => Promise<any>
    latestSyncJobImpl?: () => Promise<any>
    getPRImpl?: (prId: number) => Promise<any>
    refreshPRUsageImpl?: (prId: number) => Promise<any>
  },
) {
  const { getRepo } = await import('@/api/repo')
  const { listPRs, getPR, refreshPRUsage, getLatestPRSyncJob } = await import('@/api/pr')
  const prItems = options?.prs ?? [{
    id: 101,
    scm_pr_id: 88,
    scm_pr_url: 'https://github.com/org/repo-a/pull/88',
    author: 'alice',
    title: 'Add usage',
    source_branch: 'feat/a',
    target_branch: 'main',
    status: 'merged',
    labels: [],
    lines_added: 10,
    lines_deleted: 2,
    cycle_time_hours: 5,
    merged_at: '2026-03-30T00:00:00Z',
    created_at: '2026-03-29T00:00:00Z',
    usage_input_tokens: 1200,
    usage_output_tokens: 500,
    usage_cached_input_tokens: 300,
    usage_reasoning_tokens: 80,
    usage_credit_usage: 1.25,
    usage_request_count: 4,
    usage_commit_count: 1,
    usage_refreshed_at: '2026-03-30T01:00:00Z',
    usage_status: 'fresh',
  }]

  ;(getRepo as any).mockResolvedValue({
    data: {
      data: {
        id: 9,
        repo_key: 'github.com/org/repo-a',
        name: 'repo-a',
        full_name: 'org/repo-a',
        clone_url: 'https://github.com/org/repo-a.git',
        default_branch: 'main',
        status: 'active',
        binding_state: 'bound',
        edges: { scm_provider: { id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' } },
        group_id: 1,
        created_at: '2026-01-01T00:00:00Z',
        ...repoOverride,
      },
    },
  })
  if (options?.listPRsImpl) {
    ;(listPRs as any).mockImplementation(options.listPRsImpl)
  } else {
    ;(listPRs as any).mockResolvedValue({
      data: {
        data: {
          items: prItems,
          total: options?.total ?? prItems.length,
          ...(options?.summary ? { summary: options.summary } : {}),
        },
      },
    })
  }
  if (options?.latestSyncJobImpl) {
    ;(getLatestPRSyncJob as any).mockImplementation(options.latestSyncJobImpl)
  } else {
    ;(getLatestPRSyncJob as any).mockResolvedValue({ data: { data: null } })
  }
  if (options?.getPRImpl) {
    ;(getPR as any).mockImplementation(options.getPRImpl)
  } else {
    ;(getPR as any).mockResolvedValue(detailFor(101))
  }
  if (options?.refreshPRUsageImpl) {
    ;(refreshPRUsage as any).mockImplementation(options.refreshPRUsageImpl)
  } else {
    ;(refreshPRUsage as any).mockResolvedValue(detailFor(101))
  }

  const router = createTestRouter()
  await router.push(options?.section === 'activity' ? '/repos/9' : '/repos/9?tab=operations')
  await router.isReady()

  const activePinia = pinia ?? createPinia()
  const wrapper = mount(RepoDetailView, {
    global: {
      plugins: [activePinia, router],
    },
  })

  await flushPromises()
  return { wrapper, getPR, refreshPRUsage, router }
}

describe('RepoDetailView', () => {
  beforeEach(async () => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockResolvedValue({
      data: { data: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' }] },
    })
    const { getActivityRepository } = await import('@/api/activity')
    ;(getActivityRepository as any).mockResolvedValue({
      data: {
        data: {
          contract_version: 'activity-v1',
          scope_version: 'scope-1',
          window: { from: '2026-07-07T00:00:00Z', to: '2026-08-06T00:00:00Z' },
          repository: { repo_config_id: 9, name: 'org/repo-a' },
          participating_members: 2,
          metrics: {
            participating_prs: { value: 2, lower_bound: true },
            merged_prs: { value: 1, lower_bound: true },
            active_repositories: 1,
            commit_count: 1,
            latest_activity: '2026-08-05T12:00:00Z',
          },
          sync_coverage: {
            complete: false,
            affected_repositories: 1,
            unsynced_repositories: 1,
            stale_repositories: 0,
            partially_synced_repositories: 0,
            failed_repositories: 0,
          },
          members: { items: [] },
          prs: {
            items: [{
              repo_config_id: 9,
              repo_name: 'org/repo-a',
              pr_record_id: 101,
              scm_pr_id: 88,
              title: 'Activity PR',
              url: 'https://github.com/org/repo-a/pull/88',
              status: 'merged',
              commits: [{ repo_config_id: 9, commit_sha: 'abc123' }],
            }],
          },
          commits: {
            items: [{
              repo_config_id: 9,
              repo_name: 'org/repo-a',
              commit_sha: 'abc123',
              latest_activity: '2026-08-05T12:00:00Z',
              processed_tokens: 1700,
              prs: [{ repo_config_id: 9, pr_record_id: 101, scm_pr_id: 88 }],
            }],
          },
        },
      },
    })
  })

  it('loads repository Activity first without starting operations requests', async () => {
    const activity = await import('@/api/activity')
    const pr = await import('@/api/pr')
    const scm = await import('@/api/scmProvider')

    const { wrapper } = await mountRepoDetail(undefined, undefined, { section: 'activity' })

    expect(activity.getActivityRepository).toHaveBeenCalledOnce()
    expect(activity.getActivityRepository).toHaveBeenCalledWith(9, expect.objectContaining({
      member_limit: 50,
      pr_limit: 20,
      commit_limit: 20,
    }))
    const params = vi.mocked(activity.getActivityRepository).mock.calls[0][1]!
    expect(new Date(params.to!).getTime() - new Date(params.from!).getTime()).toBe(30 * 24 * 60 * 60 * 1000)
    expect(pr.listPRs).not.toHaveBeenCalled()
    expect(pr.getLatestPRSyncJob).not.toHaveBeenCalled()
    expect(pr.syncPRs).not.toHaveBeenCalled()
    expect(scm.listProviders).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="repo-activity"]').text()).toContain('Activity PR')
    expect(wrapper.get('[data-testid="repo-activity-latest"]').text()).toContain('2026')
    expect(wrapper.find('[data-testid="repo-pr-row"]').exists()).toBe(false)
  })

  it('reloads repository identity when the routed repository changes in the same view instance', async () => {
    const repoAPI = await import('@/api/repo')
    const { wrapper, router } = await mountRepoDetail(undefined, undefined, { section: 'activity' })
    vi.mocked(repoAPI.getRepo).mockResolvedValueOnce({
      data: {
        data: {
          id: 10,
          repo_key: 'github.com/org/repo-b',
          name: 'repo-b',
          full_name: 'org/repo-b',
          clone_url: 'https://github.com/org/repo-b.git',
          default_branch: 'main',
          status: 'active',
          binding_state: 'bound',
          group_id: 1,
          created_at: '2026-01-01T00:00:00Z',
          edges: {},
        },
      },
    } as any)

    await router.push('/repos/10')
    await flushPromises()

    expect(repoAPI.getRepo).toHaveBeenNthCalledWith(2, 10)
    expect(wrapper.text()).toContain('repo-b')
    expect(wrapper.text()).not.toContain('repo-a')
  })

  it('expands PR commits while keeping one commit row for multiple PR associations', async () => {
    const activity = await import('@/api/activity')
    vi.mocked(activity.getActivityRepository).mockResolvedValueOnce({
      data: {
        data: {
          contract_version: 'activity-v1',
          scope_version: 'scope-1',
          window: { from: '2026-07-07T00:00:00Z', to: '2026-08-06T00:00:00Z' },
          repository: { repo_config_id: 9, name: 'org/repo-a' },
          participating_members: 2,
          metrics: {
            participating_prs: { value: 2, lower_bound: false },
            merged_prs: { value: 1, lower_bound: false },
            active_repositories: 1,
            commit_count: 1,
            latest_activity: '2026-08-05T12:00:00Z',
          },
          sync_coverage: { complete: true, affected_repositories: 0, unsynced_repositories: 0, stale_repositories: 0, partially_synced_repositories: 0, failed_repositories: 0 },
          members: { items: [] },
          prs: {
            items: [
              { repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 101, scm_pr_id: 88, title: 'First PR', url: 'https://example.com/88', status: 'merged', commits: [{ repo_config_id: 9, commit_sha: 'shared123456' }] },
              { repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 102, scm_pr_id: 89, title: 'Second PR', url: 'https://example.com/89', status: 'open', commits: [{ repo_config_id: 9, commit_sha: 'shared123456' }] },
            ],
          },
          commits: {
            items: [{
              repo_config_id: 9,
              repo_name: 'org/repo-a',
              commit_sha: 'shared123456',
              latest_activity: '2026-08-05T12:00:00Z',
              processed_tokens: 1700,
              prs: [
                { repo_config_id: 9, pr_record_id: 101, scm_pr_id: 88 },
                { repo_config_id: 9, pr_record_id: 102, scm_pr_id: 89 },
              ],
            }],
          },
        },
      },
    } as any)

    const { wrapper } = await mountRepoDetail(undefined, undefined, { section: 'activity' })
    const activitySection = wrapper.get('[data-testid="repo-activity"]')
    expect(wrapper.get('[data-testid="repo-activity-details"]').classes()).toContain('xl:grid-cols-2')
    expect(activitySection.text()).not.toContain('Token')
    expect(activitySection.html().indexOf('data-testid="repo-activity-prs"')).toBeLessThan(activitySection.html().indexOf('data-testid="repo-activity-commits"'))
    expect(wrapper.find('[data-testid="repo-activity-pr-commits-101"]').exists()).toBe(false)

    await wrapper.get('[data-testid="repo-activity-pr-toggle-101"]').trigger('click')

    expect(wrapper.get('[data-testid="repo-activity-pr-commits-101"]').text()).toContain('shared1234')
    expect(wrapper.findAll('[data-testid="repo-activity-commit-9-shared123456"]')).toHaveLength(1)
    const commit = wrapper.get('[data-testid="repo-activity-commit-9-shared123456"]')
    expect(commit.text()).toContain('PR #88')
    expect(commit.text()).toContain('PR #89')
    expect(commit.classes()).toContain('sm:grid-cols-[1fr_minmax(12rem,auto)]')
    expect(commit.classes()).not.toContain('min-w-[36rem]')
    expect(wrapper.get('[data-testid="repo-activity-commits"]').classes()).not.toContain('overflow-x-auto')
  })

  it('pages repository PRs without replacing the summary or commit projection', async () => {
    const api = await import('@/api/activity')
    const first = {
      data: {
        data: {
          contract_version: 'activity-v1',
          scope_version: 'scope-1',
          window: { from: '2026-07-07T00:00:00Z', to: '2026-08-06T00:00:00Z' },
          repository: { repo_config_id: 9, name: 'org/repo-a' },
          participating_members: 2,
          metrics: {
            participating_prs: { value: 2, lower_bound: false },
            merged_prs: { value: 1, lower_bound: false },
            active_repositories: 1,
            commit_count: 1,
            latest_activity: '2026-08-05T12:00:00Z',
          },
          sync_coverage: { complete: true, affected_repositories: 0, unsynced_repositories: 0, stale_repositories: 0, partially_synced_repositories: 0, failed_repositories: 0 },
          members: { items: [] },
          prs: {
            items: [{ repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 101, scm_pr_id: 88, title: 'First PR page', url: 'https://example.com/88', status: 'merged', commits: [] }],
            next_cursor: 'signed-repo-pr-cursor',
          },
          commits: {
            items: [{ repo_config_id: 9, repo_name: 'org/repo-a', commit_sha: 'keepcommit123', latest_activity: '2026-08-05T12:00:00Z', processed_tokens: 1700, prs: [{ repo_config_id: 9, pr_record_id: 101, scm_pr_id: 88 }] }],
          },
        },
      },
    }
    const next = structuredClone(first) as any
    next.data.data.participating_members = 99
    next.data.data.prs = {
      items: [{ repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 102, scm_pr_id: 89, title: 'Second PR page', url: 'https://example.com/89', status: 'open', commits: [] }],
    }
    next.data.data.commits.items[0].commit_sha = 'must-not-replace'
    vi.mocked(api.getActivityRepository)
      .mockResolvedValueOnce(first as any)
      .mockResolvedValueOnce(next as any)

    const { wrapper } = await mountRepoDetail(undefined, undefined, { section: 'activity' })
    await wrapper.get('[data-testid="repo-activity-prs-next"]').trigger('click')
    await flushPromises()

    expect(api.getActivityRepository).toHaveBeenNthCalledWith(2, 9, expect.objectContaining({
      pr_limit: 20,
      pr_cursor: 'signed-repo-pr-cursor',
    }))
    expect(wrapper.get('[data-testid="repo-activity-prs"]').text()).toContain('Second PR page')
    expect(wrapper.get('[data-testid="repo-activity-prs"]').text()).not.toContain('First PR page')
    expect(wrapper.get('[data-testid="repo-activity"]').text()).toContain('2')
    expect(wrapper.get('[data-testid="repo-activity-commits"]').text()).toContain('keepcommit')
    expect(wrapper.get('[data-testid="repo-activity-commits"]').text()).not.toContain('must-not-replace')
  })

  it('returns to the first repository PR page using the saved cursor', async () => {
    const api = await import('@/api/activity')
    const first = {
      data: {
        data: {
          contract_version: 'activity-v1',
          scope_version: 'scope-1',
          window: { from: '2026-07-07T00:00:00Z', to: '2026-08-06T00:00:00Z' },
          repository: { repo_config_id: 9, name: 'org/repo-a' },
          participating_members: 2,
          metrics: {
            participating_prs: { value: 2, lower_bound: false },
            merged_prs: { value: 1, lower_bound: false },
            active_repositories: 1,
            commit_count: 1,
            latest_activity: '2026-08-05T12:00:00Z',
          },
          sync_coverage: { complete: true, affected_repositories: 0, unsynced_repositories: 0, stale_repositories: 0, partially_synced_repositories: 0, failed_repositories: 0 },
          members: { items: [] },
          prs: {
            items: [{ repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 101, scm_pr_id: 88, title: 'First PR page', url: 'https://example.com/88', status: 'merged', commits: [] }],
            next_cursor: 'signed-repo-pr-cursor',
          },
          commits: { items: [] },
        },
      },
    }
    const second = structuredClone(first) as any
    second.data.data.prs = {
      items: [{ repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 102, scm_pr_id: 89, title: 'Second PR page', url: 'https://example.com/89', status: 'open', commits: [] }],
    }
    vi.mocked(api.getActivityRepository)
      .mockResolvedValueOnce(first as any)
      .mockResolvedValueOnce(second as any)
      .mockResolvedValueOnce(first as any)

    const { wrapper } = await mountRepoDetail(undefined, undefined, { section: 'activity' })
    await wrapper.get('[data-testid="repo-activity-prs-next"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="repo-activity-prs-previous"]').trigger('click')
    await flushPromises()

    expect(api.getActivityRepository).toHaveBeenNthCalledWith(3, 9, expect.not.objectContaining({
      pr_cursor: expect.anything(),
    }))
    expect(wrapper.get('[data-testid="repo-activity-prs"]').text()).toContain('First PR page')
    expect(wrapper.get('[data-testid="repo-activity-prs"]').text()).not.toContain('Second PR page')
  })

  it('keeps a new repository Activity range when an older PR page arrives late', async () => {
    const api = await import('@/api/activity')
    const first = {
      data: {
        data: {
          contract_version: 'activity-v1',
          scope_version: 'scope-1',
          window: { from: '2026-07-07T00:00:00Z', to: '2026-08-06T00:00:00Z' },
          repository: { repo_config_id: 9, name: 'org/repo-a' },
          participating_members: 2,
          metrics: {
            participating_prs: { value: 2, lower_bound: false },
            merged_prs: { value: 1, lower_bound: false },
            active_repositories: 1,
            commit_count: 1,
            latest_activity: '2026-08-05T12:00:00Z',
          },
          sync_coverage: { complete: true, affected_repositories: 0, unsynced_repositories: 0, stale_repositories: 0, partially_synced_repositories: 0, failed_repositories: 0 },
          members: { items: [] },
          prs: {
            items: [{ repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 101, scm_pr_id: 88, title: 'Initial range PR', url: 'https://example.com/88', status: 'merged', commits: [] }],
            next_cursor: 'old-range-cursor',
          },
          commits: { items: [] },
        },
      },
    }
    const stalePage = structuredClone(first) as any
    stalePage.data.data.prs = {
      items: [{ repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 102, scm_pr_id: 89, title: 'Stale page PR', url: 'https://example.com/89', status: 'open', commits: [] }],
    }
    const freshRange = structuredClone(first) as any
    freshRange.data.data.prs = {
      items: [{ repo_config_id: 9, repo_name: 'org/repo-a', pr_record_id: 103, scm_pr_id: 90, title: 'Fresh seven day PR', url: 'https://example.com/90', status: 'open', commits: [] }],
      next_cursor: 'fresh-range-cursor',
    }
    let resolveStalePage!: (value: unknown) => void
    const pendingPage = new Promise((resolve) => { resolveStalePage = resolve })
    vi.mocked(api.getActivityRepository)
      .mockResolvedValueOnce(first as any)
      .mockReturnValueOnce(pendingPage as any)
      .mockResolvedValueOnce(freshRange as any)

    const { wrapper } = await mountRepoDetail(undefined, undefined, { section: 'activity' })
    await wrapper.get('[data-testid="repo-activity-prs-next"]').trigger('click')
    await wrapper.get('[data-testid="activity-range-7"]').trigger('click')
    await flushPromises()

    expect(api.getActivityRepository).toHaveBeenNthCalledWith(3, 9, expect.not.objectContaining({
      pr_cursor: expect.anything(),
    }))
    expect(wrapper.get('[data-testid="repo-activity-prs"]').text()).toContain('Fresh seven day PR')

    resolveStalePage(stalePage)
    await flushPromises()

    expect(wrapper.get('[data-testid="repo-activity-prs"]').text()).toContain('Fresh seven day PR')
    expect(wrapper.get('[data-testid="repo-activity-prs"]').text()).not.toContain('Stale page PR')
    expect(wrapper.get('[data-testid="repo-activity-prs-next"]').attributes('disabled')).toBeUndefined()
  })

  it('renders conclusion-first PR usage summary and readable default columns', async () => {
    const { wrapper } = await mountRepoDetail()
    expect(wrapper.text()).toContain('Repository health')
    expect(wrapper.get('[data-testid="repo-detail-health-metrics"]').classes()).toContain('grid-cols-2')
    expect(wrapper.text()).toContain('PR Usage Summary')
    expect(wrapper.text()).toContain('checkpoint window')
    expect(wrapper.text()).toContain('live tool context counter')
    expect(wrapper.text()).toContain('Total PRs')
    expect(wrapper.text()).toContain('With AI usage')
    expect(wrapper.text()).toContain('AI usage status')
    expect(wrapper.text()).toContain('Counted')
    expect(wrapper.text()).toContain('Token usage')
    expect(wrapper.text()).toContain('Refreshed')
    expect(wrapper.text()).not.toContain('Cache')
    expect(wrapper.text()).not.toContain('Reasoning')
    expect(wrapper.text()).not.toContain('AI Label')
    expect(wrapper.text()).not.toContain('Confidence')
    expect(wrapper.text()).not.toContain('Settle')
    expect(wrapper.text()).toContain('1,700')
    expect(wrapper.text()).not.toContain('2,000')
  })

  it('uses an Element Plus loading state while repository data is pending', async () => {
    const { wrapper } = await mountRepoDetail(undefined, undefined, {
      listPRsImpl: () => new Promise(() => {}),
    })

    expect(wrapper.find('.el-skeleton').exists()).toBe(true)
  })

  it('uses an Element Plus PR range selector', async () => {
    const { wrapper } = await mountRepoDetail()

    expect(wrapper.find('.el-select').exists()).toBe(true)
  })

  it('uses Element Plus for repository detail command buttons', async () => {
    const { wrapper } = await mountRepoDetail(undefined, createAdminPinia())

    expect(wrapper.findAll('button').every((button) => button.classes().some((name) => name.startsWith('el-')))).toBe(true)
  })

  it('renders repository and PR core content while admin provider options are still pending', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    let resolveProviders!: (value: any) => void
    const providersPromise = new Promise((resolve) => { resolveProviders = resolve })
    ;(listProviders as any).mockReturnValue(providersPromise)
    const pinia = createAdminPinia()

    const { wrapper, router } = await mountRepoDetail(undefined, pinia)
    expect(wrapper.text()).toContain('Repository health')
    expect(wrapper.text()).toContain('Add usage')
    expect(router.currentRoute.value.path).toBe('/repos/9')
    expect(wrapper.findAll('[data-testid="repo-pr-row"]')).toHaveLength(1)

    resolveProviders({ data: { data: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' }] } })
    await flushPromises()
    expect(wrapper.find('[data-testid="repo-binding-controls"]').exists()).toBe(true)
  })

  it('keeps core content and route when provider options fail', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockRejectedValue(new Error('provider timeout'))
    const pinia = createAdminPinia()

    const { wrapper, router } = await mountRepoDetail(undefined, pinia)
    expect(wrapper.text()).toContain('Repository health')
    expect(wrapper.text()).toContain('Add usage')
    expect(router.currentRoute.value.path).toBe('/repos/9')
  })

  it('mounts one PR row, one details command, and one expanded detail subtree per item', async () => {
    const prs = [101, 102].map((id) => ({
      ...detailFor(id).data.data,
      id,
      title: `PR ${id}`,
    }))
    const { wrapper } = await mountRepoDetail(undefined, undefined, {
      prs,
      getPRImpl: vi.fn(async (id: number) => detailFor(id)),
    })
    expect(wrapper.findAll('[data-testid="repo-pr-row"]')).toHaveLength(2)
    expect(wrapper.findAll('[data-testid="repo-pr-details-button"]')).toHaveLength(2)

    await wrapper.findAll('[data-testid="repo-pr-details-button"]')[0].trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="repo-pr-detail"]')).toHaveLength(1)
  })

  it('keeps PR identity and metrics stacked until the desktop breakpoint', async () => {
    const { wrapper } = await mountRepoDetail()

    const summary = wrapper.get('[data-testid="repo-pr-summary-grid"]')
    const metrics = wrapper.get('[data-testid="repo-pr-summary-metrics"]')
    expect(summary.classes()).toContain('lg:grid')
    expect(summary.classes()).not.toContain('md:grid')
    expect(metrics.classes()).toContain('lg:grid-cols-4')
    expect(metrics.classes()).not.toContain('md:grid-cols-4')
  })

  it('constrains long PR titles inside the identity column', async () => {
    const { wrapper } = await mountRepoDetail(undefined, undefined, {
      prs: [{
        id: 101,
        scm_pr_id: 88,
        scm_pr_url: 'https://github.com/org/repo-a/pull/88',
        author: 'dependabot[bot]',
        title: 'chore(deps): bump undici, release-it/bumper, release-it/conventional-changelog and release-it',
        source_branch: 'dependabot/npm_and_yarn/dependencies',
        target_branch: 'main',
        status: 'open',
        labels: [],
        lines_added: 10,
        lines_deleted: 2,
        cycle_time_hours: 5,
        created_at: '2026-03-29T00:00:00Z',
        usage_status: 'pending_upload',
      }],
    })

    const identity = wrapper.get('[data-testid="repo-pr-identity"]')
    const title = wrapper.get('[data-testid="repo-pr-title"]')
    expect(identity.classes()).toContain('min-w-0')
    expect(title.classes()).toContain('min-w-0')
    expect(title.classes()).toContain('max-w-full')
    expect(title.get('span.truncate').classes()).toContain('truncate')
  })

  it('renders aggregate PR usage summary instead of current page counts', async () => {
    const pageItems = Array.from({ length: 10 }, (_, index) => ({
      id: 200 + index,
      scm_pr_id: 300 + index,
      scm_pr_url: `https://github.com/org/repo-a/pull/${300 + index}`,
      author: 'alice',
      title: `PR ${index}`,
      source_branch: 'feat/a',
      target_branch: 'main',
      status: 'merged',
      labels: [],
      lines_added: 10,
      lines_deleted: 2,
      cycle_time_hours: 5,
      merged_at: '2026-03-30T00:00:00Z',
      created_at: '2026-03-29T00:00:00Z',
      usage_status: 'no_checkpoint',
    }))

    const { wrapper } = await mountRepoDetail(undefined, undefined, {
      prs: pageItems,
      total: 25,
      summary: {
        total: 25,
        with_usage: 4,
        pending_upload: 2,
        no_checkpoint: 18,
        refresh_failed: 1,
      },
    })

    const totalCard = wrapper.findAll('.rounded-md').find((card) => card.text().includes('Total PRs'))
    const withUsageCard = wrapper.findAll('.rounded-md').find((card) => card.text().includes('With AI usage'))
    const pendingCard = wrapper.findAll('.rounded-md').find((card) => card.text().includes('Waiting for usage upload'))
    const noCheckpointCard = wrapper.findAll('.rounded-md').find((card) => card.text().includes('Missing commit record'))
    const refreshFailedCard = wrapper.findAll('.rounded-md').find((card) => card.text().includes('Refresh failed'))

    expect(totalCard?.text()).toContain('25')
    expect(withUsageCard?.text()).toContain('4')
    expect(pendingCard?.text()).toContain('2')
    expect(noCheckpointCard?.text()).toContain('18')
    expect(refreshFailedCard?.text()).toContain('1')
  })

  it('switches repository detail summary labels to Chinese', async () => {
    setLocale('zh-CN')
    const { wrapper } = await mountRepoDetail()

    expect(wrapper.text()).toContain('仓库健康度')
    expect(wrapper.text()).toContain('PR 使用摘要')
    expect(wrapper.text()).toContain('commit checkpoint 窗口')
    expect(wrapper.text()).toContain('实时上下文计数')
    expect(wrapper.text()).toContain('默认分支')
    expect(wrapper.text()).toContain('AI 用量状态')
    expect(wrapper.text()).toContain('已统计')
    expect(wrapper.text()).toContain('Token 用量')
    expect(wrapper.text()).toContain('刷新时间')
    expect(wrapper.text()).toContain('最近 3 个月')
    expect(wrapper.text()).toContain('详情')
  })

  it('switches admin repository binding controls to Chinese', async () => {
    setLocale('zh-CN')
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const { wrapper } = await mountRepoDetail({
      binding_state: 'unbound',
      edges: {},
    }, pinia)

    expect(wrapper.text()).toContain('代码平台入口绑定')
    expect(wrapper.text()).toContain('需要绑定')
    expect(wrapper.text()).toContain('保存绑定')
    expect(wrapper.text()).toContain('清除绑定')
  })

  it('loads and renders commit usage snapshots for a PR', async () => {
    const { wrapper, getPR } = await mountRepoDetail()
    const detailsButton = wrapper.findAll('button').find((b) => b.text() === 'Details')
    expect(detailsButton).toBeTruthy()

    await detailsButton!.trigger('click')
    await flushPromises()

    expect(getPR).toHaveBeenCalledWith(101)
    expect(wrapper.text()).toContain('Commit SHA')
    expect(wrapper.text()).toContain('abc123')
    expect(wrapper.text()).toContain('1,700')
    expect(wrapper.text()).not.toContain('2,000')
    expect(wrapper.text()).toContain('300')
    expect(wrapper.text()).toContain('80')
  })

  it('refreshes PR usage when no snapshot exists yet', async () => {
    const { wrapper, refreshPRUsage } = await mountRepoDetail(undefined, undefined, {
      prs: [{
        id: 101,
        scm_pr_id: 88,
        scm_pr_url: 'https://github.com/org/repo-a/pull/88',
        author: 'alice',
        title: 'Add usage',
        source_branch: 'feat/a',
        target_branch: 'main',
        status: 'merged',
        labels: [],
        lines_added: 10,
        lines_deleted: 2,
        cycle_time_hours: 5,
        merged_at: '2026-03-30T00:00:00Z',
        created_at: '2026-03-29T00:00:00Z',
      }],
      getPRImpl: vi.fn(async () => ({
        data: { data: { ...detailFor(101).data.data, edges: { pr_commit_usage_snapshots: null }, usage_refreshed_at: null } },
      })),
    })

    const detailsButton = wrapper.findAll('button').find((b) => b.text() === 'Details')
    expect(detailsButton).toBeTruthy()

    await detailsButton!.trigger('click')
    await flushPromises()

    expect(refreshPRUsage).toHaveBeenCalledWith(101)
    expect(wrapper.text()).toContain('abc123')
  })

  it('distinguishes missing usage from real zero values', async () => {
    const { wrapper } = await mountRepoDetail(undefined, undefined, {
      prs: [{
        id: 101,
        scm_pr_id: 88,
        scm_pr_url: 'https://github.com/org/repo-a/pull/88',
        author: 'alice',
        title: 'Missing usage',
        source_branch: 'feat/a',
        target_branch: 'main',
        status: 'merged',
        labels: [],
        lines_added: 10,
        lines_deleted: 2,
        cycle_time_hours: 5,
        merged_at: '2026-03-30T00:00:00Z',
        created_at: '2026-03-29T00:00:00Z',
        usage_input_tokens: undefined,
        usage_output_tokens: 0,
        usage_cached_input_tokens: 0,
        usage_reasoning_tokens: undefined,
        usage_credit_usage: 0,
        usage_request_count: 0,
      }],
    })

    expect(wrapper.text()).toContain('—')
    expect(wrapper.text()).toContain('0')
  })

  it('renders external PR navigation with an Element Plus link and noopener protection', async () => {
    const { wrapper } = await mountRepoDetail()
    const link = wrapper.find('a[href="https://github.com/org/repo-a/pull/88"]')
    const linkComponent = wrapper.findAllComponents({ name: 'ElLink' })
      .find((component) => component.attributes('href') === 'https://github.com/org/repo-a/pull/88')

    expect(link.exists()).toBe(true)
    expect(link.classes()).toContain('el-link')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    expect(linkComponent?.props('underline')).toBe('never')
  })

  it('shows binding controls for admin on an unbound repo', async () => {
    const pinia = createAdminPinia()

    const { wrapper } = await mountRepoDetail({
      binding_state: 'unbound',
      edges: {},
    }, pinia)
    expect(wrapper.text()).toContain('Code Platform Binding')
    expect(wrapper.text()).toContain('auto-discovered by ae-cli')
  })

  it('uses an Element Plus repository binding selector', async () => {
    const { wrapper } = await mountRepoDetail({ binding_state: 'unbound', edges: {} }, createAdminPinia())

    expect(wrapper.find('[data-testid="repo-binding-controls"] .el-select').exists()).toBe(true)
  })

  it('keeps the current provider label when it is absent from the loaded option set', async () => {
    const { listProviders } = await import('@/api/scmProvider')
    ;(listProviders as any).mockResolvedValue({
      data: { data: [{ id: 1, name: 'Other Provider', type: 'github', base_url: 'https://api.github.com', status: 'active' }] },
    })

    const { wrapper } = await mountRepoDetail({
      scm_provider_id: 2,
      edges: {
        scm_provider: {
          id: 2,
          name: 'GitHub Example',
          type: 'github',
          base_url: 'https://github.example.com/api/v3',
          status: 'active',
        },
      },
    }, createAdminPinia())

    expect(wrapper.get('[data-testid="repo-provider-select"]').text()).toContain('GitHub Example')
    expect(wrapper.get('[data-testid="repo-provider-select"]').text()).not.toBe('2')
  })

  it('shows repair webhook action for admin bound webhook_failed repo', async () => {
    const pinia = createAdminPinia()

    const { wrapper } = await mountRepoDetail({
      status: 'webhook_failed',
      binding_state: 'bound',
      webhook_id: 'old-hook',
      edges: { scm_provider: { id: 2, name: 'Bitbucket', type: 'bitbucket_server', base_url: 'https://bitbucket.example.com', status: 'active' } },
    }, pinia)

    expect(wrapper.text()).toContain('Repair webhook')
    expect(wrapper.text()).toContain('Force replace')
    expect(wrapper.find('[data-testid="repo-repair-webhook-button"]').exists()).toBe(true)
  })

  it('uses an Element Plus force-repair checkbox', async () => {
    const { wrapper } = await mountRepoDetail({
      status: 'webhook_failed',
      binding_state: 'bound',
      webhook_id: 'old-hook',
      edges: { scm_provider: { id: 2, name: 'Bitbucket', type: 'bitbucket_server', base_url: 'https://bitbucket.example.com', status: 'active' } },
    }, createAdminPinia())

    expect(wrapper.find('.el-checkbox').exists()).toBe(true)
  })

  it('shows repair webhook action for admin bound repo with missing webhook id', async () => {
    const pinia = createAdminPinia()

    const { wrapper } = await mountRepoDetail({
      status: 'active',
      binding_state: 'bound',
      webhook_id: '',
      edges: { scm_provider: { id: 2, name: 'Bitbucket', type: 'bitbucket_server', base_url: 'https://bitbucket.example.com', status: 'active' } },
    }, pinia)

    expect(wrapper.text()).toContain('Repair webhook')
    expect(wrapper.text()).not.toContain('Force replace')
    expect(wrapper.find('[data-testid="repo-repair-webhook-button"]').exists()).toBe(true)
  })

  it('repairs webhook from repo detail', async () => {
    const { repairWebhook } = await import('@/api/repo')
    ;(repairWebhook as any).mockResolvedValue({
      data: { data: { repo_config_id: 9, status: 'active', webhook_status: 'registered', webhook_id: '99' } },
    })
    const pinia = createAdminPinia()

    const { wrapper } = await mountRepoDetail({
      status: 'webhook_failed',
      binding_state: 'bound',
      webhook_id: 'old-hook',
      edges: { scm_provider: { id: 2, name: 'Bitbucket', type: 'bitbucket_server', base_url: 'https://bitbucket.example.com', status: 'active' } },
    }, pinia)

    await wrapper.get('[data-testid="repo-repair-webhook-button"]').trigger('click')
    await flushPromises()

    expect(repairWebhook).toHaveBeenCalledWith(9, { force: false })
    expect(wrapper.text()).toContain('Webhook repaired')
  })

  it('shows failed webhook repair result as an error', async () => {
    const { repairWebhook } = await import('@/api/repo')
    ;(repairWebhook as any).mockResolvedValue({
      data: { data: { repo_config_id: 9, status: 'webhook_failed', webhook_status: 'failed', error: 'bitbucket API returned 502' } },
    })
    const pinia = createAdminPinia()

    const { wrapper } = await mountRepoDetail({
      status: 'webhook_failed',
      binding_state: 'bound',
      edges: { scm_provider: { id: 2, name: 'Bitbucket', type: 'bitbucket_server', base_url: 'https://bitbucket.example.com', status: 'active' } },
    }, pinia)

    await wrapper.get('[data-testid="repo-repair-webhook-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Webhook repair failed')
    expect(wrapper.text()).toContain('bitbucket API returned 502')
    expect(wrapper.text()).not.toContain('Webhook repair complete')
  })

  it('polls and shows PR sync job progress after syncing', async () => {
    vi.useFakeTimers()
    const { syncPRs, getPRSyncJob } = await import('@/api/pr')
    ;(syncPRs as any).mockResolvedValue({ data: { data: { job_id: 44, status: 'queued', phase: 'queued' } } })
    ;(getPRSyncJob as any)
      .mockResolvedValueOnce({ data: { data: { id: 44, status: 'running', phase: 'fetching_prs', current_page: 2, page_size: 100, fetched_prs: 200, total_prs: 0, processed_prs: 0, created_prs: 0, changed_prs: 0, unchanged_prs: 0, usage_total_prs: 0, usage_refreshed_prs: 0, usage_skipped_prs: 0, usage_failed_prs: 0 } } })
      .mockResolvedValueOnce({ data: { data: { id: 44, status: 'completed', phase: 'completed', current_page: 2, page_size: 100, fetched_prs: 200, total_prs: 200, processed_prs: 200, created_prs: 2, changed_prs: 3, unchanged_prs: 195, usage_total_prs: 5, usage_refreshed_prs: 4, usage_skipped_prs: 1, usage_failed_prs: 0 } } })

    const { wrapper } = await mountRepoDetail()
    const syncButton = wrapper.findAll('button').find((b) => b.text() === 'Sync PRs')
    expect(syncButton).toBeTruthy()

    try {
      await syncButton!.trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('Fetching PRs')
      expect(wrapper.text()).toContain('200 fetched')

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()
      expect(wrapper.text()).toContain('Sync completed')
      expect(wrapper.text()).toContain('2 created')
      expect(wrapper.text()).toContain('3 changed')
    } finally {
      vi.useRealTimers()
    }
  })

  it('recovers a running PR sync job on page load', async () => {
    vi.useFakeTimers()
    const { getPRSyncJob } = await import('@/api/pr')
    ;(getPRSyncJob as any).mockResolvedValue({
      data: { data: { id: 77, repo_config_id: 9, status: 'running', phase: 'refreshing_usage', current_page: 126, page_size: 100, fetched_prs: 12556, total_prs: 12556, processed_prs: 12556, created_prs: 12556, changed_prs: 0, unchanged_prs: 0, usage_total_prs: 12556, usage_refreshed_prs: 3086, usage_skipped_prs: 0, usage_failed_prs: 0 } },
    })

    try {
      const { wrapper } = await mountRepoDetail(undefined, undefined, {
        latestSyncJobImpl: vi.fn(async () => ({
          data: { data: { id: 77, repo_config_id: 9, status: 'running', phase: 'refreshing_usage', current_page: 126, page_size: 100, fetched_prs: 12556, total_prs: 12556, processed_prs: 12556, created_prs: 12556, changed_prs: 0, unchanged_prs: 0, usage_total_prs: 12556, usage_refreshed_prs: 3085, usage_skipped_prs: 0, usage_failed_prs: 0 } },
        })),
      })
      expect(wrapper.text()).toContain('Refreshing usage')
      expect(wrapper.text()).toContain('12,556 fetched')
      const syncButton = wrapper.findAll('button').find((b) => b.text() === 'Syncing...')
      expect(syncButton).toBeTruthy()
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows PR list load errors instead of empty state', async () => {
    const { wrapper } = await mountRepoDetail(undefined, undefined, {
      listPRsImpl: vi.fn(async () => {
        throw new Error('backend timeout')
      }),
    })

    expect(wrapper.text()).toContain('Failed to load pull requests')
    expect(wrapper.text()).toContain('Retry')
    expect(wrapper.text()).not.toContain('No pull requests recorded yet.')
  })

  it('uses an Element Plus empty state when no pull requests exist', async () => {
    const { wrapper } = await mountRepoDetail(undefined, undefined, { prs: [], total: 0 })

    expect(wrapper.find('.el-empty').exists()).toBe(true)
  })

  it('preserves loaded PR rows when a later PR list refresh fails', async () => {
    const listPRsImpl = vi.fn()
      .mockResolvedValueOnce({
        data: {
          data: {
            items: [{
              id: 101,
              scm_pr_id: 88,
              scm_pr_url: 'https://github.com/org/repo-a/pull/88',
              author: 'alice',
              title: 'Keep visible PR',
              source_branch: 'feat/a',
              target_branch: 'main',
              status: 'merged',
              labels: [],
              lines_added: 10,
              lines_deleted: 2,
              cycle_time_hours: 5,
              merged_at: '2026-03-30T00:00:00Z',
              created_at: '2026-03-29T00:00:00Z',
              usage_input_tokens: 1200,
              usage_output_tokens: 500,
              usage_cached_input_tokens: 300,
              usage_reasoning_tokens: 80,
              usage_credit_usage: 1.25,
              usage_request_count: 4,
              usage_commit_count: 1,
              usage_refreshed_at: '2026-03-30T01:00:00Z',
            }],
            total: 1,
          },
        },
      })
      .mockRejectedValueOnce(new Error('backend timeout'))

    const { wrapper } = await mountRepoDetail(undefined, undefined, { listPRsImpl })
    expect(wrapper.text()).toContain('Keep visible PR')

    await wrapper.get('[data-testid="repo-pr-range"] .el-select__wrapper').trigger('click')
    await flushPromises()
    const rangeOptions = Array.from(document.body.querySelectorAll<HTMLElement>('[role="option"]'))
      .filter((option) => option.textContent?.trim() === 'Last 6 months')
    rangeOptions[rangeOptions.length - 1]!.click()
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to load pull requests')
    expect(wrapper.text()).toContain('Retry')
    expect(wrapper.text()).toContain('Keep visible PR')
    expect(wrapper.text()).not.toContain('No pull requests recorded yet.')
  })

  it('renders PR usage freshness badge and commit reason', async () => {
    const { wrapper } = await mountRepoDetail(undefined, undefined, {
      prs: [{
        id: 101,
        scm_pr_id: 88,
        scm_pr_url: 'https://github.com/org/repo-a/pull/88',
        author: 'alice',
        title: 'Missing checkpoint',
        source_branch: 'feat/a',
        target_branch: 'main',
        status: 'merged',
        labels: [],
        lines_added: 10,
        lines_deleted: 2,
        cycle_time_hours: 5,
        merged_at: '2026-03-30T00:00:00Z',
        created_at: '2026-03-29T00:00:00Z',
        usage_status: 'no_checkpoint',
        usage_status_reason: 'No checkpoint matched this PR commit.',
      }],
      getPRImpl: vi.fn(async () => ({
        data: {
          data: {
            ...detailFor(101).data.data,
            usage_status: 'no_checkpoint',
            usage_status_reason: 'No checkpoint matched this PR commit.',
            commit_freshness: [{
              commit_sha: 'abc123',
              usage_status: 'no_checkpoint',
              usage_status_reason: 'No checkpoint for this commit',
              checkpoint_found: false,
              usage_event_found: false,
            }],
          },
        },
      })),
    })

    expect(wrapper.text()).toContain('Missing commit record')
    const detailsButton = wrapper.findAll('button').find((b) => b.text() === 'Details')
    await detailsButton!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('No checkpoint for this commit')
  })

  it('presents repository and PR status with Element Plus tags', async () => {
    const { wrapper } = await mountRepoDetail()

    expect(wrapper.findAll('.el-tag').length).toBeGreaterThan(0)
    expect(wrapper.get('[data-testid="repo-pr-row"] .el-tag').text()).toBe('Merged')
  })

  it('presents an inactive repository with an operator-facing status label', async () => {
    const { wrapper } = await mountRepoDetail({ status: 'inactive' })

    expect(wrapper.text()).toContain('Inactive')
    expect(wrapper.text()).not.toContain('Unknown')
  })

  it('shows PR sync error message', async () => {
    const { syncPRs } = await import('@/api/pr')
    ;(syncPRs as any).mockRejectedValue({ response: { data: { message: 'sync failed: upstream timeout' } } })

    const { wrapper } = await mountRepoDetail()
    const syncButton = wrapper.findAll('button').find((b) => b.text() === 'Sync PRs')
    expect(syncButton).toBeTruthy()

    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('sync failed: upstream timeout')
  })

  it('presents repository errors with Element Plus alerts', async () => {
    const { syncPRs } = await import('@/api/pr')
    ;(syncPRs as any).mockRejectedValue({ response: { data: { message: 'sync failed: upstream timeout' } } })
    const { wrapper } = await mountRepoDetail()

    const syncButton = wrapper.findAll('button').find((button) => button.text() === 'Sync PRs')
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-alert--error').exists()).toBe(true)
  })
})
