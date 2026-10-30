import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsMonitoringView from '@/views/admin/OpsMonitoringView.vue'
import { setLocale } from '@/i18n'
import { opsMonitoringMessages } from '@/locales/opsMonitoring'

vi.mock('@/api/relayProvider', () => ({
  listRelayProviders: vi.fn(),
}))

vi.mock('@/api/opsMonitoring', () => ({
  getOpsMonitoringGroups: vi.fn(),
  getOpsMonitoringGroupAccounts: vi.fn(),
  setOpsMonitoringAccountConcurrency: vi.fn(),
  testOpsMonitoringProxy: vi.fn(),
}))

const providers = [
  {
    id: 7,
    name: 'relay',
    display_name: 'Relay',
    base_url: 'https://relay.example.com',
    relay_type: 'sub2api',
    admin_api_key: '',
    default_model: 'gpt-5',
    is_primary: true,
    enabled: true,
  },
  {
    id: 3,
    name: 'relay-secondary',
    display_name: 'Relay Secondary',
    base_url: 'https://relay-2.example.com',
    relay_type: 'sub2api',
    admin_api_key: '',
    default_model: 'gpt-5',
    is_primary: false,
    enabled: true,
  },
]

function overview(overrides: Record<string, unknown> = {}) {
  return {
    request_count_total: 100,
    request_count_sla: 100,
    success_count: 99,
    error_count_total: 1,
    error_count_sla: 1,
    business_limited_count: 0,
    error_rate: 0.01,
    sla: 0.99,
    upstream_error_rate: 0.05,
    upstream_error_count_excl_429_529: 2,
    upstream_429_count: 3,
    upstream_529_count: 4,
    health_score: 96,
    token_consumed: 123456,
    qps: { avg: 1.5, current: 2, peak: 3 },
    tps: { avg: 10, current: 12, peak: 20 },
    output_tps: { avg: 5, p50: 4, p10: 3, p5: 2, sample_count: 7 },
    duration: { avg_ms: 100, max_ms: 300, p50_ms: 90, p90_ms: 200, p95_ms: 250, p99_ms: 290 },
    ttft: { avg_ms: 40, max_ms: 120, p50_ms: 35, p90_ms: 80, p95_ms: 100, p99_ms: 110 },
    ...overrides,
  }
}

const groupsPayload = {
  provider_id: 7,
  window: '1h',
  start_time: '2026-10-14T13:00:00Z',
  end_time: '2026-10-14T14:00:00Z',
  as_of: '2026-10-14T14:00:05Z',
  refreshing: false,
  failed_group_count: 1,
  groups: [
    { group_id: 11, group_name: 'Group Alpha', platform: 'openai', account_count: 5, active_account_count: 3, status: 'ok', error: '', overview: overview() },
    { group_id: 12, group_name: 'Group Beta', platform: 'claude', account_count: 2, active_account_count: 1, status: 'ok', error: '', overview: overview({ upstream_error_rate: 0.42 }) },
    { group_id: 13, group_name: 'Group Gamma', platform: 'openai', account_count: 0, active_account_count: 0, status: 'failed', error: 'upstream timeout', overview: null },
    {
      group_id: 14,
      group_name: 'Group Delta',
      platform: 'gemini',
      account_count: 1,
      active_account_count: 0,
      status: 'ok',
      error: '',
      overview: overview({ request_count_total: 0, request_count_sla: 0, success_count: 0, error_count_total: 0, error_count_sla: 0, token_consumed: 0, error_rate: 0, sla: 0, upstream_error_rate: 0 }),
    },
  ],
}

const accountsPayload = {
  group_id: 11,
  as_of: '2026-10-14T14:00:06Z',
  accounts: [
    {
      account_id: 21,
      name: 'Account Alpha',
      platform: 'openai',
      type: 'oauth',
      status: 'active',
      schedulable: true,
      concurrency: 5,
      current_concurrency: 2,
      proxy_id: 31,
      proxy: { proxy_id: 31, name: 'Proxy One', protocol: 'http', host: '10.0.0.1', port: 8080, status: 'active' },
    },
    {
      account_id: 22,
      name: 'Account Beta',
      platform: 'openai',
      type: 'apikey',
      status: 'inactive',
      schedulable: false,
      concurrency: 8,
      current_concurrency: 0,
      proxy_id: null,
      proxy: null,
    },
  ],
}

