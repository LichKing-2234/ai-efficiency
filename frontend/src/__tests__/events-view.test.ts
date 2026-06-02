import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import EventsView from '@/views/events/EventsView.vue'
import { useAuthStore } from '@/stores/auth'
import { setLocale } from '@/i18n'

vi.mock('@/api/events', () => ({
  getEventSummary: vi.fn(),
  listEvents: vi.fn(),
  getEventDetail: vi.fn(),
  searchEventUsers: vi.fn(),
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/events', component: EventsView },
      { path: '/repos', component: { template: '<div>Repos</div>' } },
      { path: '/user', component: { template: '<div>User</div>' } },
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
  raw_source_path: '/tmp/example-ai-events/detail.jsonl',
  raw_source_locator: 'line:10',
  raw_payload: { scope: 'admin-only' },
  binding_status: 'bound' as const,
  matched_prs: [{ pr_record_id: 1769, scm_pr_id: 38, title: 'Events page', status: 'open', scm_pr_url: 'https://example.com/pr/38' }],
}

async function mountEvents(isAdmin = false, path = '/events') {
  const { getEventSummary, listEvents, getEventDetail, searchEventUsers } = await import('@/api/events')
  ;(getEventSummary as any).mockResolvedValue({ data: { data: sampleSummary } })
  ;(listEvents as any).mockResolvedValue({ data: { data: { items: [sampleRow], total: 45, page: 0, page_size: 20 } } })
  ;(getEventDetail as any).mockResolvedValue({ data: { data: isAdmin ? sampleDetail : { ...sampleDetail, raw_source_path: undefined, raw_source_locator: undefined, raw_payload: undefined } } })
  ;(searchEventUsers as any).mockResolvedValue({
    data: {
      data: [
        {
          id: 2,
          username: 'alice',
          email: 'alice@example.com',
          role: 'admin',
          event_count: 3,
          latest_event_at: '2026-05-22T03:29:57Z',
        },
      ],
    },
  })

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.token = 'token'
  auth.user = { id: 1, username: 'alice', email: 'alice@example.com', role: isAdmin ? 'admin' : 'user', auth_source: 'sso' }

  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const wrapper = mount(EventsView, {
    global: {
      plugins: [pinia, router],
    },
  })
  await flushPromises()
  return { wrapper, router, getEventSummary, listEvents, getEventDetail, searchEventUsers }
}

describe('EventsView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
  })

  it('loads summary and event rows on mount with a 7-day default range', async () => {
    const { wrapper, getEventSummary, listEvents } = await mountEvents()

    expect(getEventSummary).toHaveBeenCalled()
    expect(listEvents).toHaveBeenCalled()
    const listParams = (listEvents as any).mock.calls[0][0]
    const from = new Date(listParams.from).getTime()
    const to = new Date(listParams.to).getTime()
    const days = Math.round((to - from) / (24 * 60 * 60 * 1000))
    expect(days).toBe(7)
    expect(listParams.limit).toBe(20)
    expect(listParams.offset).toBe(0)
    expect(wrapper.text()).toContain('Usage Records')
    expect(wrapper.text()).toContain('Total Records')
    expect(wrapper.text()).toContain('Recent usage')
    expect(wrapper.text()).toContain('Code link')
    expect(wrapper.text()).toContain('Token usage')
    expect(wrapper.text()).toContain('View details')
    expect(wrapper.text()).toContain('Linked')
    expect(wrapper.text()).not.toContain('detail.jsonl')
  })

  it('uses a collapsible filter panel on mobile markup', async () => {
    const { wrapper } = await mountEvents()

    const toggle = wrapper.get('button[aria-controls="events-filter-panel"]')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('#events-filter-panel').classes()).toContain('hidden')
    expect(wrapper.text()).toContain('Last 7 days')
    expect(wrapper.text()).toContain('All tools')
    expect(wrapper.text()).toContain('All code links')

    await toggle.trigger('click')
    await wrapper.vm.$nextTick()

    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('#events-filter-panel').classes()).toContain('block')
  })

  it('summarizes restored filters while the mobile filter panel is collapsed', async () => {
    const { wrapper } = await mountEvents(false, '/events?tool=codex&binding_status=bound&q=abc&limit=50&offset=100')

    expect(wrapper.get('button[aria-controls="events-filter-panel"]').attributes('aria-expanded')).toBe('false')
    expect(wrapper.text()).toContain('Last 7 days')
    expect(wrapper.text()).toContain('Tool: codex')
    expect(wrapper.text()).toContain('Code link: Linked')
    expect(wrapper.text()).toContain('Search: abc')
  })

  it('opens the detail drawer and hides raw payload for non-admin', async () => {
    const { wrapper, getEventDetail } = await mountEvents(false)

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()

    expect(getEventDetail).toHaveBeenCalledWith(12)
    expect(wrapper.text()).toContain('Record detail')
    expect(wrapper.text()).toContain('Code link')
    expect(wrapper.text()).toContain('Advanced event data')
    expect(wrapper.text()).not.toContain('Dedupe Key')
    expect(wrapper.text()).not.toContain('Raw Payload')
  })

  it('closes the detail drawer with Escape', async () => {
    const { wrapper } = await mountEvents(false)

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Record detail')

    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).not.toContain('Record detail')
  })

  it('shows raw payload for admin users in the detail drawer', async () => {
    const { wrapper } = await mountEvents(true)

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Raw Payload')
    expect(wrapper.text()).toContain('admin-only')
    expect(wrapper.text()).toContain('Events page')
  })

  it('shows admin-only searchable user selector and applies selected user id', async () => {
    const { wrapper, listEvents, searchEventUsers } = await mountEvents(true)

    const input = wrapper.get('[data-testid="event-user-search"]')
    await input.setValue('alice@example.com')
    await wrapper.get('[data-testid="event-user-search-button"]').trigger('click')
    await flushPromises()

    expect(searchEventUsers).toHaveBeenCalledWith({ q: 'alice@example.com', limit: 20 })
    expect(wrapper.text()).toContain('alice@example.com')

    await wrapper.get('[data-testid="event-user-option-2"]').trigger('click')
    await flushPromises()

    const latestParams = (listEvents as any).mock.calls.at(-1)[0]
    expect(latestParams.user_id).toBe(2)
    expect(latestParams.offset).toBe(0)
  })

  it('does not repeat the email when username matches email in user options', async () => {
    const { wrapper } = await mountEvents(true)
    const { searchEventUsers } = await import('@/api/events')
    ;(searchEventUsers as any).mockResolvedValue({
      data: {
        data: [
          {
            id: 3,
            username: 'alice@example.com',
            email: 'alice@example.com',
            role: 'admin',
            event_count: 7,
            latest_event_at: '2026-05-22T03:29:57Z',
          },
        ],
      },
    })

    await wrapper.get('[data-testid="event-user-search"]').setValue('alice')
    await wrapper.get('[data-testid="event-user-search-button"]').trigger('click')
    await flushPromises()

    const optionText = wrapper.get('[data-testid="event-user-option-3"]').text()
    expect(optionText.match(/alice@example.com/g)).toHaveLength(1)
    expect(optionText).toContain('· admin · 7 events')
  })

  it('hides user selector from regular users', async () => {
    const { wrapper } = await mountEvents(false)

    expect(wrapper.find('[data-testid="event-user-search"]').exists()).toBe(false)
  })

  it('updates pagination params for next page and page size changes', async () => {
    const { listEvents } = await import('@/api/events')
    ;(listEvents as any).mockResolvedValue({
      data: { data: { items: [sampleRow], total: 45, page: 0, page_size: 20 } },
    })
    const { wrapper } = await mountEvents()

    await wrapper.get('[data-testid="events-next-page"]').trigger('click')
    await flushPromises()
    expect((listEvents as any).mock.calls.at(-1)[0].offset).toBe(20)

    await wrapper.get('[data-testid="events-page-size"]').setValue('50')
    await flushPromises()
    const latestParams = (listEvents as any).mock.calls.at(-1)[0]
    expect(latestParams.limit).toBe(50)
    expect(latestParams.offset).toBe(0)
  })

  it('restores filters and pagination from the URL query', async () => {
    const from = '2026-05-20T10:00'
    const to = '2026-05-30T18:30'
    const { listEvents } = await mountEvents(false, `/events?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&tool=codex&binding_status=bound&q=abc&limit=50&offset=100`)

    const listParams = (listEvents as any).mock.calls[0][0]
    expect(listParams.tool).toBe('codex')
    expect(listParams.binding_status).toBe('bound')
    expect(listParams.q).toBe('abc')
    expect(listParams.limit).toBe(50)
    expect(listParams.offset).toBe(100)
  })

  it('restores admin selected user id from the URL query', async () => {
    const { wrapper, listEvents } = await mountEvents(true, '/events?user_id=2')

    const listParams = (listEvents as any).mock.calls[0][0]
    expect(listParams.user_id).toBe(2)
    expect(wrapper.text()).toContain('Selected user #2')
  })

  it('writes pagination changes back to the URL query', async () => {
    const { wrapper, router } = await mountEvents()

    await wrapper.get('[data-testid="events-next-page"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.query.offset).toBe('20')
  })
})
