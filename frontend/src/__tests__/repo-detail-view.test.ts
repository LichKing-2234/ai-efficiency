import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { Pinia } from 'pinia'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import RepoDetailView from '@/views/repos/RepoDetailView.vue'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/repo', () => ({
  getRepo: vi.fn(),
  updateRepo: vi.fn(),
}))

vi.mock('@/api/pr', () => ({
  listPRs: vi.fn(),
  getPR: vi.fn(),
  syncPRs: vi.fn(),
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
        total: prItems.length,
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
    vi.clearAllMocks()
  })

  it('renders usage summary columns and does not render attribution UI', async () => {
    const { wrapper } = await mountRepoDetail()
    expect(wrapper.text()).toContain('Input')
    expect(wrapper.text()).toContain('Output')
    expect(wrapper.text()).toContain('Credits')
    expect(wrapper.text()).not.toContain('AI Label')
    expect(wrapper.text()).not.toContain('Confidence')
    expect(wrapper.text()).not.toContain('Settle')
    expect(wrapper.text()).toContain('1,200')
    expect(wrapper.text()).toContain('1.25')
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
    auth.user = { id: 1, username: 'admin', email: 'a@b.com', role: 'admin', auth_source: 'sso' }

    const { wrapper } = await mountRepoDetail({
      binding_state: 'unbound',
      edges: {},
    }, pinia)
    expect(wrapper.text()).toContain('SCM Provider Binding')
    expect(wrapper.text()).toContain('auto-discovered by ae-cli attribution sync')
  })
})