const proxyTestPayload = {
  success: true,
  message: 'egress ip resolved',
  latency_ms: 123,
  ip_address: '203.0.113.9',
  city: 'Shanghai',
  region: 'Shanghai',
  country: 'China',
  country_code: 'CN',
  checked_at: '2026-10-14T14:01:00Z',
  cached: false,
}

async function mountView(options: { wide?: boolean; providerList?: typeof providers; groupsData?: unknown; groupsError?: unknown } = {}) {
  const mediaQuery = {
    matches: options.wide ?? true,
    media: '(min-width: 1280px)',
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
  }
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: vi.fn(() => mediaQuery) })

  const relayProvider = await import('@/api/relayProvider') as any
  const ops = await import('@/api/opsMonitoring') as any
  relayProvider.listRelayProviders.mockResolvedValue({ data: { data: options.providerList ?? [providers[0]] } })
  ops.getOpsMonitoringGroups.mockResolvedValue({ data: { data: structuredClone(options.groupsData ?? groupsPayload) } })
  // A one-shot failure for the first read, so the page starts with no snapshot.
  if (options.groupsError) ops.getOpsMonitoringGroups.mockRejectedValueOnce(options.groupsError)
  ops.getOpsMonitoringGroupAccounts.mockResolvedValue({ data: { data: structuredClone(accountsPayload) } })
  ops.setOpsMonitoringAccountConcurrency.mockResolvedValue({
    data: { data: { account_id: 21, concurrency: 9, current_concurrency: 4 } },
  })
  ops.testOpsMonitoringProxy.mockResolvedValue({ data: { data: structuredClone(proxyTestPayload) } })

  const wrapper = mount(OpsMonitoringView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
      },
    },
  })
  await flushPromises()
  return { wrapper, ops, relayProvider }
}

function groupNames(container: ReturnType<typeof mount>) {
  return container.findAll('[data-testid^="ops-group-name-"]').map((node) => node.text())
}

