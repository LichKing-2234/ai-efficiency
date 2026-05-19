import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { Pinia } from 'pinia'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
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
  settlePR: vi.fn(),
}))

vi.mock('@/api/scmProvider', () => ({
  listProviders: vi.fn().mockResolvedValue({
    data: { data: [{ id: 1, name: 'GitHub', type: 'github', base_url: 'https://api.github.com', status: 'active' }] },
  }),
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/repos/:id', component: RepoDetailView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

function createDeferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

function detailFor(prId: number, scmPRId = prId - 13) {
  return {
    data: {
      data: {
        id: prId,
        scm_pr_id: scmPRId,
        scm_pr_url: `https://github.com/org/repo-a/pull/${scmPRId}`,
        author: 'alice',
        title: 'Add attribution',
        source_branch: 'feat/a',
        target_branch: 'main',
        status: 'merged',
        labels: [],
        lines_added: 10,
        lines_deleted: 2,
        ai_label: 'ai_via_sub2api',
        ai_ratio: 0.8,
        token_cost: 3.2,
        cycle_time_hours: 5,
        merged_at: '2026-03-30T00:00:00Z',
        created_at: '2026-03-29T00:00:00Z',
        attribution_status: 'clear',
        attribution_confidence: 'high',
        primary_token_count: 1200,
        primary_token_cost: 1.25,
        metadata_summary: {
          intervals: [{
            commit_sha: 'abc123',
            total_tokens: 1200,
            total_cost: 1.25,
            source: 'tool_usage_events',
            checkpoint_id: 7,
          }],
        },
        last_attributed_at: '2026-03-30T01:00:00Z',
        edges: {
          last_attribution_run: {
            matched_commit_shas: ['abc123', 'def456'],
            validation_summary: { reason: 'all_matched_checkpoints_bound', result: 'consistent' },
          },
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
  },
) {
  const { getRepo } = await import('@/api/repo')
  const { listPRs, getPR, settlePR } = await import('@/api/pr')
  const prItems = options?.prs ?? [{
    id: 101,
    scm_pr_id: 88,
    scm_pr_url: 'https://github.com/org/repo-a/pull/88',
    author: 'alice',
    title: 'Add attribution',
    source_branch: 'feat/a',
    target_branch: 'main',
    status: 'merged',
    labels: [],
    lines_added: 10,
    lines_deleted: 2,
    ai_label: 'ai_via_sub2api',
    ai_ratio: 0.8,
    token_cost: 3.2,
    cycle_time_hours: 5,
    merged_at: '2026-03-30T00:00:00Z',
    created_at: '2026-03-29T00:00:00Z',
    attribution_status: 'clear',
    attribution_confidence: 'high',
    primary_token_count: 1200,
    primary_token_cost: 1.25,
    metadata_summary: {},
    last_attributed_at: '2026-03-30T01:00:00Z',
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
        last_scan_at: '2026-03-30T00:00:00Z',
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
  ;(settlePR as any).mockResolvedValue({ data: { data: { attribution_status: 'clear' } } })
  if (options?.getPRImpl) {
    ;(getPR as any).mockImplementation(options.getPRImpl)
  } else {
    ;(getPR as any).mockResolvedValue(detailFor(101, 88))
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
  return { wrapper, listPRs, getPR, settlePR }
}

describe('RepoDetailView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('renders attribution columns and primary cost', async () => {
    const { wrapper } = await mountRepoDetail()
    expect(wrapper.text()).toContain('clear')
    expect(wrapper.text()).toContain('high')
    expect(wrapper.text()).toContain('$1.25')
  })

  it('settles PR and refreshes the list', async () => {
    const { wrapper, listPRs, settlePR } = await mountRepoDetail()
    const settleButton = wrapper.findAll('button').find((b) => b.text() === 'Settle')
    expect(settleButton).toBeTruthy()

    await settleButton!.trigger('click')
    await flushPromises()

    expect(settlePR).toHaveBeenCalledWith(101)
    expect((listPRs as any).mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('loads and renders attribution details for a PR', async () => {
    const { wrapper, getPR } = await mountRepoDetail()
    const detailsButton = wrapper.findAll('button').find((b) => b.text() === 'Details')
    expect(detailsButton).toBeTruthy()

    await detailsButton!.trigger('click')
    await flushPromises()

    expect(getPR).toHaveBeenCalledWith(101)
    expect(wrapper.text()).toContain('1,200')
    expect(wrapper.text()).toContain('all_matched_checkpoints_bound')
    expect(wrapper.text()).toContain('abc123')
    expect(wrapper.text()).toContain('def456')
    expect(wrapper.text()).toContain('tool_usage_events')
  })

  it('keeps the active PR details button loading until its own request finishes', async () => {
    const firstDetail = createDeferred<any>()
    const secondDetail = createDeferred<any>()
    const { wrapper, getPR } = await mountRepoDetail(undefined, undefined, {
      prs: [
        {
          id: 101,
          scm_pr_id: 88,
          scm_pr_url: 'https://github.com/org/repo-a/pull/88',
          author: 'alice',
          title: 'Add attribution',
          source_branch: 'feat/a',
          target_branch: 'main',
          status: 'merged',
          labels: [],
          lines_added: 10,
          lines_deleted: 2,
          ai_label: 'ai_via_sub2api',
          ai_ratio: 0.8,
          token_cost: 3.2,
          cycle_time_hours: 5,
          merged_at: '2026-03-30T00:00:00Z',
          created_at: '2026-03-29T00:00:00Z',
          attribution_status: 'clear',
          attribution_confidence: 'high',
          primary_token_count: 1200,
          primary_token_cost: 1.25,
          metadata_summary: {},
          last_attributed_at: '2026-03-30T01:00:00Z',
        },
        {
          id: 102,
          scm_pr_id: 89,
          scm_pr_url: 'https://github.com/org/repo-a/pull/89',
          author: 'bob',
          title: 'Refine attribution',
          source_branch: 'feat/b',
          target_branch: 'main',
          status: 'open',
          labels: [],
          lines_added: 4,
          lines_deleted: 1,
          ai_label: 'ai_via_sub2api',
          ai_ratio: 0.6,
          token_cost: 1.1,
          cycle_time_hours: 2,
          merged_at: null,
          created_at: '2026-03-30T00:00:00Z',
          attribution_status: 'clear',
          attribution_confidence: 'medium',
          primary_token_count: 400,
          primary_token_cost: 0.5,
          metadata_summary: {},
          last_attributed_at: '2026-03-30T02:00:00Z',
        },
      ],
      getPRImpl: vi.fn((prId: number) => {
        if (prId === 101) return firstDetail.promise
        return secondDetail.promise
      }),
    })

    const detailButtons = () => wrapper.findAll('button').filter((button) => ['Details', 'Hide', 'Loading...'].includes(button.text()))

    await detailButtons()[0].trigger('click')
    await nextTick()
    await detailButtons()[1].trigger('click')
    await nextTick()

    expect(getPR).toHaveBeenNthCalledWith(1, 101)
    expect(getPR).toHaveBeenNthCalledWith(2, 102)
    expect(wrapper.findAll('button').some((button) => button.text() === 'Loading...')).toBe(true)

    firstDetail.resolve(detailFor(101, 88))
    await flushPromises()

    expect(wrapper.findAll('button').some((button) => button.text() === 'Loading...')).toBe(true)

    secondDetail.resolve(detailFor(102, 89))
    await flushPromises()

    expect(wrapper.findAll('button').some((button) => button.text() === 'Loading...')).toBe(false)
  })

  it('does not issue a duplicate details request for the same PR while one is already in flight', async () => {
    const firstDetail = createDeferred<any>()
    const secondDetail = createDeferred<any>()
    const { wrapper, getPR } = await mountRepoDetail(undefined, undefined, {
      prs: [
        {
          id: 101,
          scm_pr_id: 88,
          scm_pr_url: 'https://github.com/org/repo-a/pull/88',
          author: 'alice',
          title: 'Add attribution',
          source_branch: 'feat/a',
          target_branch: 'main',
          status: 'merged',
          labels: [],
          lines_added: 10,
          lines_deleted: 2,
          ai_label: 'ai_via_sub2api',
          ai_ratio: 0.8,
          token_cost: 3.2,
          cycle_time_hours: 5,
          merged_at: '2026-03-30T00:00:00Z',
          created_at: '2026-03-29T00:00:00Z',
          attribution_status: 'clear',
          attribution_confidence: 'high',
          primary_token_count: 1200,
          primary_token_cost: 1.25,
          metadata_summary: {},
          last_attributed_at: '2026-03-30T01:00:00Z',
        },
        {
          id: 102,
          scm_pr_id: 89,
          scm_pr_url: 'https://github.com/org/repo-a/pull/89',
          author: 'bob',
          title: 'Refine attribution',
          source_branch: 'feat/b',
          target_branch: 'main',
          status: 'open',
          labels: [],
          lines_added: 4,
          lines_deleted: 1,
          ai_label: 'ai_via_sub2api',
          ai_ratio: 0.6,
          token_cost: 1.1,
          cycle_time_hours: 2,
          merged_at: null,
          created_at: '2026-03-30T00:00:00Z',
          attribution_status: 'clear',
          attribution_confidence: 'medium',
          primary_token_count: 400,
          primary_token_cost: 0.5,
          metadata_summary: {},
          last_attributed_at: '2026-03-30T02:00:00Z',
        },
      ],
      getPRImpl: vi.fn((prId: number) => {
        if (prId === 101) return firstDetail.promise
        return secondDetail.promise
      }),
    })

    const detailButtons = () => wrapper.findAll('button').filter((button) => ['Details', 'Hide', 'Loading...'].includes(button.text()))

    await detailButtons()[0].trigger('click')
    await nextTick()
    await detailButtons()[1].trigger('click')
    await nextTick()
    await detailButtons()[0].trigger('click')
    await nextTick()

    expect(getPR).toHaveBeenCalledTimes(2)
    expect(getPR).toHaveBeenNthCalledWith(1, 101)
    expect(getPR).toHaveBeenNthCalledWith(2, 102)

    firstDetail.resolve(detailFor(101, 88))
    secondDetail.resolve(detailFor(102, 89))
    await flushPromises()
  })

  it('collapses PR details when loading the detail request fails', async () => {
    const { wrapper } = await mountRepoDetail(undefined, undefined, {
      getPRImpl: vi.fn(async () => {
        throw new Error('detail fetch failed')
      }),
    })

    const detailsButton = wrapper.findAll('button').find((button) => button.text() === 'Details')
    expect(detailsButton).toBeTruthy()

    await detailsButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Matched Commits')
    expect(wrapper.findAll('button').some((button) => button.text() === 'Hide')).toBe(false)
    expect(wrapper.findAll('button').some((button) => button.text() === 'Details')).toBe(true)
  })

  it('keeps previously loaded PR details when settle refresh fails', async () => {
    let detailCalls = 0
    const { wrapper, getPR, settlePR } = await mountRepoDetail(undefined, undefined, {
      getPRImpl: vi.fn(async () => {
        detailCalls += 1
        if (detailCalls === 1) {
          return detailFor(101, 88)
        }
        throw new Error('refresh failed')
      }),
    })

    const detailsButton = wrapper.findAll('button').find((button) => button.text() === 'Details')
    expect(detailsButton).toBeTruthy()

    await detailsButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('abc123')
    expect(wrapper.text()).toContain('def456')

    const settleButton = wrapper.findAll('button').find((button) => button.text() === 'Settle')
    expect(settleButton).toBeTruthy()

    await settleButton!.trigger('click')
    await flushPromises()

    expect(settlePR).toHaveBeenCalledWith(101)
    expect(getPR).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('abc123')
    expect(wrapper.text()).toContain('def456')
    expect(wrapper.findAll('button').some((button) => button.text() === 'Hide')).toBe(true)
  })

  it('forces a fresh PR detail fetch after settle even if an older request was already in flight', async () => {
    const firstDetail = createDeferred<any>()
    let detailCalls = 0
    const { wrapper, getPR, settlePR } = await mountRepoDetail(undefined, undefined, {
      getPRImpl: vi.fn(async () => {
        detailCalls += 1
        if (detailCalls === 1) {
          return firstDetail.promise
        }
        return detailFor(101, 88)
      }),
    })

    const detailsButton = wrapper.findAll('button').find((button) => button.text() === 'Details')
    const settleButton = wrapper.findAll('button').find((button) => button.text() === 'Settle')
    expect(detailsButton).toBeTruthy()
    expect(settleButton).toBeTruthy()

    await detailsButton!.trigger('click')
    await nextTick()
    await settleButton!.trigger('click')
    await nextTick()

    expect(getPR).toHaveBeenCalledTimes(1)

    firstDetail.resolve(detailFor(101, 88))
    await flushPromises()

    expect(settlePR).toHaveBeenCalledWith(101)
    expect(getPR).toHaveBeenCalledTimes(2)
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
      last_scan_at: null,
      edges: {},
    }, pinia)
    expect(wrapper.text()).toContain('SCM Provider Binding')
    expect(wrapper.text()).toContain('auto-discovered by ae-cli attribution sync')
    expect(wrapper.text()).not.toContain('Run Scan')
    expect(wrapper.text()).not.toContain('Auto-Optimize')
  })
})
