import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import RepoDetailView from '@/views/repos/RepoDetailView.vue'

vi.mock('@/api/repo', () => ({
  getRepo: vi.fn(),
  updateRepo: vi.fn(),
}))

vi.mock('@/api/analysis', () => ({
  triggerScan: vi.fn(),
  listScans: vi.fn(),
}))

vi.mock('@/api/pr', () => ({
  listPRs: vi.fn(),
  syncPRs: vi.fn(),
  settlePR: vi.fn(),
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
      { path: '/sessions', component: { template: '<div>Sessions</div>' } },
    ],
  })
}

async function mountRepoDetail() {
  const { getRepo } = await import('@/api/repo')
  const { listScans } = await import('@/api/analysis')
  const { listPRs, settlePR } = await import('@/api/pr')

  ;(getRepo as any).mockResolvedValue({
    data: {
      data: {
        id: 9,
        name: 'repo-a',
        full_name: 'org/repo-a',
        clone_url: 'https://github.com/org/repo-a.git',
        default_branch: 'main',
        ai_score: 82,
        status: 'active',
        last_scan_at: '2026-03-30T00:00:00Z',
        group_id: 1,
        created_at: '2026-01-01T00:00:00Z',
      },
    },
  })
  ;(listScans as any).mockResolvedValue({ data: { data: [] } })
  ;(listPRs as any).mockResolvedValue({
    data: {
      data: {
        items: [{
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
        }],
        total: 1,
      },
    },
  })
  ;(settlePR as any).mockResolvedValue({ data: { data: { attribution_status: 'clear' } } })

  const router = createTestRouter()
  await router.push('/repos/9')
  await router.isReady()

  const wrapper = mount(RepoDetailView, {
    global: {
      plugins: [createPinia(), router],
      stubs: {
        RepoChat: { template: '<div />' },
      },
    },
  })

  await flushPromises()
  return { wrapper, listPRs, settlePR }
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
})
