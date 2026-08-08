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

const sampleRows = [
  sampleRow,
  { ...sampleRow, id: 13, tool: 'codex', tool_session_id: 'codex-sess-2', dedupe_key: 'codex:2' },
  { ...sampleRow, id: 14, tool: 'kiro', tool_session_id: 'kiro-sess-3', dedupe_key: 'kiro:3' },
]

const sampleSummary = {
  total_events: 3,
  bound_events: 1,
  unbound_events: 2,
  tool_counts: [{ tool: 'claude', count: 1 }, { tool: 'kiro', count: 1 }],
}

const largeRawPayload = {
  scope: 'admin-only',
  marker: 'large-payload-marker',
  content: 'x'.repeat(4096),
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
  raw_payload: largeRawPayload,
  binding_status: 'bound' as const,
  matched_prs: [{ pr_record_id: 1769, scm_pr_id: 38, title: 'Events page', status: 'open', scm_pr_url: 'https://example.com/pr/38' }],
}

async function mountEvents(
  isAdmin = false,
  path = '/events',
  listData: { items: typeof sampleRows; total: number; page: number; page_size: number } | Error = {
    items: sampleRows,
    total: 45,
    page: 0,
    page_size: 20,
  },
) {
  const { getEventSummary, listEvents, getEventDetail, searchEventUsers } = await import('@/api/events')
  ;(getEventSummary as any).mockResolvedValue({ data: { data: sampleSummary } })
  if (listData instanceof Error) {
    ;(listEvents as any).mockRejectedValueOnce(listData)
  } else {
    ;(listEvents as any).mockResolvedValue({ data: { data: listData } })
  }
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

function installMatchMedia(initialMatches: boolean) {
  const listeners = new Set<EventListenerOrEventListenerObject>()
  const mediaQueryList = {
    matches: initialMatches,
    media: '(min-width: 768px)',
    onchange: null,
    addEventListener: vi.fn((type: string, listener: EventListenerOrEventListenerObject) => {
      if (type === 'change') listeners.add(listener)
    }),
    removeEventListener: vi.fn((type: string, listener: EventListenerOrEventListenerObject) => {
      if (type === 'change') listeners.delete(listener)
    }),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }
  const fallbackMediaQueryList = {
    matches: false,
    media: '',
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }
  const matchMedia = vi.fn((query: string) => {
    if (query === mediaQueryList.media) return mediaQueryList as unknown as MediaQueryList
    return { ...fallbackMediaQueryList, media: query } as unknown as MediaQueryList
  })
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: matchMedia,
  })

  return {
    addEventListener: mediaQueryList.addEventListener,
    removeEventListener: mediaQueryList.removeEventListener,
    listenerCount: () => listeners.size,
    matchMedia,
    setMatches(matches: boolean) {
      mediaQueryList.matches = matches
      const event = { matches, media: mediaQueryList.media } as MediaQueryListEvent
      for (const listener of listeners) {
        if (typeof listener === 'function') listener(event)
        else listener.handleEvent(event)
      }
    },
  }
}

async function selectElementPlusOption(
  wrapper: Awaited<ReturnType<typeof mountEvents>>['wrapper'],
  testId: string,
  label: string,
) {
  await wrapper.get(`[data-testid="${testId}"] .el-select__wrapper`).trigger('click')
  await flushPromises()
  const option = wrapper.findAll('.el-select-dropdown__item').find((item) => item.text() === label)
  if (!option) throw new Error(`Element Plus option ${label} was not rendered`)
  await option.trigger('click')
  await flushPromises()
}

async function openAdvancedDetails(wrapper: Awaited<ReturnType<typeof mountEvents>>['wrapper']) {
  await wrapper.get('[data-testid="event-advanced-data"] .el-collapse-item__header').trigger('click')
  await flushPromises()
}

function rawPayloadStringifyCount(spy: ReturnType<typeof vi.spyOn>) {
  return spy.mock.calls.filter((call: unknown[]) => {
    const value = call[0]
    return typeof value === 'object'
      && value != null
      && 'marker' in value
      && value.marker === largeRawPayload.marker
  }).length
}

