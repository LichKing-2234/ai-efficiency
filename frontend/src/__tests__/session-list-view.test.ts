import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import SessionListView from '@/views/sessions/SessionListView.vue'
import SessionDetailView from '@/views/sessions/SessionDetailView.vue'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/session', () => ({
  listSessions: vi.fn(),
  getSession: vi.fn(),
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
      { path: '/sessions', name: 'SessionList', component: SessionListView },
      { path: '/sessions/:id', name: 'SessionDetail', component: SessionDetailView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

function buildListResponse() {
  return {
    data: {
      data: {
        items: [{
          id: 'sess-1',
          branch: 'feat/x',
          status: 'active',
          started_at: '2026-03-30T00:00:00Z',
          ended_at: null,
          provider_name: 'sub2api-claude',
          relay_api_key_id: 123,
          last_seen_at: '2026-03-30T01:23:45Z',
          tool_invocations: [],
          edges: {
            repo_config: { full_name: 'org/repo' },
            user: { id: 9, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'sso' },
          },
        }],
        total: 1,
      },
    },
  }
}

function buildListResponseWithTotal(total: number) {
  const res = buildListResponse()
  res.data.data.total = total
  return res
}

describe('SessionListView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('navigates to session detail from the view action', async () => {
    const { listSessions, getSession } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue(buildListResponse())
    ;(getSession as any).mockResolvedValue({ data: { data: { id: 'sess-1', branch: 'feat/x', status: 'active', started_at: '2026-03-30T00:00:00Z', ended_at: null, tool_invocations: [] } } })

    const router = createTestRouter()

    await router.push('/sessions')
    await router.isReady()

    const wrapper = mount(SessionListView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const viewButton = wrapper.findAll('button').find((b) => b.text() === 'View')
    expect(viewButton).toBeTruthy()
    await viewButton!.trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.name).toBe('SessionDetail')
    expect(router.currentRoute.value.params.id).toBe('sess-1')
  })

  it('renders owner filters for admins and sends repo, branch, and owner query filters', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const { listSessions } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue(buildListResponse())

    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const router = createTestRouter()
    await router.push('/sessions')
    await router.isReady()

    const wrapper = mount(SessionListView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    expect(listSessions).toHaveBeenCalledWith({ page: 1, page_size: 20, owner_scope: 'all' })

    const repoQueryInput = wrapper.find('input[name="repo_query"]')
    const branchInput = wrapper.find('input[name="branch"]')
    const ownerQueryInput = wrapper.find('input[name="owner_query"]')
    const ownerScopeSelect = wrapper.find('select[name="owner_scope"]')
    const applyButton = wrapper.find('button[type="button"][data-testid="apply-session-filters"]')

    expect(repoQueryInput.exists()).toBe(true)
    expect(branchInput.exists()).toBe(true)
    expect(ownerQueryInput.exists()).toBe(true)
    expect(ownerScopeSelect.exists()).toBe(true)

    await repoQueryInput.setValue('team/repo')
    await branchInput.setValue('feat/session-filter')
    await ownerQueryInput.setValue('alice')
    await ownerScopeSelect.setValue('all')
    await applyButton.trigger('click')
    await flushPromises()

    expect(listSessions).toHaveBeenLastCalledWith({
      page: 1,
      page_size: 20,
      repo_query: 'team/repo',
      branch: 'feat/session-filter',
      owner_query: 'alice',
      owner_scope: 'all',
    })
  })

  it('hides owner scope for non-admin users and forces mine visibility', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const { listSessions } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue(buildListResponse())

    const auth = useAuthStore(pinia)
    auth.user = { id: 2, username: 'member', email: 'member@example.com', role: 'user', auth_source: 'sso' }

    const router = createTestRouter()
    await router.push('/sessions')
    await router.isReady()

    const wrapper = mount(SessionListView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    expect(wrapper.find('select[name="owner_scope"]').exists()).toBe(false)
    expect(listSessions).toHaveBeenCalledWith({ page: 1, page_size: 20, owner_scope: 'mine' })
  })

  it('renders owner, provider, relay key id, and last seen summary for each session', async () => {
    const { listSessions } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue(buildListResponse())

    const router = createTestRouter()
    await router.push('/sessions')
    await router.isReady()

    const wrapper = mount(SessionListView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('sub2api-claude')
    expect(wrapper.text()).toContain('123')
    expect(wrapper.text()).toContain(new Date('2026-03-30T01:23:45Z').toLocaleString())
  })

  it('does not silently apply draft repo/branch/owner filters on status or pagination changes', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const { listSessions } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue(buildListResponseWithTotal(40))

    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const router = createTestRouter()
    await router.push('/sessions')
    await router.isReady()

    const wrapper = mount(SessionListView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    // Draft values (do NOT click Apply)
    await wrapper.find('input[name="repo_query"]').setValue('draft/repo')
    await wrapper.find('input[name="branch"]').setValue('draft-branch')
    await wrapper.find('select[name="owner_scope"]').setValue('unowned')

    // Status changes should fetch immediately, but must use last applied filters (defaults).
    await wrapper.find('select[name="status"]').setValue('completed')
    await flushPromises()

    expect(listSessions).toHaveBeenLastCalledWith({
      page: 1,
      page_size: 20,
      status: 'completed',
      owner_scope: 'all',
    })

    // Pagination should also use only applied filters.
    const nextButton = wrapper.findAll('button').find((b) => b.text() === 'Next')
    expect(nextButton).toBeTruthy()
    await nextButton!.trigger('click')
    await flushPromises()

    expect(listSessions).toHaveBeenLastCalledWith({
      page: 2,
      page_size: 20,
      status: 'completed',
      owner_scope: 'all',
    })
  })

  it('keeps using the last applied repo/branch/owner filters after drafts change', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const { listSessions } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue(buildListResponseWithTotal(40))

    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const router = createTestRouter()
    await router.push('/sessions')
    await router.isReady()

    const wrapper = mount(SessionListView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    const repoQueryInput = wrapper.find('input[name="repo_query"]')
    const branchInput = wrapper.find('input[name="branch"]')
    const ownerScopeSelect = wrapper.find('select[name="owner_scope"]')
    const applyButton = wrapper.find('button[type="button"][data-testid="apply-session-filters"]')

    await repoQueryInput.setValue('applied/repo')
    await branchInput.setValue('applied-branch')
    await ownerScopeSelect.setValue('unowned')
    await applyButton.trigger('click')
    await flushPromises()

    expect(listSessions).toHaveBeenLastCalledWith({
      page: 1,
      page_size: 20,
      repo_query: 'applied/repo',
      branch: 'applied-branch',
      owner_scope: 'unowned',
    })

    await repoQueryInput.setValue('draft/repo')
    await branchInput.setValue('draft-branch')
    await ownerScopeSelect.setValue('all')

    await wrapper.find('select[name="status"]').setValue('completed')
    await flushPromises()

    expect(listSessions).toHaveBeenLastCalledWith({
      page: 1,
      page_size: 20,
      status: 'completed',
      repo_query: 'applied/repo',
      branch: 'applied-branch',
      owner_scope: 'unowned',
    })

    const nextButton = wrapper.findAll('button').find((b) => b.text() === 'Next')
    expect(nextButton).toBeTruthy()
    await nextButton!.trigger('click')
    await flushPromises()

    expect(listSessions).toHaveBeenLastCalledWith({
      page: 2,
      page_size: 20,
      status: 'completed',
      repo_query: 'applied/repo',
      branch: 'applied-branch',
      owner_scope: 'unowned',
    })
  })

  it('reset clears draft inputs and applied filters before fetching', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const { listSessions } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue(buildListResponse())

    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const router = createTestRouter()
    await router.push('/sessions')
    await router.isReady()

    const wrapper = mount(SessionListView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    const repoQueryInput = wrapper.find('input[name="repo_query"]')
    const branchInput = wrapper.find('input[name="branch"]')
    const ownerScopeSelect = wrapper.find('select[name="owner_scope"]')
    const applyButton = wrapper.find('button[type="button"][data-testid="apply-session-filters"]')
    const resetButton = wrapper.find('button[data-test="reset-filters"]')
    const statusSelect = wrapper.find('select[name="status"]')

    await repoQueryInput.setValue('applied/repo')
    await branchInput.setValue('applied-branch')
    await ownerScopeSelect.setValue('unowned')
    await applyButton.trigger('click')
    await flushPromises()

    await statusSelect.setValue('completed')
    await flushPromises()

    await repoQueryInput.setValue('draft/repo')
    await branchInput.setValue('draft-branch')
    await ownerScopeSelect.setValue('mine')
    await resetButton.trigger('click')
    await flushPromises()

    expect((repoQueryInput.element as HTMLInputElement).value).toBe('')
    expect((branchInput.element as HTMLInputElement).value).toBe('')
    expect((ownerScopeSelect.element as HTMLSelectElement).value).toBe('all')
    expect((statusSelect.element as HTMLSelectElement).value).toBe('')

    expect(listSessions).toHaveBeenLastCalledWith({
      page: 1,
      page_size: 20,
      owner_scope: 'all',
    })
  })

  it('clears and suppresses owner query when owner scope switches to unowned', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)

    const { listSessions } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue(buildListResponse())

    const auth = useAuthStore(pinia)
    auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'sso' }

    const router = createTestRouter()
    await router.push('/sessions')
    await router.isReady()

    const wrapper = mount(SessionListView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    const ownerQueryInput = wrapper.find('input[name="owner_query"]')
    const ownerScopeSelect = wrapper.find('select[name="owner_scope"]')
    const applyButton = wrapper.find('button[type="button"][data-testid="apply-session-filters"]')

    await ownerQueryInput.setValue('alice')
    await ownerScopeSelect.setValue('unowned')
    await flushPromises()

    expect((ownerQueryInput.element as HTMLInputElement).value).toBe('')

    await applyButton.trigger('click')
    await flushPromises()

    expect(listSessions).toHaveBeenLastCalledWith({
      page: 1,
      page_size: 20,
      owner_scope: 'unowned',
    })
  })
})
