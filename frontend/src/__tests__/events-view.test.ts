import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import EventsView from '@/views/events/EventsView.vue'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/api/events', () => ({
  getEventSummary: vi.fn(),
  listEvents: vi.fn(),
  getEventDetail: vi.fn(),
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/events', component: EventsView },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
}

const sampleRow = {
  id: 12,
  tool: 'claude',
  repo_id: 9,
  repo_name: 'LichKing-2234/ai-efficiency',
  username: 'alice',
  tool_session_id: 'claude-sess-1',
  tool_event_id: 'msg-1',
  dedupe_key: 'claude:1',
  observed_end_at: '2026-05-21T06:00:00Z',
  request_count: 2,
  input_tokens: 1200,
  output_tokens: 500,
  cached_input_tokens: 300,
  reasoning_tokens: 80,
  credit_usage: 1.25,
  commit_checkpoint_id: 29,
  commit_sha: 'b1e9454c572079d487835e114e4687f6c1f2d22d',
  source_basename: 'detail.jsonl',
  binding_status: 'bound' as const,
}

const sampleSummary = {
  total_events: 2,
  bound_events: 1,
  unbound_events: 1,
  tool_counts: [{ tool: 'claude', count: 1 }, { tool: 'kiro', count: 1 }],
}

const sampleDetail = {
  id: 12,
  tool: 'claude',
  repo_id: 9,
  repo_name: 'LichKing-2234/ai-efficiency',
  user_id: 1,
  username: 'alice',
  workspace_id: 'ws-1',
  tool_session_id: 'claude-sess-1',
  tool_event_id: 'msg-1',
  dedupe_key: 'claude:1',
  observed_start_at: '2026-05-21T05:59:58Z',
  observed_end_at: '2026-05-21T06:00:00Z',
  request_count: 2,
  input_tokens: 1200,
  output_tokens: 500,
  cached_input_tokens: 300,
  reasoning_tokens: 80,
  credit_usage: 1.25,
  context_usage_pct: 0,
  commit_checkpoint_id: 29,
  commit_sha: 'b1e9454c572079d487835e114e4687f6c1f2d22d',
  checkpoint_captured_at: '2026-05-21T06:05:59Z',
  source_basename: 'detail.jsonl',
  raw_source_path: '/Users/admin/.claude/projects/detail.jsonl',
  raw_source_locator: 'line:10',
  raw_payload: { scope: 'admin-only' },
  binding_status: 'bound' as const,
  matched_prs: [{ pr_record_id: 1769, scm_pr_id: 38, title: 'Events page', status: 'open', scm_pr_url: 'https://example.com/pr/38' }],
}

async function mountEvents(isAdmin = false) {
  const { getEventSummary, listEvents, getEventDetail } = await import('@/api/events')
  ;(getEventSummary as any).mockResolvedValue({ data: { data: sampleSummary } })
  ;(listEvents as any).mockResolvedValue({ data: { data: { items: [sampleRow], total: 1, page: 0, page_size: 20 } } })
  ;(getEventDetail as any).mockResolvedValue({ data: { data: isAdmin ? sampleDetail : { ...sampleDetail, raw_source_path: undefined, raw_source_locator: undefined, raw_payload: undefined } } })

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.token = 'token'
  auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: isAdmin ? 'admin' : 'user', auth_source: 'sso' }

  const router = createTestRouter()
  await router.push('/events')
  await router.isReady()

  const wrapper = mount(EventsView, {
    global: {
      plugins: [pinia, router],
    },
  })
  await flushPromises()
  return { wrapper, getEventSummary, listEvents, getEventDetail }
}

describe('EventsView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('loads summary and event rows on mount with a 24h default range', async () => {
    const { wrapper, getEventSummary, listEvents } = await mountEvents()

    expect(getEventSummary).toHaveBeenCalled()
    expect(listEvents).toHaveBeenCalled()
    const summaryParams = (getEventSummary as any).mock.calls[0][0]
    const listParams = (listEvents as any).mock.calls[0][0]
    expect(summaryParams.from).toBeTruthy()
    expect(summaryParams.to).toBeTruthy()
    expect(listParams.from).toBeTruthy()
    expect(listParams.to).toBeTruthy()
    expect(wrapper.text()).toContain('Total Events')
    expect(wrapper.text()).toContain('detail.jsonl')
  })

  it('opens the detail drawer and hides raw payload for non-admin', async () => {
    const { wrapper, getEventDetail } = await mountEvents(false)

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()

    expect(getEventDetail).toHaveBeenCalledWith(12)
    expect(wrapper.text()).toContain('Binding')
    expect(wrapper.text()).not.toContain('Raw Payload')
  })

  it('shows raw payload for admin users in the detail drawer', async () => {
    const { wrapper } = await mountEvents(true)

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Raw Payload')
    expect(wrapper.text()).toContain('admin-only')
    expect(wrapper.text()).toContain('Events page')
  })
})
