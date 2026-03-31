import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import SessionListView from '@/views/sessions/SessionListView.vue'
import SessionDetailView from '@/views/sessions/SessionDetailView.vue'

vi.mock('@/api/session', () => ({
  listSessions: vi.fn(),
  getSession: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getMe: vi.fn(),
  devLogin: vi.fn(),
}))

describe('SessionListView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('navigates to session detail from the view action', async () => {
    const { listSessions, getSession } = await import('@/api/session')
    ;(listSessions as any).mockResolvedValue({
      data: {
        data: {
          items: [{
            id: 'sess-1',
            branch: 'feat/x',
            status: 'active',
            started_at: '2026-03-30T00:00:00Z',
            ended_at: null,
            tool_invocations: [],
            edges: { repo_config: { full_name: 'org/repo' } },
          }],
          total: 1,
        },
      },
    })
    ;(getSession as any).mockResolvedValue({ data: { data: { id: 'sess-1', branch: 'feat/x', status: 'active', started_at: '2026-03-30T00:00:00Z', ended_at: null, tool_invocations: [] } } })

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/sessions', name: 'SessionList', component: SessionListView },
        { path: '/sessions/:id', name: 'SessionDetail', component: SessionDetailView },
        { path: '/login', component: { template: '<div>Login</div>' } },
        { path: '/repos', component: { template: '<div>Repos</div>' } },
        { path: '/settings', component: { template: '<div>Settings</div>' } },
      ],
    })

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
})
