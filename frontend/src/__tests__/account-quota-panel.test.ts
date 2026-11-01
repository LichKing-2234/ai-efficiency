import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ElMessage } from 'element-plus'
import AccountQuotaPanel from '@/components/admin/AccountQuotaPanel.vue'
import AdminRelayAccountsView from '@/views/admin/AdminRelayAccountsView.vue'
import {
  getLatestAccountQuotaSweep,
  listAccountQuotas,
  startAccountQuotaSweep,
} from '@/api/accountQuota'
import type {
  AccountQuotaAccount,
  AccountQuotaItem,
  AccountQuotaObservation,
  AccountQuotaPage,
  AccountQuotaSnapshot,
  AccountQuotaSweep,
  AccountQuotaSweepResult,
} from '@/api/accountQuota'

vi.mock('@/api/accountQuota', () => ({
  getLatestAccountQuotaSweep: vi.fn(),
  getAccountQuotaSweep: vi.fn(),
  listAccountQuotas: vi.fn(),
  startAccountQuotaSweep: vi.fn(),
}))

vi.mock('@/api/adminUsers', () => ({
  listAdminUserSubscriptionOptions: vi.fn(),
}))

vi.mock('@/api/relayPlanning', () => ({
  applyRelayAccountConfiguration: vi.fn(),
  listRelayAccountConfigurations: vi.fn(),
}))

const mockListAccountQuotas = vi.mocked(listAccountQuotas)
const mockStartSweep = vi.mocked(startAccountQuotaSweep)
const mockLatestSweep = vi.mocked(getLatestAccountQuotaSweep)

function makeAccount(overrides: Partial<AccountQuotaAccount> = {}): AccountQuotaAccount {
  return {
    id: 11,
    name: 'Account Alpha',
    email: 'alice@example.com',
    status: 'active',
    schedulable: true,
    shadow: false,
    auto_reset_credit_enabled: false,
    groups: [{ id: 42, name: 'Group Alpha', priority: 0 }],
    ...overrides,
  }
}

function makeItem(options: {
  account?: Partial<AccountQuotaAccount>
  observation?: AccountQuotaObservation | null
  snapshot?: AccountQuotaSnapshot | null
} = {}): AccountQuotaItem {
  return {
    account: makeAccount(options.account),
    observation: options.observation ?? null,
    snapshot: options.snapshot ?? null,
  }
}

function makeSweep(overrides: Partial<AccountQuotaSweep> = {}): AccountQuotaSweep {
  return {
    id: 9,
    status: 'completed',
    scope: 'all',
    include_auto_reset: false,
    total_count: 1,
    processed_count: 1,
    success_count: 1,
    skipped_count: 0,
    failed_count: 0,
    created_at: '2026-01-02T03:00:00Z',
    started_at: '2026-01-02T03:00:01Z',
    finished_at: '2026-01-02T03:00:03Z',
    last_error: null,
    results: [],
    ...overrides,
  }
}

function makeResult(overrides: Partial<AccountQuotaSweepResult> = {}): AccountQuotaSweepResult {
  return {
    account_id: 11,
    status: 'succeeded',
    error_code: '',
    message: '',
    observation: null,
    ...overrides,
  }
}

function makePage(items: AccountQuotaItem[], latestSweep: AccountQuotaSweep | null = null): AccountQuotaPage {
  return { provider_id: 7, platform: 'openai', items, total: items.length, latest_sweep: latestSweep }
}

function resolvePage(page: AccountQuotaPage) {
  mockListAccountQuotas.mockResolvedValue({ data: { data: page } } as any)
}

function mountPanel(platform = 'openai') {
  return mount(AccountQuotaPanel, {
    props: { providerId: 7, platform },
    global: {
      stubs: {
        teleport: true,
        // Element Plus select poppers recurse under a stubbed teleport; the panel
        // only needs the filter option labels, so use a native select in tests.
        ElSelect: {
          props: ['modelValue'],
          emits: ['update:modelValue', 'change'],
          template: '<select :value="modelValue"><slot /></select>',
        },
        ElOption: { props: ['label', 'value'], template: '<option :value="value">{{ label }}</option>' },
      },
    },
  })
}

function fullObservation(): AccountQuotaObservation {
  return {
    sweep_id: 9,
    observed_at: '2026-01-02T03:04:05Z',
    plan_type: 'plus',
    windows: [
      { used_percent: 42, window_seconds: 18000, resets_at: '2026-01-02T08:00:00Z' },
      { used_percent: 91.5, window_seconds: 604800, resets_at: '2026-01-07T08:00:00Z' },
    ],
    credits: { has_credits: true, unlimited: false, balance: '12.50' },
    reset_credits: {
      available: 2,
      expires_at: ['2026-02-01T00:00:00Z', '2026-01-15T00:00:00Z'],
    },
  }
}

function cachedSnapshot(): AccountQuotaSnapshot {
  return {
    observed_at: '2025-12-30T00:00:00Z',
    usage_updated_at: null,
    credits: { has_credits: false, unlimited: false, balance: null },
    reset_credits: { available: 0, expires_at: [] },
  }
}

