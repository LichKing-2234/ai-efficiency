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
  getOpsMonitoringAccountConcurrency: vi.fn(),
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
    { group_id: 13, group_name: 'Group Gamma', platform: 'openai', account_count: 1, active_account_count: 0, status: 'failed', error: 'upstream timeout', overview: null },
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

// Whole-provider account concurrency/membership index read lazily by the
// exact-value concurrency filter. Account 21 is Group Alpha's 5, account 22 is
// its 8, account 23 is Group Beta's 2, and account 24 keeps the failed Group
// Gamma visible at the exact value 30.
const concurrencyIndexPayload = {
  provider_id: 7,
  as_of: '2026-10-14T14:00:04Z',
  accounts: [
    { account_id: 21, concurrency: 5, group_ids: [11] },
    { account_id: 22, concurrency: 8, group_ids: [11] },
    { account_id: 23, concurrency: 2, group_ids: [12] },
    { account_id: 24, concurrency: 30, group_ids: [13] },
  ],
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
  ops.getOpsMonitoringAccountConcurrency.mockResolvedValue({ data: { data: structuredClone(concurrencyIndexPayload) } })
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

function concurrencyFilterInput(container: ReturnType<typeof mount>) {
  return container.get('[data-testid="ops-concurrency-filter"]').find('input')
}

async function applyConcurrencyFilter(container: ReturnType<typeof mount>, value: number) {
  const input = concurrencyFilterInput(container)
  await input.setValue(String(value))
  await input.trigger('change')
  await flushPromises()
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

  it('loads the account concurrency index once when the filter is first applied and keeps only exact matches', async () => {
    const { wrapper, ops } = await mountView()

    expect(ops.getOpsMonitoringAccountConcurrency).not.toHaveBeenCalled()

    await applyConcurrencyFilter(wrapper, 8)

    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledWith({ provider_id: 7 })
    // Exact match: Group Alpha has an account configured at 8, while the failed
    // Group Gamma (30) does not match and is hidden.
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])
    expect(wrapper.find('[data-testid="ops-group-failed-13"]').exists()).toBe(false)
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)
  })

  it('keeps different configured concurrency levels distinguishable without refetching', async () => {
    const { wrapper, ops } = await mountView()

    await applyConcurrencyFilter(wrapper, 5)
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])

    // 10 is a different configured level; no account is configured at 10, so
    // the table is empty instead of reusing the 5-level result.
    await applyConcurrencyFilter(wrapper, 10)
    expect(groupNames(wrapper)).toEqual([])
    expect(wrapper.get('[data-testid="ops-empty"]').text()).toContain('No groups match the current filters.')

    await applyConcurrencyFilter(wrapper, 2)
    expect(groupNames(wrapper)).toEqual(['Group Beta'])

    await applyConcurrencyFilter(wrapper, 30)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])

    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)
  })

  it('re-filters locally when the exact value changes without refetching groups or the index', async () => {
    const { wrapper, ops } = await mountView()

    await applyConcurrencyFilter(wrapper, 5)
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])

    await applyConcurrencyFilter(wrapper, 8)
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])

    await applyConcurrencyFilter(wrapper, 30)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])

    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)
  })

  it('clears the filter and restores every loaded row without another request', async () => {
    const { wrapper, ops } = await mountView()

    await applyConcurrencyFilter(wrapper, 30)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])

    await wrapper.get('[data-testid="ops-concurrency-filter-clear"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="ops-concurrency-filter-clear"]').exists()).toBe(false)
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])

    // Clearing only drops the value; the cached index is reused on the next apply.
    await applyConcurrencyFilter(wrapper, 2)
    expect(groupNames(wrapper)).toEqual(['Group Beta'])
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
  })

  it('composes the concurrency filter with the platform filter using AND', async () => {
    const { wrapper, ops } = await mountView()

    await applyConcurrencyFilter(wrapper, 5)
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])

    wrapper.findComponent({ name: 'ElSelect' }).vm.$emit('change', 'claude')
    await flushPromises()
    expect(groupNames(wrapper)).toEqual([])
    expect(wrapper.get('[data-testid="ops-empty"]').text()).toContain('No groups match the current filters.')

    wrapper.findComponent({ name: 'ElSelect' }).vm.$emit('change', 'openai')
    await flushPromises()
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
  })

  it('shows a filter-specific empty state when no loaded group matches', async () => {
    const { wrapper } = await mountView()

    await applyConcurrencyFilter(wrapper, 100)

    expect(wrapper.get('[data-testid="ops-empty"]').text()).toContain('No groups match the current filters.')
  })

  it('keeps rows unfiltered and shows the loading hint while the first index read is in flight', async () => {
    const { wrapper, ops } = await mountView()
    let resolveIndex: (value: unknown) => void = () => {}
    ops.getOpsMonitoringAccountConcurrency.mockImplementationOnce(() => new Promise((resolve) => {
      resolveIndex = resolve
    }))

    const input = concurrencyFilterInput(wrapper)
    await input.setValue('30')
    await input.trigger('change')
    await flushPromises()

    expect(wrapper.get('[data-testid="ops-concurrency-filter-loading"]').text()).toContain('Loading the account concurrency index')
    // Until the index arrives no row may be hidden or claimed as filtered.
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])

    resolveIndex({ data: { data: structuredClone(concurrencyIndexPayload) } })
    await flushPromises()

    expect(wrapper.find('[data-testid="ops-concurrency-filter-loading"]').exists()).toBe(false)
    // The failed row matches the exact value 30 and stays pinned.
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])
  })

  it('shows a localized alert and leaves every row unfiltered when the index read fails', async () => {
    const { wrapper, ops } = await mountView()
    ops.getOpsMonitoringAccountConcurrency.mockRejectedValueOnce({
      response: { data: { message: 'relay rejected the concurrency index' } },
    })

    await applyConcurrencyFilter(wrapper, 9)

    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    // The filter was never applied, so no row may be hidden or silently filtered.
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])
    const alert = wrapper.get('[data-testid="ops-concurrency-filter-error"]')
    expect(alert.text()).toContain('Failed to load the account concurrency index; the concurrency filter is not applied.')
    expect(alert.text()).toContain('relay rejected the concurrency index')
    expect(wrapper.find('[data-testid="ops-concurrency-filter-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ops-concurrency-filter-clear"]').exists()).toBe(true)
  })

  it('retries the failed index read when the value changes and applies it once the retry succeeds', async () => {
    const { wrapper, ops } = await mountView()
    ops.getOpsMonitoringAccountConcurrency.mockRejectedValueOnce({
      response: { data: { message: 'first index read failed' } },
    })

    await applyConcurrencyFilter(wrapper, 9)

    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="ops-concurrency-filter-error"]').text()).toContain('first index read failed')
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])

    // Changing the value retries the index; a retry that also fails
    // re-surfaces the localized alert and still filters nothing.
    ops.getOpsMonitoringAccountConcurrency.mockRejectedValueOnce({
      response: { data: { message: 'retry index read failed' } },
    })
    await applyConcurrencyFilter(wrapper, 8)

    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid="ops-concurrency-filter-error"]').text()).toContain('retry index read failed')
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])

    // The next value change retries exactly once and applies the readable index.
    await applyConcurrencyFilter(wrapper, 5)

    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(3)
    expect(wrapper.find('[data-testid="ops-concurrency-filter-error"]').exists()).toBe(false)
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)
  })

  it('ignores a late index failure after the filter is cleared while loading', async () => {
    const { wrapper, ops } = await mountView()
    let rejectIndex: (reason?: unknown) => void = () => {}
    ops.getOpsMonitoringAccountConcurrency.mockImplementationOnce(() => new Promise((_resolve, reject) => {
      rejectIndex = reject
    }))

    await applyConcurrencyFilter(wrapper, 9)
    expect(wrapper.find('[data-testid="ops-concurrency-filter-loading"]').exists()).toBe(true)

    await wrapper.get('[data-testid="ops-concurrency-filter-clear"]').trigger('click')
    await flushPromises()

    // Clearing invalidates the request immediately: no loading hint, no alert.
    expect(wrapper.find('[data-testid="ops-concurrency-filter-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ops-concurrency-filter-error"]').exists()).toBe(false)
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])

    rejectIndex({ response: { data: { message: 'late index failure' } } })
    await flushPromises()

    // The late failure must not post the "filter is not applied" alert.
    expect(wrapper.find('[data-testid="ops-concurrency-filter-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ops-concurrency-filter-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ops-concurrency-filter-clear"]').exists()).toBe(false)
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])
  })

  it('ignores a late index success after the filter is cleared while loading and re-reads on the next apply', async () => {
    const { wrapper, ops } = await mountView()
    let resolveIndex: (value: unknown) => void = () => {}
    ops.getOpsMonitoringAccountConcurrency.mockImplementationOnce(() => new Promise((resolve) => {
      resolveIndex = resolve
    }))

    await applyConcurrencyFilter(wrapper, 9)
    expect(wrapper.find('[data-testid="ops-concurrency-filter-loading"]').exists()).toBe(true)

    await wrapper.get('[data-testid="ops-concurrency-filter-clear"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="ops-concurrency-filter-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ops-concurrency-filter-error"]').exists()).toBe(false)

    // The invalidated request lands after the clear and must not populate the index.
    resolveIndex({ data: { data: structuredClone(concurrencyIndexPayload) } })
    await flushPromises()

    expect(wrapper.find('[data-testid="ops-concurrency-filter-error"]').exists()).toBe(false)
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])

    // No index was kept, so the next apply must issue a fresh request.
    await applyConcurrencyFilter(wrapper, 30)
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(2)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])
  })

  it('reloads the index along with forced groups on manual refresh while a filter is active', async () => {
    const { wrapper, ops } = await mountView()

    await applyConcurrencyFilter(wrapper, 8)
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])

    // A refreshed index moves Group Beta's account onto the same exact value.
    ops.getOpsMonitoringAccountConcurrency.mockResolvedValueOnce({
      data: {
        data: {
          ...concurrencyIndexPayload,
          accounts: concurrencyIndexPayload.accounts.map((account) => (
            account.account_id === 23 ? { ...account, concurrency: 8 } : account
          )),
        },
      },
    })
    await wrapper.get('[data-testid="ops-refresh"]').trigger('click')
    await flushPromises()

    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)
    expect(ops.getOpsMonitoringGroups.mock.calls[1][0]).toMatchObject({ provider_id: 7, window: '1h', force: true })
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(2)
    expect(groupNames(wrapper)).toEqual(['Group Beta', 'Group Alpha'])
  })

  it('keeps the existing filter index on the cached auto tick and reloads it only on manual refresh', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const { wrapper, ops } = await mountView()

    await applyConcurrencyFilter(wrapper, 30)
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])

    wrapper.findComponent({ name: 'ElSwitch' }).vm.$emit('change', true)
    await flushPromises()
    vi.advanceTimersByTime(60_000)
    await flushPromises()

    // The automatic tick reads the cached group snapshot and never re-reads the
    // index, so the existing filter keeps applying.
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)
    expect(ops.getOpsMonitoringGroups.mock.calls[1][0].force).toBe(false)
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])

    await wrapper.get('[data-testid="ops-refresh"]').trigger('click')
    await flushPromises()

    // Only the explicit refresh forces groups and reloads the active filter index.
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(3)
    expect(ops.getOpsMonitoringGroups.mock.calls[2][0]).toMatchObject({ provider_id: 7, window: '1h', force: true })
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(2)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])
  })

  it('drops the cached filter index and surfaces the alert when the manual index refresh fails', async () => {
    const { wrapper, ops } = await mountView()

    await applyConcurrencyFilter(wrapper, 30)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])

    ops.getOpsMonitoringAccountConcurrency.mockRejectedValueOnce({
      response: { data: { message: 'provider read failed' } },
    })
    await wrapper.get('[data-testid="ops-refresh"]').trigger('click')
    await flushPromises()

    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)
    expect(ops.getOpsMonitoringGroups.mock.calls[1][0]).toMatchObject({ provider_id: 7, window: '1h', force: true })
    expect(wrapper.get('[data-testid="ops-concurrency-filter-error"]').text()).toContain('provider read failed')
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])
  })

  it('keeps the index for the current provider when the window changes', async () => {
    const { wrapper, ops } = await mountView()

    await applyConcurrencyFilter(wrapper, 30)
    await wrapper.get('input[value="6h"]').setValue(true)
    await flushPromises()

    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)
    expect(ops.getOpsMonitoringGroups.mock.calls[1][0]).toMatchObject({ provider_id: 7, window: '6h' })
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])
  })

  it('clears the filter and the index when the provider changes', async () => {
    const { wrapper, ops } = await mountView({ providerList: [providers[0], providers[1]] })

    await applyConcurrencyFilter(wrapper, 30)
    expect(groupNames(wrapper)).toEqual(['Group Gamma'])

    const selects = wrapper.findAllComponents({ name: 'ElSelect' })
    expect(selects).toHaveLength(2)
    selects[0].vm.$emit('change', 3)
    await flushPromises()

    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(2)
    expect(ops.getOpsMonitoringGroups.mock.calls[1][0]).toMatchObject({ provider_id: 3 })
    expect(wrapper.find('[data-testid="ops-concurrency-filter-clear"]').exists()).toBe(false)
    // A cleared filter means every row of the new provider is visible.
    expect(groupNames(wrapper)).toEqual(['Group Gamma', 'Group Beta', 'Group Alpha', 'Group Delta'])

    await applyConcurrencyFilter(wrapper, 30)
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(2)
    expect(ops.getOpsMonitoringAccountConcurrency.mock.calls[1][0]).toEqual({ provider_id: 3 })
  })

  it('patches the cached index from a verified concurrency save so the active filter reflects it', async () => {
    const { wrapper, ops } = await mountView()
    ops.setOpsMonitoringAccountConcurrency.mockResolvedValueOnce({
      data: { data: { account_id: 22, concurrency: 3, current_concurrency: 0 } },
    })

    await applyConcurrencyFilter(wrapper, 8)
    // Group Alpha matches only through account 22 configured at exactly 8.
    expect(groupNames(wrapper)).toEqual(['Group Alpha'])

    await wrapper.get('[data-testid="ops-group-toggle-11"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="ops-account-concurrency-edit-11-22"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="ops-account-concurrency-input-11-22"]').find('input').setValue('3')
    await flushPromises()
    await wrapper.get('[data-testid="ops-account-concurrency-save-11-22"]').trigger('click')
    await flushPromises()

    expect(ops.setOpsMonitoringAccountConcurrency).toHaveBeenCalledWith(11, 22, 3, { provider_id: 7 })
    // The verified read-back patched the index: no account remains at exactly 8,
    // so every row leaves the filtered set without an extra index or group request.
    expect(groupNames(wrapper)).toEqual([])
    expect(wrapper.get('[data-testid="ops-empty"]').text()).toContain('No groups match the current filters.')
    expect(ops.getOpsMonitoringAccountConcurrency).toHaveBeenCalledTimes(1)
    expect(ops.getOpsMonitoringGroups).toHaveBeenCalledTimes(1)
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