describe('OpsMonitoringView', () => {
  beforeEach(async () => {
    vi.restoreAllMocks()
    vi.resetAllMocks()
    await setLocale('en-US')
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('pins failed rows on top, then sorts the rest by upstream error rate descending with verbatim API values', async () => {
    const { wrapper, ops } = await mountView()

    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)
    expect(ops.getOpsMonitoringGroups.mock.calls[0][0]).toMatchObject({ provider_id: 7, window: '1h' })
    // Group Gamma failed to load, so it is pinned above every sorted row.
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])
    // 0.42 arrives as a fraction and is only formatted for display.
    expect(wrapper.get('[data-testid="ops-cell-upstream_error_rate-12"]').text()).toBe('42.00%')
    expect(wrapper.get('[data-testid="ops-cell-request_count_total-11"]').text()).toBe('100')
    expect(wrapper.get('[data-testid="ops-cell-active_account_count-11"]').text()).toBe('3 / 5')
    expect(wrapper.get('[data-testid="ops-cell-upstream_error_count_excl_429_529-11"]').text()).toBe('2')
  })

  it('labels the data time and the resolved window in Beijing time', async () => {
    const { wrapper } = await mountView()

    expect(wrapper.get('[data-testid="ops-as-of"]').text()).toBe('Data time 2026-10-14 22:00:05')
    expect(wrapper.get('[data-testid="ops-window-range"]').text()).toBe('Last 1 hour · 21:00–22:00 Beijing time')
  })

  it('filters rows client-side by the platform derived from the loaded groups', async () => {
    const { wrapper, ops } = await mountView()

    expect(wrapper.findAll('[data-testid="ops-platform-filter"]')).toHaveLength(1)
    wrapper.findComponent({ name: 'ElSelect' }).vm.$emit('change', 'openai')
    await flushPromises()

    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Alpha'])
    // Filtering happens on the loaded payload, so no extra request is issued.
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)
  })

  it('keeps failed rows pinned when another column drives the sort', async () => {
    const { wrapper } = await mountView()

    const requestsHeader = wrapper.findAll('th').find((header) => header.text().includes('Requests'))
    expect(requestsHeader).toBeDefined()
    await requestsHeader!.get('.sort-caret.ascending').trigger('click')
    await flushPromises()

    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Delta', 'Group Alpha', 'Group Beta'])
  })

  it('keeps a failed group visible without breaking sibling rows', async () => {
    const { wrapper } = await mountView()

    expect(wrapper.get('[data-testid="ops-failed-banner"]').text()).toContain('1 group(s) failed to load')
    expect(wrapper.get('[data-testid="ops-group-failed-13"]').text()).toBe('Fetch failed')
    expect(wrapper.get('[data-testid="ops-group-error-13"]').text()).toContain('upstream timeout')
    expect(wrapper.get('[data-testid="ops-cell-request_count_total-13"]').text()).toBe('—')
    expect(groupNames(wrapper)).toHaveLength(4)
    expect(groupNames(wrapper)[0]).toBe('Group Gamma')
    expect(wrapper.get('[data-testid="ops-cell-request_count_total-11"]').text()).toBe('100')
  })

  it('distinguishes a group without traffic data from a failed group', async () => {
    const { wrapper } = await mountView()

    expect(wrapper.get('[data-testid="ops-group-nodata-14"]').text()).toBe('No data')
    expect(wrapper.get('[data-testid="ops-cell-request_count_total-14"]').text()).toBe('No data')
    expect(wrapper.get('[data-testid="ops-cell-sla-14"]').text()).toBe('No data')
    expect(wrapper.find('[data-testid="ops-group-failed-14"]').exists()).toBe(false)
    // Zero traffic mutes only the traffic-derived cells: every returned count
    // and score stays verbatim instead of being swallowed by 无数据.
    expect(wrapper.get('[data-testid="ops-cell-upstream_error_count_excl_429_529-14"]').text()).toBe('2')
    expect(wrapper.get('[data-testid="ops-cell-upstream_429_count-14"]').text()).toBe('3')
    expect(wrapper.get('[data-testid="ops-cell-upstream_529_count-14"]').text()).toBe('4')
    expect(wrapper.get('[data-testid="ops-cell-health_score-14"]').text()).toBe('96')
  })

  it('never suppresses a returned non-zero value inside a zero-traffic row', async () => {
    const sentinel = {
      ...groupsPayload,
      failed_group_count: 0,
      groups: [
        {
          group_id: 41,
          group_name: 'Group Sentinel',
          platform: 'openai',
          account_count: 2,
          active_account_count: 0,
          status: 'ok',
          error: '',
          overview: overview({
            request_count_total: 0,
            request_count_sla: 0,
            success_count: 0,
            error_count_total: 0,
            error_count_sla: 0,
            token_consumed: 0,
            sla: 0,
            error_rate: 0,
            upstream_error_rate: 0.125,
            upstream_error_count_excl_429_529: 7,
            upstream_429_count: 9,
            upstream_529_count: 1,
            health_score: 41,
          }),
        },
      ],
    }
    const { wrapper } = await mountView({ groupsData: sentinel })

    // The row is still classified as zero traffic…
    expect(wrapper.get('[data-testid="ops-group-nodata-41"]').text()).toBe('No data')
    expect(wrapper.get('[data-testid="ops-cell-request_count_total-41"]').text()).toBe('No data')
    // …but a returned non-zero value is never hidden.
    expect(wrapper.get('[data-testid="ops-cell-upstream_error_rate-41"]').text()).toBe('12.50%')
    expect(wrapper.get('[data-testid="ops-cell-upstream_error_count_excl_429_529-41"]').text()).toBe('7')
    expect(wrapper.get('[data-testid="ops-cell-upstream_429_count-41"]').text()).toBe('9')
    expect(wrapper.get('[data-testid="ops-cell-upstream_529_count-41"]').text()).toBe('1')
    expect(wrapper.get('[data-testid="ops-cell-health_score-41"]').text()).toBe('41')
  })

  it('loads a group account list lazily only after the row is expanded', async () => {
    const { wrapper, ops } = await mountView()

    expect(ops.getOpsMonitoringGroupAccounts).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="ops-account-11-21"]').exists()).toBe(false)

    await wrapper.get('[data-testid="ops-group-toggle-11"]').trigger('click')
    await flushPromises()

    expect(ops.getOpsMonitoringGroupAccounts).toHaveBeenCalledTimes(1)
    expect(ops.getOpsMonitoringGroupAccounts).toHaveBeenCalledWith(11, { provider_id: 7 })
    expect(wrapper.get('[data-testid="ops-account-11-21"]').text()).toContain('Account Alpha')
    expect(wrapper.get('[data-testid="ops-account-proxy-11-21"]').text()).toContain('Proxy One · http · 10.0.0.1:8080')
    expect(wrapper.get('[data-testid="ops-account-direct-11-22"]').text()).toBe('Direct')
    expect(wrapper.get('[data-testid="ops-metric-token_consumed"]').text()).toBe('123456')
    // sub2api's output_tps carries percentiles and a sample count, not current/peak.
    expect(wrapper.get('[data-testid="ops-metric-output_tps"]').text()).toBe('avg 5 · p50 4 · p10 3 · p5 2 · samples 7')
  })

  it('shows each account configured and current concurrency verbatim', async () => {
    const { wrapper } = await mountView()

    await wrapper.get('[data-testid="ops-group-toggle-11"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="ops-account-concurrency-11-21"]').text()).toBe('Concurrency 5 · Current concurrency 2')
    expect(wrapper.get('[data-testid="ops-account-concurrency-11-22"]').text()).toBe('Concurrency 8 · Current concurrency 0')
  })

  it('saves a concurrency change only on explicit save and applies the verified read-back', async () => {
    const { wrapper, ops } = await mountView()

    await wrapper.get('[data-testid="ops-group-toggle-11"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="ops-account-concurrency-edit-11-21"]').trigger('click')
    await wrapper.get('[data-testid="ops-account-concurrency-input-11-21"]').find('input').setValue('9')
    await flushPromises()

    // Typing alone never persists the value.
    expect(ops.setOpsMonitoringAccountConcurrency).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="ops-account-concurrency-11-21"]').text()).toBe('Concurrency 5 · Current concurrency 2')

    await wrapper.get('[data-testid="ops-account-concurrency-save-11-21"]').trigger('click')
    await flushPromises()

    expect(ops.setOpsMonitoringAccountConcurrency).toHaveBeenCalledWith(11, 21, 9, { provider_id: 7 })
    expect(wrapper.get('[data-testid="ops-account-concurrency-11-21"]').text()).toBe('Concurrency 9 · Current concurrency 4')
    expect(wrapper.get('[data-testid="ops-account-concurrency-status-11-21"]').text()).toBe('Concurrency updated')
    expect(wrapper.find('[data-testid="ops-account-concurrency-input-11-21"]').exists()).toBe(false)
  })

  it('keeps the previous concurrency and shows the server error inline when the save fails', async () => {
    const { wrapper, ops } = await mountView()
    ops.setOpsMonitoringAccountConcurrency.mockRejectedValueOnce({
      response: { data: { message: 'relay rejected the capacity change' } },
    })

    await wrapper.get('[data-testid="ops-group-toggle-11"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="ops-account-concurrency-edit-11-21"]').trigger('click')
    await wrapper.get('[data-testid="ops-account-concurrency-input-11-21"]').find('input').setValue('9')
    await wrapper.get('[data-testid="ops-account-concurrency-save-11-21"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="ops-account-concurrency-error-11-21"]').text()).toBe(
      'Failed to update concurrency: relay rejected the capacity change',
    )
    expect(wrapper.get('[data-testid="ops-account-concurrency-11-21"]').text()).toBe('Concurrency 5 · Current concurrency 2')
    expect(wrapper.find('[data-testid="ops-account-concurrency-status-11-21"]').exists()).toBe(false)
    // The editor stays open so the operator can retry the value that failed.
    expect(wrapper.find('[data-testid="ops-account-concurrency-input-11-21"]').exists()).toBe(true)
    expect(ops.setOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
  })

  it('renders the egress IP returned by the proxy test button', async () => {
    const { wrapper, ops } = await mountView()

    await wrapper.get('[data-testid="ops-group-toggle-11"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="ops-proxy-result-11-21"]').exists()).toBe(false)

    await wrapper.get('[data-testid="ops-proxy-test-11-21"]').trigger('click')
    await flushPromises()

    expect(ops.testOpsMonitoringProxy).toHaveBeenCalledWith(31, { provider_id: 7, force: true })
    const result = wrapper.get('[data-testid="ops-proxy-result-11-21"]')
    expect(result.text()).toContain('203.0.113.9')
    expect(result.text()).toContain('Shanghai · Shanghai · China')
    expect(result.text()).toContain('123 ms')
    expect(result.text()).toContain('2026-10-14 22:01:00')
  })

  it('surfaces an unsuccessful proxy test inline instead of a fake egress result', async () => {
    const { wrapper, ops } = await mountView()
    ops.testOpsMonitoringProxy.mockResolvedValueOnce({
      data: { data: { ...proxyTestPayload, success: false, message: 'connect timeout', ip_address: '', latency_ms: 0 } },
    })

    await wrapper.get('[data-testid="ops-group-toggle-11"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="ops-proxy-test-11-21"]').trigger('click')
    await flushPromises()

    const result = wrapper.get('[data-testid="ops-proxy-result-11-21"]')
    expect(result.text()).toContain('Proxy test failed: connect timeout')
    expect(result.text()).not.toContain('Egress IP')
  })

  it('loads the groups again for the selected window', async () => {
    const { wrapper, ops } = await mountView()

    await wrapper.get('input[value="6h"]').setValue(true)
    await flushPromises()

    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)
    expect(ops.getOpsMonitoringGroups.mock.calls[1][0]).toMatchObject({ provider_id: 7, window: '6h' })
  })

  it('defaults to the primary provider when several service endpoints exist', async () => {
    const { wrapper, ops } = await mountView({ providerList: [providers[1], providers[0]] })

    expect(wrapper.find('[data-testid="ops-provider-select"]').exists()).toBe(true)
    expect(ops.getOpsMonitoringGroups.mock.calls[0][0]).toMatchObject({ provider_id: 7 })
  })

  it('keeps the loaded rows and shows an inline notice when a refresh fails', async () => {
    const { wrapper, ops } = await mountView()
    ops.getOpsMonitoringGroups.mockRejectedValueOnce({ response: { data: { message: 'relay unavailable' } } })

    await wrapper.get('[data-testid="ops-refresh"]').trigger('click')
    await flushPromises()

    // The previous snapshot stays on screen instead of blanking the page.
    expect(wrapper.get('[data-testid="ops-inline-error"]').text()).toContain('relay unavailable')
    expect(wrapper.find('[data-testid="ops-error-state"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ops-group-table-layout"]').exists()).toBe(true)
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)
    // No automatic retry: the failure was not followed by another read.
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)

    // A successful load clears the notice.
    await wrapper.get('[data-testid="ops-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="ops-inline-error"]').exists()).toBe(false)
    expect(groupNames(wrapper)).toHaveLength(4)
  })

  it('shows a whole-page error state only when no snapshot exists at all', async () => {
    const { wrapper, ops } = await mountView({ groupsError: { response: { data: { message: 'relay unavailable' } } } })

    expect(wrapper.get('[data-testid="ops-error-state"]').text()).toContain('relay unavailable')
    expect(wrapper.find('[data-testid="ops-inline-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ops-group-table-layout"]').exists()).toBe(false)
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)

    // Manual recovery still works and replaces the error state with the rows.
    await wrapper.get('[data-testid="ops-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="ops-error-state"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ops-group-table-layout"]').exists()).toBe(true)
    expect(groupNames(wrapper)).toHaveLength(4)
  })

  it('forces an upstream read on manual refresh and keeps the automatic tick cached', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const { wrapper, ops } = await mountView()

    // The initial read and every automatic tick read the snapshot cache.
    expect(ops.getOpsMonitoringGroups.mock.calls[0][0].force).toBe(false)

    await wrapper.get('[data-testid="ops-refresh"]').trigger('click')
    await flushPromises()
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)
    expect(ops.getOpsMonitoringGroups.mock.calls[1][0]).toMatchObject({ provider_id: 7, window: '1h', force: true })

    wrapper.findComponent({ name: 'ElSwitch' }).vm.$emit('change', true)
    await flushPromises()
    vi.advanceTimersByTime(60_000)
    await flushPromises()

    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(3)
    expect(ops.getOpsMonitoringGroups.mock.calls[2][0].force).toBe(false)
  })

  it('keeps the same table contract in the narrow stacked layout', async () => {
    const { wrapper } = await mountView({ wide: false })

    expect(wrapper.find('[data-testid="ops-group-card-layout"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="ops-group-table-layout"]').exists()).toBe(false)
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])

    await wrapper.get('[data-testid="ops-group-toggle-11"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="ops-account-11-21"]').text()).toContain('Account Alpha')
  })

  it('renders the localized Chinese title and labels', async () => {
    await setLocale('zh-CN')
    const { wrapper } = await mountView()

    expect(wrapper.get('h1').text()).toBe('运维监控')
    expect(wrapper.get('[data-testid="ops-failed-banner"]').text()).toContain('1 个分组取数失败')
    expect(wrapper.get('[data-testid="ops-cell-upstream_error_rate-12"]').text()).toBe('42.00%')
    expect(wrapper.get('[data-testid="ops-group-failed-13"]').text()).toBe('取数失败')
  })

  it('keeps both ops monitoring dictionaries at the same key contract', () => {
    expect(Object.keys(opsMonitoringMessages['zh-CN']).sort()).toEqual(Object.keys(opsMonitoringMessages['en-US']).sort())
  })
})