describe('AccountQuotaPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('renders observation windows, credits and reset credits, and falls back to the snapshot', async () => {
    resolvePage(makePage([
      makeItem({ account: { id: 11, name: 'Account Alpha' }, observation: fullObservation() }),
      makeItem({
        account: { id: 12, name: 'Account Beta', email: 'bob@example.org', auto_reset_credit_enabled: true },
        snapshot: cachedSnapshot(),
      }),
    ]))
    const wrapper = mountPanel()
    await flushPromises()

    expect(mockListAccountQuotas).toHaveBeenCalledWith({ provider_id: 7, platform: 'openai' })
    expect(wrapper.get('[data-testid="quota-window-5h-11"]').text()).toContain('42%')
    expect(wrapper.get('[data-testid="quota-window-7d-11"]').text()).toContain('91.5%')
    expect(wrapper.get('[data-testid="quota-window-5h-11"]').text()).toContain('2026-01-02')
    expect(wrapper.get('[data-testid="quota-credits-11"]').text()).toContain('12.50')
    expect(wrapper.get('[data-testid="quota-reset-credits-11"]').text()).toContain('2')
    expect(wrapper.get('[data-testid="quota-reset-credits-11"]').text()).toContain('2026-01-15')
    expect(wrapper.get('[data-testid="quota-observed-11"]').text()).toContain('Plan plus')

    // A snapshot-only account keeps the cached credit facts but no usage windows.
    expect(wrapper.get('[data-testid="quota-window-5h-12"]').text()).toContain('—')
    expect(wrapper.get('[data-testid="quota-window-7d-12"]').text()).toContain('—')
    expect(wrapper.get('[data-testid="quota-credits-12"]').text()).toContain('No credits')
    expect(wrapper.get('[data-testid="quota-reset-credits-12"]').text()).toContain('0')
    expect(wrapper.get('[data-testid="quota-no-observation-12"]').text()).toContain('No sweep observation yet')
    expect(wrapper.get('[data-testid="quota-observed-12"]').text()).toContain('Snapshot')

    wrapper.unmount()
  })

  it('opens the refresh-all confirmation with the default eligible count and starts the sweep', async () => {
    const items = [
      makeItem({ account: { id: 11, name: 'Account Alpha' } }),
      makeItem({ account: { id: 12, name: 'Account Beta' } }),
      makeItem({ account: { id: 13, name: 'Account Gamma', auto_reset_credit_enabled: true } }),
    ]
    resolvePage(makePage(items))
    mockStartSweep.mockResolvedValue({
      data: { data: makeSweep({ status: 'queued', total_count: 2, processed_count: 0, success_count: 0 }) },
    } as any)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-testid="quota-refresh-all"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="quota-confirm-count"]').text()).toBe('2')
    expect(wrapper.get('[data-testid="quota-confirm-scope"]').text()).toContain('Refresh all eligible accounts')
    expect(wrapper.get('[data-testid="quota-confirm-auto-reset-note"]').text()).toContain('1 account(s)')

    await wrapper.get('[data-testid="quota-confirm-start"]').trigger('click')
    await flushPromises()

    expect(mockStartSweep).toHaveBeenCalledWith({ provider_id: 7, platform: 'openai' })
    wrapper.unmount()
  })

  it('shows progress for a running sweep and stops polling when the latest sweep finishes', async () => {
    const runningSweep = makeSweep({
      status: 'running',
      total_count: 3,
      processed_count: 1,
      success_count: 1,
      finished_at: null,
    })
    const completedSweep = makeSweep({ status: 'completed', total_count: 3, processed_count: 3, success_count: 3 })
    mockListAccountQuotas
      .mockResolvedValueOnce({ data: { data: makePage([makeItem()], runningSweep) } } as any)
      .mockResolvedValue({ data: { data: makePage([makeItem()], completedSweep) } } as any)
    mockLatestSweep.mockResolvedValue({ data: { data: completedSweep } } as any)
    const success = vi.spyOn(ElMessage, 'success')
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })

    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-progress"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="quota-progress-counts"]').text()).toContain('1 / 3')

    vi.advanceTimersByTime(2000)
    await flushPromises()
    await flushPromises()

    expect(mockLatestSweep).toHaveBeenCalledWith({ provider_id: 7 })
    expect(mockListAccountQuotas).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="quota-progress"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="quota-last-sweep"]').text()).toContain('Completed')
    expect(success).toHaveBeenCalledTimes(1)

    // Polling stopped: another interval tick must not read the latest sweep again.
    vi.advanceTimersByTime(4000)
    await flushPromises()
    expect(mockLatestSweep).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('renders the sanitized message and reason code of a failed account result', async () => {
    const failedResult = makeResult({
      account_id: 11,
      status: 'failed',
      error_code: 'rate-limited',
      message: 'synthetic upstream failure',
    })
    const unknownResult = makeResult({
      account_id: 12,
      status: 'failed',
      error_code: 'brand-new-reason',
      message: 'opaque failure',
    })
    resolvePage(makePage(
      [
        makeItem({ account: { id: 11, name: 'Account Alpha' } }),
        makeItem({ account: { id: 12, name: 'Account Beta', email: 'bob@example.org' } }),
      ],
      makeSweep({ results: [failedResult, unknownResult] }),
    ))
    const wrapper = mountPanel()
    await flushPromises()

    const first = wrapper.get('[data-testid="quota-result-11"]').text()
    expect(first).toContain('Failed')
    expect(first).toContain('synthetic upstream failure')
    expect(first).toContain('rate-limited')

    const second = wrapper.get('[data-testid="quota-result-12"]').text()
    expect(second).toContain('opaque failure')
    expect(second).toContain('brand-new-reason')

    wrapper.unmount()
  })

  it('skips auto-reset accounts by default and sends the include flag when enabled', async () => {
    resolvePage(makePage([
      makeItem({ account: { id: 11, name: 'Account Alpha', auto_reset_credit_enabled: true } }),
      makeItem({ account: { id: 12, name: 'Account Beta', email: 'bob@example.org' } }),
    ]))
    mockStartSweep.mockResolvedValue({
      data: { data: makeSweep({ scope: 'selected', status: 'queued', total_count: 1, processed_count: 0, success_count: 0 }) },
    } as any)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-testid="quota-select-11"] input').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="quota-refresh-selected"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="quota-confirm-count"]').text()).toBe('0')
    expect((wrapper.get('[data-testid="quota-confirm-start"]').element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.get('[data-testid="quota-include-auto-reset"] input').setValue(true)
    await flushPromises()

    expect(wrapper.get('[data-testid="quota-confirm-count"]').text()).toBe('1')
    await wrapper.get('[data-testid="quota-confirm-start"]').trigger('click')
    await flushPromises()

    expect(mockStartSweep).toHaveBeenCalledWith({
      provider_id: 7,
      platform: 'openai',
      account_ids: [11],
      include_auto_reset: true,
    })
    wrapper.unmount()
  })

  it('shows a neutral hint and hides the actions for a non-openai platform', async () => {
    const wrapper = mountPanel('anthropic')
    await flushPromises()

    expect(wrapper.find('[data-testid="quota-platform-hint"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quota-refresh-all"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quota-refresh-selected"]').exists()).toBe(false)
    expect(mockListAccountQuotas).not.toHaveBeenCalled()
    expect(mockLatestSweep).not.toHaveBeenCalled()

    wrapper.unmount()
  })
})