describe('EventsView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en-US')
    vi.clearAllMocks()
    installMatchMedia(true)
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
    expect(wrapper.text()).toContain('1,700')
    expect(wrapper.text()).not.toContain('2,000')
    expect(wrapper.text()).toContain('Linked')
    expect(wrapper.text()).not.toContain('detail.jsonl')
  })

  it('renders the refresh action as an Element Plus button', async () => {
    const { wrapper } = await mountEvents()

    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')
    expect(refresh?.classes()).toContain('el-button')
  })

  it('shows an Element Plus alert when refreshing the event list fails', async () => {
    const { wrapper, listEvents } = await mountEvents()
    ;(listEvents as any).mockRejectedValueOnce(new Error('request failed'))

    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')
    await refresh?.trigger('click')
    await flushPromises()

    const alert = wrapper.get('[data-testid="events-load-error"]')
    expect(alert.find('.el-alert').exists()).toBe(true)
    expect(alert.text()).toContain('Failed to load usage records')
  })

  it('removes results from the previous query when refreshing the event list fails', async () => {
    const { wrapper, listEvents } = await mountEvents()
    expect(wrapper.get('[data-event-list="desktop"]').findAll('.el-table__row')).toHaveLength(3)

    ;(listEvents as any).mockRejectedValueOnce(new Error('request failed'))
    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')
    await refresh?.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-event-list="desktop"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(sampleRow.repo_name)
    expect(wrapper.findAll('.el-card')[0].text()).toBe('Total Records—')
  })

  it('updates the event list error message when the locale changes', async () => {
    const { wrapper, listEvents } = await mountEvents()
    ;(listEvents as any).mockRejectedValueOnce(new Error('request failed'))

    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')
    await refresh?.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="events-load-error"]').text()).toContain('Failed to load usage records')

    await setLocale('zh-CN')
    await flushPromises()

    expect(wrapper.get('[data-testid="events-load-error"]').text()).toContain('使用记录加载失败')
  })

  it('does not present a failed initial event load as an empty result', async () => {
    const { wrapper } = await mountEvents(false, '/events', new Error('request failed'))

    expect(wrapper.get('[data-testid="events-load-error"]').text()).toContain('Failed to load usage records')
    expect(wrapper.text()).not.toContain('No usage records')
  })

  it('treats a null event summary payload as a load failure', async () => {
    const { wrapper, getEventSummary } = await mountEvents()
    ;(getEventSummary as any).mockResolvedValueOnce({ data: { data: null } })

    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')
    await refresh?.trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="events-load-error"]').text()).toContain('Failed to load usage records')
    expect(wrapper.find('[data-event-list="desktop"]').exists()).toBe(false)
  })

  it('treats a null event list payload as a load failure', async () => {
    const { wrapper, listEvents } = await mountEvents()
    ;(listEvents as any).mockResolvedValueOnce({ data: { data: null } })

    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')
    await refresh?.trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="events-load-error"]').text()).toContain('Failed to load usage records')
    expect(wrapper.find('[data-event-list="desktop"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('No usage records')
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

  it('renders exactly one mobile row subtree per event below 768px', async () => {
    const media = installMatchMedia(false)
    const { wrapper } = await mountEvents()

    expect(wrapper.findAll('[data-event-row="mobile"]')).toHaveLength(3)
    expect(wrapper.findAll('[data-event-row="desktop"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('View details')
    expect(media.matchMedia).toHaveBeenCalledWith('(min-width: 768px)')
  })

  it('renders exactly one desktop row subtree per event at or above 768px', async () => {
    const media = installMatchMedia(true)
    const { wrapper } = await mountEvents()

    expect(wrapper.findAll('[data-event-row="mobile"]')).toHaveLength(0)
    expect(wrapper.get('[data-event-list="desktop"]').findAll('.el-table__row')).toHaveLength(3)
    expect(media.matchMedia).toHaveBeenCalledWith('(min-width: 768px)')
  })

  it('switches representation once when the media query changes', async () => {
    const media = installMatchMedia(false)
    const { wrapper } = await mountEvents()

    expect(media.addEventListener).toHaveBeenCalledTimes(1)
    expect(media.listenerCount()).toBe(1)
    expect(wrapper.findAll('[data-event-row="mobile"]')).toHaveLength(3)

    media.setMatches(true)
    await wrapper.vm.$nextTick()

    expect(wrapper.findAll('[data-event-row="mobile"]')).toHaveLength(0)
    expect(wrapper.get('[data-event-list="desktop"]').findAll('.el-table__row')).toHaveLength(3)

    wrapper.unmount()
    expect(media.removeEventListener).toHaveBeenCalledTimes(1)
    expect(media.listenerCount()).toBe(0)
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

  it('shows an Element Plus alert in the drawer when event detail fails to load', async () => {
    const { wrapper, getEventDetail } = await mountEvents(false)
    ;(getEventDetail as any).mockRejectedValueOnce(new Error('request failed'))

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="event-detail-drawer"]').isVisible()).toBe(true)
    const alert = wrapper.get('[data-testid="event-detail-error"]')
    expect(alert.find('.el-alert').exists()).toBe(true)
    expect(alert.text()).toContain('Failed to load record detail')
  })

  it('treats a null event detail payload as a load failure', async () => {
    const { wrapper, getEventDetail } = await mountEvents(false)
    ;(getEventDetail as any).mockResolvedValueOnce({ data: { data: null } })

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="event-detail-error"]').text()).toContain('Failed to load record detail')
  })

  it('updates the event detail error message when the locale changes', async () => {
    const { wrapper, getEventDetail } = await mountEvents(false)
    ;(getEventDetail as any).mockRejectedValueOnce(new Error('request failed'))

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="event-detail-error"]').text()).toContain('Failed to load record detail')

    await setLocale('zh-CN')
    await flushPromises()

    expect(wrapper.get('[data-testid="event-detail-error"]').text()).toContain('记录详情加载失败')
  })

  it('closes the detail drawer with Escape', async () => {
    const { wrapper } = await mountEvents(false)

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Record detail')

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', bubbles: true }))
    await flushPromises()

    expect(wrapper.get('[data-testid="event-detail-drawer"]').isVisible()).toBe(false)
  })

  it('shows raw payload for admin users in the detail drawer', async () => {
    const { wrapper } = await mountEvents(true)

    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="event-advanced-data"]').classes()).toContain('el-collapse')
    expect(wrapper.text()).toContain('Raw Payload')
    await openAdvancedDetails(wrapper)
    expect(wrapper.text()).toContain('admin-only')
    expect(wrapper.text()).toContain('Events page')
  })

  it('does not stringify raw payload while advanced details are closed', async () => {
    const { wrapper } = await mountEvents(true)
    const stringifySpy = vi.spyOn(JSON, 'stringify')

    try {
      await wrapper.find('tbody tr').trigger('click')
      await flushPromises()

      expect(rawPayloadStringifyCount(stringifySpy)).toBe(0)
      expect(wrapper.text()).not.toContain(largeRawPayload.marker)
    } finally {
      stringifySpy.mockRestore()
    }
  })

  it('formats raw payload once after advanced details opens', async () => {
    const { wrapper } = await mountEvents(true)
    const stringifySpy = vi.spyOn(JSON, 'stringify')

    try {
      await wrapper.find('tbody tr').trigger('click')
      await flushPromises()
      expect(rawPayloadStringifyCount(stringifySpy)).toBe(0)

      await openAdvancedDetails(wrapper)

      expect(rawPayloadStringifyCount(stringifySpy)).toBe(1)
      expect(wrapper.text()).toContain(largeRawPayload.marker)
    } finally {
      stringifySpy.mockRestore()
    }
  })

  it('resets formatting state when the detail drawer closes', async () => {
    const { wrapper } = await mountEvents(true)
    const stringifySpy = vi.spyOn(JSON, 'stringify')

    try {
      await wrapper.find('tbody tr').trigger('click')
      await flushPromises()
      await openAdvancedDetails(wrapper)
      expect(rawPayloadStringifyCount(stringifySpy)).toBe(1)
      expect(wrapper.text()).toContain(largeRawPayload.marker)

      await wrapper.get('[data-testid="event-detail-drawer"] button').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="event-detail-drawer"]').isVisible()).toBe(false)

      await wrapper.find('tbody tr').trigger('click')
      await flushPromises()

      expect(wrapper.get('[data-testid="event-advanced-data"] .el-collapse-item__header').attributes('aria-expanded')).toBe('false')
      expect(rawPayloadStringifyCount(stringifySpy)).toBe(1)
      expect(wrapper.text()).not.toContain(largeRawPayload.marker)
    } finally {
      stringifySpy.mockRestore()
    }
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

  it('shows an Element Plus alert when the admin user search fails', async () => {
    const { wrapper, searchEventUsers } = await mountEvents(true)
    ;(searchEventUsers as any).mockRejectedValueOnce(new Error('request failed'))

    await wrapper.get('[data-testid="event-user-search"]').setValue('alice@example.com')
    await wrapper.get('[data-testid="event-user-search-button"]').trigger('click')
    await flushPromises()

    const alert = wrapper.get('[data-testid="event-user-search-error"]')
    expect(alert.find('.el-alert').exists()).toBe(true)
    expect(alert.text()).toContain('Failed to search users')
  })

  it('updates the user search error message when the locale changes', async () => {
    const { wrapper, searchEventUsers } = await mountEvents(true)
    ;(searchEventUsers as any).mockRejectedValueOnce(new Error('request failed'))

    await wrapper.get('[data-testid="event-user-search-button"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="event-user-search-error"]').text()).toContain('Failed to search users')

    await setLocale('zh-CN')
    await flushPromises()

    expect(wrapper.get('[data-testid="event-user-search-error"]').text()).toContain('用户搜索失败')
  })

  it('removes results from the previous admin user query when the next search fails', async () => {
    const { wrapper, searchEventUsers } = await mountEvents(true)

    await wrapper.get('[data-testid="event-user-search"]').setValue('alice@example.com')
    await wrapper.get('[data-testid="event-user-search-button"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="event-user-option-2"]').exists()).toBe(true)

    ;(searchEventUsers as any).mockRejectedValueOnce(new Error('request failed'))
    await wrapper.get('[data-testid="event-user-search"]').setValue('bob@example.org')
    await wrapper.get('[data-testid="event-user-search-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="event-user-search-error"]').text()).toContain('Failed to search users')
    expect(wrapper.find('[data-testid="event-user-option-2"]').exists()).toBe(false)
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

  it('localizes the fallback user label and event count in user options', async () => {
    const { wrapper, searchEventUsers } = await mountEvents(true)
    ;(searchEventUsers as any).mockResolvedValueOnce({
      data: {
        data: [{
          id: 4,
          username: '',
          email: '',
          role: 'admin',
          event_count: 7,
          latest_event_at: '2026-05-22T03:29:57Z',
        }],
      },
    })
    await setLocale('zh-CN')

    await wrapper.get('[data-testid="event-user-search-button"]').trigger('click')
    await flushPromises()

    const optionText = wrapper.get('[data-testid="event-user-option-4"]').text()
    expect(optionText).toContain('用户 #4')
    expect(optionText).toContain('7 条使用记录')
    expect(optionText).not.toContain('User #')
    expect(optionText).not.toContain('events')
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

    await selectElementPlusOption(wrapper, 'events-page-size', '50')
    const latestParams = (listEvents as any).mock.calls.at(-1)[0]
    expect(latestParams.limit).toBe(50)
    expect(latestParams.offset).toBe(0)
  })

  it('clamps a restored page size above 100 and advances without a gap', async () => {
    const { wrapper, router, listEvents } = await mountEvents(
      false,
      '/events?limit=101&offset=0',
      { items: sampleRows, total: 205, page: 0, page_size: 100 },
    )

    await wrapper.get('[data-testid="events-next-page"]').trigger('click')
    await flushPromises()

    expect((listEvents as any).mock.calls.slice(-2).map(([params]: [{ limit: number; offset: number }]) => ({
      limit: params.limit,
      offset: params.offset,
    }))).toEqual([
      { limit: 100, offset: 0 },
      { limit: 100, offset: 100 },
    ])
    expect(router.currentRoute.value.query).toMatchObject({ limit: '100', offset: '100' })
  })

  it('normalizes decimal pagination before the first request and next page', async () => {
    const { wrapper, router, listEvents } = await mountEvents(
      false,
      '/events?limit=20.5&offset=10.5',
      { items: sampleRows, total: 45, page: 0, page_size: 20 },
    )

    expect(wrapper.get('[data-testid="events-page-size"]').text()).toContain('20')
    expect(router.currentRoute.value.query.limit).toBeUndefined()
    expect(router.currentRoute.value.query.offset).toBeUndefined()

    await wrapper.get('[data-testid="events-next-page"]').trigger('click')
    await flushPromises()

    expect((listEvents as any).mock.calls.slice(-2).map(([params]: [{ limit: number; offset: number }]) => ({
      limit: params.limit,
      offset: params.offset,
    }))).toEqual([
      { limit: 20, offset: 0 },
      { limit: 20, offset: 20 },
    ])
    expect(router.currentRoute.value.query.limit).toBeUndefined()
    expect(router.currentRoute.value.query.offset).toBe('20')
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
