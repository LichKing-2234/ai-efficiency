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

  it('renders provider/runtime, workspace/checkpoint details, and usage token breakdown', async () => {
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
            session_usage_events: [{
              event_id: 'usage-1',
              provider_name: 'sub2api',
              model: 'claude-opus',
              started_at: '2026-03-30T00:31:00Z',
              finished_at: '2026-03-30T00:31:05Z',
              input_tokens: 100,
              output_tokens: 40,
              total_tokens: 140,
              status: 'completed',
              workspace_id: 'ws-1',
              raw_metadata: {
                cached_input_tokens: 333,
                reasoning_output_tokens: 444,
              },
              raw_response: {
                id: 'resp_1',
                usage: {
                  input_tokens: 100,
                  input_tokens_details: {
                    cached_tokens: 333,
                  },
                  output_tokens: 40,
                  output_tokens_details: {
                    reasoning_tokens: 444,
                  },
                  total_tokens: 140,
                },
              },
            }],
            agent_metadata_events: [{
              source: 'codex',
              source_session_id: 'codex-sess-1',
              usage_unit: 'token',
              input_tokens: 120,
              cached_input_tokens: 30,
              output_tokens: 25,
              reasoning_tokens: 10,
              credit_usage: 0,
              context_usage_pct: 0,
              observed_at: '2026-03-30T00:31:30Z',
              workspace_id: 'ws-1',
              raw_payload: {
                info: {
                  total_token_usage: {
                    cached_input_tokens: 30,
                    reasoning_output_tokens: 10,
                  },
                },
              },
            }],
            session_events: [{
              event_id: 'event-1',
              event_type: 'user_prompt_submit',
              source: 'codex_hook',
              captured_at: '2026-03-30T00:32:00Z',
              workspace_id: 'ws-1',
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
    expect(wrapper.text()).toContain('claude-opus')
    expect(wrapper.text()).toContain('Input')
    expect(wrapper.text()).toContain('Cached Input')
    expect(wrapper.text()).toContain('Output')
    expect(wrapper.text()).toContain('Reasoning')
    expect(wrapper.text()).toContain('Total')
    expect(wrapper.text()).toContain('100')
    expect(wrapper.text()).toContain('333')
    expect(wrapper.text()).toContain('40')
    expect(wrapper.text()).toContain('444')
    expect(wrapper.text()).toContain('140')
    expect(wrapper.text()).toContain('Agent Usage Snapshots')
    expect(wrapper.text()).toContain('Cached Input')
    expect(wrapper.text()).toContain('Reasoning')
    expect(wrapper.text()).toContain('codex-sess-1')
    expect(wrapper.text()).toContain('120')
    expect(wrapper.text()).toContain('30')
    expect(wrapper.text()).toContain('25')
    expect(wrapper.text()).toContain('10')
    expect(wrapper.text()).toContain('user_prompt_submit')
    expect(wrapper.text()).toContain('codex_hook')
  })

  it('expands raw response and raw event payloads', async () => {
    const { getSession } = await import('@/api/session')
    ;(getSession as any).mockResolvedValue({
      data: {
        data: {
          id: 'sess-raw',
          branch: 'feat/raw',
          status: 'active',
          started_at: '2026-03-30T00:00:00Z',
          ended_at: null,
          tool_invocations: [],
          edges: {
            session_usage_events: [{
              event_id: 'usage-raw-1',
              provider_name: 'sub2api',
              model: 'gpt-5.4',
              started_at: '2026-03-30T00:31:00Z',
              finished_at: '2026-03-30T00:31:05Z',
              input_tokens: 100,
              output_tokens: 40,
              total_tokens: 140,
              status: 'completed',
              workspace_id: 'ws-1',
              raw_metadata: {
                cached_input_tokens: 333,
                reasoning_output_tokens: 444,
                http_status: 200,
              },
              raw_response: {
                id: 'resp_1',
                usage: {
                  input_tokens: 100,
                  input_tokens_details: {
                    cached_tokens: 333,
                  },
                  output_tokens: 40,
                  output_tokens_details: {
                    reasoning_tokens: 444,
                  },
                  total_tokens: 140,
                },
              },
            }],
            agent_metadata_events: [{
              source: 'codex',
              source_session_id: 'codex-sess-1',
              usage_unit: 'token',
              input_tokens: 120,
              cached_input_tokens: 30,
              output_tokens: 25,
              reasoning_tokens: 10,
              credit_usage: 0,
              context_usage_pct: 0,
              observed_at: '2026-03-30T00:31:30Z',
              workspace_id: 'ws-1',
              raw_payload: {
                info: {
                  total_token_usage: {
                    cached_input_tokens: 30,
                    reasoning_output_tokens: 10,
                  },
                },
              },
            }],
          },
        },
      },
    })

    const router = createTestRouter()
    await router.push('/sessions/sess-raw')
    await router.isReady()

    const wrapper = mount(SessionDetailView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const rawResponseButtons = wrapper.findAll('button').filter((node) => node.text() === 'Raw Response')
    const rawEventButtons = wrapper.findAll('button').filter((node) => node.text() === 'Raw Event')
    const rawButtons = [...rawResponseButtons, ...rawEventButtons]
    expect(rawButtons).toHaveLength(2)

    await rawResponseButtons[0].trigger('click')
    expect(wrapper.text()).toContain('"id": "resp_1"')
    expect(wrapper.text()).toContain('"cached_tokens": 333')
    expect(wrapper.text()).toContain('"reasoning_tokens": 444')

    await rawEventButtons[0].trigger('click')
    expect(wrapper.text()).toContain('"total_token_usage"')
    expect(wrapper.text()).toContain('"cached_input_tokens": 30')
    expect(wrapper.text()).toContain('"reasoning_output_tokens": 10')
  })

  it('shows empty raw response and raw event labels when data is missing', async () => {
    const { getSession } = await import('@/api/session')
    ;(getSession as any).mockResolvedValue({
      data: {
        data: {
          id: 'sess-empty-raw',
          branch: 'feat/raw-empty',
          status: 'active',
          started_at: '2026-03-30T00:00:00Z',
          ended_at: null,
          tool_invocations: [],
          edges: {
            session_usage_events: [{
              event_id: 'usage-empty-raw-1',
              provider_name: 'sub2api',
              model: 'gpt-5.4',
              started_at: '2026-03-30T00:31:00Z',
              finished_at: '2026-03-30T00:31:05Z',
              input_tokens: 100,
              output_tokens: 40,
              total_tokens: 140,
              status: 'completed',
              workspace_id: 'ws-1',
            }],
            agent_metadata_events: [{
              source: 'codex',
              source_session_id: 'codex-sess-2',
              usage_unit: 'token',
              input_tokens: 120,
              cached_input_tokens: 30,
              output_tokens: 25,
              reasoning_tokens: 10,
              observed_at: '2026-03-30T00:31:30Z',
              workspace_id: 'ws-1',
            }],
          },
        },
      },
    })

    const router = createTestRouter()
    await router.push('/sessions/sess-empty-raw')
    await router.isReady()

    const wrapper = mount(SessionDetailView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const rawResponseButtons = wrapper.findAll('button').filter((node) => node.text() === 'Raw Response')
    const rawEventButtons = wrapper.findAll('button').filter((node) => node.text() === 'Raw Event')

    await rawResponseButtons[0].trigger('click')
    expect(wrapper.text()).toContain('No raw response.')

    await rawEventButtons[0].trigger('click')
    expect(wrapper.text()).toContain('No raw event.')
  })

  it('shows reasoning as dash only when the raw value is missing', async () => {
    const { getSession } = await import('@/api/session')
    ;(getSession as any).mockResolvedValue({
      data: {
        data: {
          id: 'sess-reasoning',
          branch: 'feat/reasoning',
          status: 'active',
          started_at: '2026-03-30T00:00:00Z',
          ended_at: null,
          tool_invocations: [],
          edges: {
            session_usage_events: [
              {
                event_id: 'usage-missing-reasoning',
                provider_name: 'sub2api',
                model: 'claude-haiku',
                started_at: '2026-03-30T00:31:00Z',
                finished_at: '2026-03-30T00:31:05Z',
                input_tokens: 20,
                output_tokens: 12,
                total_tokens: 32,
                status: 'completed',
                workspace_id: 'ws-1',
                raw_metadata: {
                  cached_input_tokens: 0,
                },
              },
              {
                event_id: 'usage-zero-reasoning',
                provider_name: 'sub2api',
                model: 'gpt-5.4',
                started_at: '2026-03-30T00:32:00Z',
                finished_at: '2026-03-30T00:32:05Z',
                input_tokens: 100,
                output_tokens: 40,
                total_tokens: 140,
                status: 'completed',
                workspace_id: 'ws-1',
                raw_metadata: {
                  cached_input_tokens: 10,
                  reasoning_output_tokens: 0,
                },
              },
            ],
          },
        },
      },
    })

    const router = createTestRouter()
    await router.push('/sessions/sess-reasoning')
    await router.isReady()

    const wrapper = mount(SessionDetailView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const usageRows = wrapper.findAll('tbody tr').filter((row) => {
      const text = row.text()
      return text.includes('claude-haiku') || text.includes('gpt-5.4')
    })
    expect(usageRows).toHaveLength(2)
    expect(usageRows[0].text()).toContain('—')
    expect(usageRows[1].text()).toContain('0')
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