describe('AdminRelayAccountsView quota tab', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  function mountView() {
    return mount(AdminRelayAccountsView, {
      global: {
        stubs: {
          teleport: true,
          AppLayout: { template: '<div><slot /></div>' },
          ElFormItem: { props: ['label'], template: '<label>{{ label }}<slot /></label>' },
          ElSelect: {
            props: ['modelValue'],
            emits: ['update:modelValue', 'change'],
            methods: {
              change(event: Event) {
                const raw = (event.target as HTMLSelectElement).value
                const value = typeof this.modelValue === 'number' ? Number(raw) : raw
                this.$emit('update:modelValue', value)
                this.$emit('change', value)
              },
            },
            template: '<select :value="modelValue" @change="change"><slot /></select>',
          },
          ElOption: { props: ['label', 'value'], template: '<option :value="value">{{ label }}</option>' },
        },
      },
    })
  }

  it('keeps the config tab free of quota calls and lazily mounts the quota panel', async () => {
    const adminUsers = await import('@/api/adminUsers') as any
    const relayPlanning = await import('@/api/relayPlanning') as any
    adminUsers.listAdminUserSubscriptionOptions.mockResolvedValue({
      data: {
        data: {
          providers: [{
            id: 7,
            name: 'relay',
            display_name: 'Relay',
            groups: [{ group_id: '42', group_name: 'Group Alpha', platform: 'openai' }],
          }],
        },
      },
    })
    relayPlanning.listRelayAccountConfigurations.mockResolvedValue({
      data: { data: { baseline_account_id: 0, items: [], total: 0 } },
    })
    resolvePage(makePage([makeItem()]))

    const wrapper = mountView()
    await flushPromises()
    expect(mockListAccountQuotas).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="platform-select"]').setValue('openai')
    await flushPromises()
    expect(wrapper.find('[data-testid="account-quota-panel"]').exists()).toBe(false)
    expect(mockListAccountQuotas).not.toHaveBeenCalled()

    await wrapper.get('#tab-quota').trigger('click')
    await flushPromises()

    expect(mockListAccountQuotas).toHaveBeenCalledWith({ provider_id: 7, platform: 'openai' })
    expect(wrapper.find('[data-testid="account-quota-panel"]').exists()).toBe(true)
    // Element Plus keeps the inactive pane mounted with v-show, so the config
    // content must be hidden rather than absent.
    expect(wrapper.find('[data-testid="baseline-account"]').isVisible()).toBe(false)

    await wrapper.get('#tab-config').trigger('click')
    await flushPromises()
    expect(wrapper.get('#pane-config').attributes('style') ?? '').not.toContain('display: none')
    expect(wrapper.find('[data-testid="account-quota-panel"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
