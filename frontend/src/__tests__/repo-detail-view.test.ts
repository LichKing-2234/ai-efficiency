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
}))

vi.mock('@/api/pr', () => ({
  listPRs: vi.fn(),
  getPR: vi.fn(),
  syncPRs: vi.fn(),
  getPRSyncJob: vi.fn(),
  refreshPRUsage: vi.fn(),
}))

vi.mock('@/api/scmProvider', () => ({
  listProviders: vi.fn().mockResolvedValue({
    data: { data: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' }] },
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
    prs?: Record<string, unknown>[]
    total?: number
    summary?: Record<string, unknown>
    getPRImpl?: (prId: number) => Promise<any>
    refreshPRUsageImpl?: (prId: number) => Promise<any>
  },
) {
  const { getRepo } = await import('@/api/repo')
  const { listPRs, getPR, refreshPRUsage } = await import('@/api/pr')
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
  ;(listPRs as any).mockResolvedValue({
    data: {
      data: {
        items: prItems,
        total: options?.total ?? prItems.length,
        ...(options?.summary ? { summary: options.summary } : {}),
      },
    },
  })
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
  await router.push('/repos/9')
  await router.isReady()

  const activePinia = pinia ?? createPinia()
  const wrapper = mount(RepoDetailView, {
    global: {
      plugins: [activePinia, router],
    },
  })

  await flushPromises()
  return { wrapper, getPR, refreshPRUsage }
}

describe('RepoDetailView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('renders conclusion-first PR usage summary and readable default columns', async () => {
    const { wrapper } = await mountRepoDetail()
    expect(wrapper.text()).toContain('Repository health')
    expect(wrapper.text()).toContain('PR Usage Summary')
    expect(wrapper.text()).toContain('Total PRs')
    expect(wrapper.text()).toContain('With AI usage')
    expect(wrapper.text()).toContain('Token usage')
    expect(wrapper.text()).toContain('Refreshed')
    expect(wrapper.text()).not.toContain('Cache')
    expect(wrapper.text()).not.toContain('Reasoning')
    expect(wrapper.text()).not.toContain('AI Label')
    expect(wrapper.text()).not.toContain('Confidence')
    expect(wrapper.text()).not.toContain('Settle')
    expect(wrapper.text()).toContain('2,000')
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
    const pendingCard = wrapper.findAll('.rounded-md').find((card) => card.text().includes('Pending upload'))
    const noCheckpointCard = wrapper.findAll('.rounded-md').find((card) => card.text().includes('No checkpoint'))
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
    expect(wrapper.text()).toContain('默认分支')
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

  it('adds noopener protection to external PR links', async () => {
    const { wrapper } = await mountRepoDetail()
    const link = wrapper.find('a[href="https://github.com/org/repo-a/pull/88"]')

    expect(link.exists()).toBe(true)
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
  })

  it('shows binding controls for admin on an unbound repo', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const { wrapper } = await mountRepoDetail({
      binding_state: 'unbound',
      edges: {},
    }, pinia)
    expect(wrapper.text()).toContain('Code Platform Binding')
    expect(wrapper.text()).toContain('auto-discovered by ae-cli')
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

    expect(wrapper.text()).toContain('No checkpoint')
    const detailsButton = wrapper.findAll('button').find((b) => b.text() === 'Details')
    await detailsButton!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('No checkpoint for this commit')
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
})
