import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import SessionDetailView from '@/views/sessions/SessionDetailView.vue'

vi.mock('@/api/session', () => ({
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
      { path: '/sessions', component: { template: '<div>Sessions</div>' } },
      { path: '/sessions/:id', component: SessionDetailView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/settings', component: { template: '<div>Settings</div>' } },
    ],
  })
}

describe('SessionDetailView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('renders provider/runtime and workspace/checkpoint details', async () => {
    const { getSession } = await import('@/api/session')
    ;(getSession as any).mockResolvedValue({
      data: {
        data: {
          id: 'sess-1',
          branch: 'feat/x',
          status: 'active',
          started_at: '2026-03-30T00:00:00Z',
          ended_at: null,
          provider_name: 'codex',
          relay_api_key_id: 777,
          runtime_ref: 'runtime/sess-1',
          initial_workspace_root: '/workspace/root',
          tool_invocations: [],
          last_seen_at: '2026-03-30T01:00:00Z',
          edges: {
            session_workspaces: [{
              workspace_id: 'ws-1',
              workspace_root: '/workspace/root',
              git_dir: '/workspace/root/.git',
              git_common_dir: '/workspace/root/.git',
              first_seen_at: '2026-03-30T00:00:00Z',
              last_seen_at: '2026-03-30T01:00:00Z',
              binding_source: 'marker',
            }],
            commit_checkpoints: [{
              event_id: 'evt-1',
              commit_sha: 'abc12345def67890',
              parent_shas: ['aaa111'],
              workspace_id: 'ws-1',
              binding_source: 'marker',
              captured_at: '2026-03-30T00:30:00Z',
            }],
          },
        },
      },
    })

    const router = createTestRouter()
    await router.push('/sessions/sess-1')
    await router.isReady()

    const wrapper = mount(SessionDetailView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('codex')
    expect(wrapper.text()).toContain('/workspace/root')
    expect(wrapper.text()).toContain('abc12345def67890')
    expect(wrapper.text()).toContain('marker')
  })

  it('replaces to session list when session load fails', async () => {
    const { getSession } = await import('@/api/session')
    ;(getSession as any).mockRejectedValue(new Error('not found'))

    const router = createTestRouter()
    const replaceSpy = vi.spyOn(router, 'replace')
    await router.push('/sessions/missing')
    await router.isReady()

    mount(SessionDetailView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(replaceSpy).toHaveBeenCalledWith('/sessions')
  })

  it('reloads when only the route param changes', async () => {
    const { getSession } = await import('@/api/session')
    ;(getSession as any)
      .mockResolvedValueOnce({
        data: { data: { id: 'sess-1', branch: 'feat/x', status: 'active', started_at: '2026-03-30T00:00:00Z', ended_at: null, tool_invocations: [] } },
      })
      .mockResolvedValueOnce({
        data: { data: { id: 'sess-2', branch: 'feat/y', status: 'completed', started_at: '2026-03-31T00:00:00Z', ended_at: null, tool_invocations: [] } },
      })

    const router = createTestRouter()
    await router.push('/sessions/sess-1')
    await router.isReady()

    const wrapper = mount(SessionDetailView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('sess-1')

    await router.push('/sessions/sess-2')
    await flushPromises()

    expect((getSession as any).mock.calls.at(-1)?.[0]).toBe('sess-2')
    expect(wrapper.text()).toContain('sess-2')
  })

  it('ignores stale async responses after route param changes', async () => {
    let resolveFirst: ((value: any) => void) | undefined
    let resolveSecond: ((value: any) => void) | undefined
    const first = new Promise((resolve) => { resolveFirst = resolve })
    const second = new Promise((resolve) => { resolveSecond = resolve })

    const { getSession } = await import('@/api/session')
    ;(getSession as any)
      .mockReturnValueOnce(first)
      .mockReturnValueOnce(second)

    const router = createTestRouter()
    const replaceSpy = vi.spyOn(router, 'replace')
    await router.push('/sessions/sess-1')
    await router.isReady()

    const wrapper = mount(SessionDetailView, {
      global: { plugins: [createPinia(), router] },
    })

    await router.push('/sessions/sess-2')
    await flushPromises()

    resolveSecond!({
      data: { data: { id: 'sess-2', branch: 'feat/y', status: 'active', started_at: '2026-03-31T00:00:00Z', ended_at: null, tool_invocations: [] } },
    })
    await flushPromises()

    resolveFirst!({
      data: { data: { id: 'sess-1', branch: 'feat/x', status: 'active', started_at: '2026-03-30T00:00:00Z', ended_at: null, tool_invocations: [] } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('sess-2')
    expect(wrapper.text()).not.toContain('sess-1')
    expect(replaceSpy).not.toHaveBeenCalled()
  })
})
